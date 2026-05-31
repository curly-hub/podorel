package correlation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const HeaderName = "X-Correlation-ID"
const idBytes = 16
const maxHeaderLength = 128

type contextKey struct{}

func NewID() string {
	var raw [idBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw[:])
}

func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

func FromContext(ctx context.Context) string {
	value, _ := ctx.Value(contextKey{}).(string)
	return value
}

func FromHeader(value string) string {
	candidate := strings.TrimSpace(value)
	if candidate == "" || len(candidate) > maxHeaderLength {
		return ""
	}
	for _, r := range candidate {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '-', '_', '.', ':':
			continue
		default:
			return ""
		}
	}
	return candidate
}

func FromContextOrNew(ctx context.Context) string {
	if id := FromContext(ctx); id != "" {
		return id
	}
	return NewID()
}
