# Humanirvana

A thin [Huma](https://github.com/danielgtaylor/huma) adapter for the existing
[Nirvana](https://github.com/caicloud/nirvana) router. New Huma operations and
legacy Nirvana descriptors share one routing tree, so you can migrate one
operation at a time.

**Status: experimental.** Full Huma support and real-application migration are
not established. CBOR and media unknown to Nirvana return 415. See the
[compatibility reference](docs/manual/compatibility.md) before you use it.

## Install

Requires Go 1.23 or later.

```sh
go get github.com/alswl/humanirvana
```

Dependencies: Huma v2.35.0 and Nirvana v0.3.1, unmodified.

## Usage

```go
builder := rest.NewBuilder()
adapter := humanirvana.NewAdapter(builder)
api := huma.NewAPI(huma.DefaultConfig("My API", "1.0.0"), adapter)

// New operations: huma.Register(api, ...)
// Existing descriptors: humanirvana.RegisterLegacy(builder, descriptors)

handler, err := builder.Build()
if err != nil {
	return err
}
if err := adapter.Bind(handler); err != nil {
	return err
}
// Serve adapter with your HTTP server.
```

Register everything before `Build`, then `Bind` once. Legacy routes keep
Nirvana's execution chain and do not appear in Huma's OpenAPI. Your
application owns startup, configuration, dependencies and shutdown.

For a complete minimal mixed-route example, see [example_test.go](example_test.go).
[`humanirvana.Unwrap`](docs/manual/adapter-contract.md#native-context-and-propagation)
exposes the native request and Nirvana response writer through Huma wrappers.
Set multipart `MaxBodyBytes` explicitly; see the [version-specific limits and
error responses](docs/manual/adapter-contract.md#multipart-body-size).

Optional [plugin lifecycle management](docs/guide/plugins.md) installs an explicit
ordered plugin list and returns an idempotent cleanup function. The application
continues to own configuration, routing and HTTP lifecycle.

## Documentation

- [Migrate an operation](docs/guide/migration.md) and its [checklist](docs/guide/checklist.md)
- [Examples](docs/guide/examples.md)
- [Compatibility reference](docs/manual/compatibility.md)
- [Build and test](docs/dev/testing.md)

## License

Apache License 2.0. See [LICENSE](LICENSE).
