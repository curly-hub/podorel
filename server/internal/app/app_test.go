package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	"github.com/curly-hub/podorel/server/internal/config"
	"github.com/curly-hub/podorel/server/internal/db"
)

type testHarness struct {
	app     *App
	handler http.Handler
	store   *db.Store
}

type loginResult struct {
	Cookie    *http.Cookie
	CSRFToken string
}

func newTestHarness(t *testing.T) testHarness {
	t.Helper()
	return newTestHarnessWithOptions(t, Options{AllowSnapshotFallback: true})
}

func newTestHarnessWithAgentClient(t *testing.T, client AgentClient) testHarness {
	t.Helper()
	return newTestHarnessWithOptions(t, Options{
		AllowSnapshotFallback: true,
		AgentClientFactory: func(ctx context.Context, agent db.Agent) (AgentClient, bool) {
			return client, true
		},
	})
}

func newTestHarnessWithOptions(t *testing.T, opts Options) testHarness {
	t.Helper()
	store, err := db.OpenMemory(context.Background(), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg, err := config.Load([]string{"--development"}, func(key string) string {
		if key == "PODOREL_ADMIN_PASSWORD" {
			return "secret-password"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.TemplatesDir == "" {
		opts.TemplatesDir = filepath.Join("..", "..", "templates", "pods")
	}
	if opts.LogDir == "" {
		opts.LogDir = t.TempDir()
	}
	application, err := New(context.Background(), cfg, store, logging.New(ioDiscard{}, podorelruntime.Development, "web"), opts)
	if err != nil {
		t.Fatal(err)
	}
	return testHarness{app: application, handler: application.Handler(), store: store}
}

func TestPasswordLoginSessionCSRFAndMe(t *testing.T) {
	harness := newTestHarness(t)
	login := harness.login(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(login.Cookie)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d body=%s", rec.Code, rec.Body.String())
	}
	refreshedCSRF := stringFieldFromEnvelope(t, rec.Body.Bytes(), "csrf_token")
	if refreshedCSRF == "" || refreshedCSRF == login.CSRFToken {
		t.Fatalf("me did not rotate csrf token: %q", refreshedCSRF)
	}

	req = jsonRequest(http.MethodPost, "/api/pods/podorel-self-pod/start", `{"confirm":true}`)
	req.AddCookie(login.Cookie)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing csrf status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/pods/podorel-self-pod/start", `{"confirm":true}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, refreshedCSRF)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pod start status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentTokenLoginIsScoped(t *testing.T) {
	harness := newTestHarness(t)
	admin := harness.login(t)

	req := jsonRequest(http.MethodPost, "/api/agents/primary/rotate-token", `{}`)
	req.AddCookie(admin.Cookie)
	req.Header.Set(csrfHeaderName, admin.CSRFToken)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate token status = %d body=%s", rec.Code, rec.Body.String())
	}
	token := stringFieldFromEnvelope(t, rec.Body.Bytes(), "token")
	if token == "" {
		t.Fatal("missing one-time token")
	}

	req = jsonRequest(http.MethodPost, "/api/auth/login-agent-token", `{"token":"`+token+`"}`)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("agent token login status = %d body=%s", rec.Code, rec.Body.String())
	}
	agentCookie := rec.Result().Cookies()[0]
	agentCSRF := stringFieldFromEnvelope(t, rec.Body.Bytes(), "csrf_token")

	req = jsonRequest(http.MethodPut, "/api/settings", `{"logs":{"retention":"1d"}}`)
	req.AddCookie(agentCookie)
	req.Header.Set(csrfHeaderName, agentCSRF)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("agent token session changed settings, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDangerousPodActionRequiresNameConfirmation(t *testing.T) {
	harness := newTestHarness(t)
	login := harness.login(t)

	req := jsonRequest(http.MethodPost, "/api/pods/podorel-self-pod/kill", `{"confirm":true}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, login.CSRFToken)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("kill without typed name status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/pods/podorel-self-pod/kill", `{"confirm_name":"podorel-web"}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, login.CSRFToken)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("kill with typed name status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTemplatesSecuritySecretsAndAudit(t *testing.T) {
	harness := newTestHarness(t)
	login := harness.login(t)

	req := httptest.NewRequest(http.MethodGet, "/api/templates", nil)
	req.AddCookie(login.Cookie)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "alpine-nodejs") {
		t.Fatalf("templates response = %d %s", rec.Code, rec.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/security/scan", `{}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, login.CSRFToken)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("security scan status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/secrets", `{"name":"db-password","value":"raw-secret-value","password":"secret-password"}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, login.CSRFToken)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("secret status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "raw-secret-value") {
		t.Fatalf("raw secret leaked in response: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	req.AddCookie(login.Cookie)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "security.scan") || !strings.Contains(rec.Body.String(), "secrets.create") {
		t.Fatalf("audit response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestSecurityScanUnavailableIsExplicitSetupState(t *testing.T) {
	harness := newTestHarnessWithOptions(t, Options{})
	harness.app.cfg.Security.Scanner = "podorel-test-missing-scanner"
	login := harness.login(t)

	req := jsonRequest(http.MethodPost, "/api/security/scan", `{}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, login.CSRFToken)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("security scan status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status":"unavailable"`) || !strings.Contains(body, "SCANNER_UNAVAILABLE") || !strings.Contains(body, "scanner_unavailable") {
		t.Fatalf("scanner unavailable was not explicit: %s", body)
	}
	if strings.Contains(body, `"status":"failed"`) {
		t.Fatalf("scanner unavailable should not be stored as a failed scan: %s", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/security/scanner-options", nil)
	req.AddCookie(login.Cookie)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "official-install-script") || !strings.Contains(rec.Body.String(), "scanner_available") {
		t.Fatalf("scanner options response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestImageDigestChecksUseAgent(t *testing.T) {
	fake := &fakeAgentClient{}
	harness := newTestHarnessWithAgentClient(t, fake)
	if err := harness.store.InsertImageDigest(context.Background(), db.ImageDigest{
		AgentID:      db.PrimaryAgentID,
		ImageName:    "old-image:latest",
		ErrorMessage: "podman CLI unavailable for local digest check",
	}); err != nil {
		t.Fatal(err)
	}

	harness.app.recordImageDigestChecks(context.Background(), db.PrimaryAgentID, []string{"alpine:3.20"})

	if len(fake.digestRequests) != 1 || fake.digestRequests[0] != "alpine:3.20" {
		t.Fatalf("digest checks did not use agent: %#v", fake.digestRequests)
	}
	digests, err := harness.store.ListImageDigests(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(digests) != 1 {
		t.Fatalf("digests = %d, want 1", len(digests))
	}
	if digests[0].ErrorMessage == "podman CLI unavailable for local digest check" {
		t.Fatalf("web-container podman error leaked into digest result: %#v", digests[0])
	}
	if digests[0].LocalDigest != "sha256:local" {
		t.Fatalf("local digest = %q", digests[0].LocalDigest)
	}
}

func TestCreateFromTemplateAndDockerfilePreview(t *testing.T) {
	harness := newTestHarness(t)
	login := harness.login(t)

	req := jsonRequest(http.MethodPost, "/api/pods/create-from-template", `{"template_id":"alpine-nodejs","pod_name":"node-demo","values":{"host_port":"31080"}}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, login.CSRFToken)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "preview_command") || !strings.Contains(rec.Body.String(), "--detach") || !strings.Contains(rec.Body.String(), "31080:3000/tcp") || !strings.Contains(rec.Body.String(), "setInterval") {
		t.Fatalf("template preview = %d %s", rec.Code, rec.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/pods/create-from-template", `{"template_id":"alpine-nodejs","pod_name":"node-demo","values":{"host_port":"not-a-port"}}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, login.CSRFToken)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "TEMPLATE_VALUES_INVALID") {
		t.Fatalf("invalid template port = %d %s", rec.Code, rec.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/pods/create-from-template", `{"template_id":"alpine-nodejs","pod_name":"node-demo","confirm":true}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, login.CSRFToken)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("template create = %d %s", rec.Code, rec.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/images/build-from-dockerfile", `{"image_name":"demo:latest","dockerfile":"FROM alpine:3.20\nENV API_TOKEN=bad"}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, login.CSRFToken)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "secret_warnings") || !strings.Contains(rec.Body.String(), "alpine:3.20") {
		t.Fatalf("dockerfile preview = %d %s", rec.Code, rec.Body.String())
	}
}

func (h testHarness) login(t *testing.T) loginResult {
	t.Helper()
	req := jsonRequest(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"secret-password"}`)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set cookie")
	}
	if !cookies[0].HttpOnly {
		t.Fatal("session cookie is not HttpOnly")
	}
	if cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want Lax", cookies[0].SameSite)
	}
	return loginResult{Cookie: cookies[0], CSRFToken: stringFieldFromEnvelope(t, rec.Body.Bytes(), "csrf_token")}
}

func TestBruteForceThrottling(t *testing.T) {
	harness := newTestHarness(t)
	for i := 0; i < config.DefaultFailedLoginLimit; i++ {
		req := jsonRequest(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"wrong"}`)
		rec := httptest.NewRecorder()
		harness.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d", i, rec.Code)
		}
	}
	req := jsonRequest(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"secret-password"}`)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("throttled valid password status = %d", rec.Code)
	}
}

func TestAgentTokenSessionCannotAccessAnotherAgent(t *testing.T) {
	harness := newTestHarness(t)
	admin := harness.login(t)

	req := jsonRequest(http.MethodPost, "/api/agents/register", `{"id":"secondary","linux_username":"alice","linux_uid":1001,"socket_path":"/run/user/1001/podorel.sock"}`)
	req.AddCookie(admin.Cookie)
	req.Header.Set(csrfHeaderName, admin.CSRFToken)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register secondary = %d %s", rec.Code, rec.Body.String())
	}
	token := stringFieldFromEnvelope(t, rec.Body.Bytes(), "token")

	req = jsonRequest(http.MethodPost, "/api/auth/login-agent-token", `{"token":"`+token+`"}`)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("agent login = %d %s", rec.Code, rec.Body.String())
	}
	agentCookie := rec.Result().Cookies()[0]

	req = httptest.NewRequest(http.MethodGet, "/api/pods/podorel-self-pod", nil)
	req.AddCookie(agentCookie)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("secondary agent accessed primary pod status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPodListHidesSeededSelfPlaceholder(t *testing.T) {
	harness := newTestHarness(t)
	login := harness.login(t)

	req := httptest.NewRequest(http.MethodGet, "/api/pods", nil)
	req.AddCookie(login.Cookie)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pods status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "podorel-web") || strings.Contains(rec.Body.String(), "podorel-self-pod") {
		t.Fatalf("seeded self placeholder leaked into pod list: %s", rec.Body.String())
	}
}

func TestProductionDiagnosticsHideRawTraces(t *testing.T) {
	store, err := db.OpenMemory(context.Background(), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg, err := config.Load([]string{"--production"}, func(key string) string {
		if key == "PODOREL_ADMIN_PASSWORD" {
			return "secret-password"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	application, err := New(context.Background(), cfg, store, logging.New(ioDiscard{}, podorelruntime.Production, "web"), Options{TemplatesDir: filepath.Join("..", "..", "templates", "pods"), LogDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	handler := application.Handler()
	req := jsonRequest(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"secret-password"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	cookie := rec.Result().Cookies()[0]
	req = httptest.NewRequest(http.MethodGet, "/api/diagnostics/traces", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"redacted":true`) {
		t.Fatalf("production traces = %d %s", rec.Code, rec.Body.String())
	}
}

func TestLogsWebSocketHandshake(t *testing.T) {
	harness := newTestHarness(t)
	login := harness.login(t)
	server := httptest.NewServer(harness.handler)
	defer server.Close()

	key := randomWebSocketKey(t)
	req, err := http.NewRequest(http.MethodGet, "http"+strings.TrimPrefix(server.URL, "http")+"/api/ws/logs", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(login.Cookie)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", key)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("ws status = %d body=%s", resp.StatusCode, string(body))
	}
}

func TestLogsHistoryDownloadFromAgentIsPlainText(t *testing.T) {
	fake := &fakeAgentClient{logLines: []agents.LogLine{{
		Timestamp: time.Date(2026, 5, 30, 7, 30, 0, 0, time.UTC),
		Source:    "container-a",
		Line:      "hello from agent",
	}}}
	harness := newTestHarnessWithAgentClient(t, fake)
	login := harness.login(t)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/history?agent_id=primary&pod_id=pod-a&container_id=container-a&download=true&limit=5", nil)
	req.AddCookie(login.Cookie)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("agent log download status = %d body=%s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("content type = %q, want text/plain", contentType)
	}
	body := rec.Body.String()
	if strings.Contains(body, `"ok"`) || !strings.Contains(body, "hello from agent") {
		t.Fatalf("agent download should be plain text, got %s", body)
	}
}

func TestLogsHistoryForPodReadsContainerLogs(t *testing.T) {
	fake := &fakeAgentClient{}
	harness := newTestHarnessWithAgentClient(t, fake)
	if err := harness.store.InsertPod(context.Background(), db.Pod{
		ID:          "pod-a",
		AgentID:     db.PrimaryAgentID,
		PodmanPodID: "pod-a",
		Name:        "pod-a",
		State:       "running",
		Health:      "unknown",
	}); err != nil {
		t.Fatal(err)
	}
	if err := harness.store.InsertContainer(context.Background(), db.Container{
		ID:                "container-a",
		AgentID:           db.PrimaryAgentID,
		PodID:             "pod-a",
		PodmanContainerID: "container-a",
		Name:              "container-a",
		Image:             "example:latest",
		State:             "running",
		Health:            "unknown",
	}); err != nil {
		t.Fatal(err)
	}
	login := harness.login(t)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/history?agent_id=primary&pod_id=pod-a&limit=5", nil)
	req.AddCookie(login.Cookie)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pod logs status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hello") || len(fake.logRequests) == 0 || fake.logRequests[0] != ":container-a:5" {
		t.Fatalf("pod logs did not read container logs: requests=%#v body=%s", fake.logRequests, rec.Body.String())
	}
}

func TestLogsWebSocketUsesRequestedAgentTarget(t *testing.T) {
	fake := &fakeAgentClient{logLines: []agents.LogLine{{
		Timestamp: time.Date(2026, 5, 30, 7, 31, 0, 0, time.UTC),
		Source:    "container-a",
		Line:      "from websocket target",
	}}}
	harness := newTestHarnessWithAgentClient(t, fake)
	login := harness.login(t)
	server := httptest.NewServer(harness.handler)
	defer server.Close()

	key := randomWebSocketKey(t)
	req, err := http.NewRequest(http.MethodGet, "http"+strings.TrimPrefix(server.URL, "http")+"/api/ws/logs?agent_id=primary&pod_id=pod-a&container_id=container-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(login.Cookie)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", key)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("ws status = %d body=%s", resp.StatusCode, string(body))
	}
	payload := readWebSocketPayload(t, resp.Body)
	if !strings.Contains(payload, "from websocket target") || !strings.Contains(payload, "container-a") {
		t.Fatalf("websocket payload did not include agent log line: %s", payload)
	}
	if len(fake.logRequests) == 0 || fake.logRequests[0] != "pod-a:container-a:100" {
		t.Fatalf("websocket did not request the selected target: %#v", fake.logRequests)
	}
}

func TestLogsHistoryEnforcesStorageLimit(t *testing.T) {
	harness := newTestHarness(t)
	login := harness.login(t)
	harness.app.cfg.Logs.TotalLimitMB = 1

	oldPath := filepath.Join(harness.app.logDir, "old.log")
	newPath := filepath.Join(harness.app.logDir, "new.log")
	writeLargeLog(t, oldPath, 600*1024)
	writeLargeLog(t, newPath, 600*1024)
	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/logs/history?limit=1", nil)
	req.AddCookie(login.Cookie)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logs status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old log should be pruned, stat err=%v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new log should remain: %v", err)
	}
}

func TestPodActionProxiesToAgentWhenAvailable(t *testing.T) {
	store, err := db.OpenMemory(context.Background(), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg, err := config.Load([]string{"--development"}, func(key string) string {
		if key == "PODOREL_ADMIN_PASSWORD" {
			return "secret-password"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeAgentClient{}
	application, err := New(context.Background(), cfg, store, logging.New(ioDiscard{}, podorelruntime.Development, "web"), Options{
		TemplatesDir: filepath.Join("..", "..", "templates", "pods"),
		LogDir:       t.TempDir(),
		AgentClientFactory: func(ctx context.Context, agent db.Agent) (AgentClient, bool) {
			return fake, true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := application.Handler()
	req := jsonRequest(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"secret-password"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	cookie := rec.Result().Cookies()[0]
	csrf := stringFieldFromEnvelope(t, rec.Body.Bytes(), "csrf_token")

	req = jsonRequest(http.MethodPost, "/api/pods/podorel-self-pod/start", `{"confirm":true}`)
	req.AddCookie(cookie)
	req.Header.Set(csrfHeaderName, csrf)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d %s", rec.Code, rec.Body.String())
	}
	if len(fake.podActions) != 1 || fake.podActions[0] != "podorel-self-pod:start" {
		t.Fatalf("pod actions = %#v", fake.podActions)
	}
}

func TestPodListAttachesShortContainerStatsToPod(t *testing.T) {
	store, err := db.OpenMemory(context.Background(), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg, err := config.Load([]string{"--development"}, func(key string) string {
		if key == "PODOREL_ADMIN_PASSWORD" {
			return "secret-password"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	fake := &statsInferenceAgentClient{stats: []agents.ContainerStats{{
		ContainerID:         "container-123",
		Name:                "real-pod-main",
		CPUPodmanRaw:        "0.16%",
		CPUPercentHostTotal: 0.02,
		MemoryPodmanRaw:     "49.15kB / 33.25GB",
		MemoryBytes:         49150,
	}}}
	application, err := New(context.Background(), cfg, store, logging.New(ioDiscard{}, podorelruntime.Development, "web"), Options{
		TemplatesDir: filepath.Join("..", "..", "templates", "pods"),
		LogDir:       t.TempDir(),
		AgentClientFactory: func(ctx context.Context, agent db.Agent) (AgentClient, bool) {
			return fake, true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := application.Handler()
	req := jsonRequest(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"secret-password"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	cookie := rec.Result().Cookies()[0]

	req = httptest.NewRequest(http.MethodGet, "/api/pods", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pods = %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"pod_id":"pod-123"`) || !strings.Contains(body, `"container_id":"container-1234567890abcdef"`) || !strings.Contains(body, `"cpu_podman_raw":"0.16%"`) {
		t.Fatalf("stats were not attached to pod with full container id: %s", body)
	}
	agent, err := store.AgentByID(context.Background(), db.PrimaryAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Status != "online" || agent.LastSeenAt.IsZero() {
		t.Fatalf("agent heartbeat was not updated: %#v", agent)
	}
	traces, err := store.ListDebugTraces(context.Background(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	foundAggregationTrace := false
	for _, trace := range traces {
		statsCount, _ := trace.Trace["stats_count"].(float64)
		if trace.Operation == "podman_stats_aggregate" && statsCount == 1 {
			foundAggregationTrace = true
		}
	}
	if !foundAggregationTrace {
		t.Fatalf("missing stats aggregation trace: %#v", traces)
	}
}

func TestPodListDoesNotReturnStaleStatsWhenPodmanOmitsContainer(t *testing.T) {
	store, err := db.OpenMemory(context.Background(), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg, err := config.Load([]string{"--development"}, func(key string) string {
		if key == "PODOREL_ADMIN_PASSWORD" {
			return "secret-password"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	fake := &statsInferenceAgentClient{stats: []agents.ContainerStats{{
		ContainerID:         "container-123",
		Name:                "real-pod-main",
		CPUPodmanRaw:        "12.00%",
		CPUPercentHostTotal: 1,
		MemoryPodmanRaw:     "10MiB / 1GiB",
		MemoryBytes:         10 * 1024 * 1024,
	}}}
	application, err := New(context.Background(), cfg, store, logging.New(ioDiscard{}, podorelruntime.Development, "web"), Options{
		TemplatesDir: filepath.Join("..", "..", "templates", "pods"),
		LogDir:       t.TempDir(),
		AgentClientFactory: func(ctx context.Context, agent db.Agent) (AgentClient, bool) {
			return fake, true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := application.Handler()
	req := jsonRequest(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"secret-password"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	cookie := rec.Result().Cookies()[0]

	req = httptest.NewRequest(http.MethodGet, "/api/pods", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"cpu_podman_raw":"12.00%"`) {
		t.Fatalf("first pods response missing fresh stats = %d %s", rec.Code, rec.Body.String())
	}

	fake.stats = nil
	req = httptest.NewRequest(http.MethodGet, "/api/pods", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second pods response = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"cpu_podman_raw":"12.00%"`) || strings.Contains(rec.Body.String(), `"memory_podman_raw":"10MiB / 1GiB"`) {
		t.Fatalf("stale stats leaked into pod response: %s", rec.Body.String())
	}
}

func TestServesAngularIndex(t *testing.T) {
	harness := newTestHarness(t)
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<app-root></app-root>"), 0o600); err != nil {
		t.Fatal(err)
	}
	harness.app.cfg.UI.DistPath = dist
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "app-root") {
		t.Fatalf("ui response = %d %s", rec.Code, rec.Body.String())
	}
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

func jsonRequest(method string, path string, body string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func writeLargeLog(t *testing.T, path string, size int) {
	t.Helper()
	var content bytes.Buffer
	for content.Len() < size {
		content.WriteString("line\n")
	}
	if err := os.WriteFile(path, content.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func stringFieldFromEnvelope(t *testing.T, body []byte, key string) string {
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

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

type fakeAgentClient struct {
	podActions       []string
	containerActions []string
	secrets          []string
	builds           []string
	composeDeploys   []string
	createdPods      []string
	logLines         []agents.LogLine
	logRequests      []string
	digestRequests   []string
}

type failingRefreshAgentClient struct{}

func (failingRefreshAgentClient) Health(ctx context.Context) (agents.Health, error) {
	return agents.Health{}, errors.New("agent offline")
}

func (failingRefreshAgentClient) ListPods(ctx context.Context) ([]agents.PodSummary, error) {
	return nil, errors.New("agent offline")
}

func (failingRefreshAgentClient) ListContainers(ctx context.Context) ([]agents.ContainerSummary, error) {
	return nil, errors.New("agent offline")
}

func (failingRefreshAgentClient) Stats(ctx context.Context) ([]agents.ContainerStats, error) {
	return nil, errors.New("agent offline")
}

func (failingRefreshAgentClient) PodAction(ctx context.Context, podID string, action string) error {
	return errors.New("agent offline")
}

func (failingRefreshAgentClient) ContainerAction(ctx context.Context, containerID string, action string) error {
	return errors.New("agent offline")
}

func (failingRefreshAgentClient) Logs(ctx context.Context, podID string, containerID string, last int) ([]agents.LogLine, error) {
	return nil, errors.New("agent offline")
}

func (failingRefreshAgentClient) Exec(ctx context.Context, req agents.ExecRequest) (agents.ExecResult, error) {
	return agents.ExecResult{}, errors.New("agent offline")
}

func (failingRefreshAgentClient) ExecWebSocket(ctx context.Context, containerID string, shell string, cols int, rows int) (*ws.Conn, error) {
	return nil, errors.New("agent offline")
}

func (failingRefreshAgentClient) CreatePodFromTemplate(ctx context.Context, req agents.CreatePodRequest) error {
	return errors.New("agent offline")
}

func (failingRefreshAgentClient) DeployComposeStack(ctx context.Context, req agents.DeployComposeRequest) error {
	return errors.New("agent offline")
}

func (failingRefreshAgentClient) BuildImage(ctx context.Context, req agents.BuildImageRequest) error {
	return errors.New("agent offline")
}

func (failingRefreshAgentClient) CreateSecret(ctx context.Context, req agents.CreateSecretRequest) error {
	return errors.New("agent offline")
}

func (failingRefreshAgentClient) ScannerStatus(ctx context.Context, scanner string) (agents.ScannerStatus, error) {
	return agents.ScannerStatus{}, errors.New("agent offline")
}

func (failingRefreshAgentClient) ScanImage(ctx context.Context, req agents.ScanImageRequest) (agents.ScanImageResult, error) {
	return agents.ScanImageResult{}, errors.New("agent offline")
}

func (failingRefreshAgentClient) ImageDigest(ctx context.Context, req agents.ImageDigestRequest) (agents.ImageDigestResult, error) {
	return agents.ImageDigestResult{}, errors.New("agent offline")
}

type statsInferenceAgentClient struct {
	fakeAgentClient
	stats []agents.ContainerStats
}

func (f *statsInferenceAgentClient) ListPods(ctx context.Context) ([]agents.PodSummary, error) {
	return []agents.PodSummary{{ID: "pod-123", Name: "real-pod", State: "running"}}, nil
}

func (f *statsInferenceAgentClient) ListContainers(ctx context.Context) ([]agents.ContainerSummary, error) {
	return []agents.ContainerSummary{{
		ID:    "container-1234567890abcdef",
		PodID: "pod-123",
		Name:  "real-pod-main",
		State: "running",
	}}, nil
}

func (f *statsInferenceAgentClient) Stats(ctx context.Context) ([]agents.ContainerStats, error) {
	return append([]agents.ContainerStats(nil), f.stats...), nil
}

func (f *fakeAgentClient) Health(ctx context.Context) (agents.Health, error) {
	return agents.Health{Status: "ok", Mode: "development", User: "test"}, nil
}

func (f *fakeAgentClient) ListPods(ctx context.Context) ([]agents.PodSummary, error) {
	return []agents.PodSummary{{ID: "agent-pod", Name: "agent-pod", State: "running"}}, nil
}

func (f *fakeAgentClient) ListContainers(ctx context.Context) ([]agents.ContainerSummary, error) {
	return []agents.ContainerSummary{{ID: "agent-container", PodID: "agent-pod", Name: "agent-container", State: "running"}}, nil
}

func (f *fakeAgentClient) Stats(ctx context.Context) ([]agents.ContainerStats, error) {
	return []agents.ContainerStats{{ContainerID: "agent-container", PodID: "agent-pod", CPUPodmanRaw: "10.00%", CPUPercentHostTotal: 2.5, MemoryPodmanRaw: "1MiB / 1GiB", MemoryBytes: 1024 * 1024}}, nil
}

func (f *fakeAgentClient) PodAction(ctx context.Context, podID string, action string) error {
	f.podActions = append(f.podActions, podID+":"+action)
	return nil
}

func (f *fakeAgentClient) ContainerAction(ctx context.Context, containerID string, action string) error {
	f.containerActions = append(f.containerActions, containerID+":"+action)
	return nil
}

func (f *fakeAgentClient) Logs(ctx context.Context, podID string, containerID string, last int) ([]agents.LogLine, error) {
	f.logRequests = append(f.logRequests, podID+":"+containerID+":"+strconv.Itoa(last))
	if len(f.logLines) > 0 {
		return append([]agents.LogLine(nil), f.logLines...), nil
	}
	return []agents.LogLine{{Source: containerID, Line: "hello"}}, nil
}

func (f *fakeAgentClient) Exec(ctx context.Context, req agents.ExecRequest) (agents.ExecResult, error) {
	return agents.ExecResult{ContainerID: req.ContainerID, Shell: req.Shell, Command: req.Command, ExitCode: 0, Stdout: "fake exec output"}, nil
}

func (f *fakeAgentClient) ExecWebSocket(ctx context.Context, containerID string, shell string, cols int, rows int) (*ws.Conn, error) {
	return nil, errors.New("fake exec websocket is not connected")
}

func (f *fakeAgentClient) CreatePodFromTemplate(ctx context.Context, req agents.CreatePodRequest) error {
	f.createdPods = append(f.createdPods, req.Name)
	return nil
}

func (f *fakeAgentClient) DeployComposeStack(ctx context.Context, req agents.DeployComposeRequest) error {
	f.composeDeploys = append(f.composeDeploys, req.ProjectName)
	return nil
}

func (f *fakeAgentClient) BuildImage(ctx context.Context, req agents.BuildImageRequest) error {
	f.builds = append(f.builds, req.ImageName)
	return nil
}

func (f *fakeAgentClient) CreateSecret(ctx context.Context, req agents.CreateSecretRequest) error {
	f.secrets = append(f.secrets, req.Name)
	return nil
}

func (f *fakeAgentClient) ScannerStatus(ctx context.Context, scanner string) (agents.ScannerStatus, error) {
	return agents.ScannerStatus{Scanner: scanner, Available: false, Error: scannerUnavailableMessage(scanner)}, nil
}

func (f *fakeAgentClient) ScanImage(ctx context.Context, req agents.ScanImageRequest) (agents.ScanImageResult, error) {
	return agents.ScanImageResult{}, errors.New("scanner unavailable")
}

func (f *fakeAgentClient) ImageDigest(ctx context.Context, req agents.ImageDigestRequest) (agents.ImageDigestResult, error) {
	f.digestRequests = append(f.digestRequests, req.Image)
	return agents.ImageDigestResult{Image: req.Image, LocalDigest: "sha256:local", RemoteDigest: "sha256:local"}, nil
}

func TestDevSupervisorStatusMissingFileIsFriendly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev-status.json")
	t.Setenv("PODOREL_DEV_STATUS_FILE", path)

	status := devSupervisorStatus()
	if status["status"] != "missing" {
		t.Fatalf("expected missing status, got %#v", status)
	}
	message, _ := status["message"].(string)
	if message == "" || strings.Contains(message, "open ") {
		t.Fatalf("expected friendly message without raw open error, got %q", message)
	}
	if status["detail"] == "" {
		t.Fatalf("expected raw detail for diagnostics, got %#v", status)
	}
}

func TestDevSupervisorStatusReadsConfiguredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev-status.json")
	if err := os.WriteFile(path, []byte(`{"status":"running","supervisor_mode":"detached"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PODOREL_DEV_STATUS_FILE", path)

	status := devSupervisorStatus()
	if status["status"] != "running" || status["supervisor_mode"] != "detached" || status["path"] != path {
		t.Fatalf("unexpected status: %#v", status)
	}
}
