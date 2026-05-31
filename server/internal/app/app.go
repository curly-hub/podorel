package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/curly-hub/podorel/internal/logging"
	ws "github.com/curly-hub/podorel/internal/websocket"
	"github.com/curly-hub/podorel/server/internal/agents"
	"github.com/curly-hub/podorel/server/internal/api"
	"github.com/curly-hub/podorel/server/internal/auth"
	"github.com/curly-hub/podorel/server/internal/composecatalog"
	"github.com/curly-hub/podorel/server/internal/config"
	"github.com/curly-hub/podorel/server/internal/db"
	"github.com/curly-hub/podorel/server/internal/templates"
)

const (
	sessionCookieName    = "podorel_session"
	csrfHeaderName       = "X-CSRF-Token"
	maxJSONBodyBytes     = 1 << 20
	maxDockerfileBytes   = 512 * 1024
	defaultLogLineLimit  = 500
	liveLogReplayLimit   = 100
	liveLogPollInterval  = 2 * time.Second
	webSocketWriteWait   = 5 * time.Second
	webSocketAcceptMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

type App struct {
	cfg                   config.Config
	store                 *db.Store
	logger                *logging.Logger
	templates             []templates.Template
	composeStacks         []composecatalog.Stack
	logDir                string
	newAgent              AgentClientFactory
	allowSnapshotFallback bool
	now                   func() time.Time

	failMu   sync.Mutex
	failures map[string][]time.Time
}

type Options struct {
	TemplatesDir          string
	ComposeTemplatesDir   string
	LogDir                string
	AgentClientFactory    AgentClientFactory
	AllowSnapshotFallback bool
}

type AgentClientFactory func(ctx context.Context, agent db.Agent) (AgentClient, bool)

type AgentClient interface {
	Health(ctx context.Context) (agents.Health, error)
	ListPods(ctx context.Context) ([]agents.PodSummary, error)
	ListContainers(ctx context.Context) ([]agents.ContainerSummary, error)
	Stats(ctx context.Context) ([]agents.ContainerStats, error)
	PodAction(ctx context.Context, podID string, action string) error
	ContainerAction(ctx context.Context, containerID string, action string) error
	Logs(ctx context.Context, podID string, containerID string, last int) ([]agents.LogLine, error)
	Exec(ctx context.Context, req agents.ExecRequest) (agents.ExecResult, error)
	ExecWebSocket(ctx context.Context, containerID string, shell string, cols int, rows int) (*ws.Conn, error)
	CreatePodFromTemplate(ctx context.Context, req agents.CreatePodRequest) error
	DeployComposeStack(ctx context.Context, req agents.DeployComposeRequest) error
	BuildImage(ctx context.Context, req agents.BuildImageRequest) error
	CreateSecret(ctx context.Context, req agents.CreateSecretRequest) error
}

func New(ctx context.Context, cfg config.Config, store *db.Store, logger *logging.Logger, opts Options) (*App, error) {
	if opts.TemplatesDir == "" {
		opts.TemplatesDir = templates.DefaultPodTemplateDir
	}
	if opts.ComposeTemplatesDir == "" {
		opts.ComposeTemplatesDir = composecatalog.DefaultComposeTemplateDir
	}
	if opts.LogDir == "" {
		opts.LogDir = filepath.Join(os.TempDir(), "podorel-logs")
	}
	if opts.AgentClientFactory == nil {
		opts.AgentClientFactory = defaultAgentClientFactory
	}
	loadedTemplates, err := templates.LoadDir(opts.TemplatesDir)
	if err != nil {
		return nil, err
	}
	loadedComposeStacks, err := composecatalog.LoadDir(opts.ComposeTemplatesDir)
	if err != nil {
		return nil, err
	}
	if err := store.BootstrapWithOptions(ctx, db.BootstrapOptions{
		AdminPassword:          cfg.Auth.AdminPassword,
		PrimaryAgentSocketPath: cfg.Agent.PrimarySocketPath,
	}); err != nil {
		return nil, err
	}
	if err := applyPersistedSettingsToConfig(ctx, store, &cfg); err != nil {
		return nil, err
	}
	app := &App{
		cfg:                   cfg,
		store:                 store,
		logger:                logger,
		templates:             loadedTemplates,
		composeStacks:         loadedComposeStacks,
		logDir:                opts.LogDir,
		newAgent:              opts.AgentClientFactory,
		allowSnapshotFallback: opts.AllowSnapshotFallback && cfg.Mode.IsDevelopment(),
		now:                   func() time.Time { return time.Now().UTC() },
		failures:              map[string][]time.Time{},
	}
	return app, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	a.registerRoutes(mux)
	return api.CorrelationMiddleware(mux)
}

func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("GET /api/system/status", a.withSession(a.handleSystemStatus))
	mux.HandleFunc("POST /api/auth/login", a.handlePasswordLogin)
	mux.HandleFunc("POST /api/auth/login-agent-token", a.handleAgentTokenLogin)
	mux.HandleFunc("POST /api/auth/logout", a.withSession(a.handleLogout))
	mux.HandleFunc("GET /api/auth/me", a.withSession(a.handleMe))

	mux.HandleFunc("GET /api/agents", a.withSession(a.handleListAgents))
	mux.HandleFunc("POST /api/agents/register", a.withSession(a.handleRegisterAgent))
	mux.HandleFunc("POST /api/agents/{id}/rotate-token", a.withSession(a.handleRotateAgentToken))
	mux.HandleFunc("GET /api/agents/{id}/health", a.withSession(a.handleAgentHealth))

	mux.HandleFunc("GET /api/pods", a.withSession(a.handleListPods))
	mux.HandleFunc("GET /api/pods/{id}", a.withSession(a.handleGetPod))
	mux.HandleFunc("POST /api/pods/{id}/start", a.withSession(a.handlePodAction("start")))
	mux.HandleFunc("POST /api/pods/{id}/stop", a.withSession(a.handlePodAction("stop")))
	mux.HandleFunc("POST /api/pods/{id}/restart", a.withSession(a.handlePodAction("restart")))
	mux.HandleFunc("POST /api/pods/{id}/kill", a.withSession(a.handlePodAction("kill")))
	mux.HandleFunc("DELETE /api/pods/{id}", a.withSession(a.handleDeletePod))

	mux.HandleFunc("GET /api/containers", a.withSession(a.handleListContainers))
	mux.HandleFunc("GET /api/containers/{id}", a.withSession(a.handleGetContainer))
	mux.HandleFunc("POST /api/containers/{id}/start", a.withSession(a.handleContainerAction("start")))
	mux.HandleFunc("POST /api/containers/{id}/stop", a.withSession(a.handleContainerAction("stop")))
	mux.HandleFunc("POST /api/containers/{id}/restart", a.withSession(a.handleContainerAction("restart")))
	mux.HandleFunc("POST /api/containers/{id}/kill", a.withSession(a.handleContainerAction("kill")))
	mux.HandleFunc("DELETE /api/containers/{id}", a.withSession(a.handleDeleteContainer))

	mux.HandleFunc("GET /api/stats/current", a.withSession(a.handleCurrentStats))
	mux.HandleFunc("GET /api/stats/history", a.withSession(a.handleStatsHistory))
	mux.HandleFunc("GET /api/logs/history", a.withSession(a.handleLogsHistory))
	mux.HandleFunc("GET /api/ws/logs", a.withSession(a.handleLogsWebSocket))
	mux.HandleFunc("GET /api/ws/builds", a.withSession(a.handleBuildsWebSocket))

	mux.HandleFunc("GET /api/security/summary", a.withSession(a.handleSecuritySummary))
	mux.HandleFunc("GET /api/security/scanner-options", a.withSession(a.handleScannerOptions))
	mux.HandleFunc("POST /api/security/scan", a.withSession(a.handleSecurityScan))
	mux.HandleFunc("GET /api/security/scans/{id}", a.withSession(a.handleSecurityScanByID))
	mux.HandleFunc("GET /api/security/findings", a.withSession(a.handleSecurityFindings))
	mux.HandleFunc("GET /api/security/image-digests", a.withSession(a.handleImageDigests))
	mux.HandleFunc("GET /api/security/host-updates", a.withSession(a.handleHostPackageUpdates))

	mux.HandleFunc("GET /api/templates", a.withSession(a.handleTemplates))
	mux.HandleFunc("POST /api/pods/create-from-template", a.withSession(a.handleCreateFromTemplate))
	mux.HandleFunc("GET /api/compose-stacks", a.withSession(a.handleComposeStacks))
	mux.HandleFunc("POST /api/compose-stacks/deploy", a.withSession(a.handleDeployComposeStack))
	mux.HandleFunc("POST /api/images/build-from-dockerfile", a.withSession(a.handleBuildFromDockerfile))
	mux.HandleFunc("POST /api/secrets", a.withSession(a.handleCreateSecret))

	mux.HandleFunc("GET /api/audit", a.withSession(a.handleAudit))
	mux.HandleFunc("GET /api/settings", a.withSession(a.handleSettings))
	mux.HandleFunc("PUT /api/settings", a.withSession(a.handleUpdateSettings))

	mux.HandleFunc("GET /api/diagnostics/runtime-mode", a.withSession(a.handleRuntimeMode))
	mux.HandleFunc("GET /api/diagnostics/traces", a.withSession(a.handleTraces))
	mux.HandleFunc("GET /api/diagnostics/stats/{container_id}", a.withSession(a.handleDiagnosticsStats))
	mux.HandleFunc("POST /api/diagnostics/bundle", a.withSession(a.handleDiagnosticsBundle))
	mux.HandleFunc("GET /api/ws/metrics", a.withSession(a.handleMetricsWebSocket))
	mux.HandleFunc("GET /api/ws/exec", a.withSession(a.handleExecWebSocket))
	mux.HandleFunc("/", a.handleUI)
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	api.WriteOK(r.Context(), w, map[string]any{
		"status":  "ok",
		"mode":    a.cfg.Mode.String(),
		"service": "podorel-web",
	})
}

type passwordLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *App) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	var req passwordLoginRequest
	if !decodeJSON(r, w, &req) {
		return
	}
	key := "password:" + strings.ToLower(strings.TrimSpace(req.Username))
	if a.throttled(key) {
		a.audit(r, "", "auth.login.password", "user", req.Username, "failure", map[string]any{"reason": "throttled"})
		api.WriteError(r.Context(), w, http.StatusUnauthorized, "AUTH_FAILED", "Invalid credentials.", nil)
		return
	}
	user, err := a.store.FindUserByUsername(r.Context(), req.Username)
	if err != nil || !auth.VerifyPassword(req.Password, user.PasswordHash) {
		a.recordFailure(key)
		a.audit(r, "", "auth.login.password", "user", req.Username, "failure", map[string]any{"reason": "invalid_credentials"})
		api.WriteError(r.Context(), w, http.StatusUnauthorized, "AUTH_FAILED", "Invalid credentials.", nil)
		return
	}
	a.clearFailures(key)
	created, err := a.store.CreateSession(r.Context(), user.ID, "", "admin_password", a.cfg.Auth.SessionTTL)
	if err != nil {
		api.WriteError(r.Context(), w, http.StatusInternalServerError, "SESSION_CREATE_FAILED", "Could not create session.", nil)
		return
	}
	a.setSessionCookie(w, created.SessionID, created.Session.ExpiresAt)
	a.audit(r, user.ID, "auth.login.password", "user", user.ID, "success", nil)
	api.WriteOK(r.Context(), w, map[string]any{
		"user":       map[string]any{"id": user.ID, "username": user.Username, "session_type": "admin_password"},
		"csrf_token": created.CSRFToken,
	})
}

type agentTokenLoginRequest struct {
	Token string `json:"token"`
}

func (a *App) handleAgentTokenLogin(w http.ResponseWriter, r *http.Request) {
	var req agentTokenLoginRequest
	if !decodeJSON(r, w, &req) {
		return
	}
	key := "agent-token:" + auth.HashToken(req.Token)
	if a.throttled(key) {
		a.audit(r, "", "auth.login.agent_token", "agent", "", "failure", map[string]any{"reason": "throttled"})
		api.WriteError(r.Context(), w, http.StatusUnauthorized, "AUTH_FAILED", "Invalid credentials.", nil)
		return
	}
	agent, err := a.store.FindAgentByToken(r.Context(), req.Token)
	if err != nil {
		a.recordFailure(key)
		a.audit(r, "", "auth.login.agent_token", "agent", "", "failure", map[string]any{"reason": "invalid_credentials"})
		api.WriteError(r.Context(), w, http.StatusUnauthorized, "AUTH_FAILED", "Invalid credentials.", nil)
		return
	}
	a.clearFailures(key)
	created, err := a.store.CreateSession(r.Context(), db.DefaultAdminUsername, agent.ID, "agent_token", a.cfg.Auth.SessionTTL)
	if err != nil {
		api.WriteError(r.Context(), w, http.StatusInternalServerError, "SESSION_CREATE_FAILED", "Could not create session.", nil)
		return
	}
	a.setSessionCookie(w, created.SessionID, created.Session.ExpiresAt)
	a.audit(r, db.DefaultAdminUsername, "auth.login.agent_token", "agent", agent.ID, "success", nil)
	api.WriteOK(r.Context(), w, map[string]any{
		"agent":      agent,
		"scope":      map[string]any{"agent_id": agent.ID, "linux_username": agent.LinuxUsername},
		"csrf_token": created.CSRFToken,
	})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireCSRF(w, r) {
		return
	}
	_ = a.store.DeleteSession(r.Context(), rawSessionID(r))
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	a.audit(r, session.UserID, "auth.logout", "session", session.ID, "success", nil)
	api.WriteOK(r.Context(), w, map[string]any{"logged_out": true})
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request, session db.Session) {
	csrfToken, err := a.store.RotateSessionCSRF(r.Context(), rawSessionID(r))
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, map[string]any{
		"user": map[string]any{
			"id":           session.UserID,
			"username":     session.Username,
			"session_type": session.SessionType,
			"agent_id":     session.AgentID,
		},
		"csrf_token": csrfToken,
	})
}

func (a *App) handleListAgents(w http.ResponseWriter, r *http.Request, _ db.Session) {
	agents, err := a.store.ListAgents(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, agents)
}

type registerAgentRequest struct {
	ID            string `json:"id"`
	LinuxUsername string `json:"linux_username"`
	LinuxUID      int    `json:"linux_uid"`
	SocketPath    string `json:"socket_path"`
}

func (a *App) handleRegisterAgent(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireAdminPasswordSession(w, r, session) || !a.requireCSRF(w, r) {
		return
	}
	var req registerAgentRequest
	if !decodeJSON(r, w, &req) {
		return
	}
	if req.LinuxUsername == "" {
		req.LinuxUsername = db.PrimaryLinuxUsername
	}
	if req.LinuxUID == 0 {
		req.LinuxUID = db.PrimaryLinuxUID
	}
	if req.SocketPath == "" {
		req.SocketPath = "/run/user/1000/podorel/podorel-agent.sock"
	}
	agent, err := a.store.UpsertAgent(r.Context(), db.Agent{
		ID:            req.ID,
		LinuxUsername: req.LinuxUsername,
		LinuxUID:      req.LinuxUID,
		SocketPath:    req.SocketPath,
		Status:        "registered",
	})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	created, err := a.store.RegisterAgentToken(r.Context(), agent.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, session.UserID, "agents.register", "agent", agent.ID, "success", map[string]any{"linux_username": agent.LinuxUsername})
	api.WriteOK(r.Context(), w, created)
}

func (a *App) handleRotateAgentToken(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireAdminPasswordSession(w, r, session) || !a.requireCSRF(w, r) {
		return
	}
	agentID := r.PathValue("id")
	if err := a.store.RevokeAgentTokens(r.Context(), agentID); err != nil {
		a.internalError(w, r, err)
		return
	}
	created, err := a.store.RegisterAgentToken(r.Context(), agentID)
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	a.audit(r, session.UserID, "agents.rotate_token", "agent", agentID, "success", nil)
	api.WriteOK(r.Context(), w, created)
}

func (a *App) handleAgentHealth(w http.ResponseWriter, r *http.Request, session db.Session) {
	agent, err := a.store.AgentByID(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	if !a.canAccessAgent(session, agent.ID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot access this agent.", nil)
		return
	}
	api.WriteOK(r.Context(), w, a.agentHealthStatus(r.Context(), agent))
}
