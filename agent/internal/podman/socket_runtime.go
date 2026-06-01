package podman

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/curly-hub/podorel/internal/logging"
)

const (
	DefaultPodmanAPIVersion = "v4.0.0"
	DefaultSocketTimeout    = 20 * time.Second
)

type PodmanSocketRuntime struct {
	SocketPath string
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration
	Logger     *logging.Logger
}

func NewSocketRuntime(socketPath string, logger *logging.Logger) *PodmanSocketRuntime {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &PodmanSocketRuntime{
		SocketPath: socketPath,
		BaseURL:    "http://podman",
		HTTPClient: &http.Client{Transport: transport, Timeout: DefaultSocketTimeout},
		Timeout:    DefaultSocketTimeout,
		Logger:     logger,
	}
}

func NewDefaultRuntime(logger *logging.Logger) PodmanRuntime {
	cli := NewCLIRuntime(logger)
	socketPath := DefaultPodmanSocketPath()
	if socketPath == "" {
		return cli
	}
	if info, err := os.Stat(socketPath); err == nil && !info.IsDir() {
		return FallbackRuntime{Preferred: NewSocketRuntime(socketPath, logger), Fallback: cli}
	}
	return cli
}

func DefaultPodmanSocketPath() string {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	}
	return filepath.Join(runtimeDir, "podman", "podman.sock")
}

func (r *PodmanSocketRuntime) ListPods(ctx context.Context) ([]PodSummary, error) {
	raw, err := r.do(ctx, http.MethodGet, "/libpod/pods/json", nil)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("parse podman socket pods json: %w", err)
	}
	pods := make([]PodSummary, 0, len(rows))
	for _, row := range rows {
		rowRaw, _ := json.Marshal(row)
		pods = append(pods, PodSummary{
			ID:        optionalStringField(row, "Id", "ID", "id"),
			Name:      optionalStringField(row, "Name", "name"),
			State:     optionalStringField(row, "Status", "status", "State", "state"),
			CreatedAt: optionalTimeField(row, "Created", "CreatedAt", "created", "created_at"),
			RawJSON:   string(rowRaw),
		})
	}
	return pods, nil
}

func (r *PodmanSocketRuntime) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	raw, err := r.do(ctx, http.MethodGet, "/libpod/containers/json?all=true", nil)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("parse podman socket containers json: %w", err)
	}
	containers := make([]ContainerSummary, 0, len(rows))
	for _, row := range rows {
		rowRaw, _ := json.Marshal(row)
		containers = append(containers, ContainerSummary{
			ID:        optionalStringField(row, "Id", "ID", "id"),
			PodID:     optionalStringField(row, "Pod", "pod", "PodID", "pod_id"),
			Name:      optionalStringField(row, "Names", "Name", "name"),
			Image:     optionalStringField(row, "Image", "image"),
			State:     optionalStringField(row, "State", "state", "Status", "status"),
			CreatedAt: optionalTimeField(row, "Created", "CreatedAt", "created", "created_at"),
			RawJSON:   string(rowRaw),
		})
	}
	return containers, nil
}

func (r *PodmanSocketRuntime) Stats(ctx context.Context) ([]ContainerStats, error) {
	raw, err := r.do(ctx, http.MethodGet, "/libpod/containers/stats?stream=false", nil)
	if err != nil {
		return nil, err
	}
	return ParseStatsJSON(raw, runtime.NumCPU())
}

func (r *PodmanSocketRuntime) StartPod(ctx context.Context, podID string) error {
	return r.action(ctx, http.MethodPost, "/libpod/pods/"+url.PathEscape(podID)+"/start")
}

func (r *PodmanSocketRuntime) StopPod(ctx context.Context, podID string) error {
	return r.action(ctx, http.MethodPost, "/libpod/pods/"+url.PathEscape(podID)+"/stop")
}

func (r *PodmanSocketRuntime) RestartPod(ctx context.Context, podID string) error {
	return r.action(ctx, http.MethodPost, "/libpod/pods/"+url.PathEscape(podID)+"/restart")
}

func (r *PodmanSocketRuntime) KillPod(ctx context.Context, podID string) error {
	return r.action(ctx, http.MethodPost, "/libpod/pods/"+url.PathEscape(podID)+"/kill")
}

func (r *PodmanSocketRuntime) DeletePod(ctx context.Context, podID string) error {
	return r.action(ctx, http.MethodDelete, "/libpod/pods/"+url.PathEscape(podID)+"?force=true&timeout=1")
}

func (r *PodmanSocketRuntime) StartContainer(ctx context.Context, containerID string) error {
	return r.action(ctx, http.MethodPost, "/libpod/containers/"+url.PathEscape(containerID)+"/start")
}

func (r *PodmanSocketRuntime) StopContainer(ctx context.Context, containerID string) error {
	return r.action(ctx, http.MethodPost, "/libpod/containers/"+url.PathEscape(containerID)+"/stop")
}

func (r *PodmanSocketRuntime) RestartContainer(ctx context.Context, containerID string) error {
	return r.action(ctx, http.MethodPost, "/libpod/containers/"+url.PathEscape(containerID)+"/restart")
}

func (r *PodmanSocketRuntime) KillContainer(ctx context.Context, containerID string) error {
	return r.action(ctx, http.MethodPost, "/libpod/containers/"+url.PathEscape(containerID)+"/kill")
}

func (r *PodmanSocketRuntime) DeleteContainer(ctx context.Context, containerID string) error {
	return r.action(ctx, http.MethodDelete, "/libpod/containers/"+url.PathEscape(containerID)+"?force=true&timeout=1")
}

func (r *PodmanSocketRuntime) Logs(ctx context.Context, req LogRequest) (<-chan LogLine, error) {
	target := req.ContainerID
	if target == "" {
		if req.PodID == "" {
			return nil, fmt.Errorf("socket log streaming requires container id or pod id")
		}
		containers, err := r.ListContainers(ctx)
		if err != nil {
			return nil, err
		}
		ch := make(chan LogLine)
		go func() {
			defer close(ch)
			emitted := 0
			for _, container := range containers {
				if container.PodID != req.PodID {
					continue
				}
				containerReq := req
				containerReq.PodID = ""
				containerReq.ContainerID = container.ID
				containerLines, err := r.Logs(ctx, containerReq)
				if err != nil {
					continue
				}
				for line := range containerLines {
					if line.Source == "" || line.Source == container.ID {
						line.Source = container.Name
						if line.Source == "" {
							line.Source = container.ID
						}
					}
					ch <- line
					emitted++
					if req.LastLines > 0 && emitted >= req.LastLines {
						return
					}
				}
			}
		}()
		return ch, nil
	}
	path := "/libpod/containers/" + url.PathEscape(target) + "/logs?stdout=true&stderr=true"
	if req.LastLines > 0 {
		path += "&tail=" + url.QueryEscape(fmt.Sprintf("%d", req.LastLines))
	}
	raw, err := r.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	ch := make(chan LogLine, 1)
	go func() {
		defer close(ch)
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) != "" {
				ch <- LogLine{Source: target, Line: line}
			}
		}
	}()
	return ch, nil
}

func (r *PodmanSocketRuntime) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	_ = ctx
	_ = req
	return ExecResult{}, fmt.Errorf("socket runtime does not support exec directly")
}

func (r *PodmanSocketRuntime) CreatePodFromTemplate(ctx context.Context, req CreatePodRequest) error {
	_ = ctx
	_ = req
	return fmt.Errorf("socket runtime does not support full template pod creation")
}

func (r *PodmanSocketRuntime) DeployComposeStack(ctx context.Context, req DeployComposeRequest) error {
	_ = ctx
	_ = req
	return fmt.Errorf("socket runtime does not support compose stack deployment")
}

func (r *PodmanSocketRuntime) BuildImage(ctx context.Context, req BuildImageRequest) error {
	if strings.TrimSpace(req.ImageName) == "" {
		return fmt.Errorf("image name is required")
	}
	if strings.TrimSpace(req.Dockerfile) == "" {
		return fmt.Errorf("dockerfile is required")
	}
	var body bytes.Buffer
	writer := tar.NewWriter(&body)
	content := []byte(req.Dockerfile)
	if err := writer.WriteHeader(&tar.Header{
		Name:     "Dockerfile",
		Mode:     0o600,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		return err
	}
	if _, err := writer.Write(content); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	path := "/libpod/build?t=" + url.QueryEscape(req.ImageName) + "&dockerfile=Dockerfile"
	_, err := r.doWithContentType(ctx, http.MethodPost, path, &body, "application/x-tar")
	return err
}

func (r *PodmanSocketRuntime) CreateSecret(ctx context.Context, req CreateSecretRequest) error {
	payload, _ := json.Marshal(map[string]any{"Name": req.Name, "Data": req.Value})
	_, err := r.do(ctx, http.MethodPost, "/libpod/secrets/create", bytes.NewReader(payload))
	return err
}

func (r *PodmanSocketRuntime) action(ctx context.Context, method string, path string) error {
	_, err := r.do(ctx, method, path, nil)
	return err
}

func (r *PodmanSocketRuntime) do(ctx context.Context, method string, path string, body io.Reader) ([]byte, error) {
	contentType := ""
	if body != nil {
		contentType = "application/json"
	}
	return r.doWithContentType(ctx, method, path, body, contentType)
}

func (r *PodmanSocketRuntime) doWithContentType(ctx context.Context, method string, path string, body io.Reader, contentType string) ([]byte, error) {
	client := r.HTTPClient
	if client == nil {
		client = NewSocketRuntime(r.SocketPath, r.Logger).HTTPClient
	}
	baseURL := r.BaseURL
	if baseURL == "" {
		baseURL = "http://podman"
	}
	url := strings.TrimRight(baseURL, "/") + "/" + DefaultPodmanAPIVersion + path
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	duration := time.Since(start)
	if err != nil {
		if r.Logger != nil {
			r.Logger.Error(ctx, "podman_socket", "podman socket request failed", map[string]any{"endpoint": path, "duration_ms": duration.Milliseconds(), "error": err.Error()})
		}
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if r.Logger != nil {
		r.Logger.Debug(ctx, "podman_socket", "podman socket response", map[string]any{"endpoint": path, "status": resp.StatusCode, "duration_ms": duration.Milliseconds(), "response_body_length": len(raw)})
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("podman socket %s %s failed with status %d: %s", method, path, resp.StatusCode, logging.RedactString(string(raw)))
	}
	return raw, nil
}
