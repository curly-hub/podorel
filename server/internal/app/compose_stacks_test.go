package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/curly-hub/podorel/server/internal/db"
)

func TestComposeStackPreviewAndDeploy(t *testing.T) {
	composeDir := t.TempDir()
	stackDir := filepath.Join(composeDir, "demo")
	if err := os.MkdirAll(stackDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(stackDir, "docker-compose.yml"), "services:\n  web:\n    image: docker.io/library/alpine:3.20\n")
	writeTestFile(t, filepath.Join(stackDir, ".env.example"), "APP_ENV=production\n")
	writeTestFile(t, filepath.Join(stackDir, "podorel-compose.json"), `{
  "id": "demo-compose",
  "version": "1.0.0",
  "name": "Demo Compose",
  "description": "Demo compose stack",
  "source_path": "demo",
  "compose_files": ["docker-compose.yml"],
  "services": [{"name": "web", "image": "docker.io/library/alpine:3.20"}],
  "environment_files": [".env"],
  "required_files": ["docker-compose.yml"],
  "notes": ["copy .env.example to .env on the target host"],
  "labels": {"io.podorel.test": "compose"}
}`)
	client := &fakeAgentClient{}
	harness := newTestHarnessWithOptions(t, Options{
		AllowSnapshotFallback: true,
		ComposeTemplatesDir:   composeDir,
		AgentClientFactory: func(ctx context.Context, agent db.Agent) (AgentClient, bool) {
			return client, true
		},
	})
	login := harness.login(t)

	req := httptest.NewRequest(http.MethodGet, "/api/compose-stacks", nil)
	req.AddCookie(login.Cookie)
	rec := httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "demo-compose") {
		t.Fatalf("compose stacks response = %d %s", rec.Code, rec.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/compose-stacks/deploy", `{"stack_id":"demo-compose","project_name":"demo project"}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, login.CSRFToken)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "podman") || !strings.Contains(rec.Body.String(), "demo-project") {
		t.Fatalf("compose preview = %d %s", rec.Code, rec.Body.String())
	}
	if len(client.composeDeploys) != 0 {
		t.Fatalf("preview deployed compose stacks: %#v", client.composeDeploys)
	}

	req = jsonRequest(http.MethodPost, "/api/compose-stacks/deploy", `{"stack_id":"demo-compose","project_name":"demo project","confirm":true}`)
	req.AddCookie(login.Cookie)
	req.Header.Set(csrfHeaderName, login.CSRFToken)
	rec = httptest.NewRecorder()
	harness.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "bundle_files") {
		t.Fatalf("compose deploy = %d %s", rec.Code, rec.Body.String())
	}
	if len(client.composeDeploys) != 1 || client.composeDeploys[0] != "demo-project" {
		t.Fatalf("compose deploys = %#v", client.composeDeploys)
	}
}
func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
