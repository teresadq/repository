package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

const oidcStateCookie = "kkrepo_oidc_state"

func randomHex(bytes int) string {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	return hex.EncodeToString(buffer)
}

func (s *Server) handleAuthOptions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"local": true, "oidc": s.auth.OIDCEnabled(),
	})
}

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	provider := s.auth.OIDC()
	if provider == nil || !provider.Enabled() {
		writeError(w, http.StatusNotFound, "OIDC is not configured")
		return
	}
	state := randomHex(16)
	nonce := randomHex(16)
	if state == "" || nonce == "" {
		writeError(w, http.StatusInternalServerError, "Unable to create OIDC state")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: s.auth.SignTemporary(state+":"+nonce, 10*time.Minute),
		Path: "/api/oidc", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	redirect, err := provider.AuthCodeURL(state, nonce)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	provider := s.auth.OIDC()
	if provider == nil || !provider.Enabled() {
		writeError(w, http.StatusNotFound, "OIDC is not configured")
		return
	}
	cookie, err := r.Cookie(oidcStateCookie)
	if err != nil {
		writeError(w, http.StatusBadRequest, "OIDC state cookie is missing")
		return
	}
	payload, ok := s.auth.VerifyTemporary(cookie.Value)
	if !ok {
		writeError(w, http.StatusBadRequest, "OIDC state is invalid or expired")
		return
	}
	state, nonce, ok := strings.Cut(payload, ":")
	if !ok || state == "" || nonce == "" || r.URL.Query().Get("state") != state {
		writeError(w, http.StatusBadRequest, "OIDC state mismatch")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "OIDC authorization code is missing")
		return
	}
	claims, err := provider.Exchange(context.Background(), code, nonce)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	principal, ok := s.auth.ProvisionExternal(claims.Username, claims.Name, claims.Email,
		claims.Groups, provider.DefaultRoles())
	if !ok {
		writeError(w, http.StatusForbidden, "OIDC user provisioning failed")
		return
	}
	s.auth.SetSession(w, principal.Username)
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: "", Path: "/api/oidc", HttpOnly: true, MaxAge: -1,
	})
	s.store.AppendAudit(store.AuditEntry{
		User: principal.Username, Action: "security.oidc.login", Status: http.StatusFound, IP: clientIP(r),
	})
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}
