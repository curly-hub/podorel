package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/curly-hub/podorel/internal/correlation"
)

type Envelope struct {
	OK            bool       `json:"ok"`
	Data          any        `json:"data"`
	Error         *ErrorBody `json:"error"`
	CorrelationID string     `json:"correlation_id"`
}

type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func CorrelationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := correlation.FromHeader(r.Header.Get(correlation.HeaderName))
		if id == "" {
			id = correlation.NewID()
		}
		w.Header().Set(correlation.HeaderName, id)
		next.ServeHTTP(w, r.WithContext(correlation.WithID(r.Context(), id)))
	})
}

func WriteOK(ctx context.Context, w http.ResponseWriter, data any) {
	writeJSON(ctx, w, http.StatusOK, Envelope{
		OK:            true,
		Data:          data,
		Error:         nil,
		CorrelationID: correlation.FromContextOrNew(ctx),
	})
}

func WriteError(ctx context.Context, w http.ResponseWriter, status int, code string, message string, details map[string]any) {
	writeJSON(ctx, w, status, Envelope{
		OK:   false,
		Data: nil,
		Error: &ErrorBody{
			Code:    code,
			Message: message,
			Details: details,
		},
		CorrelationID: correlation.FromContextOrNew(ctx),
	})
}

func writeJSON(_ context.Context, w http.ResponseWriter, status int, envelope Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope)
}
