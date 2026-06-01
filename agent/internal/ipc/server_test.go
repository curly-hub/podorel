package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/curly-hub/podorel/agent/internal/podman"
	podorelruntime "github.com/curly-hub/podorel/internal/runtime"
)

func TestServerHealthRequiresToken(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeTrivy := filepath.Join(binDir, "trivy")
	if err := os.WriteFile(fakeTrivy, []byte("#!/usr/bin/env sh\nif [ \"$1\" = \"--version\" ]; then echo 'Version: 0.50.0'; exit 0; fi\nif [ \"$1\" = \"image\" ]; then printf '{\"Results\":[]}'; exit 0; fi\necho unsupported >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	socket := filepath.Join(t.TempDir(), "agent.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readyCh := make(chan struct{}, 1)
	server := Server{
		SocketPath: socket,
		Token:      "secret-token",
		Mode:       podorelruntime.Development,
		Runtime: &podman.FakePodmanRuntime{
			Pods: []podman.PodSummary{{ID: "pod-a", Name: "pod-a", State: "running"}},
		},
		OnReady: func() error {
			readyCh <- struct{}{}
			return nil
		},
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ctx)
	}()
	waitForSocket(t, socket)
	select {
	case <-readyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not announce readiness")
	}

	client := unixClient(socket)
	req, err := http.NewRequest(http.MethodGet, "http://podorel-agent/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status without token = %d, want 401", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, "http://podorel-agent/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status with token = %d, want 200", resp.StatusCode)
	}
	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if socketPath, _ := health["podman_socket_path"].(string); !strings.Contains(socketPath, "/podman/podman.sock") {
		t.Fatalf("podman_socket_path = %#v", health["podman_socket_path"])
	}
	_ = resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, "http://podorel-agent/pods", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload struct {
		OK   bool                `json:"ok"`
		Data []podman.PodSummary `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || len(payload.Data) != 1 || payload.Data[0].ID != "pod-a" {
		t.Fatalf("pods payload = %#v", payload)
	}

	req, err = http.NewRequest(http.MethodPost, "http://podorel-agent/pods/create-from-template", bytes.NewBufferString(`{"name":"demo","template_id":"alpine-nodejs","preview_command":["podman","pod","create","demo"]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create pod status = %d", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, "http://podorel-agent/security/scanner?scanner=trivy", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var scannerPayload struct {
		OK   bool `json:"ok"`
		Data struct {
			Available bool   `json:"available"`
			Path      string `json:"path"`
			Version   string `json:"version"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&scannerPayload); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !scannerPayload.OK || !scannerPayload.Data.Available || scannerPayload.Data.Path != fakeTrivy || !strings.Contains(scannerPayload.Data.Version, "0.50.0") {
		t.Fatalf("scanner payload = %#v", scannerPayload)
	}

	req, err = http.NewRequest(http.MethodPost, "http://podorel-agent/security/scan-image", bytes.NewBufferString(`{"scanner":"trivy","image":"alpine:3.20"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var scanPayload struct {
		OK   bool `json:"ok"`
		Data struct {
			Image   string `json:"image"`
			RawJSON string `json:"raw_json"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&scanPayload); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !scanPayload.OK || scanPayload.Data.Image != "alpine:3.20" || !strings.Contains(scanPayload.Data.RawJSON, "Results") {
		t.Fatalf("scan payload = %#v", scanPayload)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
}

func unixClient(socketPath string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}}
}

func waitForSocket(t *testing.T, socketPath string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear", socketPath)
}
