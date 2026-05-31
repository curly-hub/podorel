package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultStatusURL = "http://localhost:8080/api/health"
const commandTimeout = 20 * time.Second

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "--help" {
		printHelp(stdout)
		return nil
	}
	switch args[0] {
	case "status":
		return status(stdout)
	case "logs":
		return userSystemctl(stdout, stderr, "journalctl", "--user", "-u", "podorel-web.service", "-n", "100", "--no-pager")
	case "restart", "stop", "start":
		return userSystemctl(stdout, stderr, "systemctl", "--user", args[0], "podorel-web.service")
	case "doctor":
		return doctor(stdout)
	case "agent":
		return agentCommand(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, `Usage: podorel COMMAND

Commands:
  status
  logs
  restart
  stop
  start
  agent status
  agent register --user USER
  agent rotate-token --user USER
  doctor`)
}

func status(stdout io.Writer) error {
	url := os.Getenv("PODOREL_STATUS_URL")
	if url == "" {
		url = defaultStatusURL
	}
	client := http.Client{Timeout: commandTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(stdout, resp.Body)
	if err == nil {
		_, err = fmt.Fprintln(stdout)
	}
	return err
}

func doctor(stdout io.Writer) error {
	checks := []string{"podman", "go", "npm"}
	for _, check := range checks {
		if _, err := exec.LookPath(check); err != nil {
			_, _ = fmt.Fprintf(stdout, "%s: missing\n", check)
		} else {
			_, _ = fmt.Fprintf(stdout, "%s: ok\n", check)
		}
	}
	return nil
}

func agentCommand(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("agent command required")
	}
	switch args[0] {
	case "status":
		return userSystemctl(stdout, stderr, "systemctl", "--user", "status", "podorel-agent.service", "--no-pager")
	case "register":
		user := flagValue(args[1:], "--user")
		if user == "" {
			return fmt.Errorf("agent register requires --user USER")
		}
		_, _ = fmt.Fprintf(stdout, "Register agent for %s through Admin -> Agents or POST /api/agents/register.\n", user)
		return nil
	case "rotate-token":
		user := flagValue(args[1:], "--user")
		if user == "" {
			return fmt.Errorf("agent rotate-token requires --user USER")
		}
		_, _ = fmt.Fprintf(stdout, "Rotate token for %s through Admin -> Agents or POST /api/agents/{id}/rotate-token.\n", user)
		return nil
	default:
		return fmt.Errorf("unknown agent command %q", args[0])
	}
}

func flagValue(args []string, name string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, name+"=") {
			return strings.TrimPrefix(arg, name+"=")
		}
	}
	return ""
}

func userSystemctl(stdout io.Writer, stderr io.Writer, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
