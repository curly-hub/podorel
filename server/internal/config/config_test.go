package config

import (
	"testing"

	podorelruntime "github.com/curly-hub/podorel/internal/runtime"
)

func TestLoadDefaultsToProduction(t *testing.T) {
	cfg, err := Load(nil, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != podorelruntime.Production {
		t.Fatalf("mode = %q, want production", cfg.Mode)
	}
	if cfg.Server.ListenAddr != DefaultProductionListenAddr {
		t.Fatalf("listen addr = %q", cfg.Server.ListenAddr)
	}
	if cfg.Server.PublicURL != DefaultProductionPublicURL {
		t.Fatalf("public url = %q", cfg.Server.PublicURL)
	}
}

func TestLoadDevelopmentFlag(t *testing.T) {
	cfg, err := Load([]string{"--development"}, func(key string) string {
		if key == "PODOREL_MODE" {
			return "production"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != podorelruntime.Development {
		t.Fatalf("mode = %q, want development", cfg.Mode)
	}
	if cfg.Server.ListenAddr != DefaultDevelopmentListenAddr {
		t.Fatalf("listen addr = %q, want %q", cfg.Server.ListenAddr, DefaultDevelopmentListenAddr)
	}
	if cfg.Server.PublicURL != DefaultDevelopmentPublicURL {
		t.Fatalf("public url = %q, want %q", cfg.Server.PublicURL, DefaultDevelopmentPublicURL)
	}
}

func TestLoadExplicitListenOverridesModeDefault(t *testing.T) {
	cfg, err := Load([]string{"--development", "--listen-addr", "localhost:18080", "--public-url", "http://localhost:18080"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ListenAddr != "localhost:18080" {
		t.Fatalf("listen addr = %q", cfg.Server.ListenAddr)
	}
	if cfg.Server.PublicURL != "http://localhost:18080" {
		t.Fatalf("public url = %q", cfg.Server.PublicURL)
	}
}

func TestLoadEnvironmentListenOverridesModeDefault(t *testing.T) {
	cfg, err := Load([]string{"--development"}, func(key string) string {
		switch key {
		case "PODOREL_LISTEN_ADDR":
			return "localhost:19090"
		case "PODOREL_PUBLIC_URL":
			return "http://localhost:19090"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ListenAddr != "localhost:19090" {
		t.Fatalf("listen addr = %q", cfg.Server.ListenAddr)
	}
	if cfg.Server.PublicURL != "http://localhost:19090" {
		t.Fatalf("public url = %q", cfg.Server.PublicURL)
	}
}

func TestLoadHTTPSEnvironment(t *testing.T) {
	cfg, err := Load([]string{"--production"}, func(key string) string {
		switch key {
		case "PODOREL_ADMIN_PASSWORD":
			return "secret-password"
		case "PODOREL_PUBLIC_URL":
			return "https://curly-hub.local:9095"
		case "PODOREL_TLS_CERT_FILE":
			return "/app/data/tls/curly-hub.local.crt"
		case "PODOREL_TLS_KEY_FILE":
			return "/app/data/tls/curly-hub.local.key"
		case "PODOREL_TLS_CA_FILE":
			return "/app/data/tls/podorel-local-ca.crt"
		case "PODOREL_TRUSTED_PROXY_MODE":
			return "true"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Server.TLSEnabled() || !cfg.Server.UsesHTTPS() {
		t.Fatalf("server TLS state = %#v", cfg.Server)
	}
	if !cfg.Server.TrustedProxyMode {
		t.Fatal("trusted proxy mode was not enabled")
	}
	if cfg.Server.TLSCertFile != "/app/data/tls/curly-hub.local.crt" || cfg.Server.TLSKeyFile != "/app/data/tls/curly-hub.local.key" {
		t.Fatalf("tls files = %#v", cfg.Server)
	}
	if cfg.Server.TLSCAFile != "/app/data/tls/podorel-local-ca.crt" {
		t.Fatalf("tls ca file = %#v", cfg.Server)
	}
}

func TestLoadRejectsPartialTLSConfig(t *testing.T) {
	_, err := Load([]string{"--tls-cert-file", "/tmp/cert.pem"}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected partial TLS config to fail")
	}
}

func TestLoadRejectsInvalidTrustedProxyMode(t *testing.T) {
	_, err := Load(nil, func(key string) string {
		if key == "PODOREL_TRUSTED_PROXY_MODE" {
			return "maybe"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected invalid trusted proxy mode to fail")
	}
}

func TestLoadUsesAgentSocketEnvironment(t *testing.T) {
	cfg, err := Load([]string{"--development"}, func(key string) string {
		if key == "PODOREL_AGENT_SOCKET" {
			return "/tmp/podorel-test/podorel-agent.sock"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.PrimarySocketPath != "/tmp/podorel-test/podorel-agent.sock" {
		t.Fatalf("agent socket = %q", cfg.Agent.PrimarySocketPath)
	}
}

func TestLoadRejectsModeConflict(t *testing.T) {
	if _, err := Load([]string{"--development", "--production"}, func(string) string { return "" }); err == nil {
		t.Fatal("expected conflict to fail")
	}
}
