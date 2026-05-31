package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/curly-hub/podorel/agent/internal/podman"
	podorelruntime "github.com/curly-hub/podorel/internal/runtime"
)

func TestServerHealthRequiresToken(t *testing.T) {
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
