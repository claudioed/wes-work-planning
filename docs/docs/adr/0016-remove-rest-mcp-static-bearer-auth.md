---
id: 0016-remove-rest-mcp-static-bearer-auth
slug: /adr/0016-remove-rest-mcp-static-bearer-auth
title: 0016. Remove the REST + MCP static-bearer auth layer (supersedes ADR-0015)
sidebar_label: 0016. Remove REST/MCP auth
description: ADR 0016 — the fleet decision behind ADR-0015 was reversed; this context removes the static-bearer identity layer entirely from its REST API, its reports reader, and its MCP server rather than carrying it disabled.
---

# 0016. Remove the REST + MCP static-bearer auth layer (supersedes ADR-0015)

## Status

Accepted — implemented in the same change that introduced this record.
Supersedes [ADR-0015](./0015-rest-identity-static-bearer-scopes.md).

## Context

[ADR-0015](./0015-rest-identity-static-bearer-scopes.md) adopted the fleet's
static-bearer, no-IdP REST identity decision (warehouse-ops-agent ADR 0005)
for this service's OLTP REST API, its reports reader, and (via ADR-0008) its
MCP server. The fleet has since reversed that decision: the auth layer is
being removed fleet-wide rather than carried forward, replaced, or toggled
off by configuration. This context follows suit rather than being the sole
holdout carrying dead middleware, an unused `auth` package, and Helm values
that no longer correspond to anything a caller needs to configure.

## Decision

Remove the static-bearer identity layer entirely, not just disable it:

- Delete `internal/adapters/inbound/auth` (the `Authenticator`,
  `StaticKeyAuth`, `Scope`, `Middleware` package ADR-0015 introduced).
- `internal/adapters/inbound/mcp`: the MCP handler mounts unauthenticated;
  the package's own `auth.go`/`auth_test.go` re-export shim is deleted.
- `internal/adapters/inbound/http`: `NewRouter`/`NewReportsRouter` no longer
  take or mount an auth middleware; every route (including the ones that
  used to require a scope) is reachable without a bearer token. `GET
  /healthz` was already open and is unaffected.
- Composition roots (`cmd/wes`, `cmd/wes-reports`, `cmd/mcp`) no longer
  construct a `StaticKeyAuth`, parse `AUTH_MODE`, or read
  `API_READ_KEY`/`API_READWRITE_KEY`/`MCP_READ_KEY`/`MCP_READWRITE_KEY`.
- The outbound product-classification client
  ([ADR-0009](./0009-product-classification-propagation-to-work-released.md))
  no longer sends an `Authorization` header; `INVENTORY_STORAGE_API_KEY` is
  gone.
- The Helm chart drops the `auth:` values block, `inventoryStorage.apiKey`,
  `mcp.readKey`/`mcp.readWriteKey`, the `<release>-mcp` keys Secret, and the
  `AUTH_MODE`/`API_READ_KEY`/`API_READWRITE_KEY` env wiring on the OLTP and
  reports Deployments.
- `apis/openapi.yaml` drops `components.securitySchemes.bearerAuth` and the
  top-level `security: [{bearerAuth: []}]` (and the now-redundant
  operation-level `security: []` override on `GET /healthz`).
  `apis/asyncapi.yaml` carried no auth scheme and needed no change.
  `README.md` drops the `### Authentication` section and the
  `AUTH_MODE`/`API_READ_KEY`/`API_READWRITE_KEY`/`INVENTORY_STORAGE_API_KEY`
  config-table rows.
- [ADR-0015](./0015-rest-identity-static-bearer-scopes.md) itself is left in
  place, unedited, as the historical record of why the layer existed; this
  ADR is the pointer from "why is there no auth here" back to "there used to
  be, see ADR-0015 and ADR-0016."

## Consequences

- Every REST and MCP route in this service is now reachable without a
  credential. Network-level isolation (in-cluster only, ingress/mTLS at the
  platform edge) is the only remaining access control; there is no
  service-level fallback.
- The `Authenticator` seam ADR-0008 described as the future OAuth 2.1 upgrade
  path no longer exists in this repository; a future re-introduction of auth
  would need to re-add the seam from scratch rather than swap an
  implementation behind it.
- Existing callers (warehouse-console via its BFF, e2e-tests,
  warehouse-ops-agent) that were sending an `Authorization: Bearer …` header
  are unaffected — the header is simply ignored now — but should stop
  sending it since no rotation/validity guarantee exists for the value.
