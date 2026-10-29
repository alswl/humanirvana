# Examples

The [executable minimal example](../../example_test.go) shares `/items/{id}`
between legacy GET and Huma POST, uses `Unwrap` through a Huma value wrapper,
and builds/binds explicitly. Run `go test . -run '^Example$' -v`.

| Example | Shows | Run |
| --- | --- | --- |
| [examples/basic](../../examples/basic) | Typed Huma operations (input, validation, response headers, a short-circuit middleware). `-mode mixed` adds one original Nirvana descriptor through `RegisterLegacy`. | `go run ./examples/basic/cmd -mode mixed -listen 127.0.0.1:18080` |
| [examples/middleware](../../examples/middleware/README.md) | Auth, request-ID logging and a shared dependency for a legacy descriptor and a Huma operation. | `go run ./examples/middleware -listen 127.0.0.1:18080` |
| [examples/plugins](../../examples/plugins/main.go) | Ordered plugin installation, cleanup on initialization failure, and cleanup after standard `http.Server` shutdown. | `go run ./examples/plugins` |
| [examples/coexist](../../examples/coexist/README.md) | Original and adapted runtimes side by side, per-operation cutover and rollback. | see its README |

## The coexist migration

`examples/coexist` is a synthetic application built from the Nirvana
documentation, not a production application. Its representative migrated
operation is `POST /projects/{projectID}/tasks`.

| Part | Original runtime | Adapted runtime |
| --- | --- | --- |
| Business method | `store.createTask` | the same `store.createTask` |
| Request policy Filter, `ParseRequestForm` | on the builder | installed identically |
| Audit and bearer-auth middleware | descriptor middleware | the same functions |
| Other routes | `builder.AddDescriptor` | `RegisterLegacy` with the same descriptors |
| State | its own in-memory store | an equivalent, separate store |

Only the store differs, so writes can be counted per runtime.
