package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	podorelruntime "github.com/curly-hub/podorel/internal/runtime"
)

const (
	DefaultSocketName = "podorel-agent.sock"
	DefaultConfigDir  = ".config/podorel"
	DefaultTokenFile  = "agent-token"
)

type Config struct {
	Mode       podorelruntime.Mode
	SocketPath string
	TokenFile  string
}

type getenvFunc func(string) string

func Load(args []string, getenv getenvFunc) (Config, error) {
	fs := flag.NewFlagSet("podorel-agent", flag.ContinueOnError)
	development := fs.Bool("development", false, "run in development mode")
	production := fs.Bool("production", false, "run in production mode")
	socketPath := fs.String("socket-path", defaultSocketPath(getenv), "Unix socket path")
	tokenFile := fs.String("token-file", defaultTokenFile(getenv), "agent token file")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	mode, err := podorelruntime.ResolveMode(*development, *production, getenv("PODOREL_MODE"))
	if err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(*socketPath) == "" {
		return Config{}, fmt.Errorf("socket path cannot be empty")
	}

	return Config{
		Mode:       mode,
		SocketPath: *socketPath,
		TokenFile:  *tokenFile,
	}, nil
}

func defaultSocketPath(getenv getenvFunc) string {
	if value := getenv("PODOREL_AGENT_SOCKET"); value != "" {
		return value
	}
	if runtimeDir := getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "podorel", DefaultSocketName)
	}
	return filepath.Join(os.TempDir(), "podorel-"+strconv.Itoa(os.Getuid()), DefaultSocketName)
}

func defaultTokenFile(getenv getenvFunc) string {
	if value := getenv("PODOREL_AGENT_TOKEN_FILE"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "podorel-agent-token")
	}
	return filepath.Join(home, DefaultConfigDir, DefaultTokenFile)
}

func ValidateTokenFilePermissions(path string) error {
	if path == "" {
		return fmt.Errorf("token file path cannot be empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("token file %s is a directory", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("token file %s permissions must not allow group or other access", path)
	}
	return nil
}

func ReadToken(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(content))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return token, nil
}
