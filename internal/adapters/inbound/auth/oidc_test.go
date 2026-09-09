package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/claudioed/wes-work-planning/internal/adapters/inbound/auth"
)

type oidcFixture struct {
	issuer string
	key    *rsa.PrivateKey
	kid    string
	server *httptest.Server
}

func newOIDCFixture(t *testing.T) *oidcFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &oidcFixture{key: key, kid: "test-key"}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": f.issuer, "jwks_uri": f.issuer + "/keys"})
		case "/keys":
			n := base64.RawURLEncoding.EncodeToString(f.key.N.Bytes())
			e := base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1})
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": f.kid, "alg": "RS256", "use": "sig", "n": n, "e": e}}})
		default:
			http.NotFound(w, r)
		}
	}))
	f.issuer = f.server.URL
	t.Cleanup(f.server.Close)
	return f
}
func (f *oidcFixture) token(t *testing.T, issuer, audience string, expiry time.Time, scope any) string {
	t.Helper()
	c := jwt.MapClaims{"iss": issuer, "aud": audience, "exp": expiry.Unix(), "iat": time.Now().Add(-time.Minute).Unix(), "sub": "warehouse-user", "scope": scope}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
	tok.Header["kid"] = f.kid
	raw, err := tok.SignedString(f.key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestVerifier_VerifiesProviderDiscoveryJWKSAndClaims(t *testing.T) {
	f := newOIDCFixture(t)
	v, err := auth.NewVerifier(context.Background(), f.issuer, "wes-client")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, token string
		wantErr     bool
	}{
		{"valid scope string", f.token(t, f.issuer, "wes-client", time.Now().Add(time.Hour), "wes-work-planning.read wes-work-planning.write"), false},
		{"valid scope array", f.token(t, f.issuer, "wes-client", time.Now().Add(time.Hour), []string{"wes-work-planning.read"}), false},
		{"expired", f.token(t, f.issuer, "wes-client", time.Now().Add(-time.Hour), "wes-work-planning.read"), true},
		{"wrong issuer", f.token(t, "https://other.invalid", "wes-client", time.Now().Add(time.Hour), "wes-work-planning.read"), true},
		{"wrong audience", f.token(t, f.issuer, "other-client", time.Now().Add(time.Hour), "wes-work-planning.read"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := v.Verify(context.Background(), tt.token)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Verify error=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && !claims.HasScope("wes-work-planning.read") {
				t.Fatal("scope was not parsed")
			}
		})
	}
}
func TestMiddleware_RFC6750AndScopes(t *testing.T) {
	f := newOIDCFixture(t)
	v, err := auth.NewVerifier(context.Background(), f.issuer, "wes-client")
	if err != nil {
		t.Fatal(err)
	}
	protected := auth.Middleware{Verifier: v}.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	cases := []struct {
		name, method, token string
		status              int
		www                 string
	}{
		{"missing", http.MethodGet, "", 401, "invalid_request"},
		{"read can get", http.MethodGet, f.token(t, f.issuer, "wes-client", time.Now().Add(time.Hour), "wes-work-planning.read"), 204, ""},
		{"read cannot post", http.MethodPost, f.token(t, f.issuer, "wes-client", time.Now().Add(time.Hour), "wes-work-planning.read"), 403, "insufficient_scope"},
		{"array write can post", http.MethodPost, f.token(t, f.issuer, "wes-client", time.Now().Add(time.Hour), []string{"wes-work-planning.write"}), 204, ""},
		{"missing required scope", http.MethodGet, f.token(t, f.issuer, "wes-client", time.Now().Add(time.Hour), "other"), 403, "insufficient_scope"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, "/protected", nil)
			if tt.token != "" {
				r.Header.Set("Authorization", "Bearer "+tt.token)
			}
			w := httptest.NewRecorder()
			protected.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tt.status, w.Body.String())
			}
			if tt.www != "" && !strings.Contains(w.Header().Get("WWW-Authenticate"), tt.www) {
				t.Fatalf("WWW-Authenticate=%q", w.Header().Get("WWW-Authenticate"))
			}
			if tt.status >= 400 && !strings.Contains(w.Header().Get("Content-Type"), "application/problem+json") {
				t.Fatalf("content-type=%q", w.Header().Get("Content-Type"))
			}
		})
	}
}
func TestNewVerifierRejectsInvalidConfiguration(t *testing.T) {
	_, err := auth.NewVerifier(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = url.Parse("")
	_ = err
}
