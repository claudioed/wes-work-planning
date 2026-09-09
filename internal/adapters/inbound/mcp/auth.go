package mcp

import (
	"github.com/claudioed/wes-work-planning/internal/adapters/inbound/auth"
)

// The MCP adapter's identity model (ADR-0008) is the fleet-standard one now
// held in internal/adapters/inbound/auth (ADR-0015, adopting
// warehouse-ops-agent ADR 0005): static bearer keys mapped to read /
// read-write scopes behind an Authenticator seam. This file only re-exports
// those types so that the MCP surface and the REST surface share ONE
// implementation per repository — there is no second copy of the key
// comparison or scope logic here.

// Scope is a coarse authorization class carried by an API key. Read-only
// keys may call read tools; read-write keys may additionally call write
// tools.
type Scope = auth.Scope

const (
	ScopeRead      = auth.ScopeRead
	ScopeReadWrite = auth.ScopeReadWrite
)

// Authenticator validates a request's bearer credential and reports the scope
// it grants. It is deliberately an interface: the current implementation is a
// static bearer key (StaticKeyAuth), and a future OAuth 2.1 resource-server
// implementation can replace it behind this same seam without touching any
// tool handler (ADR-0008, charter §7).
type Authenticator = auth.Authenticator

// StaticKeyAuth authenticates a request against a fixed set of bearer API
// keys, each mapped to a scope (constant-time comparison; keys never logged).
type StaticKeyAuth = auth.StaticKeyAuth

// NewStaticKeyAuth builds a StaticKeyAuth from token->scope pairs. Empty tokens
// are ignored so a blank env var cannot silently authorize every request.
func NewStaticKeyAuth(keys map[string]Scope) *StaticKeyAuth {
	return auth.NewStaticKeyAuth(keys)
}

// scopeAllows reports whether a granted scope may call a tool requiring the
// given minimum scope. read-write satisfies everything; read satisfies only
// read.
func scopeAllows(granted, required Scope) bool {
	return auth.Allows(granted, required)
}
