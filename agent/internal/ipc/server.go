package ipc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/creack/pty"
	"github.com/curly-hub/podorel/agent/internal/podman"
	"github.com/curly-hub/podorel/internal/correlation"
	"github.com/curly-hub/podorel/internal/logging"
	podorelruntime "github.com/curly-hub/podorel/internal/runtime"
	ws "github.com/curly-hub/podorel/internal/websocket"
)

const (
	socketPermissions = 0o600
	shutdownTimeout   = 5 * time.Second
)

type Server struct {
	SocketPath string
	Token      string
	Mode       podorelruntime.Mode
	Logger     *logging.Logger
	Runtime    podman.PodmanRuntime
	OnReady    func() error
}

func (s Server) Serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.SocketPath), 0o700); err != nil {
		return err
	}
	if err := os.Remove(s.SocketPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	listener, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(s.SocketPath, socketPermissions); err != nil {
		_ = listener.Close()
		return err
	}
	if s.OnReady != nil {
		if err := s.OnReady(); err != nil && s.Logger != nil {
			s.Logger.Error(ctx, "systemd_ready", "could not notify systemd readiness", map[string]any{"error": err.Error()})
		}
	}

	server := &http.Server{
		Handler: s.authMiddleware(s.routes()),
	}

	errCh := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		podmanSocketPath := podman.DefaultPodmanSocketPath()
		podmanSocketAvailable := false
		if podmanSocketPath != "" {
			if info, err := os.Stat(podmanSocketPath); err == nil && !info.IsDir() {
				podmanSocketAvailable = true
			}
		}
		_, podmanCLIErr := exec.LookPath(podman.DefaultPodmanBinary)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":                  "ok",
			"mode":                    s.Mode.String(),
			"user":                    os.Getenv("USER"),
			"socket_path":             s.SocketPath,
			"token_configured":        s.Token != "",
			"podman_socket_path":      podmanSocketPath,
			"podman_socket_available": podmanSocketAvailable,
			"podman_cli_available":    podmanCLIErr == nil,
			"last_error":              "",
			"last_seen_at":            time.Now().UTC().Format(time.RFC3339Nano),
		})
	})
	mux.HandleFunc("GET /pods", s.withRuntime(s.handleListPods))
	mux.HandleFunc("GET /containers", s.withRuntime(s.handleListContainers))
	mux.HandleFunc("GET /stats", s.withRuntime(s.handleStats))
	mux.HandleFunc("POST /pods/{id}/start", s.withRuntime(s.handlePodAction("start")))
	mux.HandleFunc("POST /pods/{id}/stop", s.withRuntime(s.handlePodAction("stop")))
	mux.HandleFunc("POST /pods/{id}/restart", s.withRuntime(s.handlePodAction("restart")))
	mux.HandleFunc("POST /pods/{id}/kill", s.withRuntime(s.handlePodAction("kill")))
	mux.HandleFunc("DELETE /pods/{id}", s.withRuntime(s.handleDeletePod))
	mux.HandleFunc("POST /containers/{id}/start", s.withRuntime(s.handleContainerAction("start")))
	mux.HandleFunc("POST /containers/{id}/stop", s.withRuntime(s.handleContainerAction("stop")))
	mux.HandleFunc("POST /containers/{id}/restart", s.withRuntime(s.handleContainerAction("restart")))
	mux.HandleFunc("POST /containers/{id}/kill", s.withRuntime(s.handleContainerAction("kill")))
	mux.HandleFunc("DELETE /containers/{id}", s.withRuntime(s.handleDeleteContainer))
	mux.HandleFunc("GET /containers/{id}/exec/ws", s.handleContainerExecWebSocket)
	mux.HandleFunc("POST /containers/{id}/exec", s.withRuntime(s.handleContainerExec))
	mux.HandleFunc("GET /logs", s.withRuntime(s.handleLogs))
	mux.HandleFunc("POST /pods/create-from-template", s.withRuntime(s.handleCreatePodFromTemplate))
	mux.HandleFunc("POST /compose-stacks/deploy", s.withRuntime(s.handleDeployComposeStack))
	mux.HandleFunc("POST /images/build-from-dockerfile", s.withRuntime(s.handleBuildImage))
	mux.HandleFunc("POST /secrets", s.withRuntime(s.handleCreateSecret))
	mux.HandleFunc("GET /security/scanner", s.handleScannerStatus)
	mux.HandleFunc("POST /security/scan-image", s.handleScanImage)
	return mux
}

func (s Server) withRuntime(next func(http.ResponseWriter, *http.Request, podman.PodmanRuntime)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Runtime == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "podman runtime unavailable"})
			return
		}
		next(w, r, s.Runtime)
	}
}

func (s Server) handleListPods(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
	pods, err := runtime.ListPods(r.Context())
	writeResult(w, pods, err)
}

func (s Server) handleListContainers(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
	containers, err := runtime.ListContainers(r.Context())
	writeResult(w, containers, err)
}

func (s Server) handleStats(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
	stats, err := runtime.Stats(r.Context())
	writeResult(w, stats, err)
}

func (s Server) handlePodAction(action string) func(http.ResponseWriter, *http.Request, podman.PodmanRuntime) {
	return func(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
		id := r.PathValue("id")
		var err error
		switch action {
		case "start":
			err = runtime.StartPod(r.Context(), id)
		case "stop":
			err = runtime.StopPod(r.Context(), id)
		case "restart":
			err = runtime.RestartPod(r.Context(), id)
		case "kill":
			err = runtime.KillPod(r.Context(), id)
		default:
			err = errors.New("unsupported pod action")
		}
		writeResult(w, map[string]any{"target": id, "action": action}, err)
	}
}

func (s Server) handleDeletePod(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
	id := r.PathValue("id")
	writeResult(w, map[string]any{"target": id, "action": "delete"}, runtime.DeletePod(r.Context(), id))
}

func (s Server) handleContainerAction(action string) func(http.ResponseWriter, *http.Request, podman.PodmanRuntime) {
	return func(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
		id := r.PathValue("id")
		var err error
		switch action {
		case "start":
			err = runtime.StartContainer(r.Context(), id)
		case "stop":
			err = runtime.StopContainer(r.Context(), id)
		case "restart":
			err = runtime.RestartContainer(r.Context(), id)
		case "kill":
			err = runtime.KillContainer(r.Context(), id)
		default:
			err = errors.New("unsupported container action")
		}
		writeResult(w, map[string]any{"target": id, "action": action}, err)
	}
}

func (s Server) handleDeleteContainer(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
	id := r.PathValue("id")
	writeResult(w, map[string]any{"target": id, "action": "delete"}, runtime.DeleteContainer(r.Context(), id))
}

func (s Server) handleContainerExec(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
	var req podman.ExecRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	req.ContainerID = r.PathValue("id")
	result, err := runtime.Exec(r.Context(), req)
	writeResult(w, result, err)
}

type execWebSocketClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (s Server) handleContainerExecWebSocket(w http.ResponseWriter, r *http.Request) {
	containerID := strings.TrimSpace(r.PathValue("id"))
	if containerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "container id is required"})
		return
	}
	shell, err := execShell(r.URL.Query().Get("shell"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if _, err := exec.LookPath(podman.DefaultPodmanBinary); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "podman CLI is required for interactive exec"})
		return
	}
	conn, err := ws.Accept(w, r)
	if err != nil {
		return
	}
	defer conn.Close()

	cols, rows := execTerminalSize(r.URL.Query().Get("cols"), r.URL.Query().Get("rows"))
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd, terminal, err := startPTYExec(ctx, containerID, shell, cols, rows)
	if err != nil {
		_ = conn.WriteText(mustExecWSJSON(map[string]any{"type": "error", "message": err.Error()}))
		return
	}
	defer terminal.Close()
	if s.Logger != nil {
		s.Logger.Debug(r.Context(), "exec_shell_open", "interactive exec shell opened", map[string]any{"container_id": containerID, "shell": shell, "terminal_mode": "pty", "cols": cols, "rows": rows})
	}
	_ = conn.WriteText(mustExecWSJSON(map[string]any{"type": "status", "status": "connected", "shell": shell, "container_id": containerID, "terminal_mode": "pty", "cols": cols, "rows": rows}))

	go streamExecOutput(conn, terminal, "stdout")
	go func() {
		for {
			raw, err := conn.ReadText()
			if err != nil {
				_ = terminal.Close()
				cancel()
				return
			}
			var message execWebSocketClientMessage
			if err := json.Unmarshal([]byte(raw), &message); err != nil {
				message = execWebSocketClientMessage{Type: "input", Data: raw}
			}
			switch message.Type {
			case "input":
				if message.Data != "" {
					_, _ = terminal.Write([]byte(message.Data))
				}
			case "close":
				_ = terminal.Close()
				cancel()
				return
			case "resize":
				resizePTY(terminal, message.Cols, message.Rows)
			default:
				_ = conn.WriteText(mustExecWSJSON(map[string]any{"type": "error", "message": "unsupported exec message type"}))
			}
		}
	}()

	err = cmd.Wait()
	cancel()
	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		} else if !errors.Is(err, os.ErrClosed) {
			_ = conn.WriteText(mustExecWSJSON(map[string]any{"type": "error", "message": err.Error()}))
			exitCode = -1
		}
	}
	_ = conn.WriteText(mustExecWSJSON(map[string]any{"type": "status", "status": "closed", "exit_code": exitCode}))
	if s.Logger != nil {
		s.Logger.Debug(r.Context(), "exec_shell_close", "interactive exec shell closed", map[string]any{"container_id": containerID, "shell": shell, "terminal_mode": "pty", "exit_code": exitCode})
	}
}

func startPTYExec(ctx context.Context, containerID string, shell string, cols uint16, rows uint16) (*exec.Cmd, *os.File, error) {
	cmd := exec.CommandContext(ctx, podman.DefaultPodmanBinary, "exec", "-it", containerID, shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, nil, fmt.Errorf("start exec pty: %w", err)
	}
	return cmd, terminal, nil
}

func resizePTY(terminal *os.File, cols int, rows int) {
	if terminal == nil {
		return
	}
	if cols < 20 || cols > 500 || rows < 5 || rows > 200 {
		return
	}
	_ = pty.Setsize(terminal, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func execTerminalSize(rawCols string, rawRows string) (uint16, uint16) {
	cols := parseTerminalSize(rawCols, 80, 20, 500)
	rows := parseTerminalSize(rawRows, 24, 5, 200)
	return uint16(cols), uint16(rows)
}

func parseTerminalSize(raw string, fallback int, minValue int, maxValue int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed < minValue || parsed > maxValue {
		return fallback
	}
	return parsed
}

func streamExecOutput(conn *ws.Conn, reader io.Reader, stream string) {
	buf := make([]byte, 8192)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			_ = conn.WriteText(mustExecWSJSON(map[string]any{"type": "output", "stream": stream, "data": string(buf[:n])}))
		}
		if err != nil {
			return
		}
	}
}

func execShell(shell string) (string, error) {
	normalized := strings.TrimSpace(shell)
	if normalized == "" {
		return "sh", nil
	}
	switch normalized {
	case "sh", "/bin/sh", "bash", "/bin/bash":
		return normalized, nil
	default:
		return "", errors.New("unsupported shell; expected sh or bash")
	}
}

func mustExecWSJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func (s Server) handleLogs(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
	lastLines := 100
	if raw := r.URL.Query().Get("last"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 1000 {
			lastLines = parsed
		}
	}
	ch, err := runtime.Logs(r.Context(), podman.LogRequest{
		PodID:       r.URL.Query().Get("pod_id"),
		ContainerID: r.URL.Query().Get("container_id"),
		Follow:      r.URL.Query().Get("follow") == "true",
		LastLines:   lastLines,
	})
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	lines := make([]podman.LogLine, 0, lastLines)
	for line := range ch {
		lines = append(lines, line)
		if len(lines) >= lastLines {
			break
		}
	}
	writeResult(w, lines, nil)
}

func (s Server) handleCreateSecret(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
	var req podman.CreateSecretRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	writeResult(w, map[string]any{"secret_name": req.Name}, runtime.CreateSecret(r.Context(), req))
}

func (s Server) handleCreatePodFromTemplate(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
	var req podman.CreatePodRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	writeResult(w, map[string]any{"pod_name": req.Name}, runtime.CreatePodFromTemplate(r.Context(), req))
}

func (s Server) handleDeployComposeStack(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
	var req podman.DeployComposeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	writeResult(w, map[string]any{"project_name": req.ProjectName, "stack_id": req.StackID}, runtime.DeployComposeStack(r.Context(), req))
}

func (s Server) handleBuildImage(w http.ResponseWriter, r *http.Request, runtime podman.PodmanRuntime) {
	var req podman.BuildImageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	writeResult(w, map[string]any{"image_name": req.ImageName}, runtime.BuildImage(r.Context(), req))
}

func writeResult(w http.ResponseWriter, data any, err error) {
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := correlation.FromHeader(r.Header.Get(correlation.HeaderName))
		if id == "" {
			id = correlation.NewID()
		}
		ctx := correlation.WithID(r.Context(), id)
		w.Header().Set(correlation.HeaderName, id)

		if s.Token != "" {
			header := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(header, prefix) {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(header, prefix)
			if subtle.ConstantTimeCompare([]byte(token), []byte(s.Token)) != 1 {
				http.Error(w, "invalid bearer token", http.StatusUnauthorized)
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
