// Package oidc implements a minimal OpenID Connect authorization-code client
// with RS256 ID token verification using only the standard library.
package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Config configures an OIDC provider.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       string
	DefaultRoles []string
}

// Claims is the subset of ID token/user claims used by kkrepo-go.
type Claims struct {
	Username string
	Name     string
	Email    string
	Groups   []string
}

// Provider performs discovery, authorization-code exchange and ID token checks.
type Provider struct {
	cfg    Config
	client *http.Client

	mu        sync.Mutex
	discovery discovery
	jwks      map[string]*rsa.PublicKey
	fetched   time.Time
}

type discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// New creates a provider. Empty issuer/client id disables OIDC.
func New(cfg Config) *Provider {
	if strings.TrimSpace(cfg.Scopes) == "" {
		cfg.Scopes = "openid profile email"
	}
	return &Provider{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}
}

// Enabled reports whether OIDC configuration is present.
func (p *Provider) Enabled() bool {
	return p != nil && p.cfg.Issuer != "" && p.cfg.ClientID != ""
}

// DefaultRoles returns roles assigned to provisioned OIDC users.
func (p *Provider) DefaultRoles() []string { return append([]string{}, p.cfg.DefaultRoles...) }

// AuthCodeURL builds the authorization redirect.
func (p *Provider) AuthCodeURL(state, nonce string) (string, error) {
	conf, err := p.config(context.Background())
	if err != nil {
		return "", err
	}
	values := url.Values{
		"client_id":     {p.cfg.ClientID},
		"response_type": {"code"},
		"redirect_uri":  {p.cfg.RedirectURL},
		"scope":         {p.cfg.Scopes},
		"state":         {state},
		"nonce":         {nonce},
	}
	return conf.AuthorizationEndpoint + "?" + values.Encode(), nil
}

// Exchange redeems an authorization code and verifies the ID token.
func (p *Provider) Exchange(ctx context.Context, code, nonce string) (*Claims, error) {
	conf, err := p.config(ctx)
	if err != nil {
		return nil, err
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.cfg.RedirectURL},
		"client_id":     {p.cfg.ClientID},
		"client_secret": {p.cfg.ClientSecret},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, conf.TokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC token endpoint returned %d: %s", response.StatusCode, body)
	}
	var token struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil || token.IDToken == "" {
		return nil, fmt.Errorf("OIDC token response has no id_token")
	}
	claims, err := p.verifyIDToken(ctx, conf, token.IDToken)
	if err != nil {
		return nil, err
	}
	if claimNonce, _ := claims["nonce"].(string); claimNonce != nonce {
		return nil, fmt.Errorf("OIDC nonce mismatch")
	}
	if issuer, _ := claims["iss"].(string); issuer != p.cfg.Issuer {
		return nil, fmt.Errorf("OIDC issuer mismatch")
	}
	if !audienceContains(claims["aud"], p.cfg.ClientID) {
		return nil, fmt.Errorf("OIDC audience mismatch")
	}
	if expiry, ok := claims["exp"].(float64); !ok || time.Now().After(time.Unix(int64(expiry), 0)) {
		return nil, fmt.Errorf("OIDC ID token expired")
	}
	username := firstString(claims, "preferred_username", "email", "sub")
	if username == "" {
		return nil, fmt.Errorf("OIDC token has no usable subject")
	}
	return &Claims{
		Username: username,
		Name:     firstString(claims, "name", "preferred_username"),
		Email:    firstString(claims, "email"),
		Groups:   stringList(claims["groups"]),
	}, nil
}

func (p *Provider) config(ctx context.Context) (discovery, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.discovery.AuthorizationEndpoint != "" && time.Since(p.fetched) < 10*time.Minute {
		return p.discovery, nil
	}
	target := strings.TrimRight(p.cfg.Issuer, "/") + "/.well-known/openid-configuration"
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	response, err := p.client.Do(request)
	if err != nil {
		return discovery{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return discovery{}, fmt.Errorf("OIDC discovery returned %d", response.StatusCode)
	}
	var conf discovery
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&conf); err != nil {
		return discovery{}, err
	}
	if conf.AuthorizationEndpoint == "" || conf.TokenEndpoint == "" || conf.JWKSURI == "" {
		return discovery{}, fmt.Errorf("OIDC discovery document is incomplete")
	}
	p.discovery = conf
	p.fetched = time.Now()
	return conf, nil
}

func (p *Provider) verifyIDToken(ctx context.Context, conf discovery, token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid ID token")
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerRaw, &header); err != nil || header.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported ID token algorithm")
	}
	keys, err := p.publicKeys(ctx, conf)
	if err != nil {
		return nil, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	signed := []byte(parts[0] + "." + parts[1])
	digest := sha256.Sum256(signed)
	verified := false
	for kid, key := range keys {
		if header.Kid != "" && kid != header.Kid {
			continue
		}
		if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) == nil {
			verified = true
			break
		}
	}
	if !verified {
		return nil, fmt.Errorf("invalid ID token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func (p *Provider) publicKeys(ctx context.Context, conf discovery) (map[string]*rsa.PublicKey, error) {
	p.mu.Lock()
	if len(p.jwks) > 0 && time.Since(p.fetched) < 10*time.Minute {
		keys := p.jwks
		p.mu.Unlock()
		return keys, nil
	}
	p.mu.Unlock()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, conf.JWKSURI, nil)
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var document struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&document); err != nil {
		return nil, err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, key := range document.Keys {
		if key.Kty != "RSA" {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			continue
		}
		exponent, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			continue
		}
		e := 0
		for _, b := range exponent {
			e = e<<8 + int(b)
		}
		keys[key.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: e}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("OIDC JWKS has no RSA keys")
	}
	p.mu.Lock()
	p.jwks = keys
	p.fetched = time.Now()
	p.mu.Unlock()
	return keys, nil
}

func audienceContains(value any, clientID string) bool {
	switch audience := value.(type) {
	case string:
		return audience == clientID
	case []any:
		for _, item := range audience {
			if text, ok := item.(string); ok && text == clientID {
				return true
			}
		}
	}
	return false
}

func firstString(claims map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := claims[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	case string:
		return []string{typed}
	}
	return nil
}
