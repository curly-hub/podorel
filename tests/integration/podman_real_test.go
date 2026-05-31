package integration

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const realPodmanEnv = "PODOREL_RUN_REAL_PODMAN_TESTS"
const realPodName = "podorel-test-real-pod"
const realContainerName = "podorel-test-real-container"

func TestRealPodmanLifecycle(t *testing.T) {
	if os.Getenv(realPodmanEnv) != "1" {
		t.Skip(realPodmanEnv + "=1 is required for real Podman integration tests")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cleanupRealPodman(ctx, t)
	t.Cleanup(func() { cleanupRealPodman(context.Background(), t) })

	runPodman(ctx, t, "pod", "create", "--name", realPodName)
	runPodman(ctx, t, "run", "--pod", realPodName, "--name", realContainerName, "-d", "docker.io/library/alpine:3.20", "sh", "-c", "while true; do echo podorel-real-test; sleep 1; done")
	runPodman(ctx, t, "pod", "stop", realPodName)
	runPodman(ctx, t, "pod", "start", realPodName)
	runPodman(ctx, t, "pod", "restart", realPodName)

	stats := runPodman(ctx, t, "stats", "--no-stream", "--format", "json", realContainerName)
	if !strings.Contains(stats, realContainerName) {
		t.Fatalf("stats did not mention test container: %s", stats)
	}
	logs := runPodman(ctx, t, "logs", "--tail", "5", realContainerName)
	if !strings.Contains(logs, "podorel-real-test") {
		t.Fatalf("logs did not include expected line: %s", logs)
	}
	runPodman(ctx, t, "kill", realContainerName)
	runPodman(ctx, t, "rm", "-f", realContainerName)
	runPodman(ctx, t, "pod", "rm", "-f", realPodName)
}

func cleanupRealPodman(ctx context.Context, t *testing.T) {
	t.Helper()
	_ = exec.CommandContext(ctx, "podman", "rm", "-f", realContainerName).Run()
	_ = exec.CommandContext(ctx, "podman", "pod", "rm", "-f", realPodName).Run()
}

func runPodman(ctx context.Context, t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "podman", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("podman %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}
