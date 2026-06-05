package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/curly-hub/podorel/server/internal/api"
	"github.com/curly-hub/podorel/server/internal/auth"
	"github.com/curly-hub/podorel/server/internal/db"
)

const (
	passkeyFlowTTL             = 5 * time.Minute
	passkeyFlowRegistration    = "registration"
	passkeyFlowLogin           = "login"
	passkeySessionType         = "passkey"
	defaultPasskeyDisplayName  = "PoDorel"
	maxPasskeyCredentialName   = 80
	passkeyRegistrationTimeout = 2 * time.Minute
	passkeyLoginTimeout        = 2 * time.Minute
)

type passkeyFlow struct {
	ID        string
	Purpose   string
	UserID    string
	Name      string
	Session   webauthn.SessionData
	ExpiresAt time.Time
}

type passkeyBeginRegistrationRequest struct {
	Name string `json:"name"`
}

type passkeyBeginResponse struct {
	FlowID    string `json:"flow_id"`
	PublicKey any    `json:"public_key"`
}

type passkeyCredentialResponse struct {
	ID           string     `json:"id"`
	UserID       string     `json:"user_id"`
	CredentialID string     `json:"credential_id"`
	Name         string     `json:"name"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

type passkeyUser struct {
	user        db.User
	credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte {
	return []byte(u.user.ID)
}

func (u *passkeyUser) WebAuthnName() string {
	return u.user.Username
}

func (u *passkeyUser) WebAuthnDisplayName() string {
	return u.user.Username
}

func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

func (a *App) handleBeginPasskeyRegistration(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireAdminPasswordSession(w, r, session) || !a.requireCSRF(w, r) {
		return
	}
	var req passkeyBeginRegistrationRequest
	if !decodeJSON(r, w, &req) {
		return
	}
	user, err := a.loadPasskeyUser(r.Context(), session.UserID)
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	webAuthn, err := a.webauthnForRequest(r)
	if err != nil {
		a.writePasskeyUnavailable(w, r, err)
		return
	}
	creation, webAuthnSession, err := webAuthn.BeginRegistration(
		user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
	)
	if err != nil {
		a.writePasskeyUnavailable(w, r, err)
		return
	}
	flowID, err := a.savePasskeyFlow(passkeyFlow{
		Purpose: passkeyFlowRegistration,
		UserID:  session.UserID,
		Name:    cleanPasskeyName(req.Name, defaultPasskeyName(r)),
		Session: *webAuthnSession,
	})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, passkeyBeginResponse{FlowID: flowID, PublicKey: creation.Response})
}

func (a *App) handleFinishPasskeyRegistration(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireAdminPasswordSession(w, r, session) || !a.requireCSRF(w, r) {
		return
	}
	flow, ok := a.consumePasskeyFlow(r.URL.Query().Get("flow_id"), passkeyFlowRegistration)
	if !ok {
		api.WriteError(r.Context(), w, http.StatusBadRequest, "PASSKEY_FLOW_EXPIRED", "Passkey registration expired. Start again.", nil)
		return
	}
	if flow.UserID != session.UserID {
		api.WriteError(r.Context(), w, http.StatusForbidden, "PASSKEY_FLOW_MISMATCH", "Passkey registration belongs to another session.", nil)
		return
	}
	user, err := a.loadPasskeyUser(r.Context(), session.UserID)
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	webAuthn, err := a.webauthnForRequest(r)
	if err != nil {
		a.writePasskeyUnavailable(w, r, err)
		return
	}
	credential, err := webAuthn.FinishRegistration(user, flow.Session, r)
	if err != nil {
		a.audit(r, session.UserID, "auth.passkey.register", "user", session.UserID, "failure", map[string]any{"reason": "verification_failed"})
		api.WriteError(r.Context(), w, http.StatusBadRequest, "PASSKEY_VERIFICATION_FAILED", "Could not verify the passkey response.", nil)
		return
	}
	rawCredential, err := json.Marshal(credential)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	stored, err := a.store.SavePasskeyCredential(r.Context(), session.UserID, flow.Name, credentialIDString(credential.ID), string(rawCredential))
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, session.UserID, "auth.passkey.register", "passkey", stored.ID, "success", map[string]any{"name": stored.Name})
	api.WriteOK(r.Context(), w, passkeyCredentialPayload(stored))
}

func (a *App) handleBeginPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	webAuthn, err := a.webauthnForRequest(r)
	if err != nil {
		a.writePasskeyUnavailable(w, r, err)
		return
	}
	assertion, webAuthnSession, err := webAuthn.BeginDiscoverableMediatedLogin(
		protocol.MediationDefault,
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		a.writePasskeyUnavailable(w, r, err)
		return
	}
	flowID, err := a.savePasskeyFlow(passkeyFlow{
		Purpose: passkeyFlowLogin,
		Session: *webAuthnSession,
	})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, passkeyBeginResponse{FlowID: flowID, PublicKey: assertion.Response})
}

func (a *App) handleFinishPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	flow, ok := a.consumePasskeyFlow(r.URL.Query().Get("flow_id"), passkeyFlowLogin)
	if !ok {
		api.WriteError(r.Context(), w, http.StatusBadRequest, "PASSKEY_FLOW_EXPIRED", "Passkey login expired. Start again.", nil)
		return
	}
	webAuthn, err := a.webauthnForRequest(r)
	if err != nil {
		a.writePasskeyUnavailable(w, r, err)
		return
	}
	validatedUser, credential, err := webAuthn.FinishPasskeyLogin(a.discoverablePasskeyUserHandler(r.Context()), flow.Session, r)
	if err != nil {
		a.audit(r, "", "auth.login.passkey", "user", "", "failure", map[string]any{"reason": "verification_failed"})
		api.WriteError(r.Context(), w, http.StatusUnauthorized, "AUTH_FAILED", "Invalid credentials.", nil)
		return
	}
	user, ok := validatedUser.(*passkeyUser)
	if !ok {
		a.internalError(w, r, fmt.Errorf("validated passkey user had unexpected type %T", validatedUser))
		return
	}
	rawCredential, err := json.Marshal(credential)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := a.store.UpdatePasskeyCredential(r.Context(), user.user.ID, credentialIDString(credential.ID), string(rawCredential)); err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	created, err := a.store.CreateSession(r.Context(), user.user.ID, "", passkeySessionType, a.cfg.Auth.SessionTTL)
	if err != nil {
		api.WriteError(r.Context(), w, http.StatusInternalServerError, "SESSION_CREATE_FAILED", "Could not create session.", nil)
		return
	}
	a.setSessionCookie(w, created.SessionID, created.Session.ExpiresAt)
	a.audit(r, user.user.ID, "auth.login.passkey", "user", user.user.ID, "success", nil)
	api.WriteOK(r.Context(), w, map[string]any{
		"user":       map[string]any{"id": user.user.ID, "username": user.user.Username, "session_type": passkeySessionType},
		"csrf_token": created.CSRFToken,
	})
}

func (a *App) handleListPasskeys(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireAdminPasswordSession(w, r, session) {
		return
	}
	credentials, err := a.store.ListPasskeyCredentials(r.Context(), session.UserID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if credentials == nil {
		credentials = []db.PasskeyCredential{}
	}
	api.WriteOK(r.Context(), w, passkeyCredentialPayloads(credentials))
}

func (a *App) handleDeletePasskey(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireAdminPasswordSession(w, r, session) || !a.requireCSRF(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := a.store.DeletePasskeyCredential(r.Context(), session.UserID, id); err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	a.audit(r, session.UserID, "auth.passkey.delete", "passkey", id, "success", nil)
	api.WriteOK(r.Context(), w, map[string]any{"deleted": true, "id": id})
}

func (a *App) loadPasskeyUser(ctx context.Context, userID string) (*passkeyUser, error) {
	user, err := a.store.FindUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	storedCredentials, err := a.store.ListPasskeyCredentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	credentials := make([]webauthn.Credential, 0, len(storedCredentials))
	for _, storedCredential := range storedCredentials {
		var credential webauthn.Credential
		if err := json.Unmarshal([]byte(storedCredential.CredentialJSON), &credential); err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return &passkeyUser{user: user, credentials: credentials}, nil
}

func (a *App) discoverablePasskeyUserHandler(ctx context.Context) webauthn.DiscoverableUserHandler {
	return func(rawID, userHandle []byte) (webauthn.User, error) {
		if len(rawID) == 0 || len(userHandle) == 0 {
			return nil, db.ErrNotFound
		}
		user, err := a.loadPasskeyUser(ctx, string(userHandle))
		if err != nil {
			return nil, err
		}
		if !user.hasCredential(rawID) {
			return nil, db.ErrNotFound
		}
		return user, nil
	}
}

func (u *passkeyUser) hasCredential(rawID []byte) bool {
	for _, credential := range u.credentials {
		if string(credential.ID) == string(rawID) {
			return true
		}
	}
	return false
}

func (a *App) webauthnForRequest(r *http.Request) (*webauthn.WebAuthn, error) {
	origin, err := a.passkeyRequestOrigin(r)
	if err != nil {
		return nil, err
	}
	rpID := strings.ToLower(origin.Hostname())
	if rpID == "" {
		return nil, errors.New("passkey origin has no host")
	}
	origins := []string{originString(origin)}
	if publicOrigin, ok := a.publicPasskeyOrigin(rpID); ok && publicOrigin != origins[0] {
		origins = append(origins, publicOrigin)
	}
	return webauthn.New(&webauthn.Config{
		RPDisplayName:         defaultPasskeyDisplayName,
		RPID:                  rpID,
		RPOrigins:             origins,
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: protocol.ResidentKeyRequired(),
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Timeout: passkeyLoginTimeout},
			Registration: webauthn.TimeoutConfig{Timeout: passkeyRegistrationTimeout},
		},
	})
}

func (a *App) passkeyRequestOrigin(r *http.Request) (*url.URL, error) {
	if headerOrigin := strings.TrimSpace(r.Header.Get("Origin")); headerOrigin != "" {
		origin, err := parseHTTPOrigin(headerOrigin)
		if err != nil {
			return nil, err
		}
		if !a.originAllowedForPasskeyRequest(origin, r) {
			return nil, fmt.Errorf("origin %q does not match this PoDorel host", headerOrigin)
		}
		return origin, nil
	}
	host := r.Host
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if a.cfg.Server.TrustedProxyMode {
		if forwardedProto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); forwardedProto == "http" || forwardedProto == "https" {
			scheme = forwardedProto
		}
		if forwardedHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
			host = forwardedHost
		}
	}
	if host == "" {
		publicURL, err := parseHTTPOrigin(a.cfg.Server.PublicURL)
		if err != nil {
			return nil, errors.New("request host and public URL are missing")
		}
		return publicURL, nil
	}
	return parseHTTPOrigin(scheme + "://" + host)
}

func (a *App) originAllowedForPasskeyRequest(origin *url.URL, r *http.Request) bool {
	originHost := strings.ToLower(origin.Hostname())
	for _, candidate := range a.passkeyHostCandidates(r) {
		if candidate == originHost {
			return true
		}
	}
	return false
}

func (a *App) passkeyHostCandidates(r *http.Request) []string {
	candidates := []string{}
	addCandidate := func(host string) {
		hostname := hostnameFromHost(host)
		if hostname != "" {
			candidates = append(candidates, hostname)
		}
	}
	addCandidate(r.Host)
	if a.cfg.Server.TrustedProxyMode {
		addCandidate(firstForwardedValue(r.Header.Get("X-Forwarded-Host")))
	}
	if publicURL, err := parseHTTPOrigin(a.cfg.Server.PublicURL); err == nil {
		addCandidate(publicURL.Host)
	}
	return candidates
}

func (a *App) publicPasskeyOrigin(rpID string) (string, bool) {
	publicURL, err := parseHTTPOrigin(a.cfg.Server.PublicURL)
	if err != nil || strings.ToLower(publicURL.Hostname()) != rpID {
		return "", false
	}
	return originString(publicURL), true
}

func parseHTTPOrigin(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("origin %q must use http or https", value)
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("origin %q has no host", value)
	}
	return &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}, nil
}

func originString(origin *url.URL) string {
	return strings.ToLower(origin.Scheme) + "://" + origin.Host
}

func hostnameFromHost(host string) string {
	if host == "" {
		return ""
	}
	parsed, err := url.Parse("//" + strings.TrimSpace(host))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func firstForwardedValue(value string) string {
	first, _, _ := strings.Cut(value, ",")
	return strings.ToLower(strings.TrimSpace(first))
}

func (a *App) savePasskeyFlow(flow passkeyFlow) (string, error) {
	token, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	flow.ID = token
	flow.ExpiresAt = a.now().Add(passkeyFlowTTL)
	a.passkeyMu.Lock()
	defer a.passkeyMu.Unlock()
	a.cleanupPasskeyFlowsLocked(a.now())
	a.passkeyFlows[flow.ID] = flow
	return flow.ID, nil
}

func (a *App) consumePasskeyFlow(flowID string, purpose string) (passkeyFlow, bool) {
	if strings.TrimSpace(flowID) == "" {
		return passkeyFlow{}, false
	}
	a.passkeyMu.Lock()
	defer a.passkeyMu.Unlock()
	now := a.now()
	a.cleanupPasskeyFlowsLocked(now)
	flow, ok := a.passkeyFlows[flowID]
	if !ok || flow.Purpose != purpose || !flow.ExpiresAt.After(now) {
		delete(a.passkeyFlows, flowID)
		return passkeyFlow{}, false
	}
	delete(a.passkeyFlows, flowID)
	return flow, true
}

func (a *App) cleanupPasskeyFlowsLocked(now time.Time) {
	for id, flow := range a.passkeyFlows {
		if !flow.ExpiresAt.After(now) {
			delete(a.passkeyFlows, id)
		}
	}
}

func credentialIDString(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}

func passkeyCredentialPayloads(credentials []db.PasskeyCredential) []passkeyCredentialResponse {
	payloads := make([]passkeyCredentialResponse, 0, len(credentials))
	for _, credential := range credentials {
		payloads = append(payloads, passkeyCredentialPayload(credential))
	}
	return payloads
}

func passkeyCredentialPayload(credential db.PasskeyCredential) passkeyCredentialResponse {
	var lastUsed *time.Time
	if !credential.LastUsedAt.IsZero() {
		value := credential.LastUsedAt
		lastUsed = &value
	}
	return passkeyCredentialResponse{
		ID:           credential.ID,
		UserID:       credential.UserID,
		CredentialID: credential.CredentialID,
		Name:         credential.Name,
		CreatedAt:    credential.CreatedAt,
		UpdatedAt:    credential.UpdatedAt,
		LastUsedAt:   lastUsed,
	}
}

func defaultPasskeyName(r *http.Request) string {
	if origin, err := parseHTTPOrigin(r.Header.Get("Origin")); err == nil {
		return "Passkey on " + origin.Hostname()
	}
	if host := hostnameFromHost(r.Host); host != "" {
		return "Passkey on " + host
	}
	return "PoDorel passkey"
}

func cleanPasskeyName(name string, fallback string) string {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	if name == "" {
		name = fallback
	}
	runes := []rune(name)
	if len(runes) > maxPasskeyCredentialName {
		name = string(runes[:maxPasskeyCredentialName])
	}
	return name
}

func (a *App) writePasskeyUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error(r.Context(), "passkey_unavailable", "passkey operation could not start", map[string]any{"error": err.Error()})
	api.WriteError(r.Context(), w, http.StatusBadRequest, "PASSKEY_UNAVAILABLE", "Passkeys are not available for this origin. Use HTTPS or localhost.", map[string]any{"reason": err.Error()})
}
