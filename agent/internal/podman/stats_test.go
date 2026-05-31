package podman

import (
	"testing"
	"time"
)

func TestParseMemoryUsageUnits(t *testing.T) {
	tests := []struct {
		raw  string
		want uint64
	}{
		{"10B / 1GiB", 10},
		{"1KiB / 1GiB", 1024},
		{"1MiB / 1GiB", 1024 * 1024},
		{"1GiB / 2GiB", 1024 * 1024 * 1024},
		{"1KB / 1GB", 1000},
		{"1MB / 1GB", 1000 * 1000},
		{"1GB / 2GB", 1000 * 1000 * 1000},
	}
	for _, tt := range tests {
		got, err := ParseMemoryUsage(tt.raw)
		if err != nil {
			t.Fatalf("%s: %v", tt.raw, err)
		}
		if got.Bytes != tt.want {
			t.Fatalf("%s bytes = %d, want %d", tt.raw, got.Bytes, tt.want)
		}
		if got.Branch == "" {
			t.Fatalf("%s missing parser branch", tt.raw)
		}
	}
}

func TestParseCPUPercentAndNormalize(t *testing.T) {
	raw, err := ParseCPUPercent("200.00%")
	if err != nil {
		t.Fatal(err)
	}
	if raw != 200 {
		t.Fatalf("raw cpu = %f", raw)
	}
	if got := NormalizeCPUHostTotal(raw, 4); got != 50 {
		t.Fatalf("host total cpu = %f, want 50", got)
	}
}

func TestParseStatsJSONAndAggregate(t *testing.T) {
	raw := []byte(`[
		{"id":"c1","pod":"p1","name":"one","cpu_percent":"100.00%","mem_usage":"1MiB / 1GiB"},
		{"id":"c2","pod":"p1","name":"two","cpu_percent":"50.00%","mem_usage":"2MiB / 1GiB"}
	]`)
	stats, err := ParseStatsJSON(raw, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("stats = %d, want 2", len(stats))
	}
	if stats[0].CPUPercentHostTotal != 25 {
		t.Fatalf("normalized cpu = %f, want 25", stats[0].CPUPercentHostTotal)
	}
	aggregated := AggregatePodStats(stats)
	pod := aggregated["p1"]
	if pod.CPUPercentHostTotal != 37.5 {
		t.Fatalf("pod cpu = %f, want 37.5", pod.CPUPercentHostTotal)
	}
	if pod.MemoryBytes != 3*1024*1024 {
		t.Fatalf("pod memory = %d", pod.MemoryBytes)
	}
}

func TestOptionalStringFieldUsesFirstArrayValue(t *testing.T) {
	row := map[string]any{"Names": []any{"node-demo-main", "alias"}}
	if got := optionalStringField(row, "Names"); got != "node-demo-main" {
		t.Fatalf("name = %q, want first array value", got)
	}
}

func TestOptionalTimeFieldParsesPodmanFormats(t *testing.T) {
	parsed := optionalTimeField(map[string]any{"Created": "2026-05-05T21:49:47.001372093Z"}, "Created")
	want := time.Date(2026, 5, 5, 21, 49, 47, 1372093, time.UTC)
	if !parsed.Equal(want) {
		t.Fatalf("rfc3339 created_at = %s, want %s", parsed, want)
	}

	parsed = optionalTimeField(map[string]any{"Created": float64(1778017787)}, "Created")
	want = time.Unix(1778017787, 0).UTC()
	if !parsed.Equal(want) {
		t.Fatalf("unix created_at = %s, want %s", parsed, want)
	}
}

func TestParseStatsRejectsMalformed(t *testing.T) {
	if _, err := ParseStatsJSON([]byte(`{"not": "an array"`), 4); err == nil {
		t.Fatal("expected malformed stats to fail")
	}
	if _, err := ParseMemoryUsage("nonsense"); err == nil {
		t.Fatal("expected bad memory to fail")
	}
}
