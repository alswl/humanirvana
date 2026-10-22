# Build and test

Requires Go 1.23 or later. CI uses Go 1.23.12.

```sh
go build ./... && go vet ./...
go test -race ./...
make cover01        # COVER-01: library statement coverage must be >= 80%
```

`make cover01` runs the root package and `tests/...` once with an atomic
profile. It fails below 80% of the library's statements. Examples are not
counted. CI runs build, vet, unit, comparison, race and COVER-01, and uploads
the COVER-01 evidence.

The external-server tests in `tests/integration/e2e_test.go` need
`HUMANIRVANA_HUMA_URL` and `HUMANIRVANA_NIRVANA_URL`. Without them that group
is skipped.

PERF-01, the latency comparison with native Huma, takes about one hour. See
[tests/performance](../../tests/performance/README.md).

## Layout

```text
adapter.go, context.go, legacy.go   the library (root package)
adapter_test.go                     public adapter tests
internal/                           historical packages, not used by the library
examples/                           basic, middleware, coexist
tests/compatibility/                Huma capability and legacy behavior comparisons
tests/integration/                  monitoring/context tests, external-server tests
tests/performance/                  PERF-01 fixture, server, load generator, runner
tests/unit/cover01/                 COVER-01 gate
```

## Recorded results

On candidate `ed87726` (Go 1.23.12, Apple M1 Pro):

- `go test -race ./...`: 137 pass, 2 skip. COVER-01: 89/93 statements.
- PERF-01: 15/15 cells pass, 75/75 valid pairs. Largest P95 delta +141µs
  (threshold 1000µs).
- Registering 100 operations takes 5.2ms vs 1.2ms on native Huma, because
  `Handle` scans every existing definition. This is a startup cost only.

On the coexist demo (binary `a2cde96e…`, same library source):

- `examples/coexist/compare_test.go`: a 43-step script gives identical status,
  headers, complete error bodies, middleware trace and side-effect counts on
  the original and adapted runtimes. A deliberate difference is detected.
- Concurrent cutover, 8 workers × 25 writes while switching owners: writes
  equal requests (race detector, 3 runs).
- Cutover drill on the binary: original → adapted → original. Final writes:
  original 2, adapted 1, equal to the 3 write requests. No replay.
- Black-box Hurl suite: 11/11.

Not compared: panic recovery and client disconnect during a legacy handler.
The Hurl suite and comparison script were written by the same team that
changed the product, so they are not an independent review.
