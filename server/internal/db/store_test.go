package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/curly-hub/podorel/server/internal/auth"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenMemory(context.Background(), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestBootstrapCreatesAdminAgentAndSelfPod(t *testing.T) {
	store := testStore(t)
	if err := store.Bootstrap(context.Background(), "secret-password"); err != nil {
		t.Fatal(err)
	}
	user, err := store.FindUserByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyPassword("secret-password", user.PasswordHash) {
		t.Fatal("admin password hash did not verify")
	}
	agents, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].ID != PrimaryAgentID {
		t.Fatalf("agents = %#v", agents)
	}
	if agents[0].SocketPath != PrimaryAgentSocket {
		t.Fatalf("primary socket path = %q", agents[0].SocketPath)
	}
	pods, err := store.ListPods(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].Name != "podorel-web" {
		t.Fatalf("pods = %#v", pods)
	}
}

func TestBootstrapUsesConfiguredPrimaryAgentSocket(t *testing.T) {
	store := testStore(t)
	if err := store.BootstrapWithOptions(context.Background(), BootstrapOptions{
		AdminPassword:          "secret-password",
		PrimaryAgentSocketPath: "/tmp/podorel-dev/podorel-agent.sock",
	}); err != nil {
		t.Fatal(err)
	}
	agent, err := store.AgentByID(context.Background(), PrimaryAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if agent.SocketPath != "/tmp/podorel-dev/podorel-agent.sock" {
		t.Fatalf("primary socket path = %q", agent.SocketPath)
	}
}

func TestTouchAgentUpdatesHeartbeat(t *testing.T) {
	store := testStore(t)
	if err := store.Bootstrap(context.Background(), "secret-password"); err != nil {
		t.Fatal(err)
	}
	if err := store.TouchAgent(context.Background(), PrimaryAgentID, "online"); err != nil {
		t.Fatal(err)
	}
	agent, err := store.AgentByID(context.Background(), PrimaryAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Status != "online" {
		t.Fatalf("status = %q", agent.Status)
	}
	if agent.LastSeenAt.IsZero() {
		t.Fatal("last_seen_at was not updated")
	}
}

func TestSessionAndCSRF(t *testing.T) {
	store := testStore(t)
	if err := store.Bootstrap(context.Background(), "secret-password"); err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(context.Background(), "admin", "", "admin_password", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SessionByID(context.Background(), session.SessionID); err != nil {
		t.Fatal(err)
	}
	if !store.ValidateCSRF(context.Background(), session.SessionID, session.CSRFToken) {
		t.Fatal("csrf did not verify")
	}
	if store.ValidateCSRF(context.Background(), session.SessionID, "wrong") {
		t.Fatal("wrong csrf verified")
	}
	rotated, err := store.RotateSessionCSRF(context.Background(), session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated == "" || rotated == session.CSRFToken {
		t.Fatalf("rotated csrf token = %q", rotated)
	}
	if store.ValidateCSRF(context.Background(), session.SessionID, session.CSRFToken) {
		t.Fatal("old csrf token still verified after rotation")
	}
	if !store.ValidateCSRF(context.Background(), session.SessionID, rotated) {
		t.Fatal("rotated csrf token did not verify")
	}
}

func TestAgentTokenShownOnceAndStoredHashed(t *testing.T) {
	store := testStore(t)
	if err := store.Bootstrap(context.Background(), "secret-password"); err != nil {
		t.Fatal(err)
	}
	created, err := store.RegisterAgentToken(context.Background(), PrimaryAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if created.Token == "" {
		t.Fatal("missing generated token")
	}
	agent, err := store.FindAgentByToken(context.Background(), created.Token)
	if err != nil {
		t.Fatal(err)
	}
	if agent.ID != PrimaryAgentID {
		t.Fatalf("agent = %#v", agent)
	}
	if _, err := store.FindAgentByToken(context.Background(), created.Token+"x"); err == nil {
		t.Fatal("wrong token authenticated")
	}
}

func TestSecretMetadataDoesNotStoreRawSecret(t *testing.T) {
	store := testStore(t)
	if err := store.Bootstrap(context.Background(), "secret-password"); err != nil {
		t.Fatal(err)
	}
	secret, err := store.CreateSecretMetadata(context.Background(), SecretMetadata{
		AgentID:     PrimaryAgentID,
		SecretName:  "db-password",
		Fingerprint: auth.HashToken("raw-secret-value"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if secret.Fingerprint == "raw-secret-value" {
		t.Fatal("raw secret stored as fingerprint")
	}
	secrets, err := store.ListSecretsMetadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(secrets) != 1 || secrets[0].Fingerprint == "raw-secret-value" {
		t.Fatalf("secret metadata leaked raw value: %#v", secrets)
	}
}

func TestSecurityListsReturnEmptySlices(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	findings, err := store.ListSecurityFindings(ctx, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if findings == nil || len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}

	digests, err := store.ListImageDigests(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if digests == nil || len(digests) != 0 {
		t.Fatalf("digests = %#v", digests)
	}

	updates, err := store.ListHostPackageUpdates(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if updates == nil || len(updates) != 0 {
		t.Fatalf("updates = %#v", updates)
	}
}
func TestSecurityFindingsOrderBySeverity(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if err := store.Bootstrap(ctx, "secret-password"); err != nil {
		t.Fatal(err)
	}
	_, err := store.CreateSecurityScan(ctx, SecurityScan{
		ID:        "scan-order",
		AgentID:   PrimaryAgentID,
		Status:    "complete",
		Scanner:   "trivy",
		StartedAt: time.Now(),
		Summary:   map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSecurityFindings(ctx, []SecurityFinding{
		{ScanID: "scan-order", Target: "image", VulnerabilityID: "low-cve", Severity: "low"},
		{ScanID: "scan-order", Target: "image", VulnerabilityID: "critical-cve", Severity: "critical"},
		{ScanID: "scan-order", Target: "image", VulnerabilityID: "medium-cve", Severity: "medium"},
		{ScanID: "scan-order", Target: "image", VulnerabilityID: "high-cve", Severity: "HIGH"},
		{ScanID: "scan-order", Target: "image", VulnerabilityID: "unknown-cve", Severity: "unknown"},
	}); err != nil {
		t.Fatal(err)
	}

	listed, err := store.ListSecurityFindings(ctx, "scan-order", 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"critical-cve", "high-cve", "medium-cve", "low-cve", "unknown-cve"}
	if len(listed) != len(want) {
		t.Fatalf("listed %d findings, want %d: %#v", len(listed), len(want), listed)
	}
	for i, expected := range want {
		if listed[i].VulnerabilityID != expected {
			t.Fatalf("finding order at %d = %q, want %q; all = %#v", i, listed[i].VulnerabilityID, expected, listed)
		}
	}
}
