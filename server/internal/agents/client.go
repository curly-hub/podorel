package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/curly-hub/podorel/internal/correlation"
	ws "github.com/curly-hub/podorel/internal/websocket"
)

const defaultAgentClientTimeout = 10 * time.Second

type Client struct {
	socketPath string
	token      string
	httpClient *http.Client
}

type Health struct {
	Status                string `json:"status"`
	Mode                  string `json:"mode"`
	User                  string `json:"user"`
	SocketPath            string `json:"socket_path"`
	TokenConfigured       bool   `json:"token_configured"`
	PodmanSocketPath      string `json:"podman_socket_path"`
	PodmanSocketAvailable bool   `json:"podman_socket_available"`
	PodmanCLIAvailable    bool   `json:"podman_cli_available"`
	LastError             string `json:"last_error"`
	LastSeenAt            string `json:"last_seen_at"`
}

type AgentEnvelope[T any] struct {
	OK    bool   `json:"ok"`
	Data  T      `json:"data"`
	Error string `json:"error"`
}

type PodSummary struct {
	ID        string    `json:"ID,omitempty"`
	Id        string    `json:"Id,omitempty"`
	Name      string    `json:"Name,omitempty"`
	State     string    `json:"State,omitempty"`
	Status    string    `json:"Status,omitempty"`
	CreatedAt time.Time `json:"CreatedAt,omitempty"`
	RawJSON   string    `json:"RawJSON,omitempty"`
}

type ContainerSummary struct {
	ID        string    `json:"ID,omitempty"`
	Id        string    `json:"Id,omitempty"`
	PodID     string    `json:"PodID,omitempty"`
	Name      string    `json:"Name,omitempty"`
	Image     string    `json:"Image,omitempty"`
	State     string    `json:"State,omitempty"`
	CreatedAt time.Time `json:"CreatedAt,omitempty"`
	RawJSON   string    `json:"RawJSON,omitempty"`
}

type ContainerStats struct {
	ContainerID         string  `json:"ContainerID"`
	PodID               string  `json:"PodID"`
	Name                string  `json:"Name"`
	CPUPodmanRaw        string  `json:"CPUPodmanRaw"`
	CPUPodmanPercent    float64 `json:"CPUPodmanPercent"`
	CPUPercentHostTotal float64 `json:"CPUPercentHostTotal"`
	MemoryPodmanRaw     string  `json:"MemoryPodmanRaw"`
	MemoryBytes         uint64  `json:"MemoryBytes"`
	MemoryLimitRaw      string  `json:"MemoryLimitRaw"`
	MemoryParserBranch  string  `json:"MemoryParserBranch"`
	RawJSON             string  `json:"RawJSON"`
}

type LogLine struct {
	Timestamp time.Time `json:"Timestamp"`
	Source    string    `json:"Source"`
	Line      string    `json:"Line"`
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

func NewClient(socketPath string, token string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		socketPath: socketPath,
		token:      token,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   defaultAgentClientTimeout,
		},
	}
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var health Health
	if err := c.do(ctx, http.MethodGet, "/health", nil, &health); err != nil {
		return Health{}, err
	}
	return health, nil
}

func (c *Client) ListPods(ctx context.Context) ([]PodSummary, error) {
	var pods []PodSummary
	err := c.doEnvelope(ctx, http.MethodGet, "/pods", nil, &pods)
	return pods, err
}

func (c *Client) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	var containers []ContainerSummary
	err := c.doEnvelope(ctx, http.MethodGet, "/containers", nil, &containers)
	return containers, err
}

func (c *Client) Stats(ctx context.Context) ([]ContainerStats, error) {
	var stats []ContainerStats
	err := c.doEnvelope(ctx, http.MethodGet, "/stats", nil, &stats)
	return stats, err
}

func (c *Client) PodAction(ctx context.Context, podID string, action string) error {
	method := http.MethodPost
	path := "/pods/" + url.PathEscape(podID) + "/" + action
	if action == "delete" {
		method = http.MethodDelete
		path = "/pods/" + url.PathEscape(podID)
	}
	var result map[string]any
	return c.doEnvelope(ctx, method, path, nil, &result)
}

func (c *Client) ContainerAction(ctx context.Context, containerID string, action string) error {
	method := http.MethodPost
	path := "/containers/" + url.PathEscape(containerID) + "/" + action
	if action == "delete" {
		method = http.MethodDelete
		path = "/containers/" + url.PathEscape(containerID)
	}
	var result map[string]any
	return c.doEnvelope(ctx, method, path, nil, &result)
}

func (c *Client) Logs(ctx context.Context, podID string, containerID string, last int) ([]LogLine, error) {
	query := url.Values{}
	if podID != "" {
		query.Set("pod_id", podID)
	}
	if containerID != "" {
		query.Set("container_id", containerID)
	}
	if last > 0 {
		query.Set("last", fmt.Sprintf("%d", last))
	}
	var lines []LogLine
	err := c.doEnvelope(ctx, http.MethodGet, "/logs?"+query.Encode(), nil, &lines)
	return lines, err
}

func (c *Client) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	var result ExecResult
	path := "/containers/" + url.PathEscape(req.ContainerID) + "/exec"
	err := c.doEnvelope(ctx, http.MethodPost, path, req, &result)
	return result, err
}

func (c *Client) ExecWebSocket(ctx context.Context, containerID string, shell string, cols int, rows int) (*ws.Conn, error) {
	query := url.Values{}
	if shell != "" {
		query.Set("shell", shell)
	}
	if cols > 0 {
		query.Set("cols", fmt.Sprintf("%d", cols))
	}
	if rows > 0 {
		query.Set("rows", fmt.Sprintf("%d", rows))
	}
	path := "/containers/" + url.PathEscape(containerID) + "/exec/ws"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	headers := map[string]string{}
	if id := correlation.FromContext(ctx); id != "" {
		headers[correlation.HeaderName] = id
	}
	return ws.DialUnix(ctx, c.socketPath, path, c.token, headers)
}

func (c *Client) CreatePodFromTemplate(ctx context.Context, req CreatePodRequest) error {
	var result map[string]any
	return c.doEnvelope(ctx, http.MethodPost, "/pods/create-from-template", req, &result)
}

func (c *Client) DeployComposeStack(ctx context.Context, req DeployComposeRequest) error {
	var result map[string]any
	return c.doEnvelope(ctx, http.MethodPost, "/compose-stacks/deploy", req, &result)
}

func (c *Client) BuildImage(ctx context.Context, req BuildImageRequest) error {
	var result map[string]any
	return c.doEnvelope(ctx, http.MethodPost, "/images/build-from-dockerfile", req, &result)
}

func (c *Client) CreateSecret(ctx context.Context, req CreateSecretRequest) error {
	var result map[string]any
	return c.doEnvelope(ctx, http.MethodPost, "/secrets", req, &result)
}

func (c *Client) doEnvelope(ctx context.Context, method string, path string, body any, out any) error {
	var envelope AgentEnvelope[json.RawMessage]
	if err := c.do(ctx, method, path, body, &envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("agent request failed: %s", envelope.Error)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}

func (c *Client) do(ctx context.Context, method string, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://podorel-agent"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if id := correlation.FromContext(ctx); id != "" {
		req.Header.Set(correlation.HeaderName, id)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
