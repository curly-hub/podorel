package podman

import (
	"testing"
	"time"
)

func TestParsePodmanLogLinePreservesTimestamp(t *testing.T) {
	line := parsePodmanLogLine("rabbitmq", "2026-06-05T22:07:38.649462+00:00 accepting AMQP connection")

	want := time.Date(2026, 6, 5, 22, 7, 38, 649462000, time.UTC)
	if !line.Timestamp.Equal(want) {
		t.Fatalf("timestamp = %s, want %s", line.Timestamp, want)
	}
	if line.Source != "rabbitmq" {
		t.Fatalf("source = %q", line.Source)
	}
	if line.Line != "accepting AMQP connection" {
		t.Fatalf("line = %q", line.Line)
	}
}

func TestParsePodmanLogLineFallsBackForUntimestampedRows(t *testing.T) {
	line := parsePodmanLogLine("rabbitmq", "plain old log")

	if !line.Timestamp.IsZero() {
		t.Fatalf("timestamp = %s, want zero", line.Timestamp)
	}
	if line.Line != "plain old log" {
		t.Fatalf("line = %q", line.Line)
	}
}
