package config

import (
	"os"
	"path/filepath"
	"testing"

	podorelruntime "github.com/curly-hub/podorel/internal/runtime"
)

func TestLoadDevelopmentMode(t *testing.T) {
	cfg, err := Load([]string{"--development", "--socket-path", "/tmp/podorel.sock"}, func(key string) string {
		if key == "XDG_RUNTIME_DIR" {
			return "/run/user/1000"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != podorelruntime.Development {
		t.Fatalf("mode = %q", cfg.Mode)
	}
	if cfg.SocketPath != "/tmp/podorel.sock" {
		t.Fatalf("socket path = %q", cfg.SocketPath)
	}
}

func TestValidateTokenFilePermissionsRejectsGroupReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTokenFilePermissions(path); err == nil {
		t.Fatal("expected insecure token permissions to fail")
	}
}

func TestValidateTokenFilePermissionsAllowsUserOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTokenFilePermissions(path); err != nil {
		t.Fatal(err)
	}
}
