package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	inboundmcp "github.com/claudioed/wes-work-planning/internal/adapters/inbound/mcp"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

const readKey = "test-read-key"
const writeKey = "test-write-key"

var serverBase = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// bearerTransport adds a fixed Authorization header to every request, so the
// in-process MCP client authenticates like a real one.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.base.RoundTrip(r)
}

// newServer builds a real MCP HTTP server over in-memory repos seeded with two
// pending PICK units on path "pick-a", and returns its httptest URL. The
// server carries both a read and a read-write key.
func newServer(t *testing.T) string {
	t.Helper()
	url, _ := newServerWithPath(t)
	return url
}

func newServerWithPath(t *testing.T) (string, string) {
	t.Helper()
	pools := memory.NewWorkPoolRepo()
	workUnits := memory.NewWorkUnitRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.FixedClock{At: serverBase}
	enqueue := usecases.NewEnqueueWorkUnit(workUnits, pools, publisher, clock)

	ctx := context.Background()
	path, _ := shared.NewPathId("pick-a")
	for i, ref := range []string{"o1", "o2"} {
		if _, err := enqueue.Execute(ctx, usecases.EnqueueWorkUnitRequest{
			WorkUnitId: "wu-" + ref,
			PathId:     path,
			CPT:        shared.NewCPT(serverBase.Add(time.Duration(i+1) * time.Hour)),
			Reference:  ref,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	deps := inboundmcp.Deps{
		SampleBacklog:     usecases.NewSampleBacklog(pools, publisher, clock),
		RebalanceDecision: usecases.NewRebalanceDecision(pools, publisher, clock),
		ReleaseNextWork:   usecases.NewReleaseNextWork(pools, workUnits, publisher, clock),
	}
	server := inboundmcp.NewServer(deps)
	auth := inboundmcp.NewStaticKeyAuth(map[string]inboundmcp.Scope{
		readKey:  inboundmcp.ScopeRead,
		writeKey: inboundmcp.ScopeReadWrite,
	})
	httpSrv := httptest.NewServer(inboundmcp.Handler(server, auth))
	t.Cleanup(httpSrv.Close)
	return httpSrv.URL, "pick-a"
}

func connect(t *testing.T, url, token string) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	transport := &sdk.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: &http.Client{Transport: bearerTransport{token: token, base: http.DefaultTransport}},
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestServer_UnauthenticatedIsRejected(t *testing.T) {
	url := newServer(t)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got == "" {
		t.Fatal("missing WWW-Authenticate challenge on 401")
	}
}

func TestServer_ToolsListAndCall(t *testing.T) {
	url := newServer(t)
	session := connect(t, url, readKey)
	ctx := context.Background()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	want := map[string]bool{"get_backlog_telemetry": false, "get_rebalance_recommendation": false, "release_next_work": false}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not advertised", name)
		}
	}

	res, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name:      "get_backlog_telemetry",
		Arguments: map[string]any{"pathId": "pick-a"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %+v", res.Content)
	}
	depth, ok := res.StructuredContent.(map[string]any)["backlogDepth"]
	if !ok {
		t.Fatalf("no backlogDepth in structured content: %+v", res.StructuredContent)
	}
	if depth.(float64) != 2 {
		t.Fatalf("pick-a backlogDepth = %v, want 2", depth)
	}
}

func TestServer_CallToolRejectsEmptyPathId(t *testing.T) {
	url := newServer(t)
	session := connect(t, url, readKey)
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "get_backlog_telemetry",
		Arguments: map[string]any{"pathId": ""},
	})
	if err != nil {
		t.Fatalf("call tool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected tool-level error for empty pathId")
	}
}

func TestServer_ResourceRead(t *testing.T) {
	url := newServer(t)
	session := connect(t, url, readKey)
	res, err := session.ReadResource(context.Background(), &sdk.ReadResourceParams{
		URI: "telemetry://pick-a/backlog",
	})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(res.Contents) == 0 || res.Contents[0].Text == "" {
		t.Fatalf("empty resource contents: %+v", res.Contents)
	}
}

func TestServer_PromptGet(t *testing.T) {
	url := newServer(t)
	session := connect(t, url, readKey)
	res, err := session.GetPrompt(context.Background(), &sdk.GetPromptParams{Name: "balance_flow"})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	if len(res.Messages) == 0 {
		t.Fatal("balance_flow prompt returned no messages")
	}
}

func TestServer_ReleaseNextWorkDeniedForReadOnlyKey(t *testing.T) {
	url, path := newServerWithPath(t)
	session := connect(t, url, readKey) // read-only key
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "release_next_work",
		Arguments: map[string]any{"pathId": path},
	})
	if err != nil {
		t.Fatalf("call tool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("release_next_work with a read-only key must be denied (scope gate)")
	}
}

func TestServer_ReleaseNextWorkSucceedsForWriteKey(t *testing.T) {
	url, path := newServerWithPath(t)
	session := connect(t, url, writeKey) // read-write key
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "release_next_work",
		Arguments: map[string]any{"pathId": path},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("release_next_work with write key returned error: %+v", res.Content)
	}
	sc := res.StructuredContent.(map[string]any)
	if sc["ref"] != "o1" {
		t.Fatalf("expected earliest-CPT unit o1 released, got %+v", sc)
	}
	if sc["workUnitId"] != "wu-o1" {
		t.Fatalf("expected workUnitId wu-o1, got %+v", sc)
	}
}
