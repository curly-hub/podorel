package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const DefaultMigrationDir = "server/migrations"

type Migration struct {
	Version int
	Name    string
	SQL     string
}

func LoadMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, name, err := parseMigrationName(entry.Name())
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, Migration{
			Version: version,
			Name:    name,
			SQL:     string(content),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

func Apply(ctx context.Context, database *sql.DB, migrations []Migration) error {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	for _, migration := range migrations {
		if _, err := database.ExecContext(ctx, migration.SQL); err != nil {
			return fmt.Errorf("apply migration %03d_%s: %w", migration.Version, migration.Name, err)
		}
		if _, err := database.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, name) VALUES(?, ?)`, migration.Version, migration.Name); err != nil {
			return err
		}
	}
	return nil
}

func parseMigrationName(filename string) (int, string, error) {
	trimmed := strings.TrimSuffix(filename, ".sql")
	prefix, name, ok := strings.Cut(trimmed, "_")
	if !ok {
		return 0, "", fmt.Errorf("invalid migration filename %q", filename)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, "", fmt.Errorf("invalid migration version in %q: %w", filename, err)
	}
	if name == "" {
		return 0, "", fmt.Errorf("invalid migration name in %q", filename)
	}
	return version, name, nil
}
