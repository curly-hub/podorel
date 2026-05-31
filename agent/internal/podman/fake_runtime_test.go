package podman

import (
	"context"
	"testing"
)

func TestFakeRuntimeRecordsActions(t *testing.T) {
	runtime := &FakePodmanRuntime{}
	if err := runtime.StartPod(context.Background(), "pod-a"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.CreateSecret(context.Background(), CreateSecretRequest{Name: "db-password", Value: "raw-secret"}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.Actions) != 2 {
		t.Fatalf("actions = %#v", runtime.Actions)
	}
	if runtime.Secrets[0] != "db-password" {
		t.Fatalf("secret metadata = %#v", runtime.Secrets)
	}
}
