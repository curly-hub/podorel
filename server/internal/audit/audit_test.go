package audit

import (
	"context"
	"testing"

	"github.com/curly-hub/podorel/internal/correlation"
)

func TestMemoryStoreAddsCorrelationID(t *testing.T) {
	store := NewMemoryStore()
	ctx := correlation.WithID(context.Background(), "audit-correlation")
	if err := store.Write(ctx, Event{
		Action:     "auth.login.agent_token",
		TargetType: "agent",
		Result:     "success",
	}); err != nil {
		t.Fatal(err)
	}
	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].CorrelationID != "audit-correlation" {
		t.Fatalf("correlation = %q", events[0].CorrelationID)
	}
}
