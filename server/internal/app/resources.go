package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/curly-hub/podorel/server/internal/agents"
	"github.com/curly-hub/podorel/server/internal/api"
	"github.com/curly-hub/podorel/server/internal/auth"
	"github.com/curly-hub/podorel/server/internal/db"
	"github.com/curly-hub/podorel/server/internal/templates"
)

type actionRequest struct {
	Confirm     bool   `json:"confirm"`
	ConfirmName string `json:"confirm_name"`
	Password    string `json:"password"`
}

func (a *App) handleListPods(w http.ResponseWriter, r *http.Request, session db.Session) {
	agentID := session.AgentID
	if agentID == "" {
		agentID = db.PrimaryAgentID
	}
	freshAfter := a.now()
	refreshSucceeded := false
	if err := a.refreshAgentSnapshots(r.Context(), agentID); err != nil {
		a.logger.Error(r.Context(), "agent_refresh", "could not refresh pod snapshots from agent", map[string]any{"agent_id": agentID, "error": err.Error()})
		if !a.allowSnapshotFallback {
			api.WriteError(r.Context(), w, http.StatusBadGateway, "AGENT_REFRESH_FAILED", "Agent refresh failed; cached snapshots are disabled for this runtime.", agentRefreshError(agentID, err))
			return
		}
	} else {
		refreshSucceeded = true
	}
	pods, err := a.store.ListPods(r.Context(), agentID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	type podView struct {
		db.Pod
		Containers     []db.Container      `json:"containers"`
		Stats          []db.ResourceSample `json:"stats"`
		Self           bool                `json:"self_management"`
		SnapshotSource string              `json:"snapshot_source"`
	}
	current, _ := a.store.CurrentStats(r.Context(), session.AgentID)
	current = freshStatsOnly(current, refreshSucceeded, freshAfter)
	views := make([]podView, 0, len(pods))
	for _, pod := range pods {
		if isSeededSelfPlaceholder(pod) {
			continue
		}
		if refreshSucceeded && pod.ObservedAt.Before(freshAfter) {
			continue
		}
		containers, _ := a.store.ListContainers(r.Context(), pod.ID, session.AgentID)
		stats := []db.ResourceSample{}
		for _, sample := range current {
			if sample.PodID == pod.ID {
				stats = append(stats, sample)
			}
		}
		views = append(views, podView{
			Pod:            pod,
			Containers:     containers,
			Stats:          stats,
			Self:           isPoDorelSelfPod(pod),
			SnapshotSource: snapshotSource(refreshSucceeded),
		})
	}
	api.WriteOK(r.Context(), w, views)
}

func isPoDorelSelfPod(pod db.Pod) bool {
	name := strings.ToLower(strings.TrimSpace(pod.Name))
	return name == "podorel" || name == "podorel-web"
}

func isSeededSelfPlaceholder(pod db.Pod) bool {
	return pod.ID == "podorel-self-pod" && pod.Name == "podorel-web" && strings.EqualFold(pod.State, "unknown")
}

func (a *App) handleGetPod(w http.ResponseWriter, r *http.Request, session db.Session) {
	pod, err := a.store.PodByID(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	if !a.canAccessAgent(session, pod.AgentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot access this pod.", nil)
		return
	}
	containers, _ := a.store.ListContainers(r.Context(), pod.ID, session.AgentID)
	api.WriteOK(r.Context(), w, map[string]any{"pod": pod, "containers": containers})
}

func (a *App) handlePodAction(action string) sessionHandler {
	return func(w http.ResponseWriter, r *http.Request, session db.Session) {
		if !a.requireCSRF(w, r) {
			return
		}
		pod, err := a.store.PodByID(r.Context(), r.PathValue("id"))
		if err != nil {
			a.writeStoreError(w, r, err)
			return
		}
		if !a.canAccessAgent(session, pod.AgentID) {
			api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot access this pod.", nil)
			return
		}
		var req actionRequest
		if !decodeJSON(r, w, &req) {
			return
		}
		if !a.confirmed(action, pod.Name, req) {
			a.audit(r, session.UserID, "pods."+action, "pod", pod.ID, "failure", map[string]any{"reason": "confirmation_required"})
			api.WriteError(r.Context(), w, http.StatusBadRequest, "CONFIRMATION_REQUIRED", "This action requires confirmation.", nil)
			return
		}
		state := stateAfterAction(action)
		if err := a.proxyPodAction(r, pod.AgentID, pod.PodmanPodID, action); err != nil {
			if !a.allowSnapshotFallback {
				a.writeAgentOperationFailure(w, r, session, "pods."+action, "pod", pod.ID, pod.AgentID, action, err)
				return
			}
			a.logger.Error(r.Context(), "agent_pod_action", "agent pod action failed; using snapshot fallback", map[string]any{"agent_id": pod.AgentID, "pod_id": pod.ID, "action": action, "error": err.Error()})
		}
		if state != "" {
			if err := a.store.UpdatePodState(r.Context(), pod.ID, state); err != nil {
				a.internalError(w, r, err)
				return
			}
		}
		a.audit(r, session.UserID, "pods."+action, "pod", pod.ID, "success", map[string]any{"confirmation_method": confirmationMethod(action)})
		api.WriteOK(r.Context(), w, map[string]any{"pod_id": pod.ID, "action": action, "state": state})
	}
}

func (a *App) handleDeletePod(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireCSRF(w, r) {
		return
	}
	pod, err := a.store.PodByID(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	if !a.canAccessAgent(session, pod.AgentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot access this pod.", nil)
		return
	}
	var req actionRequest
	if !decodeJSON(r, w, &req) {
		return
	}
	if req.ConfirmName != pod.Name && req.Password == "" {
		api.WriteError(r.Context(), w, http.StatusBadRequest, "CONFIRMATION_REQUIRED", "Delete requires typing the pod name or re-entering password.", nil)
		return
	}
	if req.Password != "" && !a.requireAdminPasswordValue(w, r, req.Password) {
		return
	}
	if err := a.proxyPodAction(r, pod.AgentID, pod.PodmanPodID, "delete"); err != nil {
		if !a.allowSnapshotFallback {
			a.writeAgentOperationFailure(w, r, session, "pods.delete", "pod", pod.ID, pod.AgentID, "delete", err)
			return
		}
		a.logger.Error(r.Context(), "agent_pod_delete", "agent pod delete failed; using snapshot fallback", map[string]any{"agent_id": pod.AgentID, "pod_id": pod.ID, "error": err.Error()})
	}
	if err := a.store.DeletePod(r.Context(), pod.ID); err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, session.UserID, "pods.delete", "pod", pod.ID, "success", map[string]any{"confirmation_method": "name_or_password"})
	api.WriteOK(r.Context(), w, map[string]any{"deleted": true, "pod_id": pod.ID})
}

func (a *App) handleListContainers(w http.ResponseWriter, r *http.Request, session db.Session) {
	containers, err := a.store.ListContainers(r.Context(), r.URL.Query().Get("pod_id"), session.AgentID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, containers)
}

func (a *App) handleGetContainer(w http.ResponseWriter, r *http.Request, session db.Session) {
	container, err := a.store.ContainerByID(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	if !a.canAccessAgent(session, container.AgentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot access this container.", nil)
		return
	}
	api.WriteOK(r.Context(), w, container)
}

func (a *App) handleContainerAction(action string) sessionHandler {
	return func(w http.ResponseWriter, r *http.Request, session db.Session) {
		if !a.requireCSRF(w, r) {
			return
		}
		container, err := a.store.ContainerByID(r.Context(), r.PathValue("id"))
		if err != nil {
			a.writeStoreError(w, r, err)
			return
		}
		if !a.canAccessAgent(session, container.AgentID) {
			api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot access this container.", nil)
			return
		}
		var req actionRequest
		if !decodeJSON(r, w, &req) {
			return
		}
		if !a.confirmed(action, container.Name, req) {
			a.audit(r, session.UserID, "containers."+action, "container", container.ID, "failure", map[string]any{"reason": "confirmation_required"})
			api.WriteError(r.Context(), w, http.StatusBadRequest, "CONFIRMATION_REQUIRED", "This action requires confirmation.", nil)
			return
		}
		state := stateAfterAction(action)
		if err := a.proxyContainerAction(r, container.AgentID, container.PodmanContainerID, action); err != nil {
			if !a.allowSnapshotFallback {
				a.writeAgentOperationFailure(w, r, session, "containers."+action, "container", container.ID, container.AgentID, action, err)
				return
			}
			a.logger.Error(r.Context(), "agent_container_action", "agent container action failed; using snapshot fallback", map[string]any{"agent_id": container.AgentID, "container_id": container.ID, "action": action, "error": err.Error()})
		}
		if state != "" {
			if err := a.store.UpdateContainerState(r.Context(), container.ID, state); err != nil {
				a.internalError(w, r, err)
				return
			}
		}
		a.audit(r, session.UserID, "containers."+action, "container", container.ID, "success", map[string]any{"confirmation_method": confirmationMethod(action)})
		api.WriteOK(r.Context(), w, map[string]any{"container_id": container.ID, "action": action, "state": state})
	}
}

func (a *App) handleDeleteContainer(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireCSRF(w, r) {
		return
	}
	container, err := a.store.ContainerByID(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	if !a.canAccessAgent(session, container.AgentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot access this container.", nil)
		return
	}
	var req actionRequest
	if !decodeJSON(r, w, &req) {
		return
	}
	if req.ConfirmName != container.Name && req.Password == "" {
		api.WriteError(r.Context(), w, http.StatusBadRequest, "CONFIRMATION_REQUIRED", "Delete requires typing the container name or re-entering password.", nil)
		return
	}
	if req.Password != "" && !a.requireAdminPasswordValue(w, r, req.Password) {
		return
	}
	if err := a.proxyContainerAction(r, container.AgentID, container.PodmanContainerID, "delete"); err != nil {
		if !a.allowSnapshotFallback {
			a.writeAgentOperationFailure(w, r, session, "containers.delete", "container", container.ID, container.AgentID, "delete", err)
			return
		}
		a.logger.Error(r.Context(), "agent_container_delete", "agent container delete failed; using snapshot fallback", map[string]any{"agent_id": container.AgentID, "container_id": container.ID, "error": err.Error()})
	}
	if err := a.store.DeleteContainer(r.Context(), container.ID); err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, session.UserID, "containers.delete", "container", container.ID, "success", map[string]any{"confirmation_method": "name_or_password"})
	api.WriteOK(r.Context(), w, map[string]any{"deleted": true, "container_id": container.ID})
}

func (a *App) handleCurrentStats(w http.ResponseWriter, r *http.Request, session db.Session) {
	agentID := session.AgentID
	if agentID == "" {
		agentID = db.PrimaryAgentID
	}
	freshAfter := a.now()
	refreshSucceeded := false
	if err := a.refreshAgentSnapshots(r.Context(), agentID); err != nil {
		a.logger.Error(r.Context(), "agent_refresh", "could not refresh current stats from agent", map[string]any{"agent_id": agentID, "error": err.Error()})
		if !a.allowSnapshotFallback {
			api.WriteError(r.Context(), w, http.StatusBadGateway, "AGENT_REFRESH_FAILED", "Agent refresh failed; cached stats are disabled for this runtime.", agentRefreshError(agentID, err))
			return
		}
	} else {
		refreshSucceeded = true
	}
	stats, err := a.store.CurrentStats(r.Context(), agentID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	stats = freshStatsOnly(stats, refreshSucceeded, freshAfter)
	api.WriteOK(r.Context(), w, stats)
}

func snapshotSource(refreshSucceeded bool) string {
	if refreshSucceeded {
		return "live"
	}
	return "cached_development_snapshot"
}

func freshStatsOnly(stats []db.ResourceSample, refreshSucceeded bool, freshAfter time.Time) []db.ResourceSample {
	if !refreshSucceeded {
		return []db.ResourceSample{}
	}
	fresh := make([]db.ResourceSample, 0, len(stats))
	for _, sample := range stats {
		if sample.SampledAt.Before(freshAfter) {
			continue
		}
		fresh = append(fresh, sample)
	}
	return fresh
}

func (a *App) handleStatsHistory(w http.ResponseWriter, r *http.Request, session db.Session) {
	since := a.now().Add(-7 * 24 * time.Hour)
	if raw := r.URL.Query().Get("since"); raw != "" {
		if duration, err := time.ParseDuration(raw); err == nil {
			since = a.now().Add(-duration)
		}
	}
	stats, err := a.store.StatsHistory(r.Context(), since, session.AgentID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, stats)
}

func (a *App) handleTemplates(w http.ResponseWriter, r *http.Request, _ db.Session) {
	api.WriteOK(r.Context(), w, a.templates)
}

type createFromTemplateRequest struct {
	AgentID    string            `json:"agent_id"`
	TemplateID string            `json:"template_id"`
	PodName    string            `json:"pod_name"`
	Values     map[string]string `json:"values"`
	Confirm    bool              `json:"confirm"`
}

func (a *App) handleCreateFromTemplate(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireCSRF(w, r) {
		return
	}
	var req createFromTemplateRequest
	if !decodeJSON(r, w, &req) {
		return
	}
	template, ok := a.templateByID(req.TemplateID)
	if !ok {
		api.WriteError(r.Context(), w, http.StatusNotFound, "TEMPLATE_NOT_FOUND", "Template was not found.", nil)
		return
	}
	var err error
	template, err = applyTemplateValues(template, req.Values)
	if err != nil {
		api.WriteError(r.Context(), w, http.StatusBadRequest, "TEMPLATE_VALUES_INVALID", err.Error(), nil)
		return
	}
	if req.PodName == "" {
		req.PodName = sanitizeName(template.ID)
	}
	agentID := req.AgentID
	if agentID == "" {
		agentID = db.PrimaryAgentID
	}
	if !a.canAccessAgent(session, agentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot create pods for this agent.", nil)
		return
	}
	preview := []string{"podman", "run", "--detach", "--pod", "new:" + req.PodName, "--name", req.PodName + "-main"}
	for _, port := range template.Ports {
		if port.Host > 0 {
			preview = append(preview, "-p", strconv.Itoa(port.Host)+":"+strconv.Itoa(port.Container)+"/"+port.Protocol)
		}
	}
	for _, key := range sortedStringMapKeys(template.Environment) {
		value := template.Environment[key]
		preview = append(preview, "-e", key+"="+value)
	}
	for _, key := range sortedStringMapKeys(template.Labels) {
		value := template.Labels[key]
		preview = append(preview, "--label", key+"="+value)
	}
	if template.ResourceLimits.CPU != "" {
		preview = append(preview, "--cpus", template.ResourceLimits.CPU)
	}
	if template.ResourceLimits.Memory != "" {
		preview = append(preview, "--memory", template.ResourceLimits.Memory)
	}
	if template.RestartPolicy != "" {
		preview = append(preview, "--restart", template.RestartPolicy)
	}
	preview = append(preview, template.Image)
	preview = append(preview, template.Command...)
	if !req.Confirm {
		api.WriteOK(r.Context(), w, map[string]any{"preview_command": preview, "template": template})
		return
	}
	createdViaAgent := false
	if err := a.proxyCreatePod(r, agentID, agents.CreatePodRequest{
		Name:           req.PodName,
		TemplateID:     template.ID,
		PreviewCommand: preview,
	}); err != nil {
		if !a.allowSnapshotFallback {
			a.writeAgentOperationFailure(w, r, session, "pods.create_from_template", "pod", req.PodName, agentID, "create_from_template", err)
			return
		}
		a.logger.Error(r.Context(), "agent_create_pod", "agent pod create failed; using snapshot fallback", map[string]any{"agent_id": agentID, "template_id": template.ID, "error": err.Error()})
	} else {
		createdViaAgent = true
	}
	podID := "created-" + sanitizeName(req.PodName)
	if createdViaAgent {
		_ = a.store.DeletePod(r.Context(), podID)
		refreshStarted := a.now()
		if err := a.refreshAgentSnapshots(r.Context(), agentID); err != nil {
			a.logger.Error(r.Context(), "agent_refresh", "could not refresh pod after template create", map[string]any{"agent_id": agentID, "pod_name": req.PodName, "error": err.Error()})
		} else if pod, ok := a.podByNameObservedAfter(r.Context(), agentID, req.PodName, refreshStarted); ok {
			a.audit(r, session.UserID, "pods.create_from_template", "pod", pod.ID, "success", map[string]any{"template_id": template.ID})
			api.WriteOK(r.Context(), w, map[string]any{"pod_id": pod.ID, "podman_pod_id": pod.PodmanPodID, "preview_command": preview})
			return
		}
	}
	if err := a.store.InsertPod(r.Context(), db.Pod{
		ID:          podID,
		AgentID:     agentID,
		PodmanPodID: podID,
		Name:        req.PodName,
		State:       "created",
		Health:      "unknown",
		RawJSON:     mustJSON(map[string]any{"template_id": template.ID, "preview_command": preview}),
	}); err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := a.store.InsertContainer(r.Context(), db.Container{
		ID:                podID + "-main",
		AgentID:           agentID,
		PodID:             podID,
		PodmanContainerID: podID + "-main",
		Name:              req.PodName + "-main",
		Image:             template.Image,
		State:             "created",
		Health:            "unknown",
		RawJSON:           mustJSON(map[string]any{"template_id": template.ID}),
	}); err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, session.UserID, "pods.create_from_template", "pod", podID, "success", map[string]any{"template_id": template.ID})
	api.WriteOK(r.Context(), w, map[string]any{"pod_id": podID, "preview_command": preview})
}

type dockerfileBuildRequest struct {
	AgentID    string `json:"agent_id"`
	ImageName  string `json:"image_name"`
	Dockerfile string `json:"dockerfile"`
	Confirm    bool   `json:"confirm"`
	Password   string `json:"password"`
}

func (a *App) handleBuildFromDockerfile(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireCSRF(w, r) {
		return
	}
	var req dockerfileBuildRequest
	if !decodeJSON(r, w, &req) {
		return
	}
	if len(req.Dockerfile) > maxDockerfileBytes {
		api.WriteError(r.Context(), w, http.StatusBadRequest, "DOCKERFILE_TOO_LARGE", "Dockerfile is too large.", nil)
		return
	}
	agentID := req.AgentID
	if agentID == "" {
		agentID = db.PrimaryAgentID
	}
	if !a.canAccessAgent(session, agentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot build images for this agent.", nil)
		return
	}
	baseImage := parseBaseImage(req.Dockerfile)
	secretWarnings := dockerfileSecretWarnings(req.Dockerfile)
	preview := []string{"podman", "build", "-t", req.ImageName, "-f", "Dockerfile", "."}
	if !req.Confirm || req.Password == "" {
		api.WriteOK(r.Context(), w, map[string]any{"preview_command": preview, "base_image": baseImage, "secret_warnings": secretWarnings, "requires_password": true})
		return
	}
	if !a.requireAdminPasswordValue(w, r, req.Password) {
		return
	}
	build, err := a.createImageBuild(r.Context(), agentID, req.ImageName, req.Dockerfile, map[string]any{
		"base_image":           baseImage,
		"preview_command":      preview,
		"secret_warnings":      secretWarnings,
		"secret_warning_count": len(secretWarnings),
	})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, session.UserID, "images.build_from_dockerfile", "image", req.ImageName, "success", map[string]any{"build_id": build.ID, "base_image": baseImage, "secret_warning_count": len(secretWarnings)})
	api.WriteOK(r.Context(), w, map[string]any{"build_id": build.ID, "image_name": req.ImageName, "status": build.Status, "base_image": baseImage})
}

type createSecretRequest struct {
	AgentID     string `json:"agent_id"`
	Name        string `json:"name"`
	Value       string `json:"value"`
	Password    string `json:"password"`
	UsedByPodID string `json:"used_by_pod_id"`
}

func (a *App) handleCreateSecret(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireCSRF(w, r) {
		return
	}
	var req createSecretRequest
	if !decodeJSON(r, w, &req) {
		return
	}
	if req.Password == "" {
		api.WriteError(r.Context(), w, http.StatusBadRequest, "PASSWORD_REQUIRED", "Secret creation requires password confirmation.", nil)
		return
	}
	if req.Name == "" || req.Value == "" {
		api.WriteError(r.Context(), w, http.StatusBadRequest, "SECRET_INVALID", "Secret name and value are required.", nil)
		return
	}
	if !a.requireAdminPasswordValue(w, r, req.Password) {
		return
	}
	agentID := req.AgentID
	if agentID == "" {
		agentID = db.PrimaryAgentID
	}
	if !a.canAccessAgent(session, agentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot create secrets for this agent.", nil)
		return
	}
	if err := a.proxyCreateSecret(r, agentID, agents.CreateSecretRequest{Name: req.Name, Value: req.Value}); err != nil {
		if !a.allowSnapshotFallback {
			a.writeAgentOperationFailure(w, r, session, "secrets.create", "secret", req.Name, agentID, "create", err)
			return
		}
		a.logger.Error(r.Context(), "agent_create_secret", "agent secret create failed; storing metadata fallback", map[string]any{"agent_id": agentID, "secret_name": req.Name, "error": err.Error()})
	}
	metadata, err := a.store.CreateSecretMetadata(r.Context(), db.SecretMetadata{
		AgentID:     agentID,
		SecretName:  req.Name,
		Fingerprint: auth.HashToken(req.Value),
		UsedByPodID: req.UsedByPodID,
	})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, session.UserID, "secrets.create", "secret", metadata.ID, "success", map[string]any{"secret_name": req.Name})
	api.WriteOK(r.Context(), w, metadata)
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request, _ db.Session) {
	api.WriteOK(r.Context(), w, a.cfg)
}

func (a *App) handleUpdateSettings(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireAdminPasswordSession(w, r, session) || !a.requireCSRF(w, r) {
		return
	}
	var payload map[string]any
	if !decodeJSON(r, w, &payload) {
		return
	}
	password, _ := payload["password"].(string)
	if !a.requireAdminPasswordValue(w, r, password) {
		return
	}
	delete(payload, "password")
	updated, requiresRestart, err := a.applySettingsPayload(r.Context(), payload)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, session.UserID, "settings.update", "settings", "global", "success", map[string]any{"keys": updated, "requires_restart": requiresRestart})
	api.WriteOK(r.Context(), w, map[string]any{"updated": updated, "requires_restart": requiresRestart, "effective_settings": a.cfg})
}

func (a *App) templateByID(id string) (templates.Template, bool) {
	for _, template := range a.templates {
		if template.ID == id {
			return template, true
		}
	}
	return templates.Template{}, false
}

func (a *App) podByNameObservedAfter(ctx context.Context, agentID string, name string, observedAfter time.Time) (db.Pod, bool) {
	pods, err := a.store.ListPods(ctx, agentID)
	if err != nil {
		return db.Pod{}, false
	}
	var newest db.Pod
	for _, pod := range pods {
		if pod.Name != name || isSeededSelfPlaceholder(pod) || strings.HasPrefix(pod.ID, "created-") || pod.ObservedAt.Before(observedAfter) {
			continue
		}
		if newest.ID == "" || pod.ObservedAt.After(newest.ObservedAt) {
			newest = pod
		}
	}
	if newest.ID != "" {
		return newest, true
	}
	return db.Pod{}, false
}

func (a *App) confirmed(action string, name string, req actionRequest) bool {
	switch action {
	case "kill":
		return req.ConfirmName == name || req.Password != ""
	case "delete":
		return req.ConfirmName == name || req.Password != ""
	default:
		return req.Confirm
	}
}

func stateAfterAction(action string) string {
	switch action {
	case "start", "restart":
		return "running"
	case "stop":
		return "stopped"
	case "kill":
		return "killed"
	default:
		return ""
	}
}

func confirmationMethod(action string) string {
	if action == "kill" || action == "delete" {
		return "name_or_password"
	}
	return "click"
}

func sanitizeName(input string) string {
	lower := strings.ToLower(strings.TrimSpace(input))
	out := strings.Builder{}
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		} else if r == '_' || r == ' ' || r == '.' {
			out.WriteRune('-')
		}
	}
	value := strings.Trim(out.String(), "-")
	if value == "" {
		return "pod"
	}
	return value
}

func applyTemplateValues(template templates.Template, values map[string]string) (templates.Template, error) {
	template.Command = append([]string(nil), template.Command...)
	template.Environment = cloneStringMap(template.Environment)
	template.Labels = cloneStringMap(template.Labels)
	if len(values) == 0 {
		return template, nil
	}
	for key, value := range values {
		if !validTemplateValueKey(key) || len(value) > 512 {
			return templates.Template{}, fmt.Errorf("template value %q is invalid", key)
		}
	}
	replace := func(input string) string {
		out := input
		for key, value := range values {
			out = strings.ReplaceAll(out, "${"+key+"}", value)
			out = strings.ReplaceAll(out, "{{"+key+"}}", value)
		}
		return out
	}
	template.Image = replace(template.Image)
	for i := range template.Command {
		template.Command[i] = replace(template.Command[i])
	}
	for i := range template.Ports {
		override, ok, err := templatePortValue(values, template.Ports[i].Container, len(template.Ports) == 1)
		if err != nil {
			return templates.Template{}, err
		}
		if ok {
			template.Ports[i].Host = override
		}
	}
	for key, value := range template.Environment {
		template.Environment[key] = replace(value)
	}
	for key, value := range template.Labels {
		template.Labels[key] = replace(value)
	}
	return template, nil
}

func templatePortValue(values map[string]string, containerPort int, singlePort bool) (int, bool, error) {
	keys := []string{
		"host_port_" + strconv.Itoa(containerPort),
		"port_" + strconv.Itoa(containerPort),
		"HOST_PORT_" + strconv.Itoa(containerPort),
		"PORT_" + strconv.Itoa(containerPort),
	}
	if singlePort {
		keys = append(keys, "host_port", "port", "HOST_PORT", "PORT")
	}
	for _, key := range keys {
		if raw, ok := values[key]; ok {
			parsed, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil || parsed < 0 || parsed > 65535 {
				return 0, false, fmt.Errorf("template port value %q must be between 0 and 65535", key)
			}
			return parsed, true, nil
		}
	}
	return 0, false, nil
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func validTemplateValueKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func sortedStringMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (a *App) writeAgentOperationFailure(w http.ResponseWriter, r *http.Request, session db.Session, auditAction string, targetType string, targetID string, agentID string, action string, err error) {
	details := agentFailureDetails(targetType, targetID, agentID, action, err)
	a.audit(r, session.UserID, auditAction, targetType, targetID, "failure", details)
	a.logger.Error(r.Context(), "agent_operation_failed", "agent operation failed", details)
	a.recordAgentFailureTrace(r.Context(), details)
	api.WriteError(r.Context(), w, http.StatusBadGateway, "AGENT_OPERATION_FAILED", "Agent operation failed.", details)
}

func (a *App) recordAgentFailureTrace(ctx context.Context, details map[string]any) {
	if !a.cfg.Mode.IsDevelopment() {
		return
	}
	trace := db.DebugTrace{
		Mode:       a.cfg.Mode.String(),
		Component:  "web",
		Operation:  "agent_operation_failed",
		AgentID:    stringValue(details["agent_id"]),
		TargetType: stringValue(details["target_type"]),
		TargetID:   stringValue(details["target_id"]),
		Trace:      details,
	}
	if err := a.store.AddDebugTrace(ctx, trace); err != nil {
		a.logger.Error(ctx, "debug_trace", "could not record agent failure trace", map[string]any{"agent_id": trace.AgentID, "target_type": trace.TargetType, "target_id": trace.TargetID, "error": err.Error()})
	}
}

func agentFailureDetails(targetType string, targetID string, agentID string, action string, err error) map[string]any {
	return map[string]any{
		"reason":      "agent_error",
		"agent_id":    agentID,
		"target_type": targetType,
		"target_id":   targetID,
		"action":      action,
		"error":       err.Error(),
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func parseBaseImage(dockerfile string) string {
	for _, line := range strings.Split(dockerfile, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), "FROM ") {
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}
	return "unknown"
}

func dockerfileSecretWarnings(dockerfile string) []string {
	var warnings []string
	for _, line := range strings.Split(dockerfile, "\n") {
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") {
			warnings = append(warnings, strings.TrimSpace(line))
		}
	}
	return warnings
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func mapKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	return keys
}

func fixturePath(parts ...string) string {
	all := append([]string{"tests", "fixtures"}, parts...)
	relative := filepath.Join(all...)
	if _, err := os.Stat(relative); err == nil {
		return relative
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		rooted := append([]string{filepath.Dir(file), "..", "..", "..", "tests", "fixtures"}, parts...)
		return filepath.Join(rooted...)
	}
	return relative
}
