package mcp

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestScopeGating_DeniesWithoutReadScope proves that a context carrying no
// (or insufficient) scope is rejected by the guard the tool/resource wrappers
// use. The transport test always presents a valid key, so this white-box test
// is what exercises the denial predicate directly.
func TestScopeGating_DeniesWithoutReadScope(t *testing.T) {
	unauth := context.Background()

	t.Run("empty-scope context does not satisfy read", func(t *testing.T) {
		if scopeAllows(scopeFromContext(unauth), ScopeRead) {
			t.Fatal("empty-scope context must not satisfy ScopeRead")
		}
	})

	t.Run("read scope satisfies read", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), scopeKey{}, ScopeRead)
		if !scopeAllows(scopeFromContext(ctx), ScopeRead) {
			t.Fatal("read scope must satisfy ScopeRead")
		}
	})

	t.Run("read scope does not satisfy read-write", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), scopeKey{}, ScopeRead)
		if scopeAllows(scopeFromContext(ctx), ScopeReadWrite) {
			t.Fatal("read scope must not satisfy ScopeReadWrite")
		}
	})
}

// TestResourceReadDeniedWithoutScope drives a resource read through a server
// whose request context lacks a scope, asserting the handler denies it. It
// uses the SDK's in-memory transport to invoke the real registered handler
// (no HTTP, so no auth middleware runs to put a scope in context).
func TestResourceReadDeniedWithoutScope(t *testing.T) {
	h := newHarness(t)
	h.seedPending(t, "pick-a", 0, "o1")
	server := NewServer(h.deps)

	client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	ct, st := sdk.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	if _, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: "telemetry://pick-a/backlog"}); err == nil {
		t.Fatal("resource read without scope must be denied")
	}
}
