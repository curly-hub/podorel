package app

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	ws "github.com/curly-hub/podorel/internal/websocket"
	"github.com/curly-hub/podorel/server/internal/api"
	"github.com/curly-hub/podorel/server/internal/db"
	logstore "github.com/curly-hub/podorel/server/internal/logs"
)

type logLine struct {
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Line      string    `json:"line"`
}

func (a *App) handleLogsHistory(w http.ResponseWriter, r *http.Request, session db.Session) {
	limit := defaultLogLineLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 5000 {
			limit = parsed
		}
	}
	agentID := selectedLogAgentID(r, session)
	if !a.canAccessAgent(session, agentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot access logs for this agent.", nil)
		return
	}
	podID := r.URL.Query().Get("pod_id")
	containerID := r.URL.Query().Get("container_id")
	download := r.URL.Query().Get("download") == "true"
	if podID != "" || containerID != "" {
		if lines, ok := a.readAgentLogs(r.Context(), agentID, podID, containerID, limit); ok {
			if download {
				writeLogLinesText(w, lines)
				return
			}
			api.WriteOK(r.Context(), w, map[string]any{"lines": lines, "source": "agent", "since": "24h"})
			return
		}
		a.logger.Error(r.Context(), "agent_logs", "could not read logs from agent", map[string]any{"agent_id": agentID, "pod_id": podID, "container_id": containerID})
	}
	lines, err := a.readHistoricalLogs(limit)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if download {
		writeLogLinesText(w, lines)
		return
	}
	api.WriteOK(r.Context(), w, map[string]any{"lines": lines, "since": "24h"})
}

func (a *App) readHistoricalLogs(limit int) ([]logLine, error) {
	if err := a.enforceLogStorageLimits(); err != nil {
		return nil, err
	}
	cutoff := a.now().Add(-24 * time.Hour)
	var lines []logLine
	err := filepath.WalkDir(a.logDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".log") && !strings.HasSuffix(path, ".log.gz") {
			return nil
		}
		fileLines, err := readLogFile(path, cutoff, limit)
		if err != nil {
			return err
		}
		lines = append(lines, fileLines...)
		if len(lines) > limit {
			lines = lines[len(lines)-limit:]
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(lines) == 0 {
		lines = append(lines, logLine{Timestamp: a.now(), Source: "podorel", Line: "No logs yet"})
	}
	return lines, nil
}

func (a *App) enforceLogStorageLimits() error {
	if _, err := logstore.RemoveExpired(a.logDir, a.cfg.Logs.Retention, a.now()); err != nil {
		return err
	}
	totalLimit := int64(a.cfg.Logs.TotalLimitMB) * 1024 * 1024
	if totalLimit <= 0 {
		totalLimit = logstore.DefaultTotalLimitBytes
	}
	_, err := logstore.PruneToTotalLimit(a.logDir, totalLimit)
	return err
}

func selectedLogAgentID(r *http.Request, session db.Session) string {
	return firstNonEmpty(r.URL.Query().Get("agent_id"), session.AgentID, db.PrimaryAgentID)
}

func (a *App) readAgentLogs(ctx context.Context, agentID string, podID string, containerID string, limit int) ([]logLine, bool) {
	_, client, ok, err := a.agentClient(ctx, agentID)
	if err != nil || !ok {
		return nil, false
	}
	if podID != "" && containerID == "" {
		if lines, err := a.readAgentContainerLogs(ctx, client, agentID, podID, limit); err == nil {
			return lines, true
		}
	}
	agentLines, err := client.Logs(ctx, podID, containerID, limit)
	if err != nil {
		return nil, false
	}
	lines := make([]logLine, 0, len(agentLines))
	for _, line := range agentLines {
		timestamp := line.Timestamp
		if timestamp.IsZero() {
			timestamp = a.now()
		}
		source := firstNonEmpty(line.Source, containerID, podID, agentID)
		lines = append(lines, logLine{Timestamp: timestamp, Source: source, Line: line.Line})
	}
	return lines, true
}

func (a *App) readAgentContainerLogs(ctx context.Context, client AgentClient, agentID string, podID string, limit int) ([]logLine, error) {
	containers, err := a.store.ListContainers(ctx, podID, agentID)
	if err != nil {
		return nil, err
	}
	lines := []logLine{}
	for _, container := range containers {
		containerID := firstNonEmpty(container.PodmanContainerID, container.ID)
		if containerID == "" {
			continue
		}
		agentLines, err := client.Logs(ctx, "", containerID, limit)
		if err != nil {
			continue
		}
		for _, line := range agentLines {
			timestamp := line.Timestamp
			if timestamp.IsZero() {
				timestamp = a.now()
			}
			source := firstNonEmpty(line.Source, container.Name, containerID)
			if source == containerID || source == container.PodmanContainerID {
				source = firstNonEmpty(container.Name, source)
			}
			lines = append(lines, logLine{Timestamp: timestamp, Source: source, Line: line.Line})
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("no container logs for pod %s", podID)
	}
	sort.SliceStable(lines, func(i int, j int) bool {
		return lines[i].Timestamp.Before(lines[j].Timestamp)
	})
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}

func (a *App) liveLogLines(ctx context.Context, agentID string, podID string, containerID string) []logLine {
	if podID != "" || containerID != "" {
		if lines, ok := a.readAgentLogs(ctx, agentID, podID, containerID, liveLogReplayLimit); ok {
			return lines
		}
		return []logLine{{Timestamp: a.now(), Source: "podorel", Line: "Agent logs are unavailable for the selected target."}}
	}
	lines, err := a.readHistoricalLogs(liveLogReplayLimit)
	if err != nil {
		return []logLine{{Timestamp: a.now(), Source: "podorel", Line: "Historical logs are unavailable."}}
	}
	return lines
}

func writeLogLinesText(w http.ResponseWriter, lines []logLine) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, line := range lines {
		_, _ = fmt.Fprintf(w, "%s %s %s\n", line.Timestamp.Format(time.RFC3339), line.Source, line.Line)
	}
}

func readLogFile(path string, cutoff time.Time, limit int) ([]logLine, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var reader io.Reader = file
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}
	scanner := bufio.NewScanner(reader)
	var lines []logLine
	for scanner.Scan() {
		parsed := parseLogLine(scanner.Text(), filepath.Base(path))
		if parsed.Timestamp.IsZero() {
			parsed.Timestamp = time.Now().UTC()
		}
		if parsed.Timestamp.Before(cutoff) {
			continue
		}
		lines = append(lines, parsed)
		if len(lines) > limit {
			lines = lines[len(lines)-limit:]
		}
	}
	return lines, scanner.Err()
}

func parseLogLine(raw string, source string) logLine {
	var payload struct {
		Timestamp     string         `json:"ts"`
		Level         string         `json:"level"`
		Message       string         `json:"message"`
		Mode          string         `json:"mode"`
		Component     string         `json:"component"`
		Operation     string         `json:"operation"`
		CorrelationID string         `json:"correlation_id"`
		Fields        map[string]any `json:"fields"`
	}
	if json.Unmarshal([]byte(raw), &payload) == nil && payload.Message != "" {
		ts, _ := time.Parse(time.RFC3339Nano, payload.Timestamp)
		if payload.Component != "" {
			source = payload.Component
		}
		parts := []string{}
		if payload.Level != "" {
			parts = append(parts, strings.ToUpper(payload.Level))
		}
		if payload.Operation != "" {
			parts = append(parts, payload.Operation)
		}
		if payload.CorrelationID != "" {
			parts = append(parts, "correlation="+payload.CorrelationID)
		}
		parts = append(parts, payload.Message)
		if len(payload.Fields) > 0 {
			fields, _ := json.Marshal(payload.Fields)
			parts = append(parts, string(fields))
		}
		return logLine{Timestamp: ts, Source: source, Line: strings.Join(parts, " | ")}
	}
	return logLine{Timestamp: time.Now().UTC(), Source: source, Line: raw}
}

func (a *App) handleLogsWebSocket(w http.ResponseWriter, r *http.Request, session db.Session) {
	agentID := selectedLogAgentID(r, session)
	if !a.canAccessAgent(session, agentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot access logs for this agent.", nil)
		return
	}
	conn, err := acceptWebSocket(w, r)
	if err != nil {
		return
	}
	defer conn.Close()
	podID := r.URL.Query().Get("pod_id")
	containerID := r.URL.Query().Get("container_id")
	a.logger.Debug(r.Context(), "ws_logs_connect", "log websocket connected", map[string]any{
		"agent_id":     agentID,
		"pod_id":       podID,
		"container_id": containerID,
	})
	defer a.logger.Debug(r.Context(), "ws_logs_disconnect", "log websocket disconnected", map[string]any{
		"clean_close": true,
	})

	seen := map[string]struct{}{}
	ticker := time.NewTicker(liveLogPollInterval)
	defer ticker.Stop()
	for {
		for _, line := range a.liveLogLines(r.Context(), agentID, podID, containerID) {
			key := line.Source + "\x00" + line.Line
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			_ = conn.SetWriteDeadline(a.now().Add(webSocketWriteWait))
			if err := writeWebSocketText(conn, mustJSON(line)); err != nil {
				a.logger.Debug(r.Context(), "ws_logs_disconnect", "log websocket write failed", map[string]any{"error": err.Error()})
				return
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) handleMetricsWebSocket(w http.ResponseWriter, r *http.Request, session db.Session) {
	_ = session
	conn, err := acceptWebSocket(w, r)
	if err != nil {
		return
	}
	defer conn.Close()
	agentID := session.AgentID
	if agentID == "" {
		agentID = db.PrimaryAgentID
	}
	freshAfter := a.now()
	refreshSucceeded := false
	if err := a.refreshAgentSnapshots(r.Context(), agentID); err != nil {
		a.logger.Error(r.Context(), "agent_refresh", "could not refresh websocket metrics from agent", map[string]any{"agent_id": agentID, "error": err.Error()})
	} else {
		refreshSucceeded = true
	}
	stats, _ := a.store.CurrentStats(r.Context(), session.AgentID)
	stats = freshStatsOnly(stats, refreshSucceeded, freshAfter)
	_ = writeWebSocketText(conn, mustJSON(map[string]any{"type": "metrics", "stats": stats}))
}

func (a *App) handleExecWebSocket(w http.ResponseWriter, r *http.Request, session db.Session) {
	targetID := strings.TrimSpace(r.URL.Query().Get("container_id"))
	if targetID == "" {
		api.WriteError(r.Context(), w, http.StatusBadRequest, "CONTAINER_ID_REQUIRED", "container_id is required.", nil)
		return
	}
	if !a.cfg.Actions.ExecEnabled {
		a.audit(r, session.UserID, "exec.open", "container", targetID, "failure", map[string]any{"reason": "disabled"})
		api.WriteError(r.Context(), w, http.StatusForbidden, "EXEC_DISABLED", "Exec shell is disabled. Enable it in Settings first.", nil)
		return
	}
	container, err := a.store.ContainerByID(r.Context(), targetID)
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	if !a.canAccessAgent(session, container.AgentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot access this container.", nil)
		return
	}
	if !strings.EqualFold(container.State, "running") {
		a.audit(r, session.UserID, "exec.open", "container", container.ID, "failure", map[string]any{"reason": "container_not_running", "state": container.State})
		api.WriteError(r.Context(), w, http.StatusConflict, "CONTAINER_NOT_RUNNING", "Container must be running before opening a shell.", map[string]any{"state": container.State})
		return
	}
	shell := strings.TrimSpace(r.URL.Query().Get("shell"))
	if shell == "" {
		shell = "sh"
	}
	_, client, ok, err := a.agentClient(r.Context(), container.AgentID)
	if err != nil {
		a.writeAgentOperationFailure(w, r, session, "exec.open", "container", container.ID, container.AgentID, "exec", err)
		return
	}
	if !ok {
		a.writeAgentOperationFailure(w, r, session, "exec.open", "container", container.ID, container.AgentID, "exec", fmt.Errorf("agent token unavailable for %s", container.AgentID))
		return
	}
	cols := parseIntQuery(r, "cols", 80)
	rows := parseIntQuery(r, "rows", 24)
	agentConn, err := client.ExecWebSocket(r.Context(), container.PodmanContainerID, shell, cols, rows)
	if err != nil {
		a.writeAgentOperationFailure(w, r, session, "exec.open", "container", container.ID, container.AgentID, "exec", err)
		return
	}
	defer agentConn.Close()
	browserConn, err := ws.Accept(w, r)
	if err != nil {
		return
	}
	defer browserConn.Close()
	a.audit(r, session.UserID, "exec.open", "container", container.ID, "success", map[string]any{"shell": shell})
	a.logger.Debug(r.Context(), "ws_exec_connect", "exec websocket connected", map[string]any{"agent_id": container.AgentID, "container_id": container.ID, "shell": shell})

	errCh := make(chan error, 2)
	go bridgeExecWebSocket(browserConn, agentConn, errCh)
	go bridgeExecWebSocket(agentConn, browserConn, errCh)
	select {
	case <-r.Context().Done():
	case <-errCh:
	}
	_ = browserConn.WriteClose()
	_ = agentConn.WriteClose()
	a.logger.Debug(r.Context(), "ws_exec_disconnect", "exec websocket disconnected", map[string]any{"agent_id": container.AgentID, "container_id": container.ID})
}

func parseIntQuery(r *http.Request, key string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func bridgeExecWebSocket(dst *ws.Conn, src *ws.Conn, errCh chan<- error) {
	for {
		message, err := src.ReadText()
		if err != nil {
			errCh <- err
			return
		}
		if err := dst.WriteText(message); err != nil {
			errCh <- err
			return
		}
	}
}

func (a *App) handleSecuritySummary(w http.ResponseWriter, r *http.Request, _ db.Session) {
	scans, err := a.store.ListSecurityScans(r.Context(), 1)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	status := "unknown"
	var latest any
	if len(scans) > 0 {
		latest = scans[0]
		status = scans[0].Status
	}
	digests, _ := a.store.ListImageDigests(r.Context(), 20)
	updates, _ := a.store.ListHostPackageUpdates(r.Context(), 20)
	scanner := a.configuredScannerName()
	scannerStatus := a.securityScannerStatus(r.Context(), db.PrimaryAgentID, scanner)
	scannerAvailable := scannerStatus.Available
	scannerError := ""
	if !scannerAvailable {
		scannerError = firstNonEmpty(scannerStatus.Error, scannerUnavailableMessage(scanner))
	}
	api.WriteOK(r.Context(), w, map[string]any{
		"status":            status,
		"latest_scan":       latest,
		"scanner":           scanner,
		"scanner_available": scannerAvailable,
		"scanner_error":     scannerError,
		"scheduled_scans":   a.cfg.Security.ScheduledScansEnabled,
		"image_digest":      securityCollectionStatus(len(digests)),
		"host_packages":     securityCollectionStatus(len(updates)),
		"image_digests":     digests,
		"host_updates":      updates,
	})
}

func securityCollectionStatus(count int) string {
	if count > 0 {
		return "available"
	}
	return "unknown"
}

func (a *App) handleSecurityScan(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireCSRF(w, r) {
		return
	}
	agentID := session.AgentID
	if agentID == "" {
		agentID = db.PrimaryAgentID
	}
	scan, err := a.runSecurityScan(r.Context(), agentID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	result := "success"
	if scan.Status == "failed" {
		result = "failure"
	}
	a.audit(r, session.UserID, "security.scan", "scan", scan.ID, result, scan.Summary)
	api.WriteOK(r.Context(), w, scan)
}

func (a *App) handleSecurityScanByID(w http.ResponseWriter, r *http.Request, _ db.Session) {
	scan, err := a.store.SecurityScanByID(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, scan)
}

func (a *App) handleAudit(w http.ResponseWriter, r *http.Request, _ db.Session) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	events, err := a.store.ListAudit(r.Context(), limit)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, events)
}

func (a *App) handleRuntimeMode(w http.ResponseWriter, r *http.Request, _ db.Session) {
	api.WriteOK(r.Context(), w, map[string]any{
		"mode":                    a.cfg.Mode.String(),
		"raw_traces_available":    a.cfg.Mode.IsDevelopment(),
		"production_safe_summary": a.cfg.Mode.IsProduction(),
	})
}

func (a *App) handleTraces(w http.ResponseWriter, r *http.Request, _ db.Session) {
	if a.cfg.Mode.IsProduction() {
		api.WriteOK(r.Context(), w, map[string]any{"traces": []db.DebugTrace{}, "redacted": true})
		return
	}
	traces, err := a.store.ListDebugTraces(r.Context(), r.URL.Query().Get("correlation_id"), 50)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, traces)
}

func (a *App) handleDiagnosticsStats(w http.ResponseWriter, r *http.Request, session db.Session) {
	agentID := session.AgentID
	if agentID == "" {
		agentID = db.PrimaryAgentID
	}
	freshAfter := a.now()
	refreshSucceeded := false
	if err := a.refreshAgentSnapshots(r.Context(), agentID); err != nil {
		a.logger.Error(r.Context(), "agent_refresh", "could not refresh diagnostics stats from agent", map[string]any{"agent_id": agentID, "error": err.Error()})
	} else {
		refreshSucceeded = true
	}
	stats, err := a.store.CurrentStats(r.Context(), session.AgentID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	stats = freshStatsOnly(stats, refreshSucceeded, freshAfter)
	containerID := r.PathValue("container_id")
	for _, stat := range stats {
		if stat.ContainerID == containerID {
			if a.cfg.Mode.IsProduction() {
				stat.RawJSON = ""
				stat.CPUPodmanRaw = ""
				stat.MemoryPodmanRaw = ""
			}
			api.WriteOK(r.Context(), w, stat)
			return
		}
	}
	api.WriteError(r.Context(), w, http.StatusNotFound, "STATS_NOT_FOUND", "Stats were not found for this container.", nil)
}

func (a *App) handleDiagnosticsBundle(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireAdminPasswordSession(w, r, session) || !a.requireCSRF(w, r) {
		return
	}
	a.audit(r, session.UserID, "diagnostics.bundle", "diagnostics", "bundle", "success", nil)
	api.WriteOK(r.Context(), w, map[string]any{
		"mode":     a.cfg.Mode.String(),
		"redacted": true,
		"health":   "ok",
	})
}

func acceptWebSocket(w http.ResponseWriter, r *http.Request) (net.Conn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "websocket upgrade required", http.StatusUpgradeRequired)
		return nil, fmt.Errorf("websocket upgrade required")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
		return nil, fmt.Errorf("hijacking unsupported")
	}
	conn, buf, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	acceptRaw := sha1.Sum([]byte(key + webSocketAcceptMagic))
	accept := base64.StdEncoding.EncodeToString(acceptRaw[:])
	_, err = fmt.Fprintf(buf, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := buf.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func writeWebSocketText(w io.Writer, message string) error {
	payload := []byte(message)
	header := []byte{0x81}
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload)))
	case len(payload) <= 65535:
		header = append(header, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		return fmt.Errorf("websocket message exceeds supported length")
	}
	_, err := w.Write(append(header, payload...))
	return err
}
