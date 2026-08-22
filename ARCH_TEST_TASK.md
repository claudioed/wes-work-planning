# Architecture Fitness Tests (Go equivalent of ArchUnit) — Task 14

Java's ArchUnit lets you write architecture rules (layer dependencies,
naming, package structure) as executable unit tests. The direct Go
equivalent is [arch-go](https://github.com/arch-go/arch-go) (module
`github.com/arch-go/arch-go`) — it can be used programmatically inside a
normal `go test` file, exactly like ArchUnit's JUnit integration, which is
why it's the right tool here (not `go-arch-lint`, which is a config-only
external CLI checker with no programmatic Go-test API).

This is a NEW, ADDITIVE quality gate — a 6th CI job. Strictly additive to
the codebase: do not modify any domain/application/adapter file's behavior.
You are only ADDING a new test file plus a small CI job.

## The architectural rule this codebase already follows (informally) — now enforce it as code

This is a hexagonal/ports-and-adapters architecture. The dependency rule:
dependencies point INWARD only, domain is at the center and knows nothing
about the outside world.

1. **`internal/domain/**`** must depend on NOTHING internal except other
   `internal/domain/**` packages (and stdlib). It must NOT import
   `internal/application/**`, `internal/adapters/**`, or `cmd/**`. This is
   the core rule — the domain layer is pure business logic with zero
   knowledge of how it's invoked or persisted.
2. **`internal/application/**`** (ports + usecases) must depend only on
   `internal/domain/**` (and stdlib/its own subpackages). It must NOT import
   `internal/adapters/**` or `cmd/**` — the application layer defines ports
   (interfaces) that adapters implement, it never imports a concrete adapter.
3. **`internal/adapters/inbound/**`** must NOT depend on
   `internal/adapters/outbound/**`. Inbound and outbound adapters are siblings
   that only communicate through the application layer's ports — never
   directly with each other.
4. **`internal/adapters/outbound/**`** must NOT depend on
   `internal/adapters/inbound/**`. Same rule, reversed direction.
5. Only `cmd/**` (the composition root / `main.go`) is allowed to import from
   every layer — that's its entire job, wiring concrete adapters into the
   application layer and starting the HTTP server.

Check this repo's actual package layout first (`internal/domain/*`,
`internal/application/{ports,usecases}`, `internal/adapters/{inbound,outbound}/*`)
— the rule above should already hold true given how this codebase was built
across Tasks 0-13, but VERIFY it, don't assume. If you find an existing
violation, do NOT silently work around it in the arch-go config — report it
explicitly and ask before either fixing the violation or excluding it, since
fixing it would touch production code (which this task's own instructions
otherwise forbid touching without explicit sign-off).

## Implementation

1. Add the dependency: `go get github.com/arch-go/arch-go@latest`

2. Create `internal/architecture/architecture_test.go` (new package,
   `internal/architecture/`) with a Go test that encodes the 5 rules above
   using arch-go's `config.DependenciesRule` /
   `archgo.CheckArchitecture(...)` API (see arch-go's README for the exact
   shape — `Package` uses glob patterns like `**.domain.**`,
   `ShouldOnlyDependsOn.Internal` lists allowed internal package globs).
   Structure it as ONE subtest per rule (`t.Run("domain has no internal
   dependencies except domain", ...)`, etc.) so a failure clearly identifies
   WHICH rule broke, not just "architecture test failed". Use this repo's
   actual Go module path (check `go.mod`'s `module` line) when calling
   `config.Load(...)`.

3. Also add at least one NAMING or LAYER-EXISTENCE convention check if
   arch-go supports it cleanly for this repo's structure (e.g. "every file
   under internal/adapters/inbound/http implementing a handler follows the
   existing naming convention", or "no package under internal/domain is
   named 'utils' or 'common'" — a real convention this codebase already
   follows, encoded so it can't silently drift). Keep this modest — the 5
   dependency rules are the main deliverable, this is a bonus if it fits
   naturally, skip it if it would require inventing a convention that
   doesn't already genuinely exist in this repo.

4. Run `go test ./internal/architecture/...` locally and confirm it PASSES
   against the current codebase (it should, if the codebase already follows
   the hexagonal rule as built) before doing anything else. If it fails,
   STOP and report the specific violation rather than adjusting the test to
   pass — a failing architecture test on first run means either a real
   violation exists (report it, don't hide it) or the rule/glob pattern is
   wrong (fix the test, re-verify by inspection that the corrected pattern
   genuinely matches this repo's real package layout, not just "makes the
   test green").

## CI integration

Add a NEW job `arch-test` to the EXISTING `.github/workflows/ci.yml`
(alongside lint/test/integration/mutation/openapi-lint/docker-publish — do
not touch those existing jobs). This is a real blocking gate (no `if:`
skip), runs on the same push/PR triggers as the other quality jobs:

```yaml
  arch-test:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Architecture fitness tests
        run: go test ./internal/architecture/... -v
```

Since `docker-publish` already depends on `needs: [lint, test, integration,
openapi-lint]`, ADD `arch-test` to that `needs` list too — a codebase that
violates its own architecture should not get published to Docker Hub either.
Do not otherwise touch `docker-publish`.

## Verification (same bar as every prior task in this workspace)

- `go test ./internal/architecture/... -v` passes locally, output pasted
  into your final report.
- `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `gofmt -l .`,
  and the FULL existing test suite (`go test ./... -race`) all still green —
  confirms nothing about the existing codebase was touched.
- `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"`
  — valid YAML, and confirm programmatically that `arch-test` is now in
  `docker-publish`'s `needs` list.
- Push to `main` (this repo's established convention — no open PRs as of
  this task) and use `gh run watch` to confirm `arch-test` passes for real
  on GitHub's runners, alongside the existing jobs, and that
  `docker-publish` still correctly waits on it before running.

## Definition of done

- `internal/architecture/architecture_test.go` exists, encodes all 5
  dependency rules as separate subtests, passes locally and matches this
  repo's real package structure (not a copy-pasted example).
- New `arch-test` CI job added, wired into `docker-publish`'s `needs`.
- Full existing suite (all prior tasks) remains green.
- Confirmed green on GitHub's real runners via `gh run watch`.
- Report: the 5 rules as implemented (with this repo's actual package glob
  patterns), confirmation no existing violation was found (or, if one WAS
  found, a clear description of it and what you did — do not paper over a
  real finding), and the GitHub Actions run confirmation.
