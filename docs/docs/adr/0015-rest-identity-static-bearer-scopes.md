---
id: 0015-rest-identity-static-bearer-scopes
slug: /adr/0015-rest-identity-static-bearer-scopes
title: 0015. REST identity — adopt the fleet's static bearer keys with read/read-write scopes
sidebar_label: 0015. REST identity (adoption)
description: ADR 0015 — this context adopts warehouse-ops-agent ADR 0005 (fleet-wide REST identity, static bearer keys with read/read-write scopes, no IdP) for its OLTP REST API and its reports reader, sharing one auth package with the MCP adapter.
---

# 0015. REST identity — adopt the fleet's static bearer keys with read/read-write scopes

## Status

Accepted — implemented in the same change that introduced this record.

## Decision

This context adopts **warehouse-ops-agent ADR 0005** ("Fleet REST identity:
static bearer keys with read/read-write scopes, no IdP") without deviation.
One `internal/adapters/inbound/auth` package (the fleet template, copied
verbatim per the no-shared-code rule) now holds `Authenticator`,
`StaticKeyAuth`, `Scope` and the `Middleware`; the MCP adapter
([ADR-0008](./0008-mcp-inbound-adapter.md)) re-exports those types instead of
carrying its own copy, so this repository has exactly one identity
implementation serving both surfaces. The middleware is mounted, via a chi
`Group`, on every route of the OLTP router (`cmd/wes`) and of the reports
reader (`cmd/wes-reports`, read scope required) — `GET /healthz` stays open
for the Kubernetes probes. `GET`/`HEAD`/`OPTIONS` need `read`; every other
method needs `read-write`; failures are the service's existing RFC 7807
problem details ([ADR-0005](./0005-rfc-7807-problem-details.md)):
`.../unauthenticated` (401 + `WWW-Authenticate`) and
`.../insufficient-scope` (403). Keys come from `API_READ_KEY` /
`API_READWRITE_KEY` (falling back to `MCP_READ_KEY` / `MCP_READWRITE_KEY`),
mode from `AUTH_MODE=enforce|log|off`; the composition roots default to
`enforce` when a key is configured and to `off` — with a loud WARN — when none
is, so local runs and the existing handler tests are unchanged. The outbound
product-classification client ([ADR-0009](./0009-product-classification-propagation-to-work-released.md))
presents `INVENTORY_STORAGE_API_KEY` as a bearer when set. The Helm chart
gains `auth.mode` / `auth.readKey` / `auth.readWriteKey` /
`auth.existingSecret` and `inventoryStorage.apiKey`; the rollout follows the
fleet plan (`log` first, then `enforce`, rollback is configuration only).

## Consequences

- The seam for a future OAuth 2.1 resource server is unchanged: only the
  `Authenticator` passed in by the composition root would move.
- Callers of this API (warehouse-console via its BFF, e2e-tests,
  warehouse-ops-agent) must present a key once warehouse-infra flips
  `AUTH_MODE=enforce`; until then `log` mode reports every would-be
  rejection as `auth: would-reject`.
