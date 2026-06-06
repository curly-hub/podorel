package podman

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/curly-hub/podorel/internal/logging"
)

const (
	DefaultPodmanBinary       = "podman"
	DefaultCommandTimeout     = 20 * time.Second
	maxComposeBundleFileBytes = 2 * 1024 * 1024
)

type PodmanCLIRuntime struct {
	Binary     string
	Timeout    time.Duration
	Logger     *logging.Logger
	CPUTracker CPUTracker
}

type PodmanCommandError struct {
	Command  string
	Args     []string
	ExitCode int
	Stderr   string
}

func (e PodmanCommandError) Error() string {
	return fmt.Sprintf("%s failed with exit code %d: %s", e.Command, e.ExitCode, e.Stderr)
}

func NewCLIRuntime(logger *logging.Logger) *PodmanCLIRuntime {
	return &PodmanCLIRuntime{
		Binary:  DefaultPodmanBinary,
		Timeout: DefaultCommandTimeout,
		Logger:  logger,
	}
}

func (r *PodmanCLIRuntime) ListPods(ctx context.Context) ([]PodSummary, error) {
	out, err := r.run(ctx, "pod", "ps", "--format", "json")
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse podman pod ps json: %w", err)
	}
	pods := make([]PodSummary, 0, len(rows))
	for _, row := range rows {
		health := healthFromPodmanRow(row)
		if health != "" {
			row["Health"] = health
		}
		raw, _ := json.Marshal(row)
		pods = append(pods, PodSummary{
			ID:        optionalStringField(row, "Id", "ID", "id"),
			Name:      optionalStringField(row, "Name", "name"),
			State:     optionalStringField(row, "Status", "status", "State", "state"),
			Health:    health,
			CreatedAt: optionalTimeField(row, "Created", "CreatedAt", "created", "created_at"),
			RawJSON:   string(raw),
		})
	}
	return pods, nil
}

func (r *PodmanCLIRuntime) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	out, err := r.run(ctx, "ps", "--all", "--format", "json")
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse podman ps json: %w", err)
	}
	containers := make([]ContainerSummary, 0, len(rows))
	for _, row := range rows {
		health := healthFromPodmanRow(row)
		if health != "" {
			row["Health"] = health
		}
		raw, _ := json.Marshal(row)
		containers = append(containers, ContainerSummary{
			ID:        optionalStringField(row, "Id", "ID", "id"),
			PodID:     optionalStringField(row, "Pod", "pod", "PodID", "pod_id"),
			Name:      optionalStringField(row, "Names", "Name", "name"),
			Image:     optionalStringField(row, "Image", "image"),
			State:     optionalStringField(row, "State", "state", "Status", "status"),
			Health:    health,
			CreatedAt: optionalTimeField(row, "Created", "CreatedAt", "created", "created_at"),
			RawJSON:   string(raw),
		})
	}
	return containers, nil
}

func (r *PodmanCLIRuntime) Stats(ctx context.Context) ([]ContainerStats, error) {
	out, err := r.run(ctx, "stats", "--no-stream", "--format", "json")
	if err != nil {
		return nil, err
	}
	stats, err := ParseStatsJSON(out, runtime.NumCPU())
	if err != nil {
		return nil, err
	}
	stats = r.CPUTracker.Apply(stats, time.Now())
	if r.Logger != nil {
		r.Logger.Debug(ctx, "podman_stats_parse", "parsed podman stats", map[string]any{
			"raw_payload_length": len(out),
			"container_count":    len(stats),
		})
	}
	return stats, nil
}

func (r *PodmanCLIRuntime) StartPod(ctx context.Context, podID string) error {
	return r.action(ctx, "pod_start", "pod", "start", podID)
}

func (r *PodmanCLIRuntime) StopPod(ctx context.Context, podID string) error {
	return r.action(ctx, "pod_stop", "pod", "stop", podID)
}

func (r *PodmanCLIRuntime) RestartPod(ctx context.Context, podID string) error {
	return r.action(ctx, "pod_restart", "pod", "restart", podID)
}

func (r *PodmanCLIRuntime) KillPod(ctx context.Context, podID string) error {
	return r.action(ctx, "pod_kill", "pod", "kill", podID)
}

func (r *PodmanCLIRuntime) DeletePod(ctx context.Context, podID string) error {
	return r.action(ctx, "pod_delete", "pod", "rm", "-f", "--time", "1", podID)
}

func (r *PodmanCLIRuntime) StartContainer(ctx context.Context, containerID string) error {
	return r.action(ctx, "container_start", "start", containerID)
}

func (r *PodmanCLIRuntime) StopContainer(ctx context.Context, containerID string) error {
	return r.action(ctx, "container_stop", "stop", containerID)
}

func (r *PodmanCLIRuntime) RestartContainer(ctx context.Context, containerID string) error {
	return r.action(ctx, "container_restart", "restart", containerID)
}

func (r *PodmanCLIRuntime) KillContainer(ctx context.Context, containerID string) error {
	return r.action(ctx, "container_kill", "kill", containerID)
}

func (r *PodmanCLIRuntime) DeleteContainer(ctx context.Context, containerID string) error {
	return r.action(ctx, "container_delete", "rm", "-f", "--time", "1", containerID)
}

func (r *PodmanCLIRuntime) Logs(ctx context.Context, req LogRequest) (<-chan LogLine, error) {
	args := []string{"logs", "--timestamps"}
	if req.Follow {
		args = append(args, "--follow")
	}
	if !req.Since.IsZero() {
		args = append(args, "--since", req.Since.Format(time.RFC3339))
	}
	if req.LastLines > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", req.LastLines))
	}
	target := req.ContainerID
	if target == "" {
		target = req.PodID
		args = append(args, "--pod")
	}
	args = append(args, target)

	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	ch := make(chan LogLine)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(bytes.NewReader(out))
		for scanner.Scan() {
			ch <- parsePodmanLogLine(target, scanner.Text())
		}
	}()
	return ch, nil
}

func (r *PodmanCLIRuntime) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	containerID := strings.TrimSpace(req.ContainerID)
	command := strings.TrimSpace(req.Command)
	shell, err := normalizeExecShell(req.Shell)
	if err != nil {
		return ExecResult{}, err
	}
	if containerID == "" {
		return ExecResult{}, fmt.Errorf("container id is required")
	}
	if command == "" {
		return ExecResult{}, fmt.Errorf("command is required")
	}
	timeout := r.timeout()
	if req.TimeoutSeconds > 0 {
		if req.TimeoutSeconds > 120 {
			return ExecResult{}, fmt.Errorf("timeout cannot exceed 120 seconds")
		}
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"exec", containerID, shell, "-lc", command}
	cmd := exec.CommandContext(commandCtx, r.binary(), args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now().UTC()
	err = cmd.Run()
	finished := time.Now().UTC()

	exitCode := 0
	timedOut := commandCtx.Err() == context.DeadlineExceeded
	if err != nil {
		var exitError *exec.ExitError
		if errorAs(err, &exitError) {
			exitCode = exitError.ExitCode()
		} else if timedOut {
			exitCode = -1
			if stderr.Len() > 0 {
				stderr.WriteByte('\n')
			}
			stderr.WriteString("command timed out")
		} else {
			return ExecResult{}, commandError(r.binary(), args, err, stderr.String())
		}
	}
	stdoutText, stdoutTruncated := truncateExecOutput(stdout.String())
	stderrText, stderrTruncated := truncateExecOutput(stderr.String())
	result := ExecResult{
		ContainerID:     containerID,
		Shell:           shell,
		Command:         command,
		ExitCode:        exitCode,
		Stdout:          stdoutText,
		Stderr:          stderrText,
		DurationMillis:  finished.Sub(started).Milliseconds(),
		StartedAt:       started,
		FinishedAt:      finished,
		TimedOut:        timedOut,
		OutputTruncated: stdoutTruncated || stderrTruncated,
	}
	if r.Logger != nil {
		fields := map[string]any{
			"command":          r.binary(),
			"args":             logging.SanitizeArgs([]string{"exec", containerID, shell, "-lc", "<redacted>"}),
			"container_id":     containerID,
			"shell":            shell,
			"command_length":   len(command),
			"exit_code":        exitCode,
			"duration_ms":      result.DurationMillis,
			"stdout_length":    stdout.Len(),
			"stderr_length":    stderr.Len(),
			"timed_out":        timedOut,
			"output_truncated": result.OutputTruncated,
		}
		if err != nil && exitCode != 0 {
			r.Logger.Error(ctx, "podman_exec", "podman exec command returned non-zero", fields)
		} else {
			r.Logger.Debug(ctx, "podman_exec", "podman exec command completed", fields)
		}
	}
	return result, nil
}

func (r *PodmanCLIRuntime) CreatePodFromTemplate(ctx context.Context, req CreatePodRequest) error {
	if len(req.PreviewCommand) == 0 {
		return fmt.Errorf("create pod request requires preview command")
	}
	args := req.PreviewCommand
	if args[0] == r.binary() || args[0] == DefaultPodmanBinary {
		args = args[1:]
	}
	_, err := r.run(ctx, args...)
	return err
}

func (r *PodmanCLIRuntime) DeployComposeStack(ctx context.Context, req DeployComposeRequest) error {
	projectName, err := cleanComposeProjectName(req.ProjectName)
	if err != nil {
		return err
	}
	if len(req.Files) == 0 {
		return fmt.Errorf("compose deployment requires bundle files")
	}
	composeFiles := append([]string(nil), req.ComposeFiles...)
	if len(composeFiles) == 0 {
		composeFiles = []string{"docker-compose.yml"}
	}
	for i, composeFile := range composeFiles {
		clean, err := cleanComposeBundlePath(composeFile)
		if err != nil {
			return err
		}
		composeFiles[i] = clean
	}
	dir, err := composeProjectDir(projectName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, file := range req.Files {
		if err := writeComposeBundleFile(dir, file); err != nil {
			return err
		}
	}
	command, prefix, err := r.composeProvider(ctx)
	if err != nil {
		return err
	}
	args := append([]string(nil), prefix...)
	args = append(args, "-p", projectName)
	for _, composeFile := range composeFiles {
		args = append(args, "-f", composeFile)
	}
	args = append(args, "up", "-d")
	_, err = r.runCommand(ctx, dir, command, args...)
	return err
}

func (r *PodmanCLIRuntime) BuildImage(ctx context.Context, req BuildImageRequest) error {
	if strings.TrimSpace(req.ImageName) == "" {
		return fmt.Errorf("image name is required")
	}
	if strings.TrimSpace(req.Dockerfile) == "" {
		return fmt.Errorf("dockerfile is required")
	}
	dir, err := os.MkdirTemp("", "podorel-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	dockerfilePath := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(req.Dockerfile), 0o600); err != nil {
		return err
	}
	_, err = r.run(ctx, "build", "-t", req.ImageName, "-f", dockerfilePath, dir)
	return err
}

func (r *PodmanCLIRuntime) CreateSecret(ctx context.Context, req CreateSecretRequest) error {
	if strings.TrimSpace(req.Name) == "" || req.Value == "" {
		return fmt.Errorf("secret name and value are required")
	}
	commandCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	cmd := exec.CommandContext(commandCtx, r.binary(), "secret", "create", req.Name, "-")
	cmd.Stdin = strings.NewReader(req.Value)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)
	if r.Logger != nil {
		fields := map[string]any{
			"command":        r.binary(),
			"args":           logging.SanitizeArgs([]string{"secret", "create", req.Name, "-"}),
			"duration_ms":    duration.Milliseconds(),
			"stdout_length":  stdout.Len(),
			"stderr_length":  stderr.Len(),
			"secret_name":    req.Name,
			"secret_value":   req.Value,
			"parser_target":  "podman_secret_create",
			"podman_runtime": "cli",
		}
		if err != nil {
			r.Logger.Error(ctx, "podman_secret_create", "podman secret create failed", fields)
		} else {
			r.Logger.Debug(ctx, "podman_secret_create", "podman secret created", fields)
		}
	}
	if err != nil {
		return commandError(r.binary(), []string{"secret", "create", req.Name, "-"}, err, stderr.String())
	}
	return nil
}

func normalizeExecShell(shell string) (string, error) {
	normalized := strings.TrimSpace(shell)
	if normalized == "" {
		return "sh", nil
	}
	switch normalized {
	case "sh", "/bin/sh", "bash", "/bin/bash":
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported shell %q; expected sh or bash", normalized)
	}
}

func truncateExecOutput(value string) (string, bool) {
	const max = 64 * 1024
	if len(value) <= max {
		return value, false
	}
	return value[:max] + "\n[output truncated]", true
}

func (r *PodmanCLIRuntime) action(ctx context.Context, operation string, args ...string) error {
	_, err := r.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func (r *PodmanCLIRuntime) run(ctx context.Context, args ...string) ([]byte, error) {
	return r.runCommand(ctx, "", r.binary(), args...)
}

func (r *PodmanCLIRuntime) runCommand(ctx context.Context, dir string, command string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()

	cmd := exec.CommandContext(commandCtx, command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	fields := map[string]any{
		"command":       command,
		"args":          logging.SanitizeArgs(args),
		"duration_ms":   duration.Milliseconds(),
		"stdout_length": stdout.Len(),
		"stderr_length": stderr.Len(),
	}
	if dir != "" {
		fields["workdir"] = dir
	}
	if err != nil {
		if r.Logger != nil {
			r.Logger.Error(ctx, "podman_cli", "podman command failed", fields)
		}
		return nil, commandError(command, args, err, stderr.String())
	}
	if r.Logger != nil {
		r.Logger.Debug(ctx, "podman_cli", "podman command completed", fields)
	}
	return stdout.Bytes(), nil
}

func (r *PodmanCLIRuntime) composeProvider(ctx context.Context) (string, []string, error) {
	if _, err := r.run(ctx, "compose", "version"); err == nil {
		return r.binary(), []string{"compose"}, nil
	}
	if _, err := exec.LookPath("podman-compose"); err == nil {
		return "podman-compose", nil, nil
	}
	return "", nil, fmt.Errorf("podman compose provider unavailable; install podman-compose or enable podman compose")
}

func composeProjectDir(projectName string) (string, error) {
	if base := strings.TrimSpace(os.Getenv("PODOREL_AGENT_COMPOSE_DIR")); base != "" {
		return filepath.Join(base, projectName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("could not resolve home directory for compose stack storage")
	}
	return filepath.Join(home, ".local", "share", "podorel", "compose-stacks", projectName), nil
}

func cleanComposeProjectName(value string) (string, error) {
	projectName := strings.TrimSpace(value)
	if projectName == "" {
		return "", fmt.Errorf("compose project name is required")
	}
	if projectName == "." || projectName == ".." || strings.Contains(projectName, "/") {
		return "", fmt.Errorf("unsafe compose project name %q", value)
	}
	if len(projectName) > 100 {
		return "", fmt.Errorf("compose project name is too long")
	}
	return projectName, nil
}

func writeComposeBundleFile(dir string, file ComposeBundleFile) error {
	clean, err := cleanComposeBundlePath(file.Path)
	if err != nil {
		return err
	}
	if len(file.Content) > maxComposeBundleFileBytes {
		return fmt.Errorf("compose bundle file %s is too large", clean)
	}
	target := filepath.Join(dir, filepath.FromSlash(clean))
	rel, err := filepath.Rel(dir, target)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("unsafe compose bundle path %q", file.Path)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(file.Content), 0o600)
}

func cleanComposeBundlePath(value string) (string, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if trimmed == "" {
		return "", fmt.Errorf("compose bundle path is required")
	}
	clean := path.Clean(trimmed)
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("unsafe compose bundle path %q", value)
	}
	return clean, nil
}

func (r *PodmanCLIRuntime) binary() string {
	if r.Binary == "" {
		return DefaultPodmanBinary
	}
	return r.Binary
}

func (r *PodmanCLIRuntime) timeout() time.Duration {
	if r.Timeout <= 0 {
		return DefaultCommandTimeout
	}
	return r.Timeout
}

func commandError(command string, args []string, err error, stderr string) error {
	exitCode := -1
	var exitError *exec.ExitError
	if ok := errorAs(err, &exitError); ok {
		exitCode = exitError.ExitCode()
	}
	return PodmanCommandError{
		Command:  command,
		Args:     logging.SanitizeArgs(args),
		ExitCode: exitCode,
		Stderr:   logging.RedactString(stderr),
	}
}

func errorAs(err error, target any) bool {
	switch typed := target.(type) {
	case **exec.ExitError:
		exitError, ok := err.(*exec.ExitError)
		if ok {
			*typed = exitError
		}
		return ok
	default:
		return false
	}
}
