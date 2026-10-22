# PERF-01: framework path performance

PERF-01 compares the HTTP latency of the same Huma operations served two ways:

- **native**: Huma's standard `humago` adapter on `net/http.ServeMux`;
- **adapter**: Huma through `humanirvana` on a Nirvana v0.3.1 router.

Both arms register the same operations with the same Huma configuration
(`fixture.Config`), so they do equivalent work. A cell passes when all 5 valid
pairs satisfy `P95(adapter) − P95(native) < 1000µs`; PERF-01 passes only when
all 15 cells (P01–P05 × concurrency 1/8/32) pass.

| ID | Request | Expected |
| --- | --- | --- |
| P01 | `GET /perf/params/7?count=2&enabled=true`, `X-Tenant: fixture` | 200, parsed values echoed |
| P02 | `POST /perf/json-small`, 1,024-byte JSON body | 201 |
| P03 | `POST /perf/json-nested`, 16,384-byte JSON body with 32 nested items | 200, `{"count":32}` |
| P04 | `POST /perf/validate`, P02-shaped body with `quantity: 0` | 422 Huma validation error |
| P05 | `GET /perf/middleware`, 3 Huma middlewares setting and reading context | 200, fixed order and values |

## Layout

```text
fixture/     operations, frozen-case loader, equivalence and freeze tests, benchmarks
testdata/    frozen requests and expected responses, MANIFEST.sha256
server/      serves one arm on loopback until SIGTERM
loadgen/     closed-loop client for one arm: warm-up, sampling, 1µs histogram
perf01/      runs the whole protocol and writes cells.json and report.md
```

## Running

```sh
go test ./tests/performance/fixture          # both arms return the frozen responses
out=$(mktemp -d)
go build -o $out/server  ./tests/performance/server
go build -o $out/loadgen ./tests/performance/loadgen
go build -o $out/perf01  ./tests/performance/perf01
$out/perf01 -out $out -server $out/server -loadgen $out/loadgen   # about one hour
```

Do not run other load on the machine during a run. Each arm runs on a fresh
server process with `GOMAXPROCS` set to the CPU count; warm-up is 5 s; sampling
runs for at least 15 s and 10,000 requests, at most 120 s. Pairs alternate
native→adapter (attempts 1, 3, 5) and adapter→native (2, 4). An arm with any
unexpected response, connection error or too few samples invalidates its pair;
each cell gets at most 2 extra attempts. Every attempt is kept in `runs/`.

Allocation and registration costs are separate Go benchmarks and are not
compared with the latency threshold:

```sh
go test -run '^$' -bench . -benchmem -benchtime=1s -count=5 ./tests/performance/fixture
```

To change the fixture, bump `fixture.ProtocolRevision`, run
`go test ./tests/performance/fixture -run TestFreezeFixtures -freeze`, and
re-run the whole protocol.

## Limits

The client and server share one machine. Go's HTTP client may retry an
idempotent request once on a reused connection that closed before the request
was written; no such connection error was counted in the recorded run. The
delta is an end-to-end percentile difference, not the adapter's own CPU time
per request.
