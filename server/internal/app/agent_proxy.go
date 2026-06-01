package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/curly-hub/podorel/server/internal/agents"
	"github.com/curly-hub/podorel/server/internal/db"
)

const podmanShortIDLength = 12

func defaultAgentClientFactory(_ context.Context, agent db.Agent) (AgentClient, bool) {
	token := resolveAgentToken(agent)
	if token == "" {
		return nil, false
	}
	return agents.NewClient(agent.SocketPath, token), true
}

func resolveAgentToken(agent db.Agent) string {
	envKeys := []string{
		"PODOREL_AGENT_TOKEN_" + envSafe(agent.ID),
		"PODOREL_AGENT_TOKEN_UID_" + strconv.Itoa(agent.LinuxUID),
		"PODOREL_AGENT_TOKEN",
	}
	for _, key := range envKeys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	fileKeys := []string{
		"PODOREL_AGENT_TOKEN_FILE_" + envSafe(agent.ID),
		"PODOREL_AGENT_TOKEN_FILE",
	}
	for _, key := range fileKeys {
		if path := strings.TrimSpace(os.Getenv(key)); path != "" {
			if token := readTokenFile(path); token != "" {
				return token
			}
		}
	}
	if agent.ID == db.PrimaryAgentID {
		if home, err := os.UserHomeDir(); err == nil {
			if token := readTokenFile(filepath.Join(home, ".config", "podorel", "agent-token")); token != "" {
				return token
			}
		}
	}
	return ""
}

func readTokenFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func envSafe(input string) string {
	replacer := strings.NewReplacer("-", "_", ".", "_", ":", "_")
	return strings.ToUpper(replacer.Replace(input))
}

func (a *App) agentClient(ctx context.Context, agentID string) (db.Agent, AgentClient, bool, error) {
	agent, err := a.store.AgentByID(ctx, agentID)
	if err != nil {
		return db.Agent{}, nil, false, err
	}
	client, ok := a.newAgent(ctx, agent)
	return agent, client, ok, nil
}

func (a *App) refreshAgentSnapshots(ctx context.Context, agentID string) error {
	_, client, ok, err := a.agentClient(ctx, agentID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("agent token unavailable for %s", agentID)
	}
	pods, err := client.ListPods(ctx)
	if err != nil {
		return err
	}
	if err := a.store.TouchAgent(ctx, agentID, "online"); err != nil {
		return err
	}
	podRecords := make([]db.Pod, 0, len(pods))
	for _, pod := range pods {
		id := firstNonEmpty(pod.ID, pod.Id)
		if id == "" {
			continue
		}
		name := pod.Name
		if name == "" {
			name = id
		}
		state := firstNonEmpty(pod.State, pod.Status, "unknown")
		podRecords = append(podRecords, db.Pod{
			ID:          id,
			AgentID:     agentID,
			PodmanPodID: id,
			Name:        name,
			State:       state,
			Health:      observedHealth(pod.Health),
			CreatedAt:   pod.CreatedAt,
			RawJSON:     pod.RawJSON,
		})
	}
	containers, err := client.ListContainers(ctx)
	if err != nil {
		return err
	}
	containerIndex := newContainerSnapshotIndex(containers)
	containerRecords := make([]db.Container, 0, len(containers))
	containersByPod := map[string][]db.Container{}
	for _, container := range containers {
		id := firstNonEmpty(container.ID, container.Id)
		if id == "" {
			continue
		}
		name := container.Name
		if name == "" {
			name = id
		}
		state := firstNonEmpty(container.State, container.Status, "unknown")
		record := db.Container{
			ID:                id,
			AgentID:           agentID,
			PodID:             container.PodID,
			PodmanContainerID: id,
			Name:              name,
			Image:             container.Image,
			State:             state,
			Health:            observedHealth(container.Health),
			CreatedAt:         container.CreatedAt,
			RawJSON:           container.RawJSON,
		}
		containerRecords = append(containerRecords, record)
		if record.PodID != "" {
			containersByPod[record.PodID] = append(containersByPod[record.PodID], record)
		}
	}
	for _, pod := range podRecords {
		pod.Health = aggregatePodHealth(pod.Health, containersByPod[pod.ID])
		if err := a.store.InsertPod(ctx, pod); err != nil {
			return err
		}
	}
	for _, container := range containerRecords {
		if err := a.store.InsertContainer(ctx, container); err != nil {
			return err
		}
	}
	stats, err := client.Stats(ctx)
	if err != nil {
		return err
	}
	resolvedStats := make([]resolvedAgentStat, 0, len(stats))
	for _, stat := range stats {
		containerID, podID := containerIndex.resolveStat(stat.ContainerID, stat.Name, stat.PodID)
		resolvedStats = append(resolvedStats, resolvedAgentStat{
			Stat:                stat,
			ResolvedContainerID: containerID,
			ResolvedPodID:       podID,
		})
	}
	a.recordStatsAggregationTrace(ctx, agentID, resolvedStats)
	for _, resolved := range resolvedStats {
		stat := resolved.Stat
		raw := stat.RawJSON
		if raw == "" {
			rawBytes, _ := json.Marshal(stat)
			raw = string(rawBytes)
		}
		if err := a.store.InsertResourceSample(ctx, db.ResourceSample{
			AgentID:             agentID,
			PodID:               resolved.ResolvedPodID,
			ContainerID:         resolved.ResolvedContainerID,
			CPUPodmanRaw:        stat.CPUPodmanRaw,
			CPUPercentHostTotal: stat.CPUPercentHostTotal,
			MemoryPodmanRaw:     stat.MemoryPodmanRaw,
			MemoryBytes:         stat.MemoryBytes,
			RawJSON:             raw,
		}); err != nil {
			return err
		}
	}
	return nil
}

func observedHealth(value string) string {
	if health := normalizeObservedHealth(value); health != "" {
		return health
	}
	return "unknown"
}

func aggregatePodHealth(podHealth string, containers []db.Container) string {
	if health := normalizeObservedHealth(podHealth); health == "unhealthy" || health == "starting" {
		return health
	}
	sawHealthy := false
	sawStarting := false
	for _, container := range containers {
		switch normalizeObservedHealth(container.Health) {
		case "unhealthy":
			return "unhealthy"
		case "starting":
			sawStarting = true
		case "healthy":
			sawHealthy = true
		}
	}
	if sawStarting {
		return "starting"
	}
	if sawHealthy {
		return "healthy"
	}
	return observedHealth(podHealth)
}

func normalizeObservedHealth(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "healthy":
		return "healthy"
	case "unhealthy":
		return "unhealthy"
	case "starting":
		return "starting"
	}
	return ""
}

type resolvedAgentStat struct {
	Stat                agents.ContainerStats
	ResolvedContainerID string
	ResolvedPodID       string
}

func (a *App) recordStatsAggregationTrace(ctx context.Context, agentID string, stats []resolvedAgentStat) {
	if !a.cfg.Mode.IsDevelopment() {
		return
	}
	type aggregate struct {
		ContainerIDs        []string `json:"container_ids"`
		CPUPercentHostTotal float64  `json:"cpu_percent_host_total"`
		MemoryBytes         uint64   `json:"memory_bytes"`
	}
	aggregates := map[string]aggregate{}
	unassigned := 0
	for _, stat := range stats {
		if stat.ResolvedPodID == "" {
			unassigned++
			continue
		}
		current := aggregates[stat.ResolvedPodID]
		current.ContainerIDs = append(current.ContainerIDs, stat.ResolvedContainerID)
		current.CPUPercentHostTotal += stat.Stat.CPUPercentHostTotal
		current.MemoryBytes += stat.Stat.MemoryBytes
		aggregates[stat.ResolvedPodID] = current
	}
	if err := a.store.AddDebugTrace(ctx, db.DebugTrace{
		Mode:       a.cfg.Mode.String(),
		Component:  "web",
		Operation:  "podman_stats_aggregate",
		AgentID:    agentID,
		TargetType: "agent",
		TargetID:   agentID,
		Trace:      map[string]any{"stats_count": len(stats), "unassigned_count": unassigned, "aggregates": aggregates},
	}); err != nil {
		a.logger.Error(ctx, "debug_trace", "could not record stats aggregation trace", map[string]any{"agent_id": agentID, "error": err.Error()})
	}
}

type containerSnapshotIndex struct {
	podIDByKey       map[string]string
	containerIDByKey map[string]string
}

func newContainerSnapshotIndex(containers []agents.ContainerSummary) containerSnapshotIndex {
	index := containerSnapshotIndex{
		podIDByKey:       map[string]string{},
		containerIDByKey: map[string]string{},
	}
	for _, container := range containers {
		containerID := container.ID
		if containerID == "" {
			continue
		}
		index.add(containerID, containerID, container.PodID)
		if len(containerID) > podmanShortIDLength {
			index.add(containerID[:podmanShortIDLength], containerID, container.PodID)
		}
		index.add(container.Name, containerID, container.PodID)
	}
	return index
}

func (i containerSnapshotIndex) add(key string, containerID string, podID string) {
	normalized := normalizeContainerLookupKey(key)
	if normalized == "" {
		return
	}
	if podID != "" {
		i.podIDByKey[normalized] = podID
	}
	if containerID != "" {
		i.containerIDByKey[normalized] = containerID
	}
}

func (i containerSnapshotIndex) resolveStat(containerID string, containerName string, podID string) (string, string) {
	resolvedContainerID := containerID
	resolvedPodID := podID
	for _, key := range []string{containerID, containerName} {
		normalized := normalizeContainerLookupKey(key)
		if normalized == "" {
			continue
		}
		if fullID := i.containerIDByKey[normalized]; fullID != "" {
			resolvedContainerID = fullID
		}
		if resolvedPodID == "" {
			resolvedPodID = i.podIDByKey[normalized]
		}
	}
	return resolvedContainerID, resolvedPodID
}

func normalizeContainerLookupKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (a *App) proxyPodAction(r *http.Request, agentID string, podID string, action string) error {
	_, client, ok, err := a.agentClient(r.Context(), agentID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("agent token unavailable for %s", agentID)
	}
	return client.PodAction(r.Context(), podID, action)
}

func (a *App) proxyContainerAction(r *http.Request, agentID string, containerID string, action string) error {
	_, client, ok, err := a.agentClient(r.Context(), agentID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("agent token unavailable for %s", agentID)
	}
	return client.ContainerAction(r.Context(), containerID, action)
}

func (a *App) proxyCreatePod(r *http.Request, agentID string, req agents.CreatePodRequest) error {
	_, client, ok, err := a.agentClient(r.Context(), agentID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("agent token unavailable for %s", agentID)
	}
	return client.CreatePodFromTemplate(r.Context(), req)
}

func (a *App) proxyDeployComposeStack(r *http.Request, agentID string, req agents.DeployComposeRequest) error {
	_, client, ok, err := a.agentClient(r.Context(), agentID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("agent token unavailable for %s", agentID)
	}
	return client.DeployComposeStack(r.Context(), req)
}

func (a *App) proxyBuildImage(r *http.Request, agentID string, req agents.BuildImageRequest) error {
	_, client, ok, err := a.agentClient(r.Context(), agentID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("agent token unavailable for %s", agentID)
	}
	return client.BuildImage(r.Context(), req)
}

func (a *App) proxyCreateSecret(r *http.Request, agentID string, req agents.CreateSecretRequest) error {
	_, client, ok, err := a.agentClient(r.Context(), agentID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("agent token unavailable for %s", agentID)
	}
	return client.CreateSecret(r.Context(), req)
}
