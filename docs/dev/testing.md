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
*_contract_test.go, example_test.go  pinned adapter contracts and minimal mixed routes
internal/                           historical packages, not used by the library
examples/                           basic, middleware, coexist
tests/compatibility/                Huma capability and legacy behavior comparisons
tests/integration/                  monitoring/context tests, external-server tests
tests/performance/                  PERF-01 fixture, server, load generator, runner
tests/unit/cover01/                 COVER-01 gate
```
