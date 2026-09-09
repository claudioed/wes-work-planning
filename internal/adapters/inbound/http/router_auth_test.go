package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/claudioed/wes-work-planning/internal/adapters/inbound/auth"
	inboundhttp "github.com/claudioed/wes-work-planning/internal/adapters/inbound/http"
)

const (
	testReadKey      = "router-auth-read-key"
	testReadWriteKey = "router-auth-rw-key"
)

func newEnforcingRouter() http.Handler {
	authn := auth.NewStaticKeyAuth(map[string]auth.Scope{
		testReadKey:      auth.ScopeRead,
		testReadWriteKey: auth.ScopeReadWrite,
	})
	return inboundhttp.NewRouterWithAuth(newTestHandlers(), "wes-work-planning", nil,
		auth.Middleware{Authn: authn, Mode: auth.ModeEnforce})
}

func doAuth(t *testing.T, router http.Handler, method, path, token string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// chargeBody is a valid POST /paths/{pathId}/charge payload so a permitted
// mutating request exercises the real handler (2xx), not a 400.
const chargeBody = `{"buckets":[{"cpt":"2026-08-21T14:00:00Z","quantity":100}]}`

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantSlug string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var problem map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	wantType := "https://errors.wes-work-planning.warehouse-systems.dev/" + wantSlug
	if problem["type"] != wantType {
		t.Fatalf("problem type = %v, want %s", problem["type"], wantType)
	}
	if int(problem["status"].(float64)) != wantStatus {
		t.Fatalf("problem status = %v, want %d", problem["status"], wantStatus)
	}
}

// TestRouterAuth_Table is the fleet-standard router table (ADR-0015): the
// enforce-mode middleware on one GET and one mutating route, plus the open
// /healthz probe.
func TestRouterAuth_Table(t *testing.T) {
	router := newEnforcingRouter()

	t.Run("no token on GET -> 401 problem+json with WWW-Authenticate", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodGet, "/paths/pick-zone-a/telemetry", "", "")
		assertProblem(t, rec, http.StatusUnauthorized, "unauthenticated")
		if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
			t.Fatalf("WWW-Authenticate = %q, want Bearer challenge", got)
		}
	})

	t.Run("no token on POST -> 401", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodPost, "/paths/pick-zone-a/charge", "", chargeBody)
		assertProblem(t, rec, http.StatusUnauthorized, "unauthenticated")
	})

	t.Run("read key on GET -> passes the middleware", func(t *testing.T) {
		// The pool does not exist in this fresh router, so the HANDLER
		// answers 404 — what matters is that the middleware let it through
		// (not 401/403).
		rec := doAuth(t, router, http.MethodGet, "/paths/pick-zone-a/telemetry", testReadKey, "")
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Fatalf("read key on GET rejected with %d; body=%s", rec.Code, rec.Body.String())
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 from the handler (unknown path); body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("read key on GET /work-units -> 2xx", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodGet, "/work-units?reference=order-1", testReadKey, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("read key on POST -> 403 insufficient-scope", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodPost, "/paths/pick-zone-a/charge", testReadKey, chargeBody)
		assertProblem(t, rec, http.StatusForbidden, "insufficient-scope")
		if rec.Header().Get("WWW-Authenticate") != "" {
			t.Fatalf("403 must not carry a WWW-Authenticate challenge")
		}
	})

	t.Run("read-write key on POST -> 2xx", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodPost, "/paths/pick-zone-a/charge", testReadWriteKey, chargeBody)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("read-write key on GET -> 2xx", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodGet, "/work-units?reference=order-1", testReadWriteKey, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown token -> 401", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodGet, "/work-units?reference=order-1", "not-a-key", "")
		assertProblem(t, rec, http.StatusUnauthorized, "unauthenticated")
	})

	t.Run("/healthz with no token -> 200", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodGet, "/healthz", "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	})
}

// TestRouterAuth_OffModeIsNoOp pins the contract every existing handler test
// relies on: NewRouter (no keys) runs with auth off and no request is
// challenged.
func TestRouterAuth_OffModeIsNoOp(t *testing.T) {
	router := newTestRouter()
	rec := doAuth(t, router, http.MethodPost, "/paths/pick-zone-a/charge", "", chargeBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 with auth off; body=%s", rec.Code, rec.Body.String())
	}
}

// TestRouterAuth_LogModeLetsThrough proves the rollout gate: in log mode an
// unauthenticated request is served (and only logged).
func TestRouterAuth_LogModeLetsThrough(t *testing.T) {
	authn := auth.NewStaticKeyAuth(map[string]auth.Scope{testReadKey: auth.ScopeRead})
	router := inboundhttp.NewRouterWithAuth(newTestHandlers(), "wes-work-planning", nil,
		auth.Middleware{Authn: authn, Mode: auth.ModeLog})
	rec := doAuth(t, router, http.MethodPost, "/paths/pick-zone-a/charge", "", chargeBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 in log mode; body=%s", rec.Code, rec.Body.String())
	}
}

// TestReportsRouterAuth_Table covers the reports binary's router: every
// /reports route requires the read scope; /healthz stays open.
func TestReportsRouterAuth_Table(t *testing.T) {
	authn := auth.NewStaticKeyAuth(map[string]auth.Scope{
		testReadKey:      auth.ScopeRead,
		testReadWriteKey: auth.ScopeReadWrite,
	})
	store := &fakeReportStore{}
	router := inboundhttp.NewReportsRouterWithAuth(&inboundhttp.ReportsHandlers{Store: store}, "wes-reports-test", nil,
		auth.Middleware{Authn: authn, Mode: auth.ModeEnforce})

	const throughput = "/reports/throughput?from=2026-06-01T00:00:00Z&to=2026-06-02T00:00:00Z"

	t.Run("no token -> 401 problem+json with WWW-Authenticate", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodGet, throughput, "", "")
		assertProblem(t, rec, http.StatusUnauthorized, "unauthenticated")
		if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
			t.Fatalf("WWW-Authenticate = %q, want Bearer challenge", got)
		}
	})

	t.Run("read key -> 200", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodGet, throughput, testReadKey, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("read-write key -> 200", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodGet, "/reports/throughput/freshness", testReadWriteKey, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("/healthz with no token -> 200", func(t *testing.T) {
		rec := doAuth(t, router, http.MethodGet, "/healthz", "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	})
}
