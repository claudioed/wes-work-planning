# Mutation testing — internal/domain

Tool: [gremlins](https://github.com/go-gremlins/gremlins) v0.6.0, scoped to
`internal/domain/...` only, per QUALITY.md Stage 4.

## Command used

```sh
gremlins unleash ./internal/domain --workers 1 --timeout-coefficient 20
```

Note on flags: the default concurrent-worker mode produced spurious
`TIMED OUT` verdicts for nearly every mutant (each worker recompiles/reruns
the test binary, and concurrent `go test` invocations contend on the local
build cache badly enough to blow the default per-mutant timeout on this
machine). Running with `--workers 1` (serialized) and a larger
`--timeout-coefficient` eliminated all spurious timeouts and produced a
clean, deterministic result — this is a local build-cache-contention
artifact, not a code issue.

## Final result

```
Killed: 51, Lived: 0, Not covered: 0
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 100.00%
Mutator coverage: 100.00%
```

51 mutants generated across `charge`, `plan`, `release`, `shared`, and
`workunit`; all 51 killed.

## Survived mutants triaged

Two mutants survived on the first run (before triage) and were fixed by
adding a missing boundary-value test — both are documented here for the
record even though the final state above shows them killed:

1. **`shared/quantity.go:10:11`** — `CONDITIONALS_BOUNDARY` mutated
   `value < 0` to `value <= 0` in `NewQuantity`. Survived because no
   existing test constructed `NewQuantity(0)` and asserted success — the
   zero-quantity boundary was genuinely untested. Fixed by adding
   `TestQuantity/NewQuantity_zero` in `internal/domain/shared/shared_test.go`
   asserting `NewQuantity(0)` succeeds with `Value() == 0`. Mutant now
   killed.

2. **`shared/station_count.go:11:11`** — same `CONDITIONALS_BOUNDARY`
   mutation (`value < 0` → `value <= 0`) in `NewStationCount`, same root
   cause: no test for the zero-station-count boundary. Fixed by adding
   `TestStationCount/NewStationCount_zero` asserting `NewStationCount(0)`
   succeeds with `Value() == 0`. Mutant now killed.

No mutant was judged equivalent/unkillable and left un-triaged — every
mutant reported across all runs was either killed outright or killed after
adding the one missing boundary test above. No source code was changed to
chase a mutant; only tests were added.
