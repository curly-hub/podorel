package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelp(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--help"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "agent rotate-token") {
		t.Fatalf("help output = %s", out.String())
	}
}

func TestAgentRegisterCommand(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"agent", "register", "--user", "alice"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "alice") {
		t.Fatalf("register output = %s", out.String())
	}
}

func TestDefaultStatusURLUsesLocalhost(t *testing.T) {
	if !strings.Contains(defaultStatusURL, "localhost") {
		t.Fatalf("default status URL = %q, want localhost", defaultStatusURL)
	}
	if strings.Contains(defaultStatusURL, "127.0.0.1") {
		t.Fatalf("default status URL must not use 127.0.0.1: %q", defaultStatusURL)
	}
}
