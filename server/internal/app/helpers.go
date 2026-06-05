package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/curly-hub/podorel/internal/logging"
	"github.com/curly-hub/podorel/server/internal/api"
	"github.com/curly-hub/podorel/server/internal/db"
)

type sessionHandler func(http.ResponseWriter, *http.Request, db.Session)

func (a *App) withSession(next sessionHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := rawSessionID(r)
		if sessionID == "" {
			api.WriteError(r.Context(), w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required.", nil)
			return
		}
		session, err := a.store.SessionByID(r.Context(), sessionID)
		if err != nil {
			api.WriteError(r.Context(), w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required.", nil)
			return
		}
		next(w, r, session)
	}
}

func rawSessionID(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (a *App) setSessionCookie(w http.ResponseWriter, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.secureSessionCookies(),
	})
}

func (a *App) secureSessionCookies() bool {
	return a.cfg.Server.UsesHTTPS()
}

func (a *App) requireCSRF(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	token := r.Header.Get(csrfHeaderName)
	if token == "" {
		api.WriteError(r.Context(), w, http.StatusForbidden, "CSRF_REQUIRED", "CSRF token is required.", nil)
		return false
	}
	if !a.store.ValidateCSRF(r.Context(), rawSessionID(r), token) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "CSRF_INVALID", "CSRF token is invalid.", nil)
		return false
	}
	return true
}

func (a *App) requireAdminPasswordSession(w http.ResponseWriter, r *http.Request, session db.Session) bool {
	if !isAdminSessionType(session.SessionType) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "ADMIN_SESSION_REQUIRED", "Admin session is required.", nil)
		return false
	}
	return true
}

func (a *App) canAccessAgent(session db.Session, agentID string) bool {
	if isAdminSessionType(session.SessionType) {
		return true
	}
	return session.AgentID != "" && session.AgentID == agentID
}

func isAdminSessionType(sessionType string) bool {
	return sessionType == "admin_password" || sessionType == "passkey"
}

func decodeJSON(r *http.Request, w http.ResponseWriter, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		api.WriteError(r.Context(), w, http.StatusBadRequest, "INVALID_JSON", "Request JSON is invalid.", nil)
		return false
	}
	return true
}

func (a *App) audit(r *http.Request, actorUserID string, action string, targetType string, targetID string, result string, details map[string]any) {
	safeDetails := logging.SanitizeMap(details)
	if err := a.store.WriteAudit(r.Context(), db.AuditEvent{
		ActorUserID: actorUserID,
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		Result:      result,
		Details:     safeDetails,
	}); err != nil {
		a.logger.Error(r.Context(), "audit_write", "failed to write audit event", map[string]any{
			"action": action,
			"error":  err.Error(),
		})
	}
}

func (a *App) internalError(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error(r.Context(), "api_error", "api operation failed", map[string]any{"error": err.Error()})
	api.WriteError(r.Context(), w, http.StatusInternalServerError, "INTERNAL_ERROR", "Operation failed.", nil)
}

func (a *App) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, db.ErrNotFound) {
		api.WriteError(r.Context(), w, http.StatusNotFound, "NOT_FOUND", "Resource was not found.", nil)
		return
	}
	a.internalError(w, r, err)
}

func (a *App) throttled(key string) bool {
	a.failMu.Lock()
	defer a.failMu.Unlock()
	windowStart := a.now().Add(-a.cfg.Auth.FailedWindow)
	failures := compactFailures(a.failures[key], windowStart)
	a.failures[key] = failures
	return len(failures) >= a.cfg.Auth.FailedLoginLimit
}

func (a *App) recordFailure(key string) {
	a.failMu.Lock()
	defer a.failMu.Unlock()
	windowStart := a.now().Add(-a.cfg.Auth.FailedWindow)
	failures := compactFailures(a.failures[key], windowStart)
	failures = append(failures, a.now())
	a.failures[key] = failures
}

func (a *App) clearFailures(key string) {
	a.failMu.Lock()
	defer a.failMu.Unlock()
	delete(a.failures, key)
}

func compactFailures(input []time.Time, windowStart time.Time) []time.Time {
	output := input[:0]
	for _, failure := range input {
		if failure.After(windowStart) {
			output = append(output, failure)
		}
	}
	return output
}

func joinPathParts(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.Trim(part, "/"); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "/")
}
