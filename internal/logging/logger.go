package logging

import (
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/curly-hub/podorel/internal/correlation"
	podorelruntime "github.com/curly-hub/podorel/internal/runtime"
)

type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

type Entry struct {
	Timestamp     string         `json:"ts"`
	Level         Level          `json:"level"`
	Mode          string         `json:"mode"`
	Component     string         `json:"component"`
	Operation     string         `json:"operation"`
	CorrelationID string         `json:"correlation_id"`
	AgentID       string         `json:"agent_id,omitempty"`
	LinuxUser     string         `json:"linux_user,omitempty"`
	PodID         string         `json:"pod_id,omitempty"`
	ContainerID   string         `json:"container_id,omitempty"`
	Message       string         `json:"message"`
	Fields        map[string]any `json:"fields"`
}

type Logger struct {
	mode      podorelruntime.Mode
	component string
	out       io.Writer
	now       func() time.Time
	mu        sync.Mutex
}

func New(out io.Writer, mode podorelruntime.Mode, component string) *Logger {
	return &Logger{
		mode:      mode,
		component: component,
		out:       out,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (l *Logger) WithNow(now func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = now
}

func (l *Logger) Debug(ctx context.Context, operation string, message string, fields map[string]any) {
	l.write(ctx, LevelDebug, operation, message, fields)
}

func (l *Logger) Info(ctx context.Context, operation string, message string, fields map[string]any) {
	l.write(ctx, LevelInfo, operation, message, fields)
}

func (l *Logger) Warn(ctx context.Context, operation string, message string, fields map[string]any) {
	l.write(ctx, LevelWarn, operation, message, fields)
}

func (l *Logger) Error(ctx context.Context, operation string, message string, fields map[string]any) {
	l.write(ctx, LevelError, operation, message, fields)
}

func (l *Logger) write(ctx context.Context, level Level, operation string, message string, fields map[string]any) {
	if !l.shouldEmit(level) {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	entry := Entry{
		Timestamp:     l.now().Format(time.RFC3339Nano),
		Level:         level,
		Mode:          l.mode.String(),
		Component:     l.component,
		Operation:     operation,
		CorrelationID: correlation.FromContextOrNew(ctx),
		Message:       RedactString(message),
		Fields:        SanitizeMap(fields),
	}
	_ = json.NewEncoder(l.out).Encode(entry)
}

func (l *Logger) shouldEmit(level Level) bool {
	if l.mode.IsDevelopment() {
		return true
	}
	return level == LevelError
}

func SanitizeMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		if IsSensitiveKey(key) {
			output[key] = "[REDACTED]"
			continue
		}
		output[key] = sanitizeValue(value)
	}
	return output
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case string:
		return RedactString(typed)
	case []string:
		return SanitizeArgs(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeValue(item))
		}
		return out
	case map[string]any:
		return SanitizeMap(typed)
	default:
		return typed
	}
}

func IsSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	sensitive := []string{
		"password",
		"passwd",
		"token",
		"secret",
		"authorization",
		"api_key",
		"apikey",
		"credential",
		"cookie",
		"build_arg",
	}
	for _, needle := range sensitive {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

var assignmentSecretPattern = regexp.MustCompile(`(?i)(password|passwd|token|secret|authorization|api[_-]?key)=([^,\s;&]+)`)
var bearerSecretPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/\-=]+`)

func RedactString(input string) string {
	redacted := bearerSecretPattern.ReplaceAllString(input, "Bearer [REDACTED]")
	return assignmentSecretPattern.ReplaceAllString(redacted, "$1=[REDACTED]")
}

func SanitizeArgs(args []string) []string {
	out := make([]string, len(args))
	redactNext := false
	for i, arg := range args {
		if redactNext {
			out[i] = "[REDACTED]"
			redactNext = false
			continue
		}
		if strings.HasPrefix(arg, "--") {
			name := strings.TrimPrefix(arg, "--")
			if before, _, ok := strings.Cut(name, "="); ok {
				if IsSensitiveKey(before) {
					out[i] = "--" + before + "=[REDACTED]"
					continue
				}
			}
			if IsSensitiveKey(name) {
				out[i] = arg
				redactNext = true
				continue
			}
		}
		out[i] = RedactString(arg)
	}
	return out
}
