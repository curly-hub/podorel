package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/curly-hub/podorel/internal/correlation"
	"github.com/curly-hub/podorel/server/internal/auth"
)

const (
	PrimaryAgentID       = "primary"
	PrimaryLinuxUsername = "current"
	PrimaryLinuxUID      = 1000
	PrimaryAgentSocket   = "/run/user/1000/podorel/podorel-agent.sock"
	DefaultAdminUsername = "admin"
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

type BootstrapOptions struct {
	AdminPassword          string
	PrimaryAgentSocketPath string
}

type User struct {
	ID           string
	Username     string
	PasswordHash string
}

type Session struct {
	ID            string
	UserID        string
	Username      string
	AgentID       string
	SessionType   string
	CSRFTokenHash string
	ExpiresAt     time.Time
}

type CreatedSession struct {
	SessionID string
	CSRFToken string
	Session   Session
}

type Agent struct {
	ID            string    `json:"id"`
	LinuxUsername string    `json:"linux_username"`
	LinuxUID      int       `json:"linux_uid"`
	SocketPath    string    `json:"socket_path"`
	Status        string    `json:"status"`
	LastSeenAt    time.Time `json:"last_seen_at,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreatedAgentToken struct {
	Agent Agent  `json:"agent"`
	Token string `json:"token"`
}

type Pod struct {
	ID          string    `json:"id"`
	AgentID     string    `json:"agent_id"`
	PodmanPodID string    `json:"podman_pod_id"`
	Name        string    `json:"name"`
	State       string    `json:"state"`
	Health      string    `json:"health"`
	CreatedAt   time.Time `json:"created_at"`
	ObservedAt  time.Time `json:"observed_at"`
	RawJSON     string    `json:"raw_json"`
}

type Container struct {
	ID                string    `json:"id"`
	AgentID           string    `json:"agent_id"`
	PodID             string    `json:"pod_id"`
	PodmanContainerID string    `json:"podman_container_id"`
	Name              string    `json:"name"`
	Image             string    `json:"image"`
	State             string    `json:"state"`
	Health            string    `json:"health"`
	CreatedAt         time.Time `json:"created_at"`
	ObservedAt        time.Time `json:"observed_at"`
	RawJSON           string    `json:"raw_json"`
}

type ResourceSample struct {
	ID                  int64     `json:"id"`
	AgentID             string    `json:"agent_id"`
	PodID               string    `json:"pod_id"`
	ContainerID         string    `json:"container_id"`
	SampledAt           time.Time `json:"sampled_at"`
	CPUPodmanRaw        string    `json:"cpu_podman_raw"`
	CPUPercentHostTotal float64   `json:"cpu_percent_host_total"`
	MemoryPodmanRaw     string    `json:"memory_podman_raw"`
	MemoryBytes         uint64    `json:"memory_bytes"`
	RawJSON             string    `json:"raw_json"`
}

type AuditEvent struct {
	ID            int64          `json:"id"`
	CreatedAt     time.Time      `json:"created_at"`
	ActorUserID   string         `json:"actor_user_id"`
	AgentID       string         `json:"agent_id"`
	Action        string         `json:"action"`
	TargetType    string         `json:"target_type"`
	TargetID      string         `json:"target_id"`
	Result        string         `json:"result"`
	CorrelationID string         `json:"correlation_id"`
	Details       map[string]any `json:"details"`
}

type SecurityScan struct {
	ID             string         `json:"id"`
	AgentID        string         `json:"agent_id"`
	Status         string         `json:"status"`
	Scanner        string         `json:"scanner"`
	ScannerVersion string         `json:"scanner_version"`
	StartedAt      time.Time      `json:"started_at"`
	FinishedAt     time.Time      `json:"finished_at,omitempty"`
	Summary        map[string]any `json:"summary"`
	ErrorCode      string         `json:"error_code,omitempty"`
	ErrorMessage   string         `json:"error_message,omitempty"`
}

type SecurityFinding struct {
	ID               int64  `json:"id"`
	ScanID           string `json:"scan_id"`
	ImageDigest      string `json:"image_digest"`
	Target           string `json:"target"`
	VulnerabilityID  string `json:"vulnerability_id"`
	Severity         string `json:"severity"`
	Title            string `json:"title"`
	PackageName      string `json:"package_name"`
	InstalledVersion string `json:"installed_version"`
	FixedVersion     string `json:"fixed_version"`
	RawJSON          string `json:"raw_json"`
}

type ImageDigest struct {
	ID              int64     `json:"id"`
	AgentID         string    `json:"agent_id"`
	ImageName       string    `json:"image_name"`
	LocalDigest     string    `json:"local_digest"`
	RemoteDigest    string    `json:"remote_digest"`
	UpdateAvailable bool      `json:"update_available"`
	CheckedAt       time.Time `json:"checked_at"`
	ErrorMessage    string    `json:"error_message"`
}

type HostPackageUpdate struct {
	ID               int64     `json:"id"`
	AgentID          string    `json:"agent_id"`
	PackageName      string    `json:"package_name"`
	InstalledVersion string    `json:"installed_version"`
	AvailableVersion string    `json:"available_version"`
	UpdateAvailable  bool      `json:"update_available"`
	CheckedAt        time.Time `json:"checked_at"`
	RawJSON          string    `json:"raw_json"`
}

type ImageBuild struct {
	ID             string         `json:"id"`
	AgentID        string         `json:"agent_id"`
	ImageName      string         `json:"image_name"`
	DockerfileHash string         `json:"dockerfile_hash"`
	Status         string         `json:"status"`
	StartedAt      time.Time      `json:"started_at"`
	FinishedAt     time.Time      `json:"finished_at,omitempty"`
	Metadata       map[string]any `json:"metadata"`
}

type Setting struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type SecretMetadata struct {
	ID                string    `json:"id"`
	AgentID           string    `json:"agent_id"`
	SecretName        string    `json:"secret_name"`
	Fingerprint       string    `json:"fingerprint"`
	UsedByPodID       string    `json:"used_by_pod_id"`
	UsedByContainerID string    `json:"used_by_container_id"`
	CreatedAt         time.Time `json:"created_at"`
}

type DebugTrace struct {
	ID            int64          `json:"id"`
	CreatedAt     time.Time      `json:"created_at"`
	Mode          string         `json:"mode"`
	Component     string         `json:"component"`
	Operation     string         `json:"operation"`
	CorrelationID string         `json:"correlation_id"`
	AgentID       string         `json:"agent_id"`
	TargetType    string         `json:"target_type"`
	TargetID      string         `json:"target_id"`
	Trace         map[string]any `json:"trace"`
}

func Open(ctx context.Context, path string, migrationsDir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	migrations, err := LoadMigrations(migrationsDir)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := Apply(ctx, database, migrations); err != nil {
		_ = database.Close()
		return nil, err
	}
	return &Store{db: database, now: func() time.Time { return time.Now().UTC() }}, nil
}

func OpenMemory(ctx context.Context, migrationsDir string) (*Store, error) {
	database, err := sql.Open("sqlite3", "file:podorel-test?mode=memory&cache=shared&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	migrations, err := LoadMigrations(migrationsDir)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := Apply(ctx, database, migrations); err != nil {
		_ = database.Close()
		return nil, err
	}
	return &Store{db: database, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Bootstrap(ctx context.Context, adminPassword string) error {
	return s.BootstrapWithOptions(ctx, BootstrapOptions{AdminPassword: adminPassword})
}

func (s *Store) BootstrapWithOptions(ctx context.Context, opts BootstrapOptions) error {
	adminPassword := opts.AdminPassword
	if adminPassword == "" {
		adminPassword = "podorel-development-password"
	}
	primarySocketPath := opts.PrimaryAgentSocketPath
	if primarySocketPath == "" {
		primarySocketPath = PrimaryAgentSocket
	}
	hash, err := auth.HashPassword(adminPassword)
	if err != nil {
		return err
	}
	now := s.now()
	if _, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO users(id, username, password_hash, created_at, updated_at)
		VALUES('admin', ?, ?, ?, ?)`, DefaultAdminUsername, hash, now, now); err != nil {
		return err
	}
	agent := Agent{
		ID:            PrimaryAgentID,
		LinuxUsername: PrimaryLinuxUsername,
		LinuxUID:      PrimaryLinuxUID,
		SocketPath:    primarySocketPath,
		Status:        "registered",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := s.UpsertAgent(ctx, agent); err != nil {
		return err
	}
	if err := s.SeedSelfPod(ctx, PrimaryAgentID); err != nil {
		return err
	}
	return nil
}

func (s *Store) FindUserByUsername(ctx context.Context, username string) (User, error) {
	var user User
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash FROM users WHERE username = ?`, username).
		Scan(&user.ID, &user.Username, &user.PasswordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return user, nil
}

func (s *Store) CreateSession(ctx context.Context, userID string, agentID string, sessionType string, ttl time.Duration) (CreatedSession, error) {
	sessionID, err := auth.NewToken()
	if err != nil {
		return CreatedSession{}, err
	}
	csrfToken, err := auth.NewToken()
	if err != nil {
		return CreatedSession{}, err
	}
	now := s.now()
	expires := now.Add(ttl)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions(id, user_id, agent_id, session_type, csrf_token_hash, expires_at, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		auth.HashToken(sessionID), userID, nullable(agentID), sessionType, auth.HashToken(csrfToken), expires, now, now)
	if err != nil {
		return CreatedSession{}, err
	}
	session, err := s.SessionByID(ctx, sessionID)
	if err != nil {
		return CreatedSession{}, err
	}
	return CreatedSession{
		SessionID: sessionID,
		CSRFToken: csrfToken,
		Session:   session,
	}, nil
}

func (s *Store) SessionByID(ctx context.Context, rawSessionID string) (Session, error) {
	var session Session
	var expires string
	err := s.db.QueryRowContext(ctx, `
		SELECT sessions.id, user_id, users.username, COALESCE(agent_id, ''), session_type, csrf_token_hash, expires_at
		FROM sessions
		JOIN users ON users.id = sessions.user_id
		WHERE sessions.id = ?`, auth.HashToken(rawSessionID)).
		Scan(&session.ID, &session.UserID, &session.Username, &session.AgentID, &session.SessionType, &session.CSRFTokenHash, &expires)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, err
	}
	parsed, err := parseTime(expires)
	if err != nil {
		return Session{}, err
	}
	session.ExpiresAt = parsed
	if !session.ExpiresAt.After(s.now()) {
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (s *Store) ValidateCSRF(ctx context.Context, rawSessionID string, token string) bool {
	session, err := s.SessionByID(ctx, rawSessionID)
	if err != nil {
		return false
	}
	return auth.VerifyToken(token, session.CSRFTokenHash)
}

func (s *Store) RotateSessionCSRF(ctx context.Context, rawSessionID string) (string, error) {
	if _, err := s.SessionByID(ctx, rawSessionID); err != nil {
		return "", err
	}
	csrfToken, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE sessions SET csrf_token_hash = ?, updated_at = ? WHERE id = ?`, auth.HashToken(csrfToken), s.now(), auth.HashToken(rawSessionID))
	if err != nil {
		return "", err
	}
	return csrfToken, nil
}

func (s *Store) DeleteSession(ctx context.Context, rawSessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, auth.HashToken(rawSessionID))
	return err
}

func (s *Store) UpsertAgent(ctx context.Context, agent Agent) (Agent, error) {
	now := s.now()
	if agent.ID == "" {
		id, err := auth.NewToken()
		if err != nil {
			return Agent{}, err
		}
		agent.ID = "agent-" + id[:12]
	}
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = now
	}
	agent.UpdatedAt = now
	if agent.Status == "" {
		agent.Status = "registered"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents(id, linux_username, linux_uid, socket_path, status, last_seen_at, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			linux_username = excluded.linux_username,
			linux_uid = excluded.linux_uid,
			socket_path = excluded.socket_path,
			status = excluded.status,
			last_seen_at = excluded.last_seen_at,
			updated_at = excluded.updated_at`,
		agent.ID, agent.LinuxUsername, agent.LinuxUID, agent.SocketPath, agent.Status, nullableTime(agent.LastSeenAt), agent.CreatedAt, agent.UpdatedAt)
	return agent, err
}

func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, linux_username, linux_uid, socket_path, status, COALESCE(last_seen_at, ''), created_at, updated_at FROM agents ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []Agent
	for rows.Next() {
		var agent Agent
		var lastSeen, created, updated string
		if err := rows.Scan(&agent.ID, &agent.LinuxUsername, &agent.LinuxUID, &agent.SocketPath, &agent.Status, &lastSeen, &created, &updated); err != nil {
			return nil, err
		}
		agent.LastSeenAt, _ = parseOptionalTime(lastSeen)
		agent.CreatedAt, _ = parseTime(created)
		agent.UpdatedAt, _ = parseTime(updated)
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

func (s *Store) RegisterAgentToken(ctx context.Context, agentID string) (CreatedAgentToken, error) {
	agent, err := s.AgentByID(ctx, agentID)
	if err != nil {
		return CreatedAgentToken{}, err
	}
	token, err := auth.NewToken()
	if err != nil {
		return CreatedAgentToken{}, err
	}
	tokenID, err := auth.NewToken()
	if err != nil {
		return CreatedAgentToken{}, err
	}
	now := s.now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO agent_tokens(id, agent_id, token_hash, created_at) VALUES(?, ?, ?, ?)`, "agtok-"+tokenID[:12], agentID, auth.HashToken(token), now); err != nil {
		return CreatedAgentToken{}, err
	}
	return CreatedAgentToken{Agent: agent, Token: token}, nil
}

func (s *Store) AgentByID(ctx context.Context, agentID string) (Agent, error) {
	var agent Agent
	var lastSeen, created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id, linux_username, linux_uid, socket_path, status, COALESCE(last_seen_at, ''), created_at, updated_at FROM agents WHERE id = ?`, agentID).
		Scan(&agent.ID, &agent.LinuxUsername, &agent.LinuxUID, &agent.SocketPath, &agent.Status, &lastSeen, &created, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Agent{}, ErrNotFound
		}
		return Agent{}, err
	}
	agent.LastSeenAt, _ = parseOptionalTime(lastSeen)
	agent.CreatedAt, _ = parseTime(created)
	agent.UpdatedAt, _ = parseTime(updated)
	return agent, nil
}

func (s *Store) TouchAgent(ctx context.Context, agentID string, status string) error {
	if status == "" {
		status = "online"
	}
	now := s.now()
	_, err := s.db.ExecContext(ctx, `UPDATE agents SET status = ?, last_seen_at = ?, updated_at = ? WHERE id = ?`, status, now, now, agentID)
	return err
}

func (s *Store) FindAgentByToken(ctx context.Context, rawToken string) (Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT agent_id, token_hash FROM agent_tokens WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`, s.now())
	if err != nil {
		return Agent{}, err
	}
	defer rows.Close()
	matchedAgentID := ""
	for rows.Next() {
		var agentID, hash string
		if err := rows.Scan(&agentID, &hash); err != nil {
			return Agent{}, err
		}
		if auth.VerifyToken(rawToken, hash) {
			matchedAgentID = agentID
			break
		}
	}
	if err := rows.Err(); err != nil {
		return Agent{}, err
	}
	if matchedAgentID != "" {
		if err := rows.Close(); err != nil {
			return Agent{}, err
		}
		return s.AgentByID(ctx, matchedAgentID)
	}
	return Agent{}, ErrNotFound
}

func (s *Store) RevokeAgentTokens(ctx context.Context, agentID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_tokens SET revoked_at = ? WHERE agent_id = ? AND revoked_at IS NULL`, s.now(), agentID)
	return err
}

func (s *Store) SeedSelfPod(ctx context.Context, agentID string) error {
	now := s.now()
	rawPod := `{"self":true,"managed_by":"podorel"}`
	if _, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO pods(id, agent_id, podman_pod_id, name, state, health, created_at, observed_at, raw_json)
		VALUES('podorel-self-pod', ?, 'podorel-self-pod', 'podorel-web', 'unknown', 'unknown', ?, ?, ?)`,
		agentID, now, now, rawPod); err != nil {
		return err
	}
	rawContainer := `{"self":true,"managed_by":"podorel"}`
	if _, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO containers(id, agent_id, pod_id, podman_container_id, name, image, state, health, created_at, observed_at, raw_json)
		VALUES('podorel-self-container', ?, 'podorel-self-pod', 'podorel-self-container', 'podorel-web', 'podorel-web:latest', 'unknown', 'unknown', ?, ?, ?)`,
		agentID, now, now, rawContainer); err != nil {
		return err
	}
	return s.InsertResourceSample(ctx, ResourceSample{
		AgentID:             agentID,
		PodID:               "podorel-self-pod",
		ContainerID:         "podorel-self-container",
		SampledAt:           now,
		CPUPodmanRaw:        "0.00%",
		CPUPercentHostTotal: 0,
		MemoryPodmanRaw:     "0B / 0B",
		MemoryBytes:         0,
		RawJSON:             rawContainer,
	})
}

func (s *Store) ListPods(ctx context.Context, agentID string) ([]Pod, error) {
	query := `SELECT id, agent_id, podman_pod_id, name, state, COALESCE(health, ''), COALESCE(created_at, ''), observed_at, raw_json FROM pods`
	args := []any{}
	if agentID != "" {
		query += ` WHERE agent_id = ?`
		args = append(args, agentID)
	}
	query += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pods []Pod
	for rows.Next() {
		pod, err := scanPod(rows)
		if err != nil {
			return nil, err
		}
		pods = append(pods, pod)
	}
	return pods, rows.Err()
}

func (s *Store) PodByID(ctx context.Context, id string) (Pod, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, agent_id, podman_pod_id, name, state, COALESCE(health, ''), COALESCE(created_at, ''), observed_at, raw_json FROM pods WHERE id = ? OR podman_pod_id = ?`, id, id)
	return scanPod(row)
}

func (s *Store) UpdatePodState(ctx context.Context, id string, state string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pods SET state = ?, observed_at = ? WHERE id = ? OR podman_pod_id = ?`, state, s.now(), id, id)
	return err
}

func (s *Store) DeletePod(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM resource_samples WHERE pod_id IN (SELECT id FROM pods WHERE id = ? OR podman_pod_id = ?)`, id, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM resource_samples WHERE container_id IN (SELECT id FROM containers WHERE pod_id IN (SELECT id FROM pods WHERE id = ? OR podman_pod_id = ?))`, id, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM containers WHERE pod_id IN (SELECT id FROM pods WHERE id = ? OR podman_pod_id = ?)`, id, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pods WHERE id = ? OR podman_pod_id = ?`, id, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) InsertPod(ctx context.Context, pod Pod) error {
	now := s.now()
	if pod.ID == "" {
		pod.ID = pod.PodmanPodID
	}
	if pod.AgentID == "" {
		pod.AgentID = PrimaryAgentID
	}
	if pod.CreatedAt.IsZero() {
		pod.CreatedAt = now
	}
	if pod.ObservedAt.IsZero() {
		pod.ObservedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO pods(id, agent_id, podman_pod_id, name, state, health, created_at, observed_at, raw_json)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pod.ID, pod.AgentID, pod.PodmanPodID, pod.Name, pod.State, pod.Health, pod.CreatedAt, pod.ObservedAt, pod.RawJSON)
	return err
}

func (s *Store) ListContainers(ctx context.Context, podID string, agentID string) ([]Container, error) {
	query := `SELECT id, agent_id, COALESCE(pod_id, ''), podman_container_id, name, COALESCE(image, ''), state, COALESCE(health, ''), COALESCE(created_at, ''), observed_at, raw_json FROM containers`
	args := []any{}
	where := []string{}
	if podID != "" {
		where = append(where, `pod_id = ?`)
		args = append(args, podID)
	}
	if agentID != "" {
		where = append(where, `agent_id = ?`)
		args = append(args, agentID)
	}
	if len(where) > 0 {
		query += ` WHERE ` + joinAnd(where)
	}
	query += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var containers []Container
	for rows.Next() {
		container, err := scanContainer(rows)
		if err != nil {
			return nil, err
		}
		containers = append(containers, container)
	}
	return containers, rows.Err()
}

func (s *Store) ContainerByID(ctx context.Context, id string) (Container, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, agent_id, COALESCE(pod_id, ''), podman_container_id, name, COALESCE(image, ''), state, COALESCE(health, ''), COALESCE(created_at, ''), observed_at, raw_json FROM containers WHERE id = ? OR podman_container_id = ?`, id, id)
	return scanContainer(row)
}

func (s *Store) UpdateContainerState(ctx context.Context, id string, state string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE containers SET state = ?, observed_at = ? WHERE id = ? OR podman_container_id = ?`, state, s.now(), id, id)
	return err
}

func (s *Store) DeleteContainer(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM resource_samples WHERE container_id IN (SELECT id FROM containers WHERE id = ? OR podman_container_id = ?)`, id, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM containers WHERE id = ? OR podman_container_id = ?`, id, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) InsertContainer(ctx context.Context, container Container) error {
	now := s.now()
	if container.ID == "" {
		container.ID = container.PodmanContainerID
	}
	if container.AgentID == "" {
		container.AgentID = PrimaryAgentID
	}
	if container.CreatedAt.IsZero() {
		container.CreatedAt = now
	}
	if container.ObservedAt.IsZero() {
		container.ObservedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO containers(id, agent_id, pod_id, podman_container_id, name, image, state, health, created_at, observed_at, raw_json)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		container.ID, container.AgentID, nullable(container.PodID), container.PodmanContainerID, container.Name, container.Image, container.State, container.Health, container.CreatedAt, container.ObservedAt, container.RawJSON)
	return err
}

func (s *Store) InsertResourceSample(ctx context.Context, sample ResourceSample) error {
	if sample.SampledAt.IsZero() {
		sample.SampledAt = s.now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO resource_samples(agent_id, pod_id, container_id, sampled_at, cpu_podman_raw, cpu_percent_host_total, memory_podman_raw, memory_bytes, raw_json)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sample.AgentID, nullable(sample.PodID), nullable(sample.ContainerID), sample.SampledAt, sample.CPUPodmanRaw, sample.CPUPercentHostTotal, sample.MemoryPodmanRaw, sample.MemoryBytes, sample.RawJSON)
	return err
}

func (s *Store) CurrentStats(ctx context.Context, agentID string) ([]ResourceSample, error) {
	query := `
		SELECT rs.id, rs.agent_id, COALESCE(rs.pod_id, ''), COALESCE(rs.container_id, ''), rs.sampled_at, COALESCE(rs.cpu_podman_raw, ''), COALESCE(rs.cpu_percent_host_total, 0), COALESCE(rs.memory_podman_raw, ''), COALESCE(rs.memory_bytes, 0), COALESCE(rs.raw_json, '')
		FROM resource_samples rs
		JOIN (
			SELECT COALESCE(container_id, pod_id, agent_id) AS target, MAX(sampled_at) AS sampled_at
			FROM resource_samples
			GROUP BY COALESCE(container_id, pod_id, agent_id)
		) latest ON latest.target = COALESCE(rs.container_id, rs.pod_id, rs.agent_id) AND latest.sampled_at = rs.sampled_at`
	args := []any{}
	if agentID != "" {
		query += ` WHERE rs.agent_id = ?`
		args = append(args, agentID)
	}
	query += ` ORDER BY rs.sampled_at DESC`
	return s.scanResourceSamples(ctx, query, args...)
}

func (s *Store) StatsHistory(ctx context.Context, since time.Time, agentID string) ([]ResourceSample, error) {
	query := `SELECT id, agent_id, COALESCE(pod_id, ''), COALESCE(container_id, ''), sampled_at, COALESCE(cpu_podman_raw, ''), COALESCE(cpu_percent_host_total, 0), COALESCE(memory_podman_raw, ''), COALESCE(memory_bytes, 0), COALESCE(raw_json, '') FROM resource_samples WHERE sampled_at >= ?`
	args := []any{since}
	if agentID != "" {
		query += ` AND agent_id = ?`
		args = append(args, agentID)
	}
	query += ` ORDER BY sampled_at DESC`
	return s.scanResourceSamples(ctx, query, args...)
}

func (s *Store) scanResourceSamples(ctx context.Context, query string, args ...any) ([]ResourceSample, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var samples []ResourceSample
	for rows.Next() {
		var sample ResourceSample
		var sampledAt string
		if err := rows.Scan(&sample.ID, &sample.AgentID, &sample.PodID, &sample.ContainerID, &sampledAt, &sample.CPUPodmanRaw, &sample.CPUPercentHostTotal, &sample.MemoryPodmanRaw, &sample.MemoryBytes, &sample.RawJSON); err != nil {
			return nil, err
		}
		sample.SampledAt, _ = parseTime(sampledAt)
		samples = append(samples, sample)
	}
	return samples, rows.Err()
}

func (s *Store) WriteAudit(ctx context.Context, event AuditEvent) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = s.now()
	}
	if event.CorrelationID == "" {
		event.CorrelationID = correlation.FromContextOrNew(ctx)
	}
	details, err := json.Marshal(event.Details)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO audit_events(created_at, actor_user_id, agent_id, action, target_type, target_id, result, correlation_id, details_json)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.CreatedAt, nullable(event.ActorUserID), nullable(event.AgentID), event.Action, event.TargetType, nullable(event.TargetID), event.Result, event.CorrelationID, string(details))
	return err
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, created_at, COALESCE(actor_user_id, ''), COALESCE(agent_id, ''), action, target_type, COALESCE(target_id, ''), result, correlation_id, COALESCE(details_json, '{}') FROM audit_events ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		var created, details string
		if err := rows.Scan(&event.ID, &created, &event.ActorUserID, &event.AgentID, &event.Action, &event.TargetType, &event.TargetID, &event.Result, &event.CorrelationID, &details); err != nil {
			return nil, err
		}
		event.CreatedAt, _ = parseTime(created)
		_ = json.Unmarshal([]byte(details), &event.Details)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) CreateSecurityScan(ctx context.Context, scan SecurityScan) (SecurityScan, error) {
	if scan.ID == "" {
		id, err := auth.NewToken()
		if err != nil {
			return SecurityScan{}, err
		}
		scan.ID = "scan-" + id[:12]
	}
	if scan.StartedAt.IsZero() {
		scan.StartedAt = s.now()
	}
	summary, err := json.Marshal(scan.Summary)
	if err != nil {
		return SecurityScan{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO security_scans(id, agent_id, status, scanner, scanner_version, started_at, finished_at, summary_json, error_code, error_message)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		scan.ID, scan.AgentID, scan.Status, scan.Scanner, scan.ScannerVersion, scan.StartedAt, nullableTime(scan.FinishedAt), string(summary), nullable(scan.ErrorCode), nullable(scan.ErrorMessage))
	if err != nil {
		return SecurityScan{}, err
	}
	return scan, nil
}

func (s *Store) ListSecurityScans(ctx context.Context, limit int) ([]SecurityScan, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, agent_id, status, scanner, COALESCE(scanner_version, ''), started_at, COALESCE(finished_at, ''), COALESCE(summary_json, '{}'), COALESCE(error_code, ''), COALESCE(error_message, '') FROM security_scans ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scans []SecurityScan
	for rows.Next() {
		var scan SecurityScan
		var started, finished, summary string
		if err := rows.Scan(&scan.ID, &scan.AgentID, &scan.Status, &scan.Scanner, &scan.ScannerVersion, &started, &finished, &summary, &scan.ErrorCode, &scan.ErrorMessage); err != nil {
			return nil, err
		}
		scan.StartedAt, _ = parseTime(started)
		scan.FinishedAt, _ = parseOptionalTime(finished)
		_ = json.Unmarshal([]byte(summary), &scan.Summary)
		scans = append(scans, scan)
	}
	return scans, rows.Err()
}

func (s *Store) SecurityScanByID(ctx context.Context, id string) (SecurityScan, error) {
	var scan SecurityScan
	var started, finished, summary string
	err := s.db.QueryRowContext(ctx, `SELECT id, agent_id, status, scanner, COALESCE(scanner_version, ''), started_at, COALESCE(finished_at, ''), COALESCE(summary_json, '{}'), COALESCE(error_code, ''), COALESCE(error_message, '') FROM security_scans WHERE id = ?`, id).
		Scan(&scan.ID, &scan.AgentID, &scan.Status, &scan.Scanner, &scan.ScannerVersion, &started, &finished, &summary, &scan.ErrorCode, &scan.ErrorMessage)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SecurityScan{}, ErrNotFound
		}
		return SecurityScan{}, err
	}
	scan.StartedAt, _ = parseTime(started)
	scan.FinishedAt, _ = parseOptionalTime(finished)
	_ = json.Unmarshal([]byte(summary), &scan.Summary)
	return scan, nil
}

func (s *Store) CreateSecretMetadata(ctx context.Context, secret SecretMetadata) (SecretMetadata, error) {
	if secret.ID == "" {
		id, err := auth.NewToken()
		if err != nil {
			return SecretMetadata{}, err
		}
		secret.ID = "secret-" + id[:12]
	}
	if secret.CreatedAt.IsZero() {
		secret.CreatedAt = s.now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO secrets_metadata(id, agent_id, secret_name, fingerprint, used_by_pod_id, used_by_container_id, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		secret.ID, secret.AgentID, secret.SecretName, nullable(secret.Fingerprint), nullable(secret.UsedByPodID), nullable(secret.UsedByContainerID), secret.CreatedAt)
	return secret, err
}

func (s *Store) ListSecretsMetadata(ctx context.Context) ([]SecretMetadata, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, agent_id, secret_name, COALESCE(fingerprint, ''), COALESCE(used_by_pod_id, ''), COALESCE(used_by_container_id, ''), created_at FROM secrets_metadata ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var secrets []SecretMetadata
	for rows.Next() {
		var secret SecretMetadata
		var created string
		if err := rows.Scan(&secret.ID, &secret.AgentID, &secret.SecretName, &secret.Fingerprint, &secret.UsedByPodID, &secret.UsedByContainerID, &created); err != nil {
			return nil, err
		}
		secret.CreatedAt, _ = parseTime(created)
		secrets = append(secrets, secret)
	}
	return secrets, rows.Err()
}

func (s *Store) AddDebugTrace(ctx context.Context, trace DebugTrace) error {
	if trace.CreatedAt.IsZero() {
		trace.CreatedAt = s.now()
	}
	if trace.CorrelationID == "" {
		trace.CorrelationID = correlation.FromContextOrNew(ctx)
	}
	payload, err := json.Marshal(trace.Trace)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO debug_traces(created_at, mode, component, operation, correlation_id, agent_id, target_type, target_id, trace_json) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trace.CreatedAt, trace.Mode, trace.Component, trace.Operation, trace.CorrelationID, nullable(trace.AgentID), nullable(trace.TargetType), nullable(trace.TargetID), string(payload))
	return err
}

func (s *Store) ListDebugTraces(ctx context.Context, correlationID string, limit int) ([]DebugTrace, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT id, created_at, mode, component, operation, correlation_id, COALESCE(agent_id, ''), COALESCE(target_type, ''), COALESCE(target_id, ''), trace_json FROM debug_traces`
	args := []any{}
	if correlationID != "" {
		query += ` WHERE correlation_id = ?`
		args = append(args, correlationID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var traces []DebugTrace
	for rows.Next() {
		var trace DebugTrace
		var created, payload string
		if err := rows.Scan(&trace.ID, &created, &trace.Mode, &trace.Component, &trace.Operation, &trace.CorrelationID, &trace.AgentID, &trace.TargetType, &trace.TargetID, &payload); err != nil {
			return nil, err
		}
		trace.CreatedAt, _ = parseTime(created)
		_ = json.Unmarshal([]byte(payload), &trace.Trace)
		traces = append(traces, trace)
	}
	return traces, rows.Err()
}

func (s *Store) UpsertSetting(ctx context.Context, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO settings(key, value_json, updated_at)
		VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		key, string(payload), s.now())
	return err
}

func (s *Store) ListSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value_json FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := map[string]json.RawMessage{}
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		settings[key] = json.RawMessage(value)
	}
	return settings, rows.Err()
}

func (s *Store) CreateImageBuild(ctx context.Context, build ImageBuild) (ImageBuild, error) {
	if build.ID == "" {
		id, err := auth.NewToken()
		if err != nil {
			return ImageBuild{}, err
		}
		build.ID = "build-" + id[:12]
	}
	if build.StartedAt.IsZero() {
		build.StartedAt = s.now()
	}
	if build.Status == "" {
		build.Status = "queued"
	}
	metadata, err := json.Marshal(build.Metadata)
	if err != nil {
		return ImageBuild{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO image_builds(id, agent_id, image_name, dockerfile_hash, status, started_at, finished_at, metadata_json)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		build.ID, build.AgentID, build.ImageName, build.DockerfileHash, build.Status, build.StartedAt, nullableTime(build.FinishedAt), string(metadata))
	if err != nil {
		return ImageBuild{}, err
	}
	return build, nil
}

func (s *Store) UpdateImageBuild(ctx context.Context, build ImageBuild) error {
	metadata, err := json.Marshal(build.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE image_builds SET status = ?, finished_at = ?, metadata_json = ? WHERE id = ?`,
		build.Status, nullableTime(build.FinishedAt), string(metadata), build.ID)
	return err
}

func (s *Store) ImageBuildByID(ctx context.Context, id string) (ImageBuild, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, agent_id, image_name, dockerfile_hash, status, started_at, COALESCE(finished_at, ''), COALESCE(metadata_json, '{}') FROM image_builds WHERE id = ?`, id)
	return scanImageBuild(row)
}

func (s *Store) AppendImageBuildLog(ctx context.Context, id string, entry map[string]any) (ImageBuild, error) {
	build, err := s.ImageBuildByID(ctx, id)
	if err != nil {
		return ImageBuild{}, err
	}
	if build.Metadata == nil {
		build.Metadata = map[string]any{}
	}
	logs, _ := build.Metadata["logs"].([]any)
	logs = append(logs, entry)
	build.Metadata["logs"] = logs
	if err := s.UpdateImageBuild(ctx, build); err != nil {
		return ImageBuild{}, err
	}
	return build, nil
}

func (s *Store) InsertSecurityFindings(ctx context.Context, findings []SecurityFinding) error {
	for _, finding := range findings {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO security_findings(scan_id, image_digest, target, vulnerability_id, severity, title, package_name, installed_version, fixed_version, raw_json)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			finding.ScanID, nullable(finding.ImageDigest), finding.Target, finding.VulnerabilityID, finding.Severity, nullable(finding.Title), nullable(finding.PackageName), nullable(finding.InstalledVersion), nullable(finding.FixedVersion), nullable(finding.RawJSON))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListSecurityFindings(ctx context.Context, scanID string, limit int) ([]SecurityFinding, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	args := []any{}
	query := `SELECT id, scan_id, COALESCE(image_digest, ''), target, vulnerability_id, severity, COALESCE(title, ''), COALESCE(package_name, ''), COALESCE(installed_version, ''), COALESCE(fixed_version, ''), COALESCE(raw_json, '') FROM security_findings`
	if scanID != "" {
		query += ` WHERE scan_id = ?`
		args = append(args, scanID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var findings []SecurityFinding
	for rows.Next() {
		var finding SecurityFinding
		if err := rows.Scan(&finding.ID, &finding.ScanID, &finding.ImageDigest, &finding.Target, &finding.VulnerabilityID, &finding.Severity, &finding.Title, &finding.PackageName, &finding.InstalledVersion, &finding.FixedVersion, &finding.RawJSON); err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

func (s *Store) InsertImageDigest(ctx context.Context, digest ImageDigest) error {
	if digest.CheckedAt.IsZero() {
		digest.CheckedAt = s.now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO image_digests(agent_id, image_name, local_digest, remote_digest, update_available, checked_at, error_message)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		digest.AgentID, digest.ImageName, nullable(digest.LocalDigest), nullable(digest.RemoteDigest), boolInt(digest.UpdateAvailable), digest.CheckedAt, nullable(digest.ErrorMessage))
	return err
}

func (s *Store) ListImageDigests(ctx context.Context, limit int) ([]ImageDigest, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, agent_id, image_name, COALESCE(local_digest, ''), COALESCE(remote_digest, ''), update_available, checked_at, COALESCE(error_message, '') FROM image_digests ORDER BY checked_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var digests []ImageDigest
	for rows.Next() {
		var digest ImageDigest
		var checked string
		var update int
		if err := rows.Scan(&digest.ID, &digest.AgentID, &digest.ImageName, &digest.LocalDigest, &digest.RemoteDigest, &update, &checked, &digest.ErrorMessage); err != nil {
			return nil, err
		}
		digest.UpdateAvailable = update != 0
		digest.CheckedAt, _ = parseTime(checked)
		digests = append(digests, digest)
	}
	return digests, rows.Err()
}

func (s *Store) InsertHostPackageUpdate(ctx context.Context, update HostPackageUpdate) error {
	if update.CheckedAt.IsZero() {
		update.CheckedAt = s.now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO host_package_updates(agent_id, package_name, installed_version, available_version, update_available, checked_at, raw_json)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		update.AgentID, update.PackageName, nullable(update.InstalledVersion), nullable(update.AvailableVersion), boolInt(update.UpdateAvailable), update.CheckedAt, nullable(update.RawJSON))
	return err
}

func (s *Store) ListHostPackageUpdates(ctx context.Context, limit int) ([]HostPackageUpdate, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, agent_id, package_name, COALESCE(installed_version, ''), COALESCE(available_version, ''), update_available, checked_at, COALESCE(raw_json, '') FROM host_package_updates ORDER BY checked_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var updates []HostPackageUpdate
	for rows.Next() {
		var update HostPackageUpdate
		var checked string
		var available int
		if err := rows.Scan(&update.ID, &update.AgentID, &update.PackageName, &update.InstalledVersion, &update.AvailableVersion, &available, &checked, &update.RawJSON); err != nil {
			return nil, err
		}
		update.UpdateAvailable = available != 0
		update.CheckedAt, _ = parseTime(checked)
		updates = append(updates, update)
	}
	return updates, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPod(row scanner) (Pod, error) {
	var pod Pod
	var created, observed string
	err := row.Scan(&pod.ID, &pod.AgentID, &pod.PodmanPodID, &pod.Name, &pod.State, &pod.Health, &created, &observed, &pod.RawJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Pod{}, ErrNotFound
		}
		return Pod{}, err
	}
	pod.CreatedAt, _ = parseOptionalTime(created)
	pod.ObservedAt, _ = parseTime(observed)
	return pod, nil
}

func scanContainer(row scanner) (Container, error) {
	var container Container
	var created, observed string
	err := row.Scan(&container.ID, &container.AgentID, &container.PodID, &container.PodmanContainerID, &container.Name, &container.Image, &container.State, &container.Health, &created, &observed, &container.RawJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Container{}, ErrNotFound
		}
		return Container{}, err
	}
	container.CreatedAt, _ = parseOptionalTime(created)
	container.ObservedAt, _ = parseTime(observed)
	return container, nil
}

var ErrNotFound = errors.New("not found")

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return time.Parse("2006-01-02 15:04:05-07:00", value)
}

func parseOptionalTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return parseTime(value)
}

func scanImageBuild(row scanner) (ImageBuild, error) {
	var build ImageBuild
	var started, finished, metadata string
	err := row.Scan(&build.ID, &build.AgentID, &build.ImageName, &build.DockerfileHash, &build.Status, &started, &finished, &metadata)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ImageBuild{}, ErrNotFound
		}
		return ImageBuild{}, err
	}
	build.StartedAt, _ = parseTime(started)
	build.FinishedAt, _ = parseOptionalTime(finished)
	_ = json.Unmarshal([]byte(metadata), &build.Metadata)
	return build, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func joinAnd(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += " AND "
		}
		out += item
	}
	return out
}
