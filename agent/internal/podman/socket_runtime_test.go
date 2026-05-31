package podman

import (
	"archive/tar"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSocketRuntimeListPodsAndAction(t *testing.T) {
	var actionPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + DefaultPodmanAPIVersion + "/libpod/pods/json":
			_, _ = w.Write([]byte(`[{"Id":"pod-a","Name":"pod-a","Status":"Running","Created":"2026-05-05T21:49:47.001372093Z"}]`))
		case "/" + DefaultPodmanAPIVersion + "/libpod/pods/pod-a/start":
			actionPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	runtime := &PodmanSocketRuntime{BaseURL: server.URL, HTTPClient: server.Client()}
	pods, err := runtime.ListPods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].ID != "pod-a" {
		t.Fatalf("pods = %#v", pods)
	}
	if pods[0].CreatedAt.IsZero() {
		t.Fatalf("created_at was not parsed from socket payload: %#v", pods[0])
	}
	if err := runtime.StartPod(context.Background(), "pod-a"); err != nil {
		t.Fatal(err)
	}
	if actionPath == "" {
		t.Fatal("start action did not hit socket API")
	}
}

func TestSocketRuntimeBuildImageSendsDockerfileTar(t *testing.T) {
	var sawBuild bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+DefaultPodmanAPIVersion+"/libpod/build" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("t") != "demo:latest" {
			t.Fatalf("tag query = %q", r.URL.Query().Get("t"))
		}
		if r.Header.Get("Content-Type") != "application/x-tar" {
			t.Fatalf("content type = %q", r.Header.Get("Content-Type"))
		}
		reader := tar.NewReader(r.Body)
		header, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != "Dockerfile" {
			t.Fatalf("tar entry = %q, want Dockerfile", header.Name)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "FROM alpine:3.20") {
			t.Fatalf("dockerfile content = %q", string(content))
		}
		sawBuild = true
		_, _ = w.Write([]byte(`{"stream":"ok"}`))
	}))
	defer server.Close()

	runtime := &PodmanSocketRuntime{BaseURL: server.URL, HTTPClient: server.Client()}
	if err := runtime.BuildImage(context.Background(), BuildImageRequest{ImageName: "demo:latest", Dockerfile: "FROM alpine:3.20"}); err != nil {
		t.Fatal(err)
	}
	if !sawBuild {
		t.Fatal("build endpoint was not called")
	}
}

func TestFallbackRuntimeUsesFallbackOnPreferredError(t *testing.T) {
	preferred := &FakePodmanRuntime{FailAction: "pod:start:pod-a"}
	fallback := &FakePodmanRuntime{}
	runtime := FallbackRuntime{Preferred: preferred, Fallback: fallback}
	if err := runtime.StartPod(context.Background(), "pod-a"); err != nil {
		t.Fatal(err)
	}
	if len(fallback.Actions) != 1 || fallback.Actions[0] != "pod:start:pod-a" {
		t.Fatalf("fallback actions = %#v", fallback.Actions)
	}
}

func TestSocketTemplateCreateFallsBackToCLI(t *testing.T) {
	preferred := &PodmanSocketRuntime{}
	fallback := &FakePodmanRuntime{}
	runtime := FallbackRuntime{Preferred: preferred, Fallback: fallback}

	req := CreatePodRequest{Name: "pod-a", PreviewCommand: []string{"podman", "run", "--detach", "alpine"}}
	if err := runtime.CreatePodFromTemplate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(fallback.Actions) != 1 || fallback.Actions[0] != "pod:create:pod-a" {
		t.Fatalf("fallback actions = %#v", fallback.Actions)
	}
}
