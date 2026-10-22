# Humanirvana

A thin [Huma](https://github.com/danielgtaylor/huma) adapter for the existing
[Nirvana](https://github.com/caicloud/nirvana) router. New Huma operations and
legacy Nirvana descriptors share the same routing tree.

## Status

Experimental. The adapter builds against unmodified Nirvana v0.3.1 and passes
the local build, race, comparison and coverage checks listed below. Full Huma
coverage and real-application migration acceptance are **not** established:
of 60 tracked Huma capabilities, 56 are partially supported (only the listed
cases are verified), 2 are unsupported (CBOR, merge-patch) and 2 are not
applicable. Review the media and method restrictions below before trying it.

## Install

Requires Go 1.23 or later (CI uses Go 1.23.12).

```sh
go get github.com/alswl/humanirvana
```

The module depends on `github.com/danielgtaylor/huma/v2` v2.35.0 and
`github.com/caicloud/nirvana` v0.3.1; no `replace` directive or Go workspace
is needed.

## Usage

Import `github.com/alswl/humanirvana` and use the standard Huma API:

```go
builder := rest.NewBuilder()
adapter := humanirvana.NewAdapter(builder)
api := huma.NewAPI(huma.DefaultConfig("My API", "1.0.0"), adapter)

// Register typed operations with huma.Register(api, ...).
// Register existing descriptors with builder.AddDescriptor(...).

handler, err := builder.Build()
if err != nil {
    return err
}
if err := adapter.Bind(handler); err != nil {
    return err
}
// Serve adapter with the application's HTTP server.
_ = api
```

`RegisterLegacy` is an optional convenience for adding existing descriptors.
Legacy routes retain Nirvana's execution chain; they are not automatically
converted into Huma validation or OpenAPI operations. The application owns
startup, configuration, dependency injection, and server shutdown.

## Supported behavior and verification scope

The current baseline is Huma v2.35.0 on unmodified Nirvana v0.3.1.
Use standard `huma.Register` typed handlers and Huma API/operation middleware.
Nirvana owns routing and media admission; admitted requests enter the Huma
middleware, binding, validation, handler, and response chain.

| Capability | Current support | Boundary |
| --- | --- | --- |
| GET, HEAD, POST, PUT, PATCH, DELETE | Supported | Other methods panic during registration. |
| Typed path/query/header and JSON input; defaults and validation | Tested subset | Compatibility tests compare accepted/rejected requests, status, JSON, and Content-Type. This is not a claim that every Huma type/tag combination is covered. |
| Typed output, response headers/status, errors and OpenAPI | Tested subset | Only the scenarios in the compatibility suite are verified; media admission can disagree with generated OpenAPI. |
| Huma API/operation middleware and request context | Tested subset | Order, short-circuit and context cases are covered. Nirvana admission can reject a request before Huma middleware runs. |
| Nirvana Filters/node Middleware and legacy descriptors | Preserved with tested cases | DefinitionModifiers must preserve the bridge callback shape. Real-application migration still requires behavior and side-effect comparisons. |
| New Huma and existing Nirvana routes on one router | Supported in examples/tests | A method/path has one owner. Legacy routes do not automatically gain Huma validation or OpenAPI. |
| CBOR with the default Nirvana media registry | Unsupported | Native Huma succeeds; this adapter returns 415 before the Huma callback. |
| AutoPatch merge-patch, custom raw/custom format media absent from Nirvana's registry | Unsupported for those media | For example application/merge-patch+json, application/x-raw and application/x-test are rejected before their Huma handler runs. This does not mean every RawBody or PATCH use is unsupported. |
| Identical media-admission errors across Huma and Nirvana | Unsupported | Unknown media can return 415 on both sides with different Content-Type and error bodies. Unsupported Accept can return 406. |
| All Huma capabilities, real-application lossless migration, coverage/performance targets | Not fully verified | Internal passing tests and synthetic examples do not establish these guarantees. |

## Startup rejection and request-time rejection

Registration already panics for unsupported HTTP methods, invalid Nirvana route
syntax, conflicting routes, and registration errors. A nil builder, operation,
or callback also panics. Register all operations before starting the HTTP server;
discard the unpublished instance if registration fails. Registration is not a
transaction and does not promise rollback of earlier registrations.

**Unsupported media currently does not cause a startup panic.** It is rejected
at request time with 415/406. The adapter receives an operation and callback; it
does not receive the complete Huma format configuration, and middleware/handlers
can choose media dynamically. It therefore cannot reliably detect every
unsupported behavior before serving. Do not treat successful startup as proof
that every configured format is supported.

A startup media preflight would need an explicit application call after all
registrations and configuration, before serving, to validate declared media
against the effective Nirvana registry. Such a preflight is not currently
implemented. It could reject incompatible declarations, but could not prove
arbitrary middleware/handler behavior compatible or prevent clients from sending
unknown media. Runtime 415/406 handling would still be required.

## Compatibility limits

The adapter uses the public descriptor API in unmodified Nirvana v0.3.1.
It does not require a Nirvana fork, a RawHandler extension, a local replacement,
or a Go workspace. Run `go build ./...` and `go test -race ./...` with Go 1.23
or later.

Nirvana checks request and response media types before invoking Huma.
`Content-Type: application/cbor` is not supported by the default Nirvana
consumers and returns **HTTP 415 before the Huma callback runs**, even when
CBOR is enabled in Huma's format configuration. An unsupported `Accept` type
can likewise be rejected with HTTP 406. Huma's serializer configuration alone
cannot bypass this outer check. The adapter does not modify Nirvana's global
consumer/producer registries or rewrite request headers to hide this limit.
Do not advertise CBOR support in an application's Huma configuration/OpenAPI
when using the default Nirvana media registry.

This admission rule also applies to unregistered custom media such as
`application/merge-patch+json`, `application/x-raw`, and `application/x-test`:
they return 415 before Huma AutoPatch, RawBody handling, or a custom decoder
can run. A media-admission error uses Nirvana's JSON error body, rather than
Huma's problem response. Huma may still generate OpenAPI entries for a format
configured by the application; generated documentation is not evidence that
Nirvana admits that format.

The standard GET, HEAD, POST, PUT, PATCH and DELETE methods are supported.
Other methods are rejected at registration rather than changing Nirvana's
global method registry. Existing builder DefinitionModifiers also apply to
Huma bridge definitions; modifiers must preserve the callback's single prefab
context parameter and empty result list.

```sh
go build ./...
go test -race ./...
go run ./examples/basic/cmd -mode mixed -listen 127.0.0.1:18080
```

The compatibility suite includes independent Huma baselines and synthetic
legacy applications; it records the CBOR restriction rather than claiming
complete Huma equivalence. Passing it does not establish a real application's
lossless migration. The external-server tests in `tests/integration/e2e_test.go`
additionally require explicit `HUMANIRVANA_HUMA_URL` and
`HUMANIRVANA_NIRVANA_URL` fixture addresses; otherwise that group is skipped.

## Quality checks

```sh
make cover01   # COVER-01: library statement coverage must be >= 80%
go run ./tests/performance/perf01 -h   # PERF-01 harness, see tests/performance/README.md
```

`make cover01` runs the library and `tests/` packages once with an atomic
profile and fails below 80% of the library's statements (examples are not
counted). CI runs build, vet, unit, comparison, race and COVER-01 checks.

## Documentation

- [Migration guide](docs/manual-migration.md): choose, bind, compare, cut over
  and roll back one operation at a time.
- [Migration checklist](docs/checklist.md)
- [Examples and recorded results](docs/examples.md)

## Layout

```text
adapter.go                 Huma route registration and HTTP delegation
context.go                 Per-request Huma context
legacy.go                  Optional legacy descriptor registration
adapter_test.go            Public adapter regression tests
internal/                  Historical conversion and pilot packages, not used by the adapter
docs/                      Migration guide, checklist and example results
examples/basic/            Small typed/legacy coexistence application
examples/middleware/       Auth, request-ID logging and shared dependencies
examples/coexist/          Larger synthetic Nirvana feature application with cutover
tests/compatibility/       Huma capability and legacy behavior comparisons
tests/integration/         Monitoring/context tests and external-server demo tests
tests/performance/         PERF-01 fixture, server, load generator and runner
tests/unit/cover01/        COVER-01 gate
```

Unit tests live beside the package they exercise. Experimental code in
`internal/pilot` is not part of the public adapter API.

## License

Apache License 2.0. See [LICENSE](LICENSE).
