# Adapter contract

Supported versions: **Huma v2.35.0**, **Nirvana v0.3.1**, as declared in
`go.mod`, without dependency changes or forks. Other dependency versions are
not verified.

Upstream references for these versions:

- Huma v2.35.0 [humachi](https://github.com/danielgtaylor/huma/blob/v2.35.0/adapters/humachi/humachi.go), [humagin](https://github.com/danielgtaylor/huma/blob/v2.35.0/adapters/humagin/humagin.go), [humago](https://github.com/danielgtaylor/huma/blob/v2.35.0/adapters/humago/humago.go).
- Huma v2.35.0 [shared adapter tests](https://github.com/danielgtaylor/huma/blob/v2.35.0/adapters/adapters_test.go), [context wrappers](https://github.com/danielgtaylor/huma/blob/v2.35.0/api.go), and [body processing](https://github.com/danielgtaylor/huma/blob/v2.35.0/huma.go).
- Nirvana v0.3.1 [HTTP context](https://github.com/caicloud/nirvana/blob/v0.3.1/service/context.go) and [REST builder](https://github.com/caicloud/nirvana/blob/v0.3.1/service/rest/builder.go).

## Native context and propagation

```go
api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
    ctx = huma.WithValue(ctx, requestIDKey{}, "req-42")
    request, writer := humanirvana.Unwrap(ctx)
    writer.Header().Set("X-Method", request.Method)
    nirvana := service.HTTPContextFrom(ctx.Context())
    _ = nirvana // matched route, values and tracking response writer
    next(ctx)
    log.Printf("status=%d", ctx.Status())
})
```

`Unwrap` returns `(*http.Request, http.ResponseWriter)`. It walks nested
`Unwrap() huma.Context` wrappers, including `huma.WithContext`, `huma.WithValue`
and custom wrappers. A context from another adapter, or nil, panics, as with
the official adapters. The response writer is Nirvana's tracking writer;
there is no API for obtaining its private server writer.

Huma v2.35.0's `WithContext` replaces only the context visible through
`ctx.Context()`. `WithValue` derives from that context. Neither changes
`Unwrap(ctx)`'s request or `request.Context()`. This matches the official
adapters in this version. Use `ctx.Context()` for downstream Huma middleware,
resolvers and typed handlers. Derive from it to preserve the Nirvana context,
incoming values, deadline and cancellation. Replacing it with an unrelated
`context.Background()` also discards those values and Nirvana access.

Nirvana node middleware values passed through `chain.Continue(derived)` reach
Huma middleware and handlers. Huma values passed through `next(derived)` reach
subsequent Huma middleware and handlers. They do not propagate back into
earlier middleware's context or native request context. An HTTP middleware
that explicitly needs the Huma override can receive
`request.WithContext(ctx.Context())` from a Huma middleware bridge.

Huma wrappers delegate response methods to the same adapter context. Outer
Huma middleware can read the final `ctx.Status()` after `next`, including
success, handler errors, validation errors and short circuits. Initial Huma
status is zero. Nirvana node middleware can read the final
`ResponseWriter().StatusCode()` after `chain.Continue`.
This status tracking applies to responses written through Huma's `SetStatus`; direct
writes to the unwrapped writer bypass Huma's status field.

## Multipart body size

Set a positive `Operation.MaxBodyBytes` explicitly for multipart operations.
`GetMultipartForm` installs `http.MaxBytesReader` before parsing and counts
the entire body, including boundaries, headers, fields, files and epilogue.
A body of exactly the configured size is accepted; one byte more returns a
`*http.MaxBytesError`. Unknown `ContentLength` does not bypass the limit.
Repeated calls return the cached parse result/error.

The `ParseMultipartForm(8 << 10)` argument is a **file memory threshold**.
Larger files spill to temporary files. It does not impose an 8 KiB total
upload limit, and it does not cap all parser allocations at 8 KiB.

Version-specific behavior:

| Case | Huma v2.35.0 / this adapter |
| --- | --- |
| Positive multipart `MaxBodyBytes` | Enforced by this adapter. The three pinned official adapters do not enforce it themselves. |
| `MaxBodyBytes: -1` | No adapter body limit. |
| `MaxBodyBytes: 0` with only multipart `RawBody` | No adapter body limit. Huma v2.35.0 does not initialize the documented 1 MiB default for this input form. |
| Multipart limit exceeded or malformed form | Huma emits **422**, `application/problem+json`, `detail: validation failed`, with a `body` error. |
| Ordinary typed `Body` | Huma initializes its 1 MiB default and handles body-limit errors as 413. |

The multipart 422 is Huma v2.35.0's actual `readForm`/validation behavior,
despite `Operation.MaxBodyBytes` documentation describing 413 generally.
Errors use Huma's standard problem-response format.

The adapter cannot count bytes already consumed before `GetMultipartForm`.
For Huma uploads, avoid `service.ParseRequestForm()` and other middleware
that pre-read or pre-parse multipart. If parsing must occur earlier, install
an application HTTP body limiter **before** that parsing. Huma middleware
short circuits that never parse a form also do not invoke this limit.
Legacy upload handlers retain Nirvana's own behavior.

## Routing and binding

| Scenario | Contract |
| --- | --- |
| Registered methods | Uppercase GET, HEAD, POST, PUT, PATCH, DELETE. |
| Unsupported registration | OPTIONS, TRACE, CONNECT, custom, lowercase or empty methods panic at the adapter boundary. Huma may reject invalid operations earlier. No global Nirvana method registration is performed. |
| Unsupported request method on an existing route | Nirvana 405. HEAD requires explicit registration; GET does not imply HEAD. OPTIONS/CORS is application-owned. |
| Unknown path | Nirvana 404. |
| Same path, different methods | Supported, including legacy and Huma methods on the same path. |
| Simple `{param}` | Supported, including escaped slashes and static-route precedence. Huma methods with the same complete route shape may use different or reordered parameter names. |
| Duplicate Huma route or Huma added after conflicting legacy route | Adapter registration panic, including a legacy `Any` or Huma system-route conflict. |
| Conflicting legacy descriptor added after Huma | Direct builder calls bypass the adapter checks. Nirvana may reject it during `Build`; registration is not transactional. |
| Unbound adapter request | 503, without an implicit build. |
| `Bind(nil)` / repeated `Bind` | Returns an error; the previously bound handler is retained. |

For shared prefixes with different parameter names, Nirvana keeps separate
parameter branches and tries their method inspectors. Keep each method/path
under one owner: direct legacy registration of the same route shape and
method can bypass adapter conflict checks, especially with renamed parameters.
The adapter only reuses names when it registers a Huma operation with the same
complete route shape. Nirvana regex/catch-all syntax is passed to its parser; remapping parameter
names for these patterns is not part of the supported Huma path contract.

Registration, plugin installation and building are startup operations. Finish
them before serving; concurrent route registration is not supported.
The sequence remains explicit:

1. Configure the builder and, if used, call `InstallPlugins` with an explicit list.
2. Register all legacy descriptors and Huma operations.
3. Call `builder.Build()` and handle its error.
4. Call `adapter.Bind(builtService)` once, then serve the adapter.

`Build` produces a snapshot; it does not bind the adapter. If routes are
registered after a build, build again **before** binding. A bound service does
not acquire later registrations. `Bind` accepts an `http.Handler` and cannot
verify its builder identity or whether the application built it in the
correct order. Create a new adapter/service instance when replacing a router.

## Examples and migration

The executable [minimal mixed-route example](../../example_test.go) combines
legacy GET and Huma POST on `/items/{id}`, shows `Unwrap` after `WithValue`,
and uses explicit Build/Bind. Run it with `go test . -run '^Example$' -v`.

When migrating multipart uploads, set a positive `MaxBodyBytes` budget that
includes multipart overhead, or use `-1` if the application deliberately
permits unlimited uploads. Clients must handle Huma v2.35.0's 422 for
multipart size errors.

## Contract tests

- [context_contract_test.go](../../context_contract_test.go): nested wrappers,
  native identity, values/deadline/cancellation and final status against humago.
  Existing `adapter_test.go` covers Nirvana middleware propagation and logging.
- [multipart_contract_test.go](../../multipart_contract_test.go): exact/over
  limits, epilogue, unknown length, zero/disabled limits, spill to disk, repeated
  parsing, raw/decoded multipart, malformed forms and pinned Huma errors.
- [routing_contract_test.go](../../routing_contract_test.go): methods, parameters,
  conflicts, mixed registration order, shared prefixes and Build/Bind snapshots.
