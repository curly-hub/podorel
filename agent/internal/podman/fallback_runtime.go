package podman

import "context"

type FallbackRuntime struct {
	Preferred PodmanRuntime
	Fallback  PodmanRuntime
}

func (r FallbackRuntime) ListPods(ctx context.Context) ([]PodSummary, error) {
	out, err := r.Preferred.ListPods(ctx)
	if err == nil {
		return out, nil
	}
	return r.Fallback.ListPods(ctx)
}

func (r FallbackRuntime) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	out, err := r.Preferred.ListContainers(ctx)
	if err == nil {
		return out, nil
	}
	return r.Fallback.ListContainers(ctx)
}

func (r FallbackRuntime) Stats(ctx context.Context) ([]ContainerStats, error) {
	out, err := r.Preferred.Stats(ctx)
	if err == nil {
		return out, nil
	}
	return r.Fallback.Stats(ctx)
}

func (r FallbackRuntime) StartPod(ctx context.Context, podID string) error {
	return fallbackErr(r.Preferred.StartPod(ctx, podID), func() error { return r.Fallback.StartPod(ctx, podID) })
}

func (r FallbackRuntime) StopPod(ctx context.Context, podID string) error {
	return fallbackErr(r.Preferred.StopPod(ctx, podID), func() error { return r.Fallback.StopPod(ctx, podID) })
}

func (r FallbackRuntime) RestartPod(ctx context.Context, podID string) error {
	return fallbackErr(r.Preferred.RestartPod(ctx, podID), func() error { return r.Fallback.RestartPod(ctx, podID) })
}

func (r FallbackRuntime) KillPod(ctx context.Context, podID string) error {
	return fallbackErr(r.Preferred.KillPod(ctx, podID), func() error { return r.Fallback.KillPod(ctx, podID) })
}

func (r FallbackRuntime) DeletePod(ctx context.Context, podID string) error {
	return fallbackErr(r.Preferred.DeletePod(ctx, podID), func() error { return r.Fallback.DeletePod(ctx, podID) })
}

func (r FallbackRuntime) StartContainer(ctx context.Context, containerID string) error {
	return fallbackErr(r.Preferred.StartContainer(ctx, containerID), func() error { return r.Fallback.StartContainer(ctx, containerID) })
}

func (r FallbackRuntime) StopContainer(ctx context.Context, containerID string) error {
	return fallbackErr(r.Preferred.StopContainer(ctx, containerID), func() error { return r.Fallback.StopContainer(ctx, containerID) })
}

func (r FallbackRuntime) RestartContainer(ctx context.Context, containerID string) error {
	return fallbackErr(r.Preferred.RestartContainer(ctx, containerID), func() error { return r.Fallback.RestartContainer(ctx, containerID) })
}

func (r FallbackRuntime) KillContainer(ctx context.Context, containerID string) error {
	return fallbackErr(r.Preferred.KillContainer(ctx, containerID), func() error { return r.Fallback.KillContainer(ctx, containerID) })
}

func (r FallbackRuntime) DeleteContainer(ctx context.Context, containerID string) error {
	return fallbackErr(r.Preferred.DeleteContainer(ctx, containerID), func() error { return r.Fallback.DeleteContainer(ctx, containerID) })
}

func (r FallbackRuntime) Logs(ctx context.Context, req LogRequest) (<-chan LogLine, error) {
	out, err := r.Preferred.Logs(ctx, req)
	if err == nil {
		return out, nil
	}
	return r.Fallback.Logs(ctx, req)
}

func (r FallbackRuntime) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	out, err := r.Preferred.Exec(ctx, req)
	if err == nil {
		return out, nil
	}
	return r.Fallback.Exec(ctx, req)
}

func (r FallbackRuntime) CreatePodFromTemplate(ctx context.Context, req CreatePodRequest) error {
	return fallbackErr(r.Preferred.CreatePodFromTemplate(ctx, req), func() error { return r.Fallback.CreatePodFromTemplate(ctx, req) })
}

func (r FallbackRuntime) DeployComposeStack(ctx context.Context, req DeployComposeRequest) error {
	return fallbackErr(r.Preferred.DeployComposeStack(ctx, req), func() error { return r.Fallback.DeployComposeStack(ctx, req) })
}

func (r FallbackRuntime) BuildImage(ctx context.Context, req BuildImageRequest) error {
	return fallbackErr(r.Preferred.BuildImage(ctx, req), func() error { return r.Fallback.BuildImage(ctx, req) })
}

func (r FallbackRuntime) CreateSecret(ctx context.Context, req CreateSecretRequest) error {
	return fallbackErr(r.Preferred.CreateSecret(ctx, req), func() error { return r.Fallback.CreateSecret(ctx, req) })
}

func fallbackErr(err error, fallback func() error) error {
	if err == nil {
		return nil
	}
	return fallback()
}
