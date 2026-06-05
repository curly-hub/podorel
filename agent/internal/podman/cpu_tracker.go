package podman

import (
	"fmt"
	"math"
	"sync"
	"time"
)

type cpuTimeSample struct {
	cpuTimeNanos int64
	sampledAt    time.Time
}

type CPUTracker struct {
	mu       sync.Mutex
	previous map[string]cpuTimeSample
}

func (t *CPUTracker) Apply(stats []ContainerStats, now time.Time) []ContainerStats {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.previous == nil {
		t.previous = make(map[string]cpuTimeSample)
	}

	seen := make(map[string]struct{}, len(stats))
	for i := range stats {
		key := cpuTrackerKey(stats[i])
		if key == "" || stats[i].CPUTimeNanos <= 0 || now.IsZero() {
			continue
		}
		seen[key] = struct{}{}

		liveCPU := 0.0
		if previous, ok := t.previous[key]; ok {
			deltaCPU := stats[i].CPUTimeNanos - previous.cpuTimeNanos
			deltaWall := now.Sub(previous.sampledAt)
			if deltaCPU > 0 && deltaWall > 0 {
				liveCPU = (float64(deltaCPU) / float64(deltaWall.Nanoseconds())) * 100
			}
			if liveCPU < 0 || math.IsNaN(liveCPU) || math.IsInf(liveCPU, 0) {
				liveCPU = 0
			}
		}

		t.previous[key] = cpuTimeSample{
			cpuTimeNanos: stats[i].CPUTimeNanos,
			sampledAt:    now,
		}
		stats[i].CPUPercentHostTotal = liveCPU
		stats[i].CPUPodmanPercent = liveCPU
		stats[i].CPUPodmanRaw = fmt.Sprintf("%.2f%%", liveCPU)
	}

	for key := range t.previous {
		if _, ok := seen[key]; !ok {
			delete(t.previous, key)
		}
	}

	return stats
}

func cpuTrackerKey(stat ContainerStats) string {
	if stat.ContainerID != "" {
		return stat.ContainerID
	}
	return stat.Name
}
