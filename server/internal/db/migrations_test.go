package db

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMigrationsDefault(t *testing.T) {
	migrations, err := LoadMigrations(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) == 0 {
		t.Fatal("expected at least one migration")
	}
	if migrations[0].Version != 1 {
		t.Fatalf("first migration version = %d, want 1", migrations[0].Version)
	}
	for _, table := range []string{"users", "agents", "pods", "containers", "audit_events", "debug_traces"} {
		if !strings.Contains(migrations[0].SQL, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("initial migration missing table %s", table)
		}
	}
}
