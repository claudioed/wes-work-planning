package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	inboundmcp "github.com/claudioed/wes-work-planning/internal/adapters/inbound/mcp"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
)

const testReadKey = "router-test-read-key"

// newTestRouter wires a real MCP handler (in-memory adapters, one static read
// key) behind newRouter, exactly as run() does.
func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	pools := memory.NewWorkPoolRepo()
	workUnits := memory.NewWorkUnitRepo()
	clock := memory.SystemClock{}
	publisher := events.NewLogPublisher(slog.Default())
	deps := inboundmcp.Deps{
		SampleBacklog:     usecases.NewSampleBacklog(pools, publisher, clock),
		RebalanceDecision: usecases.NewRebalanceDecision(pools, publisher, clock),
		ReleaseNextWork:   usecases.NewReleaseNextWork(pools, workUnits, publisher, clock),
	}
	server := inboundmcp.NewServer(deps)
	auth := inboundmcp.NewStaticKeyAuth(map[string]inboundmcp.Scope{testReadKey: inboundmcp.ScopeRead})
	return newRouter(inboundmcp.Handler(server, auth))
}

func TestRouter_HealthzIsUnauthenticated(t *testing.T) {
	router := newTestRouter(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v, want status=ok", body)
	}
}

func TestRouter_MCPMountsRequireBearer(t *testing.T) {
	router := newTestRouter(t)
	for _, path := range []string{"/", "/mcp"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("POST %s without bearer: status = %d, want 401", path, rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
				t.Fatalf("WWW-Authenticate = %q, want a Bearer challenge", got)
			}
		})
	}
}

func TestRouter_MCPMountsReachHandlerWithBearer(t *testing.T) {
	router := newTestRouter(t)
	const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"router-test","version":"0.0.1"}}}`
	for _, path := range []string{"/", "/mcp"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(initialize))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Authorization", "Bearer "+testReadKey)
			router.ServeHTTP(rec, req)

			// A valid key gets past auth and into the Streamable HTTP handler,
			// which answers the initialize handshake and opens a session.
			if rec.Code != http.StatusOK {
				t.Fatalf("POST %s initialize with bearer: status = %d, want 200 (body %q)", path, rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Mcp-Session-Id") == "" {
				t.Fatalf("POST %s initialize: no Mcp-Session-Id header — request did not reach the MCP handler", path)
			}
		})
	}
}

func TestRouter_UnknownPathIsNotHealthz(t *testing.T) {
	router := newTestRouter(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /nope status = %d, want 404", rec.Code)
	}
}
