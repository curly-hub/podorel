package podman

import (
	"context"
	"time"
)

type PodmanRuntime interface {
	ListPods(ctx context.Context) ([]PodSummary, error)
	ListContainers(ctx context.Context) ([]ContainerSummary, error)
	Stats(ctx context.Context) ([]ContainerStats, error)
	StartPod(ctx context.Context, podID string) error
	StopPod(ctx context.Context, podID string) error
	RestartPod(ctx context.Context, podID string) error
	KillPod(ctx context.Context, podID string) error
	DeletePod(ctx context.Context, podID string) error
	StartContainer(ctx context.Context, containerID string) error
	StopContainer(ctx context.Context, containerID string) error
	RestartContainer(ctx context.Context, containerID string) error
	KillContainer(ctx context.Context, containerID string) error
	DeleteContainer(ctx context.Context, containerID string) error
	Logs(ctx context.Context, req LogRequest) (<-chan LogLine, error)
	Exec(ctx context.Context, req ExecRequest) (ExecResult, error)
	CreatePodFromTemplate(ctx context.Context, req CreatePodRequest) error
	DeployComposeStack(ctx context.Context, req DeployComposeRequest) error
	BuildImage(ctx context.Context, req BuildImageRequest) error
	CreateSecret(ctx context.Context, req CreateSecretRequest) error
}

type PodSummary struct {
	ID        string
	Name      string
	State     string
	Health    string
	CreatedAt time.Time
	RawJSON   string
}

type ContainerSummary struct {
	ID        string
	PodID     string
	Name      string
	Image     string
	State     string
	Health    string
	CreatedAt time.Time
	RawJSON   string
}

type ContainerStats struct {
	ContainerID         string
	PodID               string
	Name                string
	CPUPodmanRaw        string
	CPUPodmanPercent    float64
	CPUPercentHostTotal float64
	MemoryPodmanRaw     string
	MemoryBytes         uint64
	MemoryLimitRaw      string
	MemoryParserBranch  string
	RawJSON             string
}

type PodStats struct {
	PodID               string
	ContainerIDs        []string
	CPUPercentHostTotal float64
	MemoryBytes         uint64
}

type LogRequest struct {
	PodID       string
	ContainerID string
	Follow      bool
	Since       time.Time
	LastLines   int
}

type LogLine struct {
	Timestamp time.Time
	Source    string
	Line      string
}

type ExecRequest struct {
	ContainerID    string `json:"container_id"`
	Shell          string `json:"shell"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type ExecResult struct {
	ContainerID     string    `json:"container_id"`
	Shell           string    `json:"shell"`
	Command         string    `json:"command"`
	ExitCode        int       `json:"exit_code"`
	Stdout          string    `json:"stdout"`
	Stderr          string    `json:"stderr"`
	DurationMillis  int64     `json:"duration_ms"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
	TimedOut        bool      `json:"timed_out"`
	OutputTruncated bool      `json:"output_truncated"`
}

type CreatePodRequest struct {
	Name           string   `json:"name"`
	TemplateID     string   `json:"template_id"`
	PreviewCommand []string `json:"preview_command"`
}

type ComposeBundleFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type DeployComposeRequest struct {
	ProjectName  string              `json:"project_name"`
	StackID      string              `json:"stack_id"`
	ComposeFiles []string            `json:"compose_files"`
	Files        []ComposeBundleFile `json:"files"`
}

type BuildImageRequest struct {
	ImageName  string `json:"image_name"`
	Dockerfile string `json:"dockerfile"`
}

type CreateSecretRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
