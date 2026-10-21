# Humanirvana

A thin [Huma](https://github.com/danielgtaylor/huma) adapter for the existing
[Nirvana](https://github.com/caicloud/nirvana) router. New Huma operations and
legacy Nirvana descriptors share the same routing tree.

## Status

Experimental checkpoint for development and evaluation. The adapter builds
against unmodified Nirvana v0.3.1 and has passed local Go/race and HTTP checks.
The latest descriptor-based candidate has not yet had an independent API
verification pass or real-application migration acceptance. Full Huma coverage,
coverage/performance targets, and final acceptance remain open. Review the media
and method restrictions below before trying it.

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
lossless migration. Tests under `tests/integration/` additionally require
explicit `HUMANIRVANA_HUMA_URL` and `HUMANIRVANA_NIRVANA_URL` fixture addresses;
otherwise that external-server group is skipped.

## Layout

```text
adapter.go                 Huma route registration and HTTP delegation
context.go                 Per-request Huma context
legacy.go                  Optional legacy descriptor registration
adapter_test.go            Public adapter regression tests
internal/                  Auxiliary conversion and experimental bridge packages
examples/basic/            Small typed/legacy coexistence application
examples/coexist/          Larger synthetic Nirvana feature application
tests/compatibility/       Huma capability and legacy behavior comparisons
tests/integration/         External-server demo integration tests
```

Unit tests live beside the package they exercise. Experimental code in
`internal/pilot` is not part of the public adapter API.

## License

Apache License 2.0. See [LICENSE](LICENSE).
