package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	podorelruntime "github.com/curly-hub/podorel/internal/runtime"
)

const (
	DefaultProductionListenAddr  = "0.0.0.0:8080"
	DefaultDevelopmentListenAddr = "localhost:8080"
	DefaultProductionPublicURL   = "http://podorel.lan:8080"
	DefaultDevelopmentPublicURL  = "http://localhost:8080"
	DefaultDatabasePath          = "~/.local/share/podorel/podorel.db"
	DefaultUIDistPath            = "ui/dist/podorel-ui/browser"
	DefaultSessionTTL            = 24 * time.Hour
	DefaultFailedLoginLimit      = 5
	DefaultFailedWindow          = 10 * time.Minute
	DefaultLiveInterval          = 5 * time.Second
	DefaultPersistInterval       = 30 * time.Second
	DefaultMetricsRetention      = 7 * 24 * time.Hour
	DefaultLogsRetention         = 7 * 24 * time.Hour
	DefaultPerPodLimitMB         = 100
	DefaultTotalLimitMB          = 5120
	DefaultAgentSocketName       = "podorel-agent.sock"
	DefaultAgentSocketDir        = "podorel"
)

type Config struct {
	Mode     podorelruntime.Mode `json:"mode"`
	Database DatabaseConfig      `json:"database"`
	UI       UIConfig            `json:"ui"`
	Server   ServerConfig        `json:"server"`
	Agent    AgentConfig         `json:"agent"`
	Auth     AuthConfig          `json:"auth"`
	Metrics  MetricsConfig       `json:"metrics"`
	Logs     LogsConfig          `json:"logs"`
	Security SecurityConfig      `json:"security"`
	Actions  ActionsConfig       `json:"actions"`
}

type DatabaseConfig struct {
	Path string `json:"path"`
}

type UIConfig struct {
	DistPath string `json:"dist_path"`
}

type ServerConfig struct {
	ListenAddr       string `json:"listen_addr"`
	PublicURL        string `json:"public_url"`
	TrustedProxyMode bool   `json:"trusted_proxy_mode"`
	TLSCertFile      string `json:"tls_cert_file"`
	TLSKeyFile       string `json:"tls_key_file"`
}

type AgentConfig struct {
	PrimarySocketPath string `json:"primary_socket_path"`
}

type AuthConfig struct {
	SessionTTL       time.Duration `json:"session_ttl"`
	FailedLoginLimit int           `json:"failed_login_limit"`
	FailedWindow     time.Duration `json:"failed_login_window"`
	AdminPassword    string        `json:"-"`
}

type MetricsConfig struct {
	LiveInterval    time.Duration `json:"live_interval"`
	PersistInterval time.Duration `json:"persist_interval"`
	Retention       time.Duration `json:"retention"`
}

type LogsConfig struct {
	Retention     time.Duration `json:"retention"`
	PerPodLimitMB int           `json:"per_pod_limit_mb"`
	TotalLimitMB  int           `json:"total_limit_mb"`
}

type SecurityConfig struct {
	Scanner               string `json:"scanner"`
	ScheduledScansEnabled bool   `json:"scheduled_scans_enabled"`
	Schedule              string `json:"schedule"`
}

type ActionsConfig struct {
	ExecEnabled       bool `json:"exec_enabled"`
	AutomationEnabled bool `json:"automation_enabled"`
}

type getenvFunc func(string) string

func Load(args []string, getenv getenvFunc) (Config, error) {
	trustedProxyModeDefault, err := envBoolOrDefault(getenv, "PODOREL_TRUSTED_PROXY_MODE", false)
	if err != nil {
		return Config{}, err
	}
	fs := flag.NewFlagSet("podorel-web", flag.ContinueOnError)
	development := fs.Bool("development", false, "run in development mode")
	production := fs.Bool("production", false, "run in production mode")
	listenAddr := fs.String("listen-addr", envOrDefault(getenv, "PODOREL_LISTEN_ADDR", ""), "HTTP listen address")
	publicURL := fs.String("public-url", envOrDefault(getenv, "PODOREL_PUBLIC_URL", ""), "public URL")
	trustedProxyMode := fs.Bool("trusted-proxy-mode", trustedProxyModeDefault, "trust X-Forwarded-Proto and X-Forwarded-Host headers")
	tlsCertFile := fs.String("tls-cert-file", envOrDefault(getenv, "PODOREL_TLS_CERT_FILE", ""), "TLS certificate file for native HTTPS")
	tlsKeyFile := fs.String("tls-key-file", envOrDefault(getenv, "PODOREL_TLS_KEY_FILE", ""), "TLS private key file for native HTTPS")
	dbPath := fs.String("db-path", expandHome(envOrDefault(getenv, "PODOREL_DB_PATH", DefaultDatabasePath)), "SQLite database path")
	uiDistPath := fs.String("ui-dist-path", envOrDefault(getenv, "PODOREL_UI_DIST_PATH", DefaultUIDistPath), "Angular distribution path")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	mode, err := podorelruntime.ResolveMode(*development, *production, getenv("PODOREL_MODE"))
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Mode: mode,
		Database: DatabaseConfig{
			Path: *dbPath,
		},
		UI: UIConfig{
			DistPath: *uiDistPath,
		},
		Server: ServerConfig{
			ListenAddr:       *listenAddr,
			PublicURL:        *publicURL,
			TrustedProxyMode: *trustedProxyMode,
			TLSCertFile:      *tlsCertFile,
			TLSKeyFile:       *tlsKeyFile,
		},
		Agent: AgentConfig{
			PrimarySocketPath: defaultAgentSocketPath(getenv),
		},
		Auth: AuthConfig{
			SessionTTL:       DefaultSessionTTL,
			FailedLoginLimit: DefaultFailedLoginLimit,
			FailedWindow:     DefaultFailedWindow,
			AdminPassword:    getenv("PODOREL_ADMIN_PASSWORD"),
		},
		Metrics: MetricsConfig{
			LiveInterval:    DefaultLiveInterval,
			PersistInterval: DefaultPersistInterval,
			Retention:       DefaultMetricsRetention,
		},
		Logs: LogsConfig{
			Retention:     DefaultLogsRetention,
			PerPodLimitMB: DefaultPerPodLimitMB,
			TotalLimitMB:  DefaultTotalLimitMB,
		},
		Security: SecurityConfig{
			Scanner:               "trivy",
			ScheduledScansEnabled: false,
			Schedule:              "daily",
		},
		Actions: ActionsConfig{
			ExecEnabled:       false,
			AutomationEnabled: false,
		},
	}
	if cfg.Server.ListenAddr == "" {
		cfg.Server.ListenAddr = defaultListenAddrForMode(mode)
	}
	if cfg.Server.PublicURL == "" {
		cfg.Server.PublicURL = defaultPublicURLForMode(mode)
	}
	if cfg.Server.ListenAddr == "" {
		return Config{}, fmt.Errorf("listen address cannot be empty")
	}
	if (cfg.Server.TLSCertFile == "") != (cfg.Server.TLSKeyFile == "") {
		return Config{}, fmt.Errorf("tls cert file and tls key file must be configured together")
	}
	return cfg, nil
}

func (s ServerConfig) TLSEnabled() bool {
	return s.TLSCertFile != "" && s.TLSKeyFile != ""
}

func (s ServerConfig) UsesHTTPS() bool {
	return s.TLSEnabled() || strings.HasPrefix(strings.ToLower(strings.TrimSpace(s.PublicURL)), "https://")
}

func defaultListenAddrForMode(mode podorelruntime.Mode) string {
	if mode.IsDevelopment() {
		return DefaultDevelopmentListenAddr
	}
	return DefaultProductionListenAddr
}

func defaultPublicURLForMode(mode podorelruntime.Mode) string {
	if mode.IsDevelopment() {
		return DefaultDevelopmentPublicURL
	}
	return DefaultProductionPublicURL
}

func defaultAgentSocketPath(getenv getenvFunc) string {
	if value := getenv("PODOREL_AGENT_SOCKET"); value != "" {
		return value
	}
	if runtimeDir := getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, DefaultAgentSocketDir, DefaultAgentSocketName)
	}
	return filepath.Join(os.TempDir(), "podorel-"+strconv.Itoa(os.Getuid()), DefaultAgentSocketName)
}

func envOrDefault(getenv getenvFunc, key string, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBoolOrDefault(getenv getenvFunc, key string, fallback bool) (bool, error) {
	value := strings.ToLower(strings.TrimSpace(getenv(key)))
	if value == "" {
		return fallback, nil
	}
	switch value {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", key)
	}
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
