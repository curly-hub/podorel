package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/curly-hub/podorel/internal/logging"
	podorelruntime "github.com/curly-hub/podorel/internal/runtime"
	ws "github.com/curly-hub/podorel/internal/websocket"
	"github.com/curly-hub/podorel/server/internal/agents"
	"github.com/curly-hub/podorel/server/internal/api"
	"github.com/curly-hub/podorel/server/internal/app"
	"github.com/curly-hub/podorel/server/internal/config"
	"github.com/curly-hub/podorel/server/internal/db"
)

const realPodmanE2EEnv = "PODOREL_RUN_REAL_PODMAN_E2E"

type e2eHarness struct {
	server *httptest.Server
	client *http.Client
	csrf   string
	store  *db.Store
}

func TestE2EUserJourneysWithFakeAgent(t *testing.T) {
	fake := newFakeAgent()
	h := newHarness(t, app.Options{
		AllowSnapshotFallback: true,
		AgentClientFactory: func(ctx context.Context, agent db.Agent) (app.AgentClient, bool) {
			return fake, true
		},
	})
	h.login(t)

	status := h.get(t, "/api/system/status")
	assertContains(t, status, "primary_agent_health")
	assertContains(t, status, "podman_cli_available")

	pods := h.get(t, "/api/pods")
	assertContains(t, pods, "e2e-existing")
	assertContains(t, pods, "cpu_podman_raw")
	assertContains(t, pods, "snapshot_source")

	logs := h.get(t, "/api/logs/history?agent_id=primary&pod_id=e2e-existing&container_id=e2e-existing-main&limit=10")
	assertContains(t, logs, "hello-from-fake-agent")

	created := h.post(t, "/api/pods/create-from-template", map[string]any{
		"template_id": "alpine-nodejs",
		"pod_name":    "e2e-created",
		"values":      map[string]string{"host_port": "31080"},
		"confirm":     true,
	})
	assertContains(t, created, "e2e-created")
	pods = h.get(t, "/api/pods")
	assertContains(t, pods, "e2e-created")
	if len(fake.createdPreview) == 0 || !containsArg(fake.createdPreview, "31080:3000/tcp") {
		t.Fatalf("template port value was not applied to preview: %#v", fake.createdPreview)
	}

	secret := h.post(t, "/api/secrets", map[string]any{"name": "e2e-secret", "value": "super-secret-value", "password": "secret-password"})
	assertContains(t, secret, "e2e-secret")
	assertNotContains(t, secret, "super-secret-value")

	buildPreview := h.post(t, "/api/images/build-from-dockerfile", map[string]any{"image_name": "e2e:latest", "dockerfile": "FROM alpine:3.20\nENV API_TOKEN=bad"})
	assertContains(t, buildPreview, "secret_warnings")
	build := h.post(t, "/api/images/build-from-dockerfile", map[string]any{"image_name": "e2e:latest", "dockerfile": "FROM alpine:3.20", "confirm": true, "password": "secret-password"})
	assertContains(t, build, "build_id")
	buildID := fieldFromEnvelope(t, []byte(build), "build_id")
	waitFor(t, 2*time.Second, func() bool { return len(fake.builds) == 1 })
	payload := h.websocket(t, "/api/ws/builds?build_id="+url.QueryEscape(buildID))
	assertContains(t, payload, "Podman build")

	settings := h.put(t, "/api/settings", map[string]any{"password": "secret-password", "actions": map[string]bool{"exec_enabled": true, "automation_enabled": false}})
	assertContains(t, settings, "effective_settings")
	assertContains(t, settings, "exec_enabled")

	scan := h.post(t, "/api/security/scan", map[string]any{})
	assertContains(t, scan, "scan-")
	findings := h.get(t, "/api/security/findings")
	assertContains(t, findings, "CVE-0000-0001")
}

func TestRealPodmanE2ECreateObserveLogsAndStats(t *testing.T) {
	if os.Getenv(realPodmanE2EEnv) != "1" {
		t.Skip(realPodmanE2EEnv + "=1 is required for real Podman E2E")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	if err := exec.Command("podman", "image", "exists", "docker.io/library/alpine:3.20").Run(); err != nil {
		t.Skip("docker.io/library/alpine:3.20 is not available locally")
	}

	podName := "podorel-test-e2e-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	cleanupPodmanPod(t, podName)
	t.Cleanup(func() { cleanupPodmanPod(t, podName) })

	tokenFile := filepath.Join(t.TempDir(), "agent-token")
	if err := os.WriteFile(tokenFile, []byte("real-e2e-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agentBinary := filepath.Join(t.TempDir(), "podorel-agent")
	buildAgent := exec.Command("go", "build", "-o", agentBinary, "../../agent/cmd/podorel-agent")
	buildAgent.Stdout = io.Discard
	buildAgent.Stderr = io.Discard
	if err := buildAgent.Run(); err != nil {
		t.Fatalf("build test agent: %v", err)
	}
	agent := exec.CommandContext(ctx, agentBinary, "--development", "--socket-path", socketPath, "--token-file", tokenFile)
	agent.Stdout = io.Discard
	agent.Stderr = io.Discard
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = agent.Wait()
	})
	waitForAgentSocket(t, socketPath, "real-e2e-token")

	t.Setenv("PODOREL_AGENT_SOCKET", socketPath)
	t.Setenv("PODOREL_AGENT_TOKEN_FILE", tokenFile)
	templatesDir := writeRealE2ETemplate(t)
	h := newHarness(t, app.Options{TemplatesDir: templatesDir})
	h.login(t)

	created := h.post(t, "/api/pods/create-from-template", map[string]any{"template_id": "real-alpine-e2e", "pod_name": podName, "confirm": true})
	assertContains(t, created, podName)

	var pods string
	waitFor(t, 30*time.Second, func() bool {
		pods = h.get(t, "/api/pods")
		return strings.Contains(pods, podName) && strings.Contains(pods, "cpu_podman_raw")
	})
	assertContains(t, pods, podName)
	assertContains(t, pods, "\"snapshot_source\":\"live\"")

	podID := podIDForName(t, pods, podName)
	containerID := containerIDForPod(t, pods, podName)
	logsPath := "/api/logs/history?pod_id=" + url.QueryEscape(podName) + "&container_id=" + url.QueryEscape(containerID) + "&limit=20"
	waitFor(t, 20*time.Second, func() bool {
		return strings.Contains(h.get(t, logsPath), "podorel-real-e2e-log")
	})
	stats := h.get(t, "/api/stats/current")
	assertContains(t, stats, containerID)

	deleted := h.delete(t, "/api/pods/"+url.PathEscape(podID), map[string]any{"confirm_name": podName})
	assertContains(t, deleted, "deleted")
}

func newHarness(t *testing.T, opts app.Options) e2eHarness {
	t.Helper()
	store, err := db.OpenMemory(context.Background(), filepath.Join("..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg, err := config.Load([]string{"--development"}, func(key string) string {
		switch key {
		case "PODOREL_ADMIN_PASSWORD":
			return "secret-password"
		case "PODOREL_AGENT_SOCKET":
			return os.Getenv(key)
		case "PODOREL_AGENT_TOKEN_FILE":
			return os.Getenv(key)
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.AllowSnapshotFallback {
		cfg.Security.Scanner = "podorel-e2e-missing-scanner"
	}
	if opts.TemplatesDir == "" {
		opts.TemplatesDir = filepath.Join("..", "templates", "pods")
	}
	if opts.LogDir == "" {
		opts.LogDir = t.TempDir()
	}
	application, err := app.New(context.Background(), cfg, store, logging.New(ioDiscard{}, podorelruntime.Development, "web"), opts)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler())
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return e2eHarness{server: server, client: &http.Client{Jar: jar}, store: store}
}

func (h *e2eHarness) login(t *testing.T) {
	body := h.postNoCSRF(t, "/api/auth/login", map[string]string{"username": "admin", "password": "secret-password"})
	h.csrf = fieldFromEnvelope(t, []byte(body), "csrf_token")
	if h.csrf == "" {
		t.Fatalf("missing csrf token in login: %s", body)
	}
}

func (h e2eHarness) get(t *testing.T, path string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return h.do(t, req)
}

func (h e2eHarness) post(t *testing.T, path string, payload any) string {
	t.Helper()
	return h.mutate(t, http.MethodPost, path, payload, true)
}

func (h e2eHarness) postNoCSRF(t *testing.T, path string, payload any) string {
	t.Helper()
	return h.mutate(t, http.MethodPost, path, payload, false)
}

func (h e2eHarness) put(t *testing.T, path string, payload any) string {
	t.Helper()
	return h.mutate(t, http.MethodPut, path, payload, true)
}

func (h e2eHarness) delete(t *testing.T, path string, payload any) string {
	t.Helper()
	return h.mutate(t, http.MethodDelete, path, payload, true)
}

func (h e2eHarness) mutate(t *testing.T, method string, path string, payload any, csrf bool) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if csrf {
		req.Header.Set("X-CSRF-Token", h.csrf)
	}
	return h.do(t, req)
}

func (h e2eHarness) do(t *testing.T, req *http.Request) string {
	t.Helper()
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("%s %s status=%d body=%s", req.Method, req.URL.Path, resp.StatusCode, string(raw))
	}
	return string(raw)
}

func (h e2eHarness) websocket(t *testing.T, path string) string {
	t.Helper()
	key := randomWebSocketKey(t)
	req, err := http.NewRequest(http.MethodGet, h.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", key)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("websocket status=%d body=%s", resp.StatusCode, string(raw))
	}
	return readWebSocketPayload(t, resp.Body)
}

type fakeAgent struct {
	pods           []agents.PodSummary
	containers     []agents.ContainerSummary
	stats          []agents.ContainerStats
	logs           []agents.LogLine
	builds         []string
	secrets        []string
	createdPreview []string
}

func newFakeAgent() *fakeAgent {
	created := time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC)
	return &fakeAgent{
		pods:       []agents.PodSummary{{ID: "e2e-existing", Name: "e2e-existing", State: "running", CreatedAt: created}},
		containers: []agents.ContainerSummary{{ID: "e2e-existing-main", PodID: "e2e-existing", Name: "e2e-existing-main", Image: "alpine:3.20", State: "running", CreatedAt: created}},
		stats:      []agents.ContainerStats{{ContainerID: "e2e-existing-main", PodID: "e2e-existing", Name: "e2e-existing-main", CPUPodmanRaw: "1.00%", CPUPercentHostTotal: 0.08, MemoryPodmanRaw: "1MiB / 1GiB", MemoryBytes: 1024 * 1024}},
		logs:       []agents.LogLine{{Timestamp: created, Source: "e2e-existing-main", Line: "hello-from-fake-agent"}},
	}
}

func (f *fakeAgent) Health(ctx context.Context) (agents.Health, error) {
	return agents.Health{Status: "ok", Mode: "development", User: "e2e", PodmanCLIAvailable: true, LastSeenAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}
func (f *fakeAgent) ListPods(ctx context.Context) ([]agents.PodSummary, error) {
	return append([]agents.PodSummary(nil), f.pods...), nil
}
func (f *fakeAgent) ListContainers(ctx context.Context) ([]agents.ContainerSummary, error) {
	return append([]agents.ContainerSummary(nil), f.containers...), nil
}
func (f *fakeAgent) Stats(ctx context.Context) ([]agents.ContainerStats, error) {
	return append([]agents.ContainerStats(nil), f.stats...), nil
}
func (f *fakeAgent) PodAction(ctx context.Context, podID string, action string) error { return nil }
func (f *fakeAgent) ContainerAction(ctx context.Context, containerID string, action string) error {
	return nil
}
func (f *fakeAgent) Logs(ctx context.Context, podID string, containerID string, last int) ([]agents.LogLine, error) {
	return append([]agents.LogLine(nil), f.logs...), nil
}
func (f *fakeAgent) Exec(ctx context.Context, req agents.ExecRequest) (agents.ExecResult, error) {
	return agents.ExecResult{ContainerID: req.ContainerID, Shell: req.Shell, Command: req.Command, ExitCode: 0, Stdout: "fake exec output"}, nil
}
func (f *fakeAgent) ExecWebSocket(ctx context.Context, containerID string, shell string, cols int, rows int) (*ws.Conn, error) {
	return nil, errors.New("fake exec websocket is not connected")
}
func (f *fakeAgent) CreatePodFromTemplate(ctx context.Context, req agents.CreatePodRequest) error {
	f.createdPreview = append([]string(nil), req.PreviewCommand...)
	created := time.Now().UTC()
	f.pods = append(f.pods, agents.PodSummary{ID: req.Name, Name: req.Name, State: "running", CreatedAt: created})
	f.containers = append(f.containers, agents.ContainerSummary{ID: req.Name + "-main", PodID: req.Name, Name: req.Name + "-main", Image: "node:22-alpine", State: "running", CreatedAt: created})
	f.stats = append(f.stats, agents.ContainerStats{ContainerID: req.Name + "-main", PodID: req.Name, Name: req.Name + "-main", CPUPodmanRaw: "0.50%", CPUPercentHostTotal: 0.04, MemoryPodmanRaw: "2MiB / 1GiB", MemoryBytes: 2 * 1024 * 1024})
	return nil
}
func (f *fakeAgent) DeployComposeStack(ctx context.Context, req agents.DeployComposeRequest) error {
	return nil
}
func (f *fakeAgent) BuildImage(ctx context.Context, req agents.BuildImageRequest) error {
	f.builds = append(f.builds, req.ImageName)
	return nil
}
func (f *fakeAgent) CreateSecret(ctx context.Context, req agents.CreateSecretRequest) error {
	f.secrets = append(f.secrets, req.Name)
	return nil
}

func writeRealE2ETemplate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	template := `{
  "id": "real-alpine-e2e",
  "version": "1.0.0",
  "name": "Real Alpine E2E",
  "description": "Real Podman E2E template",
  "image": "docker.io/library/alpine:3.20",
  "command": ["sh", "-c", "while true; do echo podorel-real-e2e-log; sleep 1; done"],
  "ports": [],
  "volumes": [],
  "environment": {},
  "secrets": [],
  "health_command": [],
  "resource_limits": {"cpu": "", "memory": ""},
  "restart_policy": "on-failure",
  "labels": {"io.podorel.test": "real-e2e"},
  "ui_notes": []
}`
	if err := os.WriteFile(filepath.Join(dir, "real-alpine-e2e.json"), []byte(template), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func waitForAgentSocket(t *testing.T, socketPath string, token string) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}}
	waitFor(t, 20*time.Second, func() bool {
		req, err := http.NewRequest(http.MethodGet, "http://podorel-agent/health", nil)
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}

func podIDForName(t *testing.T, podsBody string, podName string) string {
	t.Helper()
	var envelope api.Envelope
	if err := json.Unmarshal([]byte(podsBody), &envelope); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(envelope.Data)
	var pods []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &pods); err != nil {
		t.Fatal(err)
	}
	for _, pod := range pods {
		if pod.Name == podName {
			return pod.ID
		}
	}
	t.Fatalf("pod id for %s not found in %s", podName, podsBody)
	return ""
}

func containerIDForPod(t *testing.T, podsBody string, podName string) string {
	t.Helper()
	var envelope api.Envelope
	if err := json.Unmarshal([]byte(podsBody), &envelope); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(envelope.Data)
	var pods []struct {
		Name       string `json:"name"`
		Containers []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(raw, &pods); err != nil {
		t.Fatal(err)
	}
	for _, pod := range pods {
		if pod.Name == podName && len(pod.Containers) > 0 {
			for _, container := range pod.Containers {
				if container.Name == podName+"-main" {
					return container.ID
				}
			}
			for _, container := range pod.Containers {
				if !strings.Contains(strings.ToLower(container.Name), "infra") {
					return container.ID
				}
			}
			return pod.Containers[0].ID
		}
	}
	t.Fatalf("container id for pod %s not found in %s", podName, podsBody)
	return ""
}

func cleanupPodmanPod(t *testing.T, podName string) {
	t.Helper()
	_ = exec.Command("podman", "pod", "rm", "-f", podName).Run()
}

func fieldFromEnvelope(t *testing.T, body []byte, key string) string {
	t.Helper()
	var envelope api.Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	value, _ := payload[key].(string)
	return value
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("condition did not pass before timeout")
}

func assertContains(t *testing.T, haystack string, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("response did not contain %q: %s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack string, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("response unexpectedly contained %q: %s", needle, haystack)
	}
}

func containsArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func randomWebSocketKey(t *testing.T) string {
	t.Helper()
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw[:])
}

func readWebSocketPayload(t *testing.T, reader io.Reader) string {
	t.Helper()
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		t.Fatal(err)
	}
	length := int(header[1] & 0x7f)
	if length == 126 {
		extended := make([]byte, 2)
		if _, err := io.ReadFull(reader, extended); err != nil {
			t.Fatal(err)
		}
		length = int(extended[0])<<8 | int(extended[1])
	} else if length == 127 {
		t.Fatal("test websocket reader does not support 64-bit frame lengths")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
