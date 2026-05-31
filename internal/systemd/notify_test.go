package systemd

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotifyNoSocketIsNoop(t *testing.T) {
	if err := notify("READY=1", func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyRejectsEmptyState(t *testing.T) {
	if err := notify("", func(string) string { return "" }); err == nil {
		t.Fatal("expected empty state to fail")
	}
}

func TestNotifyWritesToUnixDatagramSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "notify.sock")
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socketPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	readCh := make(chan string, 1)
	go func() {
		buffer := make([]byte, 128)
		_ = listener.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := listener.ReadFromUnix(buffer)
		if err != nil {
			readCh <- ""
			return
		}
		readCh <- string(buffer[:n])
	}()

	if err := notify("READY=1", func(key string) string {
		if key == notifySocketEnv {
			return socketPath
		}
		return ""
	}); err != nil {
		t.Fatal(err)
	}
	if got := <-readCh; got != "READY=1" {
		t.Fatalf("notification = %q, want READY=1", got)
	}
}

func TestWatchdogInterval(t *testing.T) {
	interval, ok := WatchdogInterval(func(key string) string {
		if key == watchdogUsecEnv {
			return "30000000"
		}
		return ""
	}, os.Getpid())
	if !ok {
		t.Fatal("expected watchdog interval")
	}
	if interval != 15*time.Second {
		t.Fatalf("interval = %s, want 15s", interval)
	}
}

func TestWatchdogIntervalHonorsPID(t *testing.T) {
	_, ok := WatchdogInterval(func(key string) string {
		switch key {
		case watchdogUsecEnv:
			return "30000000"
		case watchdogPIDEnv:
			return "1"
		default:
			return ""
		}
	}, 2)
	if ok {
		t.Fatal("expected mismatched watchdog pid to disable watchdog")
	}
}
