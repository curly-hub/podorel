package podman

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type FakePodmanRuntime struct {
	mu         sync.Mutex
	Pods       []PodSummary
	Containers []ContainerSummary
	StatsRows  []ContainerStats
	LogRows    []LogLine
	Actions    []string
	Secrets    []string
	FailAction string
}

func (r *FakePodmanRuntime) ListPods(ctx context.Context) ([]PodSummary, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]PodSummary(nil), r.Pods...), nil
}

func (r *FakePodmanRuntime) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ContainerSummary(nil), r.Containers...), nil
}

func (r *FakePodmanRuntime) Stats(ctx context.Context) ([]ContainerStats, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ContainerStats(nil), r.StatsRows...), nil
}

func (r *FakePodmanRuntime) StartPod(ctx context.Context, podID string) error {
	return r.record(ctx, "pod:start:"+podID)
}

func (r *FakePodmanRuntime) StopPod(ctx context.Context, podID string) error {
	return r.record(ctx, "pod:stop:"+podID)
}

func (r *FakePodmanRuntime) RestartPod(ctx context.Context, podID string) error {
	return r.record(ctx, "pod:restart:"+podID)
}

func (r *FakePodmanRuntime) KillPod(ctx context.Context, podID string) error {
	return r.record(ctx, "pod:kill:"+podID)
}

func (r *FakePodmanRuntime) DeletePod(ctx context.Context, podID string) error {
	return r.record(ctx, "pod:delete:"+podID)
}

func (r *FakePodmanRuntime) StartContainer(ctx context.Context, containerID string) error {
	return r.record(ctx, "container:start:"+containerID)
}

func (r *FakePodmanRuntime) StopContainer(ctx context.Context, containerID string) error {
	return r.record(ctx, "container:stop:"+containerID)
}

func (r *FakePodmanRuntime) RestartContainer(ctx context.Context, containerID string) error {
	return r.record(ctx, "container:restart:"+containerID)
}

func (r *FakePodmanRuntime) KillContainer(ctx context.Context, containerID string) error {
	return r.record(ctx, "container:kill:"+containerID)
}

func (r *FakePodmanRuntime) DeleteContainer(ctx context.Context, containerID string) error {
	return r.record(ctx, "container:delete:"+containerID)
}

func (r *FakePodmanRuntime) Logs(ctx context.Context, req LogRequest) (<-chan LogLine, error) {
	_ = ctx
	_ = req
	r.mu.Lock()
	rows := append([]LogLine(nil), r.LogRows...)
	r.mu.Unlock()
	ch := make(chan LogLine, len(rows))
	for _, row := range rows {
		ch <- row
	}
	close(ch)
	return ch, nil
}

func (r *FakePodmanRuntime) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	if err := r.record(ctx, "exec"); err != nil {
		return ExecResult{}, err
	}
	shell := strings.TrimSpace(req.Shell)
	if shell == "" {
		shell = "sh"
	}
	return ExecResult{ContainerID: req.ContainerID, Shell: shell, Command: req.Command, ExitCode: 0, Stdout: "fake exec output"}, nil
}

func (r *FakePodmanRuntime) CreatePodFromTemplate(ctx context.Context, req CreatePodRequest) error {
	return r.record(ctx, "pod:create:"+req.Name)
}

func (r *FakePodmanRuntime) DeployComposeStack(ctx context.Context, req DeployComposeRequest) error {
	return r.record(ctx, "compose:deploy:"+req.ProjectName)
}

func (r *FakePodmanRuntime) BuildImage(ctx context.Context, req BuildImageRequest) error {
	return r.record(ctx, "image:build:"+req.ImageName)
}

func (r *FakePodmanRuntime) CreateSecret(ctx context.Context, req CreateSecretRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.FailAction == "secret:create:"+req.Name {
		return fmt.Errorf("fake action failed: %s", r.FailAction)
	}
	r.Actions = append(r.Actions, "secret:create:"+req.Name)
	r.Secrets = append(r.Secrets, req.Name)
	return nil
}

func (r *FakePodmanRuntime) record(ctx context.Context, action string) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.FailAction == action {
		return fmt.Errorf("fake action failed: %s", action)
	}
	r.Actions = append(r.Actions, action)
	return nil
}
