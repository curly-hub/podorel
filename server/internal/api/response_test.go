package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/curly-hub/podorel/internal/correlation"
)

func TestCorrelationMiddlewareUsesInboundID(t *testing.T) {
	handler := CorrelationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteOK(r.Context(), w, map[string]string{"status": "ok"})
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set(correlation.HeaderName, "test-correlation")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get(correlation.HeaderName); got != "test-correlation" {
		t.Fatalf("header = %q", got)
	}
	var envelope Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.CorrelationID != "test-correlation" {
		t.Fatalf("correlation id = %q", envelope.CorrelationID)
	}
}
