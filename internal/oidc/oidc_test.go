package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExchangeVerifiesIDToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer": serverURL(r), "authorization_endpoint": serverURL(r) + "/auth",
				"token_endpoint": serverURL(r) + "/token", "jwks_uri": serverURL(r) + "/jwks",
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
				"kty": "RSA", "kid": "test", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}}})
		case "/token":
			claims := map[string]any{
				"iss": serverURL(r), "aud": "client", "exp": time.Now().Add(5 * time.Minute).Unix(),
				"nonce": "nonce-1", "preferred_username": "alice", "name": "Alice",
				"email": "alice@example.test", "groups": []string{"nx-developer"},
			}
			header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
			payload, _ := json.Marshal(claims)
			signed := header + "." + base64.RawURLEncoding.EncodeToString(payload)
			digest := sha256.Sum256([]byte(signed))
			signature, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id_token": signed + "." + base64.RawURLEncoding.EncodeToString(signature),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := New(Config{
		Issuer: server.URL, ClientID: "client", ClientSecret: "secret",
		RedirectURL: "http://app.test/callback",
	})
	redirect, err := provider.AuthCodeURL("state-1", "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"client_id=client", "state=state-1", "nonce=nonce-1"} {
		if !strings.Contains(redirect, expected) {
			t.Fatalf("redirect missing %s: %s", expected, redirect)
		}
	}
	claims, err := provider.Exchange(context.Background(), "code-1", "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	if claims.Username != "alice" || claims.Email != "alice@example.test" || len(claims.Groups) != 1 {
		t.Fatalf("claims = %+v", claims)
	}
	if _, err := provider.Exchange(context.Background(), "code-1", "wrong"); err == nil {
		t.Fatal("wrong nonce should fail")
	}
}

func serverURL(r *http.Request) string { return "http://" + r.Host }
