package audit

import (
	"context"
	"sync"
	"time"

	"github.com/curly-hub/podorel/internal/correlation"
)

type Event struct {
	CreatedAt     time.Time
	ActorUserID   string
	AgentID       string
	Action        string
	TargetType    string
	TargetID      string
	Result        string
	CorrelationID string
	Details       map[string]any
}

type Store interface {
	Write(ctx context.Context, event Event) error
}

type MemoryStore struct {
	mu     sync.Mutex
	events []Event
	now    func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *MemoryStore) Write(ctx context.Context, event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.CreatedAt.IsZero() {
		event.CreatedAt = s.now()
	}
	if event.CorrelationID == "" {
		event.CorrelationID = correlation.FromContextOrNew(ctx)
	}
	s.events = append(s.events, event)
	return nil
}

func (s *MemoryStore) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}
