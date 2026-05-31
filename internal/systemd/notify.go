package systemd

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	notifySocketEnv = "NOTIFY_SOCKET"
	watchdogUsecEnv = "WATCHDOG_USEC"
	watchdogPIDEnv  = "WATCHDOG_PID"
)

type getenvFunc func(string) string

func Ready() error {
	return Notify("READY=1")
}

func Notify(state string) error {
	return notify(state, os.Getenv)
}

func StartWatchdog(ctx context.Context, getenv getenvFunc) bool {
	interval, ok := WatchdogInterval(getenv, os.Getpid())
	if !ok {
		return false
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = notify("WATCHDOG=1", getenv)
			}
		}
	}()
	return true
}

func WatchdogInterval(getenv getenvFunc, pid int) (time.Duration, bool) {
	if rawPID := strings.TrimSpace(getenv(watchdogPIDEnv)); rawPID != "" {
		expected, err := strconv.Atoi(rawPID)
		if err != nil || expected != pid {
			return 0, false
		}
	}
	raw := strings.TrimSpace(getenv(watchdogUsecEnv))
	if raw == "" {
		return 0, false
	}
	usec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || usec <= 0 {
		return 0, false
	}
	interval := time.Duration(usec) * time.Microsecond / 2
	if interval <= 0 {
		return time.Microsecond, true
	}
	return interval, true
}

func notify(state string, getenv getenvFunc) error {
	if strings.TrimSpace(state) == "" {
		return fmt.Errorf("systemd notification state cannot be empty")
	}
	socket := strings.TrimSpace(getenv(notifySocketEnv))
	if socket == "" {
		return nil
	}
	addr := notifyAddr(socket)
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(state))
	return err
}

func notifyAddr(socket string) *net.UnixAddr {
	if strings.HasPrefix(socket, "@") {
		socket = "\x00" + strings.TrimPrefix(socket, "@")
	}
	return &net.UnixAddr{Name: socket, Net: "unixgram"}
}
