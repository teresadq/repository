package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"kkrepo-go/internal/ldapauth"
	"kkrepo-go/internal/oidc"
	"kkrepo-go/internal/security"
	"kkrepo-go/internal/store"
)

const sessionCookie = "kkrepo_session"

// Auth implements store-backed sessions, HTTP Basic, bearer API tokens and
// signed short-lived protocol tokens.
// LDAPAuthenticator is the subset of ldapauth used by Auth (kept as an
// interface so tests can inject a fake).
type LDAPAuthenticator interface {
	Authenticate(username, password string) (*ldapauth.User, bool)
	DefaultRoles() []string
}

type Auth struct {
	store  *store.Store
	secret []byte
	ttl    time.Duration

	ldap LDAPAuthenticator
	oidc *oidc.Provider
}

// NewAuth builds an authenticator.
func NewAuth(st *store.Store, secret string, ttl time.Duration) *Auth {
	key := []byte(secret)
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			panic(err)
		}
	}
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &Auth{store: st, secret: key, ttl: ttl}
}

// SetLDAP configures LDAP fallback authentication.
func (a *Auth) SetLDAP(authenticator LDAPAuthenticator) { a.ldap = authenticator }

// SetOIDC configures the OIDC provider.
func (a *Auth) SetOIDC(provider *oidc.Provider) { a.oidc = provider }

// OIDCEnabled reports whether OIDC login is available.
func (a *Auth) OIDCEnabled() bool { return a.oidc != nil && a.oidc.Enabled() }

// OIDC returns the configured provider.
func (a *Auth) OIDC() *oidc.Provider { return a.oidc }

// Login validates local credentials, then falls back to LDAP.
func (a *Auth) Login(username, password string) (security.Principal, bool) {
	user, err := a.store.GetUser(username)
	if err == nil && user.Active && security.VerifyPassword(user.PasswordHash, password) {
		return a.principal(user)
	}
	if a.ldap != nil {
		if ldapUser, ok := a.ldap.Authenticate(username, password); ok {
			return a.provisionExternal(ldapUser.Username, ldapUser.Name, ldapUser.Email,
				ldapUser.Groups, a.ldap.DefaultRoles())
		}
	}
	return security.Principal{}, false
}

// ProvisionExternal creates or updates an OIDC/LDAP user and returns a principal.
func (a *Auth) ProvisionExternal(username, name, email string, groups, defaultRoles []string) (security.Principal, bool) {
	return a.provisionExternal(username, name, email, groups, defaultRoles)
}

func (a *Auth) provisionExternal(username, name, email string, groups, defaultRoles []string) (security.Principal, bool) {
	username = sanitizeUsername(username)
	if username == "" {
		return security.Principal{}, false
	}
	user, err := a.store.GetUser(username)
	if err != nil {
		user = &store.User{Username: username, Active: true}
	}
	if name != "" {
		user.Name = name
	} else if user.Name == "" {
		user.Name = username
	}
	if email != "" {
		user.Email = email
	}
	roles := append([]string{}, user.Roles...)
	roles = append(roles, defaultRoles...)
	for _, group := range groups {
		if _, err := a.store.GetRole(group); err == nil {
			roles = append(roles, group)
		}
	}
	user.Roles = uniqueStrings(roles)
	user.Groups = uniqueStrings(append(user.Groups, groups...))
	user.Active = true
	persisted, err := a.store.PutUser(user)
	if err != nil {
		return security.Principal{}, false
	}
	return a.principal(persisted)
}

func sanitizeUsername(value string) string {
	var builder strings.Builder
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			ch == '.', ch == '_', ch == '-', ch == '@':
			builder.WriteRune(ch)
		case ch == ' ', ch == '/', ch == '\\':
			builder.WriteRune('-')
		}
	}
	return builder.String()
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// SignTemporary signs a short-lived payload (used for OIDC state cookies).
func (a *Auth) SignTemporary(payload string, ttl time.Duration) string {
	return a.sign(payload, time.Now().Add(ttl))
}

// VerifyTemporary verifies a payload produced by SignTemporary.
func (a *Auth) VerifyTemporary(token string) (string, bool) { return a.verify(token) }

func (a *Auth) principal(user *store.User) (security.Principal, bool) {
	privileges, ok := a.store.EffectivePrivileges(user.Username)
	if !ok {
		return security.Principal{}, false
	}
	return security.Principal{Username: user.Username, Name: user.Name, Privileges: privileges}, true
}

// Authenticate tries the session cookie, bearer token and HTTP Basic.
func (a *Auth) Authenticate(r *http.Request) (security.Principal, bool) {
	if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
		if username, ok := a.verify(cookie.Value); ok {
			if user, err := a.store.GetUser(username); err == nil && user.Active {
				return a.principal(user)
			}
		}
	}
	authorization := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
		token := strings.TrimSpace(authorization[len("Bearer "):])
		if username, ok := a.verify(token); ok {
			if user, err := a.store.GetUser(username); err == nil && user.Active {
				return a.principal(user)
			}
		}
		if username, ok := a.store.FindTokenByHash(store.HashToken(token), time.Now()); ok {
			if user, err := a.store.GetUser(username); err == nil && user.Active {
				return a.principal(user)
			}
		}
	}
	if username, password, ok := r.BasicAuth(); ok {
		if principal, ok := a.Login(username, password); ok {
			return principal, true
		}
		if tokenOwner, ok := a.store.FindTokenByHash(store.HashToken(password), time.Now()); ok &&
			(tokenOwner == username || username == "token") {
			if user, err := a.store.GetUser(tokenOwner); err == nil && user.Active {
				return a.principal(user)
			}
		}
	}
	return security.Principal{}, false
}

// AuthenticateAny returns an authenticated principal or the anonymous one when
// anonymous access is enabled.
func (a *Auth) AuthenticateAny(r *http.Request) (security.Principal, bool) {
	if principal, ok := a.Authenticate(r); ok {
		return principal, true
	}
	if privileges, ok := a.store.AnonymousPrivileges(); ok {
		return security.Principal{Username: "anonymous", Anonymous: true, Privileges: privileges}, true
	}
	return security.Principal{}, false
}

// SetSession writes the signed session cookie.
func (a *Auth) SetSession(w http.ResponseWriter, username string) {
	expiry := time.Now().Add(a.ttl)
	token := a.sign(username, expiry)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiry,
		MaxAge:   int(a.ttl.Seconds()),
	})
}

// ClearSession expires the session cookie.
func (a *Auth) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (a *Auth) sign(username string, expiry time.Time) string {
	payload := username + "|" + strconv.FormatInt(expiry.Unix(), 10)
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + signature
}

func (a *Auth) verify(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	payload := string(payloadBytes)
	index := strings.LastIndexByte(payload, '|')
	if index <= 0 {
		return "", false
	}
	username := payload[:index]
	expiryUnix, err := strconv.ParseInt(payload[index+1:], 10, 64)
	if err != nil {
		return "", false
	}
	if time.Now().After(time.Unix(expiryUnix, 0)) {
		return "", false
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) != 1 {
		return "", false
	}
	return username, true
}

type principalContextKey struct{}

// WithPrincipal attaches the caller to a request context.
func WithPrincipal(r *http.Request, principal security.Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal))
}

// PrincipalFrom returns the request principal.
func PrincipalFrom(r *http.Request) security.Principal {
	principal, _ := r.Context().Value(principalContextKey{}).(security.Principal)
	return principal
}
