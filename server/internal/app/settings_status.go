package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/curly-hub/podorel/server/internal/api"
	"github.com/curly-hub/podorel/server/internal/auth"
	"github.com/curly-hub/podorel/server/internal/config"
	"github.com/curly-hub/podorel/server/internal/db"
)

func applyPersistedSettingsToConfig(ctx context.Context, store *db.Store, cfg *config.Config) error {
	settings, err := store.ListSettings(ctx)
	if err != nil {
		return err
	}
	for key, raw := range settings {
		switch key {
		case "actions":
			var value struct {
				ExecEnabled       bool `json:"exec_enabled"`
				AutomationEnabled bool `json:"automation_enabled"`
			}
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			cfg.Actions.ExecEnabled = value.ExecEnabled
			cfg.Actions.AutomationEnabled = value.AutomationEnabled
		case "logs":
			var value struct {
				RetentionHours int `json:"retention_hours"`
				PerPodLimitMB  int `json:"per_pod_limit_mb"`
				TotalLimitMB   int `json:"total_limit_mb"`
			}
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			if value.RetentionHours > 0 {
				cfg.Logs.Retention = time.Duration(value.RetentionHours) * time.Hour
			}
			if value.PerPodLimitMB > 0 {
				cfg.Logs.PerPodLimitMB = value.PerPodLimitMB
			}
			if value.TotalLimitMB > 0 {
				cfg.Logs.TotalLimitMB = value.TotalLimitMB
			}
		case "metrics":
			var value struct {
				RetentionHours int `json:"retention_hours"`
			}
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			if value.RetentionHours > 0 {
				cfg.Metrics.Retention = time.Duration(value.RetentionHours) * time.Hour
			}
		case "security":
			var value struct {
				ScheduledScansEnabled bool   `json:"scheduled_scans_enabled"`
				Schedule              string `json:"schedule"`
				Scanner               string `json:"scanner"`
			}
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			cfg.Security.ScheduledScansEnabled = value.ScheduledScansEnabled
			if strings.TrimSpace(value.Schedule) != "" {
				cfg.Security.Schedule = value.Schedule
			}
			if strings.TrimSpace(value.Scanner) != "" {
				cfg.Security.Scanner = value.Scanner
			}
		}
	}
	return nil
}

func (a *App) handleSystemStatus(w http.ResponseWriter, r *http.Request, _ db.Session) {
	agent, err := a.store.AgentByID(r.Context(), db.PrimaryAgentID)
	var primary map[string]any
	if err != nil {
		primary = map[string]any{"agent_id": db.PrimaryAgentID, "status": "missing", "last_error": err.Error()}
	} else {
		primary = a.agentHealthStatus(r.Context(), agent)
	}
	api.WriteOK(r.Context(), w, map[string]any{
		"runtime_mode":         a.cfg.Mode.String(),
		"public_url":           a.cfg.Server.PublicURL,
		"active_backend_port":  listenAddrPort(a.cfg.Server.ListenAddr),
		"ui_build_timestamp":   uiBuildTimestamp(a.cfg.UI.DistPath),
		"primary_agent_health": primary,
		"podman_availability": map[string]any{
			"socket": primary["podman_socket_available"],
			"cli":    primary["podman_cli_available"],
		},
		"fallback_mode":  fallbackModeLabel(a.allowSnapshotFallback),
		"dev_supervisor": devSupervisorStatus(),
	})
}

func devSupervisorStatus() map[string]any {
	path := strings.TrimSpace(os.Getenv("PODOREL_DEV_STATUS_FILE"))
	if path == "" {
		path = filepath.Join(".podorel", "dev-status.json")
		if !fileExists(path) {
			return map[string]any{
				"status":  "unknown",
				"path":    path,
				"message": "Development supervisor status is not configured. Start PoDorel with scripts/deploy-dev.sh --detach to publish process status.",
			}
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		message := "Development supervisor status is not available yet. Start or restart PoDorel with scripts/deploy-dev.sh --detach."
		if !os.IsNotExist(err) {
			message = "Development supervisor status could not be read. Check file permissions and restart PoDorel dev services."
		}
		return map[string]any{"status": "missing", "path": path, "message": message, "detail": err.Error()}
	}
	var status map[string]any
	if err := json.Unmarshal(raw, &status); err != nil {
		return map[string]any{"status": "invalid", "path": path, "message": "Development supervisor status is invalid JSON.", "detail": err.Error()}
	}
	status["path"] = path
	return status
}

func (a *App) agentHealthStatus(ctx context.Context, agent db.Agent) map[string]any {
	status := map[string]any{
		"agent_id":               agent.ID,
		"status":                 agent.Status,
		"mode":                   a.cfg.Mode.String(),
		"socket_path":            agent.SocketPath,
		"agent_socket_available": fileExists(agent.SocketPath),
		"token_available":        resolveAgentToken(agent) != "",
		"web_server":             "ok",
		"ui_proxy":               "ok",
		"self_management":        true,
		"last_seen_at":           optionalTimeString(agent.LastSeenAt),
		"last_error":             "",
	}
	_, client, ok, err := a.agentClient(ctx, agent.ID)
	if err != nil {
		status["status"] = "offline"
		status["agent_api"] = "unavailable"
		status["last_error"] = err.Error()
		return status
	}
	if !ok {
		status["status"] = "offline"
		status["agent_api"] = "token_unavailable"
		status["last_error"] = "agent token unavailable"
		return status
	}
	health, err := client.Health(ctx)
	if err != nil {
		status["status"] = "offline"
		status["agent_api"] = "error"
		status["last_error"] = err.Error()
		_ = a.store.TouchAgent(ctx, agent.ID, "offline")
		return status
	}
	now := a.now()
	_ = a.store.TouchAgent(ctx, agent.ID, "online")
	status["status"] = firstNonEmpty(health.Status, "online")
	status["agent_api"] = "ok"
	status["agent_mode"] = health.Mode
	status["agent_user"] = health.User
	status["podman_socket_path"] = health.PodmanSocketPath
	status["podman_socket_available"] = health.PodmanSocketAvailable
	status["podman_cli_available"] = health.PodmanCLIAvailable
	status["last_error"] = health.LastError
	status["last_seen_at"] = now.Format(time.RFC3339Nano)
	return status
}

func (a *App) applySettingsPayload(ctx context.Context, payload map[string]any) ([]string, []string, error) {
	updated := []string{}
	requiresRestart := []string{}
	if actions, ok := objectValue(payload["actions"]); ok {
		value := map[string]any{
			"exec_enabled":       boolValue(actions["exec_enabled"], a.cfg.Actions.ExecEnabled),
			"automation_enabled": boolValue(actions["automation_enabled"], a.cfg.Actions.AutomationEnabled),
		}
		if err := a.store.UpsertSetting(ctx, "actions", value); err != nil {
			return nil, nil, err
		}
		a.cfg.Actions.ExecEnabled = value["exec_enabled"].(bool)
		a.cfg.Actions.AutomationEnabled = value["automation_enabled"].(bool)
		updated = append(updated, "actions")
	}
	if logs, ok := objectValue(payload["logs"]); ok {
		retentionHours := positiveIntValue(logs["retention_hours"], int(a.cfg.Logs.Retention/time.Hour))
		perPod := positiveIntValue(logs["per_pod_limit_mb"], a.cfg.Logs.PerPodLimitMB)
		total := positiveIntValue(logs["total_limit_mb"], a.cfg.Logs.TotalLimitMB)
		value := map[string]any{"retention_hours": retentionHours, "per_pod_limit_mb": perPod, "total_limit_mb": total}
		if err := a.store.UpsertSetting(ctx, "logs", value); err != nil {
			return nil, nil, err
		}
		a.cfg.Logs.Retention = time.Duration(retentionHours) * time.Hour
		a.cfg.Logs.PerPodLimitMB = perPod
		a.cfg.Logs.TotalLimitMB = total
		updated = append(updated, "logs")
	}
	if metrics, ok := objectValue(payload["metrics"]); ok {
		retentionHours := positiveIntValue(metrics["retention_hours"], int(a.cfg.Metrics.Retention/time.Hour))
		value := map[string]any{"retention_hours": retentionHours}
		if err := a.store.UpsertSetting(ctx, "metrics", value); err != nil {
			return nil, nil, err
		}
		a.cfg.Metrics.Retention = time.Duration(retentionHours) * time.Hour
		updated = append(updated, "metrics")
	}
	if securitySettings, ok := objectValue(payload["security"]); ok {
		schedule := stringValueOr(securitySettings["schedule"], a.cfg.Security.Schedule)
		scanner := stringValueOr(securitySettings["scanner"], a.cfg.Security.Scanner)
		value := map[string]any{
			"scheduled_scans_enabled": boolValue(securitySettings["scheduled_scans_enabled"], a.cfg.Security.ScheduledScansEnabled),
			"schedule":                schedule,
			"scanner":                 scanner,
		}
		if err := a.store.UpsertSetting(ctx, "security", value); err != nil {
			return nil, nil, err
		}
		a.cfg.Security.ScheduledScansEnabled = value["scheduled_scans_enabled"].(bool)
		a.cfg.Security.Schedule = schedule
		a.cfg.Security.Scanner = scanner
		updated = append(updated, "security")
	}
	for _, key := range []string{"database", "server", "ui", "agent", "auth"} {
		if _, ok := payload[key]; ok {
			requiresRestart = append(requiresRestart, key)
		}
	}
	return updated, requiresRestart, nil
}

func (a *App) verifyAdminPassword(ctx context.Context, password string) bool {
	if strings.TrimSpace(password) == "" {
		return false
	}
	user, err := a.store.FindUserByUsername(ctx, db.DefaultAdminUsername)
	if err != nil {
		return false
	}
	return auth.VerifyPassword(password, user.PasswordHash)
}

func (a *App) requireAdminPasswordValue(w http.ResponseWriter, r *http.Request, password string) bool {
	if !a.verifyAdminPassword(r.Context(), password) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "ADMIN_PASSWORD_INVALID", "Admin password verification failed.", nil)
		return false
	}
	return true
}

func objectValue(value any) (map[string]any, bool) {
	out, ok := value.(map[string]any)
	return out, ok
}

func boolValue(value any, fallback bool) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	return fallback
}

func positiveIntValue(value any, fallback int) int {
	var parsed int
	switch typed := value.(type) {
	case float64:
		parsed = int(typed)
	case int:
		parsed = typed
	case string:
		value, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			parsed = value
		}
	}
	if parsed <= 0 {
		return fallback
	}
	return parsed
}

func stringValueOr(value any, fallback string) string {
	if typed, ok := value.(string); ok && strings.TrimSpace(typed) != "" {
		return strings.TrimSpace(typed)
	}
	return fallback
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func optionalTimeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func listenAddrPort(listenAddr string) string {
	_, port, err := net.SplitHostPort(listenAddr)
	if err == nil {
		return port
	}
	if idx := strings.LastIndex(listenAddr, ":"); idx >= 0 {
		return listenAddr[idx+1:]
	}
	return ""
}

func uiBuildTimestamp(distPath string) string {
	info, err := os.Stat(filepath.Join(distPath, "index.html"))
	if err != nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339Nano)
}

func fallbackModeLabel(allowed bool) string {
	if allowed {
		return "development_cached_snapshot"
	}
	return "disabled"
}

func agentRefreshError(agentID string, err error) map[string]any {
	return map[string]any{"agent_id": agentID, "error": fmt.Sprint(err), "fallback_mode": "disabled"}
}
