# Examples and recorded results

## examples/basic

A small application with typed Huma operations (path/query/header/body input,
validation, response headers, an operation middleware that short-circuits) on
the Nirvana router. `-mode mixed` also registers one original Nirvana
descriptor and handler through `RegisterLegacy`.

```sh
go run ./examples/basic/cmd -mode mixed -listen 127.0.0.1:18080
```

## examples/middleware

Authentication, request-ID logging and an application-owned dependency shared
by a legacy Nirvana descriptor and a Huma operation on one router. See
[its README](../examples/middleware/README.md).

## examples/coexist: the representative migrated operation

`examples/coexist` is a synthetic application built from the Nirvana
documentation. It is **not** a production application; real-application
acceptance is still open. Its representative operation is
`POST /projects/{projectID}/tasks`.

### What is reused

| Part | Original runtime | Adapted runtime |
| --- | --- | --- |
| Business method | `store.createTask` | the same `store.createTask` |
| Request policy Filter, `ParseRequestForm` | installed on the builder | installed identically |
| Audit and bearer-auth node middleware | descriptor middleware | the same middleware functions |
| Legacy routes (projects, tasks, messages, jobs, Auto/Prefab/Form/File/Modifier) | `builder.AddDescriptor` | `RegisterLegacy` with the same descriptors |
| State | its own in-memory store | an equivalent, separate store |

Only the per-arm store differs, so writes can be counted separately.

### Recorded results (candidate binary SHA-256 `a2cde96e…`)

- `compare_test.go`: a 43-step script (successes, validation, binding, auth,
  Filter, handler and routing errors, media admission) gives identical status,
  headers, complete error bodies, middleware trace and side-effect counts on
  both arms; a deliberate difference is detected.
- Concurrent cutover: 8 workers × 25 writes while switching owners
  repeatedly; total writes equal requests (race detector, 3 runs).
- Cutover drill on the binary (`-arm cutover -observe`): original write id 2
  (1 write) → switch to adapted, write id 2 in the adapted store → unmigrated
  `GET /projects/1/summary` still 200 → unknown owner rejected with 400 →
  roll back, write id 3; final writes original 2, adapted 1, equal to the 3
  write requests; no replay.
- Black-box Hurl suite (my-tests, run `20261022-010843-98493-1`): 11/11,
  including parity of the extra routes and of seven error chains.

### Known limits

- CBOR, merge-patch and unregistered media return 415/406 before Huma.
- Legacy routes do not appear in the Huma OpenAPI document.
- Panic recovery and client disconnect during a legacy handler were not
  compared.
- The Hurl suite and comparison script were written by the same team that
  changed the product; they are not an independent review.
