package logs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRotateIfNeededCompressesAndTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "container.current.log")
	if err := os.WriteFile(path, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	rotated, ok, err := RotateIfNeeded(path, 5, time.Date(2026, 5, 3, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected rotation")
	}
	if _, err := os.Stat(rotated); err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("current log size = %d, want 0", info.Size())
	}
}

func TestPruneToTotalLimitRemovesOldestLogs(t *testing.T) {
	dir := t.TempDir()
	oldLog := writeSizedLog(t, filepath.Join(dir, "old.log"), 6)
	newLog := writeSizedLog(t, filepath.Join(dir, "new.log"), 6)
	if err := os.Chtimes(oldLog, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newLog, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	removed, err := PruneToTotalLimit(dir, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != oldLog {
		t.Fatalf("removed = %#v, want old log", removed)
	}
	if _, err := os.Stat(newLog); err != nil {
		t.Fatalf("new log should remain: %v", err)
	}
}

func TestRemoveExpiredDeletesLogsOlderThanRetention(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	oldLog := writeSizedLog(t, filepath.Join(dir, "old.log.gz"), 6)
	newLog := writeSizedLog(t, filepath.Join(dir, "new.log.gz"), 6)
	if err := os.Chtimes(oldLog, now.Add(-25*time.Hour), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newLog, now.Add(-23*time.Hour), now.Add(-23*time.Hour)); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveExpired(dir, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != oldLog {
		t.Fatalf("removed = %#v, want old log", removed)
	}
	if _, err := os.Stat(newLog); err != nil {
		t.Fatalf("new log should remain: %v", err)
	}
}

func writeSizedLog(t *testing.T, path string, size int) string {
	t.Helper()
	content := make([]byte, size)
	for i := range content {
		content[i] = 'x'
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
