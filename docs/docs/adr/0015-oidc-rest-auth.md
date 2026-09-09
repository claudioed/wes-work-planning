---
title: "ADR-0015 — OIDC bearer-token authorization for REST APIs"
description: Standards-compliant authentication and scope authorization for WES REST APIs.
---

# ADR-0015 — OIDC bearer-token authorization for REST APIs

## Decision

The OLTP and reports REST APIs require OpenID Connect bearer tokens. At startup
each process discovers `OIDC_ISSUER_URL` and builds a verifier with
`github.com/coreos/go-oidc/v3/oidc` and `OIDC_CLIENT_ID`. Discovery/JWKS or
configuration failure stops startup; verification never skips issuer, signature,
expiry, or audience checks.

Safe methods require `wes-work-planning.read`; mutating methods require
`wes-work-planning.write`. The OAuth scope claim may be either the standard
space-delimited string or a string array. `GET /healthz` is the sole public REST
endpoint. Authentication errors use RFC 6750 `WWW-Authenticate: Bearer` and RFC
7807 `application/problem+json` bodies; authorization failures return 403 with
`insufficient_scope`.

## Consequences

Deployments must provide public issuer and client-id configuration. No bearer
tokens, client secrets, signature bypasses, or token logging are permitted.
