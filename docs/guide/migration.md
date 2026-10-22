# Migrating a Nirvana application to Huma, one operation at a time

This guide moves an existing Nirvana v0.3.1 application to Huma v2.35.0
operations without a big-bang rewrite. Each step is done by the application;
Humanirvana only provides the thin Web adapter (`NewAdapter`, `Handle`, `Bind`,
the per-request Huma context, and `RegisterLegacy`). There is no migration
CLI, code generator, or gateway.

Before you start, read [the checklist](checklist.md) and the
[compatibility reference](../manual/compatibility.md).

## 1. Choose an operation

Pick one `METHOD /path` that has a clear owner and observable side effects you
can count (for example a create endpoint that writes once). Record, from the
existing code:

- the `definition.Definition` and its descriptor path, parameters (`Path`,
  `Query`, `Header`, `Body`, `Form`, `File`, `Auto`, `Prefab`) and results;
- the Filters, descriptor/node Middlewares and DefinitionModifiers that apply;
- the dependencies the handler uses and their scope (singleton, per request);
- the error types it returns and their wire format;
- every side effect (writes, calls, messages).

Start with operations that use JSON or text. CBOR, `application/merge-patch+json`
and other media absent from Nirvana's registry are rejected with 415 before Huma
runs.

## 2. Check capabilities

Look up each input, output, validation and middleware feature the operation
needs in the [compatibility reference](../manual/compatibility.md). "Tested subset" means only the listed cases
are verified; check your exact combination in step 4. Stop and keep the
operation on Nirvana if it needs an unsupported capability.

## 3. Bind both runtimes in one process

Keep the existing builder and its descriptors as the *original* runtime. Build
a second builder for the *adapted* runtime, attach the Huma API to it, and add
the unmigrated descriptors unchanged:

```go
original := rest.NewBuilder()
// ... existing Filters, Modifiers and builder.AddDescriptor(...) calls

adaptedBuilder := rest.NewBuilder()
// install the same Filters and Modifiers as the original builder
adapter := humanirvana.NewAdapter(adaptedBuilder)
api := huma.NewAPI(huma.DefaultConfig("orders", "1.0.0"), adapter)

// Unmigrated operations keep Nirvana's execution chain.
if err := humanirvana.RegisterLegacy(adaptedBuilder, unmigratedDescriptors); err != nil {
	return err
}
// The migrated operation calls the same service method as the old handler.
huma.Register(api, huma.Operation{Method: http.MethodPost, Path: "/projects/{projectID}/tasks"},
	func(ctx context.Context, in *CreateTaskInput) (*CreateTaskOutput, error) {
		task, err := svc.CreateTask(ctx, in.ProjectID, in.Body)
		// map err to a huma error with the same status and body as before
		...
	})

handler, err := adaptedBuilder.Build()
if err != nil {
	return err // discard the whole adapted instance; nothing is served
}
if err := adapter.Bind(handler); err != nil {
	return err
}
```

Rules that matter:

- Register everything before `Build`, and `Bind` once after `Build` succeeds.
  Registration panics on unsupported methods, invalid routes and duplicates;
  a failed build must not be served. Registration is not transactional.
- Reuse the same service method and dependency instances; do not copy the
  business logic into the Huma handler.
- Install the same Filters on both builders. A missing Filter on the adapted
  builder changed a 403 into a 200 in the Stage04 demo before it was fixed.
- Nirvana node middleware still wraps Huma operations, and values it puts in
  the request context reach Huma middleware and handlers.

## 4. Compare behavior

Run the same requests against both runtimes, each with an isolated but
equivalent initial state, and compare:

- status, `Content-Type` and key headers;
- the complete response body, including error bodies, after normalizing
  declared non-deterministic fields;
- middleware order and short-circuit behavior;
- side-effect counts (writes, handler calls).

`examples/coexist/compare_test.go` is a working pattern: it runs a 43-step
script against both arms, fails on any difference, and checks that the
comparison itself detects a deliberate difference. Add negative cases (missing
auth, validation failures, handler errors, unknown routes, wrong media).

## 5. Cut over and roll back

Put an application-side selector in front of both runtimes that gives every
`METHOD /path` exactly one owner and sends undeclared routes to the original
runtime. `examples/coexist/cutover.go` (`RouteCutover`) shows one:

- the ownership map is validated before it is published (malformed keys,
  unknown owners, duplicates and overlapping patterns are rejected);
- each request reads the owner once on entry, so a switch only affects
  requests that arrive afterwards; in-flight requests finish where they
  started and nothing is retried or replayed;
- switching back restores the original owner.

Cut over one operation, watch its side-effect counts and errors, then either
keep it or roll back. Restarting with a different owner configuration also
works, but resets any in-memory state. Neither path migrates data between
stores; the two runtimes must share the real backing services.

## 6. Repeat and retire

Repeat for the next operation. When all operations of a descriptor are owned
by the adapted runtime and have been stable, remove them from the original
runtime and from `RegisterLegacy`. Keep the comparison tests until the
original runtime is retired.
