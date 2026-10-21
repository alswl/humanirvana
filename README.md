# Humanirvana

A thin [Huma](https://github.com/danielgtaylor/huma) adapter for the existing
[Nirvana](https://github.com/caicloud/nirvana) router. New Huma operations and
legacy Nirvana descriptors share the same routing tree.

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

## Development dependency

This work-in-progress snapshot requires a Nirvana REST builder implementing
`service.RawHandlerRegistrar`. Unmodified Nirvana v0.3.1, currently listed in
`go.mod`, does not provide this extension. Validation uses a local Go workspace
containing this module and the patched Nirvana checkout. A standalone checkout
is not yet a reproducible build; an available upstream revision must be pinned
before release. No machine-specific dependency path is committed.

With that workspace configured, use Go 1.23 or later:

```sh
go build ./...
go test -race . ./internal/... ./tests/compatibility/... ./examples/...
go run ./examples/basic/cmd -mode mixed -listen 127.0.0.1:18080
```

The compatibility suite includes independent Huma baselines and synthetic
legacy applications. Passing it does not establish a real application's
lossless migration. Tests under `tests/integration/` additionally require the
legacy demo servers. Set `HUMANIRVANA_HUMA_URL` and
`HUMANIRVANA_NIRVANA_URL` to explicitly select those fixtures. Without both
addresses, this external-server group is skipped without contacting fixed ports.

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
