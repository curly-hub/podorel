package podman

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const parserNameStats = "podman_stats"

type MemoryParseResult struct {
	Raw       string
	UsageRaw  string
	LimitRaw  string
	Bytes     uint64
	Branch    string
	ParseName string
}

func ParseCPUPercent(raw string) (float64, error) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	if trimmed == "" {
		return 0, fmt.Errorf("cpu percent is empty")
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cpu percent %q: %w", raw, err)
	}
	return value, nil
}

func ParseMemoryUsage(raw string) (MemoryParseResult, error) {
	result := MemoryParseResult{
		Raw:       raw,
		ParseName: "memory_usage",
	}
	parts := strings.SplitN(raw, "/", 2)
	usage := strings.TrimSpace(parts[0])
	if len(parts) == 2 {
		result.LimitRaw = strings.TrimSpace(parts[1])
	}
	if usage == "" {
		return result, fmt.Errorf("memory usage is empty")
	}

	valueText, unitText := splitNumberAndUnit(usage)
	if valueText == "" || unitText == "" {
		return result, fmt.Errorf("parse memory usage %q: expected number and unit", raw)
	}
	value, err := strconv.ParseFloat(valueText, 64)
	if err != nil {
		return result, fmt.Errorf("parse memory value %q: %w", valueText, err)
	}
	factor, branch, ok := memoryUnitFactor(unitText)
	if !ok {
		return result, fmt.Errorf("unsupported memory unit %q in %q", unitText, raw)
	}
	result.UsageRaw = usage
	result.Branch = branch
	result.Bytes = uint64(math.Round(value * factor))
	return result, nil
}

func splitNumberAndUnit(input string) (string, string) {
	trimmed := strings.TrimSpace(input)
	for i, r := range trimmed {
		if (r < '0' || r > '9') && r != '.' {
			return strings.TrimSpace(trimmed[:i]), strings.TrimSpace(trimmed[i:])
		}
	}
	return trimmed, ""
}

func memoryUnitFactor(unit string) (float64, string, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "b":
		return 1, "bytes", true
	case "kib":
		return 1024, "binary_kib", true
	case "mib":
		return 1024 * 1024, "binary_mib", true
	case "gib":
		return 1024 * 1024 * 1024, "binary_gib", true
	case "kb":
		return 1000, "decimal_kb", true
	case "mb":
		return 1000 * 1000, "decimal_mb", true
	case "gb":
		return 1000 * 1000 * 1000, "decimal_gb", true
	default:
		return 0, "", false
	}
}

func ParseStatsJSON(raw []byte, _ int) ([]ContainerStats, error) {
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		var envelope struct {
			Stats []map[string]any `json:"stats"`
		}
		if err2 := json.Unmarshal(raw, &envelope); err2 != nil {
			return nil, fmt.Errorf("%s malformed json: %w", parserNameStats, err)
		}
		rows = envelope.Stats
	}

	stats := make([]ContainerStats, 0, len(rows))
	for _, row := range rows {
		rowRaw, _ := json.Marshal(row)
		cpuRaw, err := stringField(row, "cpu_percent", "CPUPerc", "cpu", "CPU")
		if err != nil {
			return nil, err
		}
		memRaw, err := stringField(row, "mem_usage", "MemUsage", "memory", "MEM")
		if err != nil {
			return nil, err
		}
		cpu, err := ParseCPUPercent(cpuRaw)
		if err != nil {
			return nil, err
		}
		cpuTime := optionalDurationField(row, "cpu_time", "CPUTime", "cpuTime")
		memory, err := ParseMemoryUsage(memRaw)
		if err != nil {
			return nil, err
		}
		stats = append(stats, ContainerStats{
			ContainerID:      optionalStringField(row, "id", "ID", "container_id", "ContainerID"),
			PodID:            optionalStringField(row, "pod", "pod_id", "PodID"),
			Name:             optionalStringField(row, "name", "Name"),
			CPUPodmanRaw:     cpuRaw,
			CPUPodmanPercent: cpu,
			// Podman already reports the top-style container CPU percentage.
			// It can exceed 100% when a container uses multiple CPUs. Dividing
			// by the host CPU count made busy containers look nearly idle.
			CPUPercentHostTotal: cpu,
			CPUTimeNanos:        cpuTime.Nanoseconds(),
			MemoryPodmanRaw:     memRaw,
			MemoryBytes:         memory.Bytes,
			MemoryLimitRaw:      memory.LimitRaw,
			MemoryParserBranch:  memory.Branch,
			RawJSON:             string(rowRaw),
		})
	}
	return stats, nil
}

func AggregatePodStats(containers []ContainerStats) map[string]PodStats {
	out := make(map[string]PodStats)
	for _, container := range containers {
		podID := container.PodID
		if podID == "" {
			podID = "_unassigned"
		}
		aggregate := out[podID]
		aggregate.PodID = podID
		aggregate.ContainerIDs = append(aggregate.ContainerIDs, container.ContainerID)
		aggregate.CPUPercentHostTotal += container.CPUPercentHostTotal
		aggregate.MemoryBytes += container.MemoryBytes
		out[podID] = aggregate
	}
	return out
}

func stringField(row map[string]any, keys ...string) (string, error) {
	value := optionalStringField(row, keys...)
	if value == "" {
		return "", fmt.Errorf("%s missing field %s", parserNameStats, strings.Join(keys, "|"))
	}
	return value, nil
}

func optionalTimeField(row map[string]any, keys ...string) time.Time {
	for _, key := range keys {
		value, ok := row[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if parsed, ok := parsePodmanTimeString(typed); ok {
				return parsed
			}
		case float64:
			if typed > 0 {
				return time.Unix(int64(typed), 0).UTC()
			}
		case int:
			if typed > 0 {
				return time.Unix(int64(typed), 0).UTC()
			}
		case int64:
			if typed > 0 {
				return time.Unix(typed, 0).UTC()
			}
		}
	}
	return time.Time{}
}

func optionalDurationField(row map[string]any, keys ...string) time.Duration {
	for _, key := range keys {
		value, ok := row[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			parsed, err := time.ParseDuration(strings.TrimSpace(typed))
			if err == nil {
				return parsed
			}
		case float64:
			if typed > 0 {
				return time.Duration(typed)
			}
		case int:
			if typed > 0 {
				return time.Duration(typed)
			}
		case int64:
			if typed > 0 {
				return time.Duration(typed)
			}
		}
	}
	return 0
}

func parsePodmanTimeString(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	formats := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05 -0700 MST", "2006-01-02 15:04:05 -0700"}
	for _, format := range formats {
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func optionalStringField(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			switch typed := value.(type) {
			case string:
				return typed
			case []any:
				if len(typed) > 0 {
					if first, ok := typed[0].(string); ok {
						return first
					}
				}
			case []string:
				if len(typed) > 0 {
					return typed[0]
				}
			case float64:
				return strconv.FormatFloat(typed, 'f', -1, 64)
			case int:
				return strconv.Itoa(typed)
			}
		}
	}
	return ""
}

func healthFromPodmanRow(row map[string]any) string {
	if health := normalizeHealth(optionalStringField(row, "Health", "health", "HealthStatus", "health_status")); health != "" {
		return health
	}
	if health := nestedHealthStatus(row["State"]); health != "" {
		return health
	}
	if health := nestedHealthStatus(row["state"]); health != "" {
		return health
	}
	if health := healthFromStatusText(optionalStringField(row, "Status", "status")); health != "" {
		return health
	}
	return ""
}

func nestedHealthStatus(value any) string {
	state, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if health := normalizeHealth(optionalStringField(state, "Health", "health")); health != "" {
		return health
	}
	for _, key := range []string{"Health", "health"} {
		healthValue, ok := state[key].(map[string]any)
		if !ok {
			continue
		}
		if health := normalizeHealth(optionalStringField(healthValue, "Status", "status")); health != "" {
			return health
		}
	}
	return ""
}

func healthFromStatusText(value string) string {
	text := strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(text, "unhealthy"):
		return "unhealthy"
	case strings.Contains(text, "(healthy)") || strings.Contains(text, "health: healthy"):
		return "healthy"
	case strings.Contains(text, "health: starting") || strings.Contains(text, "(starting)"):
		return "starting"
	}
	return ""
}

func normalizeHealth(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "healthy":
		return "healthy"
	case "unhealthy":
		return "unhealthy"
	case "starting":
		return "starting"
	}
	return ""
}
