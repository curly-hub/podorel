package logging

import (
	"bytes"
	"context"
	"strings"
	"testing"

	podorelruntime "github.com/curly-hub/podorel/internal/runtime"
)

func TestProductionSuppressesOperationalInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, podorelruntime.Production, "web")
	logger.Info(context.Background(), "startup", "server started", nil)
	if buf.Len() != 0 {
		t.Fatalf("production info log was emitted: %s", buf.String())
	}
	logger.Error(context.Background(), "startup", "failed", nil)
	if !strings.Contains(buf.String(), `"level":"error"`) {
		t.Fatalf("production error log missing: %s", buf.String())
	}
}

func TestDevelopmentEmitsRawAndParsedFields(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, podorelruntime.Development, "stats")
	logger.Debug(context.Background(), "parse_stats", "parsed raw stats", map[string]any{
		"raw_cpu":        "50.00%",
		"parsed_cpu":     50.0,
		"normalized_cpu": 12.5,
		"parser_branch":  "percent",
	})
	logged := buf.String()
	for _, want := range []string{"raw_cpu", "parsed_cpu", "normalized_cpu", "parser_branch"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("development log missing %q: %s", want, logged)
		}
	}
}

func TestRedaction(t *testing.T) {
	args := SanitizeArgs([]string{"podman", "--secret", "raw-value", "--password=hunter2", "token=abc123"})
	joined := strings.Join(args, " ")
	for _, leaked := range []string{"raw-value", "hunter2", "abc123"} {
		if strings.Contains(joined, leaked) {
			t.Fatalf("secret leaked in args %q", joined)
		}
	}

	fields := SanitizeMap(map[string]any{
		"agent_token": "abc123",
		"message":     "Authorization=Bearer abc123",
	})
	if fields["agent_token"] != "[REDACTED]" {
		t.Fatalf("token field not redacted: %#v", fields)
	}
	if strings.Contains(fields["message"].(string), "abc123") {
		t.Fatalf("message value leaked: %#v", fields)
	}
}
