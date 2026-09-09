// Package auth verifies OIDC bearer tokens and authorizes REST requests.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	ScopeRead   = "wes-work-planning.read"
	ScopeWrite  = "wes-work-planning.write"
	problemBase = "https://errors.wes-work-planning.warehouse-systems.dev/"
)

// Claims is the verified identity information used for authorization.
type Claims struct {
	Subject string          `json:"sub"`
	Scope   json.RawMessage `json:"scope"`
}

// HasScope accepts both the OAuth 2.0 conventional space-delimited scope
// string and providers that emit scope as a JSON array.
func (c Claims) HasScope(want string) bool {
	var text string
	if json.Unmarshal(c.Scope, &text) == nil {
		return contains(strings.Fields(text), want)
	}
	var list []string
	if json.Unmarshal(c.Scope, &list) == nil {
		return contains(list, want)
	}
	return false
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// Verifier performs discovered-provider signature, issuer, expiry and audience verification.
type Verifier struct{ verifier *oidc.IDTokenVerifier }

// NewVerifier discovers an OIDC provider and builds its standards-compliant verifier.
func NewVerifier(ctx context.Context, issuerURL, clientID string) (*Verifier, error) {
	if strings.TrimSpace(issuerURL) == "" || strings.TrimSpace(clientID) == "" {
		return nil, errors.New("OIDC_ISSUER_URL and OIDC_CLIENT_ID are required")
	}
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC provider discovery: %w", err)
	}
	return &Verifier{verifier: provider.Verifier(&oidc.Config{ClientID: clientID})}, nil
}

// Verify verifies the signed ID token with the discovered JWKS and extracts scopes.
func (v *Verifier) Verify(ctx context.Context, raw string) (Claims, error) {
	if v == nil || v.verifier == nil {
		return Claims{}, errors.New("OIDC verifier is not configured")
	}
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return Claims{}, err
	}
	var claims Claims
	if err := token.Claims(&claims); err != nil {
		return Claims{}, fmt.Errorf("OIDC token claims: %w", err)
	}
	return claims, nil
}

// Middleware requires read scope for safe methods and write scope for mutations.
type Middleware struct{ Verifier *Verifier }

func (m Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeProblem(w, r, http.StatusUnauthorized, "invalid_request", "Bearer token is required", "")
			return
		}
		claims, err := m.Verifier.Verify(r.Context(), raw)
		if err != nil {
			writeProblem(w, r, http.StatusUnauthorized, "invalid_token", "Bearer token is invalid", "")
			return
		}
		required := ScopeWrite
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			required = ScopeRead
		}
		if !claims.HasScope(required) {
			writeProblem(w, r, http.StatusForbidden, "insufficient_scope", "Bearer token lacks the required scope", required)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	returnValue := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != "" {
		returnValue = parts[1]
	}
	return returnValue, returnValue != ""
}

type problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, detail, scope string) {
	w.Header().Set("Content-Type", "application/problem+json")
	challenge := `Bearer error="` + code + `"`
	if scope != "" {
		challenge += `, scope="` + scope + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Type: problemBase + code, Title: http.StatusText(status), Status: status, Detail: detail, Instance: r.URL.Path})
}
