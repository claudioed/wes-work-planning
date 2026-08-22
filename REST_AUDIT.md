# REST API Audit — wes-work-planning

Audit of `internal/adapters/inbound/http/` against Richardson Maturity Level 2
(resource nouns, correct verbs, correct status codes — HATEOAS is explicitly
out of scope). Covers the 6 checks from `REST_API_TASK.md` Stage 1.

Routes audited (from `router.go`):

```
GET  /healthz
POST /paths/{pathId}/charge
POST /paths/{pathId}/plan
POST /paths/{pathId}/work-units
POST /paths/{pathId}/release
GET  /paths/{pathId}/telemetry
GET  /paths/{pathId}/rebalance
GET  /paths/{pathId}/labor-plan-view
POST /work-units/{id}/complete
GET  /inventory-view/{sku}
```

## 1. Resource nouns, not verbs, in URLs

**No violation found.** Every path is scoped to a resource collection or a
single resource: `/paths/{pathId}/...`, `/work-units/{id}/...`,
`/inventory-view/{sku}`. There is no bare RPC-style endpoint analogous to the
known cross-codebase issue (`POST /admin/expire-leases` with no resource
scope) — this service has no `/admin/*` routes at all.

`POST /paths/{pathId}/release` and `POST /work-units/{id}/complete` are
verb-suffixed, but both are legitimate domain commands scoped to a resource
(collection-level release-next-work on the `paths/{pathId}` work pool, and a
state-transition command on a specific work unit) — the same pattern the task
explicitly allows for `/tasks/{id}/complete` / `/associates/{id}/start-shift`.
No change made.

## 2. Correct HTTP methods

**No violation found.** Every `Get*` handler (`getTelemetry`, `getRebalance`,
`getLaborPlanView`, `getInventoryView`, `healthz`) only reads through a query
use case (`SampleBacklog`, `RebalanceDecision`, `LaborPlanView`,
`InventoryView`) and writes a response — none call a use case that mutates
aggregate state. (Note: `SampleBacklog`/`RebalanceDecision` may emit
*read-model telemetry events* such as `BacklogThresholdBreached` as a side
effect of sampling — this is existing, unchanged application-layer behavior
from Task 2/QUALITY.md, not something introduced or altered by this HTTP
audit, and does not mutate the `WorkPool`/`WorkUnit` aggregates themselves.)
All state-changing operations (`postChargeForecast`, `postShiftPlan`,
`postWorkUnit`, `postRelease`, `postComplete`) are POST. This service has no
PUT/PATCH/DELETE endpoints, which matches CLAUDE.md's REST API table — no
change made.

## 3. Correct status codes

Audited every handler against: 201 (+Location) for creation, 200 for
reads/actions-with-body, 204 for no-body actions, 404/409/422/400 for errors.

**Violations found and fixed:**

- **Bug — malformed JSON decoded to 500, not 400.** `postChargeForecast`,
  `postShiftPlan`, and `postWorkUnit` all called `json.NewDecoder(...).Decode`
  and passed a decode failure straight to `writeError`. Since a decode error
  doesn't match any sentinel in `statusFor`'s switch, it fell through to the
  `default: http.StatusInternalServerError` case — a client sending malformed
  JSON got a 500, which is wrong (500 must mean "the server is broken", not
  "the client sent bad input"). **Fix:** added a `decodeJSON` helper
  (`handlers.go`) that wraps decode failures in a new `errMalformedBody`
  sentinel; `statusFor` now maps it to 400 explicitly (`errors.go`). Covered
  by new tests `TestPostChargeForecast_MalformedJSONReturns400` and
  `TestPostWorkUnit_MalformedJSONReturns400`.
- **Missing `Location` header on 201 responses.** None of the three creation
  endpoints set `Location`. **Fix:** added it to all three —
  `POST /paths/{pathId}/charge` → `Location: /paths/{pathId}/charge`,
  `POST /paths/{pathId}/plan` → `Location: /paths/{pathId}/plan`,
  `POST /paths/{pathId}/work-units` → `Location: /work-units/{id}`. Covered
  by assertions added to the existing `TestPostChargeForecast`,
  `TestPostShiftPlan`, and `TestPostWorkUnit` tests. (Charge and plan have no
  dedicated GET-by-id endpoint today — `Location` still correctly identifies
  the created resource's canonical URI per RFC 9110 even without a
  corresponding GET.)

**Checked, no violation (200 vs 201):** `postChargeForecast`, `postShiftPlan`,
and `postWorkUnit` all already return 201 for resource creation.
`postRelease` and `postComplete` return 200, which is correct — both are
commands with a response body on an *existing* resource (a work unit
transitioning state), not creation of a new one; there is no "200 where 201
is correct" mismatch.

**Checked, no violation (404/409):** `ports.ErrNotFound` → 404;
`release.ErrWIPLimitReached`, `ErrAlreadyReleased`, `ErrDuplicateEntry`,
`ErrEmptyPool`, `workunit.ErrAlreadyReleased`, `ErrAlreadyCompleted`,
`ErrNotReleased` → 409. These are all genuine state conflicts (double-claim,
double-complete, WIP-limit-exceeded, empty pool) — correct.

**422 vs 400 — considered, not changed.** The status table in
`REST_API_TASK.md` lists 422 for "semantically invalid input" separately from
400 for "malformed JSON/missing required fields". This service's remaining
error mappings (`shared.ErrInvalidQuantity`, `ErrInvalidRate`,
`ErrInvalidStationCount`, `ErrInvalidHours`, `charge.ErrNoBuckets`,
`ErrUnknownCPT`, `plan.ErrHeadsExceedStations`, `plan.ErrNoPathPlans`,
`workunit.ErrEmptyId`, `ErrEmptyReference`, `release.ErrUnknownEntry`) are all
currently 400. Some of these (e.g. `ErrHeadsExceedStations`, a business
invariant violation on otherwise well-formed input) are textbook 422
candidates. This was a deliberate judgment call to leave as-is rather than a
gap: 400-for-all-validation-errors is this codebase's established, tested
convention from Task 4/6/10 (see e.g. `TestPostShiftPlan_HeadsExceedStationsReturns400`,
README's "Fails with 400 if plannedHeads > installedStations"), 400 is not
*wrong* per RFC 9110 (422 is optional refinement, not mandatory), and
splitting the 11 sentinel errors across 400/422 would rename/rewrite a large
swath of already-green, already-covered tests and README examples for a
design preference rather than a correctness bug. No code change made here.

## 4. Idempotency semantics (documented, not changed)

| Endpoint | Idempotent? | Why |
|---|---|---|
| `POST /paths/{pathId}/charge` | No | Each call creates/overwrites the forecast for the path from scratch; not keyed by a client-supplied request ID, so a retried request re-executes `ReceiveChargeForecast` — but since the whole forecast is replaced (not appended), a naive retry is at least safe (not additive), just not a strict no-op check-first design. |
| `POST /paths/{pathId}/plan` | No | Same shape as above — commits a new `ShiftPlan` for the path each call. |
| `POST /paths/{pathId}/work-units` | Partially — client-supplied ID | `workUnitId` is supplied by the client, and the domain (`workunit`/`release` aggregates) rejects a duplicate ID (`release.ErrDuplicateEntry`, 409) rather than silently creating a second entry. This is the "idempotent by client-supplied ID" pattern the task calls out — **checked, this is not a bug**: a retried request with the same `workUnitId` gets a 409, not a silent duplicate. |
| `POST /paths/{pathId}/release` | No (by design) | Each call intentionally releases the *next* eligible unit — a command, not a create-if-absent; retrying advances the pool. Not a bug. |
| `POST /work-units/{id}/complete` | Yes (effectively) | First call transitions Released→Completed; a retried call gets `workunit.ErrAlreadyCompleted` (409) rather than double-applying the completion — safe to retry, just not a silent 200 no-op. |
| `GET *` | Yes | All reads are naturally idempotent/side-effect-free (see check 2). |

No code changes required here (per task instructions, only fix a *clear*
idempotency bug — none found; `EnqueueWorkUnit`'s duplicate-ID rejection is
already correct, not a bug).

## 5. Consistent JSON casing

**Verified, no violation.** Every request/response DTO in `dto.go` uses
camelCase JSON tags (`pathId`, `workUnitId`, `plannedHeads`,
`rateUnitsPerHour`, `installedStations`, `backlogDepth`,
`overAlarmThreshold`, etc.) — confirmed by inspection of all 12 DTO structs.
No renames made.

## 6. Content negotiation

**Verified, no violation for success responses.** Every handler writes
`Content-Type: application/json` via the shared `writeJSON` helper. Error
responses are migrated to `Content-Type: application/problem+json` in Stage 2
below.

---

## Stage 1 summary

**2 real violations found and fixed:**
1. Malformed JSON on `charge`/`plan`/`work-units` POST bodies returned 500
   instead of 400 (`decodeJSON` + `errMalformedBody` fix).
2. Missing `Location` header on all three 201 Created responses.

All other checks (verb/noun usage, GET side-effect freedom, 200-vs-201 for
existing creates, 404/409 mapping, camelCase, content-type) were audited and
found already correct — documented above rather than left as silent gaps.

`go build ./...`, `go vet ./...`, `go test ./... -race`, `golangci-lint run
./...`, and `gofmt -l .` are all clean. Existing httptest coverage passes
unchanged; 5 new assertions/tests added for the two fixes (no existing test
deleted or weakened).

---

## Stage 2 — RFC 7807 migration

Migrated `errorResponse`/`writeError` (`dto.go`, `errors.go`) from the bespoke
`{"error": "..."}` shape to RFC 7807 `application/problem+json`. Details:

- `errorResponse` replaced with `problemDetails{Type, Title, Status, Detail,
  Instance}` (`json:"type","title","status","detail","instance,omitempty"`).
- `writeError` now takes the `*http.Request` (to read `r.URL.Path` for
  `instance`) and looks up `type`/`title` from a new `problemFor(err)` lookup
  table keyed by the same sentinel errors `statusFor` already switches on —
  `statusFor` itself is untouched.
- `type` URIs follow `https://errors.wes-work-planning.warehouse-systems.dev/<slug>`,
  slug derived from the sentinel name, e.g. `ErrAlreadyCompleted` →
  `work-unit-already-completed`, `ErrHeadsExceedStations` →
  `heads-exceed-installed-stations`.
- `instance` is set to `r.URL.Path` for every error that has a natural
  resource path (i.e. every current error case — all routes are
  resource-scoped) and omitted (`omitempty`) only for the theoretical case of
  a body-only validation error with no path segment identifying a resource
  (none currently exist in this service, but the field supports it).
- `Content-Type: application/problem+json` set on every error response
  (`writeProblem`, mirrors `writeJSON` but with the RFC 7807 media type).
- Every existing httptest asserting the old `{"error":...}` shape was updated
  to assert `type`/`title`/`status`/`detail`/`instance` instead — no test
  deleted.
- README's error-response examples updated to show the RFC 7807 shape.

### Manual verification (raw curl output — real, against a running binary)

Server started with in-memory adapters (`HTTP_ADDR=:8090 go run ./cmd/wes`,
port 8080 was occupied by another warehouse-systems service on this
machine). Evidence collected against 3 error scenarios (404, 409, and the
Stage 1 malformed-JSON 400 fix) plus the 201 Location-header fix, all as raw
`curl -sD -` output:

**404 — unknown path telemetry:**

```
$ curl -sD - localhost:8090/paths/does-not-exist/telemetry
HTTP/1.1 404 Not Found
Content-Type: application/problem+json
Date: Sat, 22 Aug 2026 14:19:57 GMT
Content-Length: 184

{"type":"https://errors.wes-work-planning.warehouse-systems.dev/not-found","title":"Resource not found","status":404,"detail":"not found","instance":"/paths/does-not-exist/telemetry"}
```

**201 Created with Location (setup for the 409 case below):**

```
$ curl -sD - -X POST localhost:8090/paths/pick-a/work-units \
  -H 'Content-Type: application/json' \
  -d '{"workUnitId":"wu-1","cpt":"2026-08-21T12:00:00Z","reference":"order-line-1"}'
HTTP/1.1 201 Created
Content-Type: application/json
Location: /work-units/wu-1
Date: Sat, 22 Aug 2026 14:19:57 GMT
Content-Length: 106

{"id":"wu-1","pathId":"pick-a","cpt":"2026-08-21T12:00:00Z","reference":"order-line-1","state":"Pending"}
```

**409 — double-complete:**

```
$ curl -sD - -X POST localhost:8090/paths/pick-a/release   # released -> Released
$ curl -sD - -X POST localhost:8090/work-units/wu-1/complete   # first call -> 200 Completed
$ curl -sD - -X POST localhost:8090/work-units/wu-1/complete   # second call
HTTP/1.1 409 Conflict
Content-Type: application/problem+json
Date: Sat, 22 Aug 2026 14:19:57 GMT
Content-Length: 226

{"type":"https://errors.wes-work-planning.warehouse-systems.dev/work-unit-already-completed","title":"Work unit already completed","status":409,"detail":"work unit is already completed","instance":"/work-units/wu-1/complete"}
```

**400 — malformed JSON (Stage 1's decode-error bug, confirmed no longer 500):**

```
$ curl -sD - -X POST localhost:8090/paths/pick-a/charge -H 'Content-Type: application/json' -d '{not-json'
HTTP/1.1 400 Bad Request
Content-Type: application/problem+json
Date: Sat, 22 Aug 2026 14:19:57 GMT
Content-Length: 269

{"type":"https://errors.wes-work-planning.warehouse-systems.dev/malformed-request-body","title":"Malformed request body","status":400,"detail":"malformed request body: invalid character 'n' looking for beginning of object key string","instance":"/paths/pick-a/charge"}
```

---

## Stage 3 — OpenAPI 3.0.3

`openapi.yaml` at repo root, OpenAPI 3.0.3, documents every route.

**Route cross-check (router.go vs. openapi.yaml `paths`):**

| Route | Documented? |
|---|---|
| `GET /healthz` | ✅ |
| `POST /paths/{pathId}/charge` | ✅ |
| `POST /paths/{pathId}/plan` | ✅ |
| `POST /paths/{pathId}/work-units` | ✅ |
| `POST /paths/{pathId}/release` | ✅ |
| `GET /paths/{pathId}/telemetry` | ✅ |
| `GET /paths/{pathId}/rebalance` | ✅ |
| `GET /paths/{pathId}/labor-plan-view` | ✅ |
| `POST /work-units/{id}/complete` | ✅ |
| `GET /inventory-view/{sku}` | ✅ |

**10/10 routes documented.** Every operation has `operationId`,
`summary`+`description` (with real domain context — CPT/drum-buffer-rope/
release-fed vs. flow-fed/bounded-context distinctions pulled from CLAUDE.md
and the domain code, not generic filler), `tags`, full request/response
schemas with `format`s and realistic examples using this repo's actual
ubiquitous-language values (`pick-a`, `wu-482910`, `sku-88213`, real CPT
timestamps), and every status code each handler can actually return
(cross-referenced against the use case source in
`internal/application/usecases/*.go`, not guessed). All error responses
`$ref` the single shared `components/schemas/Problem` component.

**Validation:**

```
$ python3 -c "import yaml; yaml.safe_load(open('openapi.yaml'))"
YAML OK
```

```
$ npx --yes @redocly/cli lint openapi.yaml
...
openapi.yaml: validated in 92ms

Woohoo! Your API description is valid. 🎉
You have 3 warnings.
```

**0 errors.** Fixed 2 real errors found by the first lint pass: (1)
`nullable-type-sibling` on `RebalanceResponse.laborPlan` (removed `nullable:
true` — the DTO's actual Go behavior is `omitempty`, i.e. the field is
absent, never present-as-null, so dropping `nullable` is the more accurate
fix, not a suppression); (2) `security-defined` on every operation (added a
top-level `security: []` — this service genuinely has no auth today, so an
explicit empty security requirement is the truthful fix, not a workaround).

**3 warnings left as warnings** (per task instructions — "warnings are fine
to leave, but note them"):
- `info-license` — no `license` field. This is an internal reference-system
  service, not a published package; no license applies.
- `no-server-example.com` — flags the `http://localhost:8080` server URL.
  This is exactly the server REST_API_TASK.md's Stage 3 spec requires ("at
  minimum `http://localhost:8080` for local dev").
- `operation-4xx-response` on `GET /healthz` — genuinely has no 4xx path
  (see handler: it's a static 200, no parameters, no error branch).

---

## Stage 4 — Spectral CI gate

`.spectral.yaml` added at repo root (extends `spectral:oas`, escalates
`operation-operationId`/`operation-description`/`operation-tags`/
`info-description`/`oas3-api-servers` to `error`, exactly per
REST_API_TASK.md — no additional systematic gaps found worth hard-gating
beyond these). New `openapi-lint` job added to `.github/workflows/ci.yml`
alongside the existing `lint-test` and `mutation` jobs (neither touched),
inherits the workflow-level `on:` triggers (push/PR to main), no `if:`
skip condition — a real blocking gate.

**Local verification:**

```
$ spectral lint openapi.yaml --ruleset .spectral.yaml --fail-severity=warn
No results with a severity of 'warn' or higher found!
$ echo $?
0
```

```
$ python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"
YAML OK, jobs: ['lint-test', 'openapi-lint', 'mutation']
```

GitHub Actions verification (pushed to `main` — this repo's established
branch convention from Tasks 7/8/10, all of which committed straight to
`main`; the fulfillment-execution PR-branch exception in REST_API_TASK.md
does not apply here) is reported below once the run completes.
