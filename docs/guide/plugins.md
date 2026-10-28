# Optional plugin lifecycle

`InstallPlugins(builder service.Builder, config *nirvana.Config, names []string)
(cleanup func() error, err error)` manages explicitly selected Nirvana plugins.
`NewAdapter` and `Bind` remain independent of this helper. Applications still
choose plugins, register routes, build the service, and own HTTP startup and shutdown.

```go
cfg := nirvana.NewConfig().Configure(healthcheck.Path("/healthz"))
cleanup, err := humanirvana.InstallPlugins(builder, cfg, []string{"healthcheck"})
if err != nil {
    return err // Includes automatic rollback errors.
}
defer func() { err = errors.Join(err, cleanup()) }() // In a named-error function.
// Register routes, Build, Bind, and serve with the application's http.Server.
// Stop serving and wait for requests before this function returns.
```

See the [runnable standard http.Server example](../../examples/plugins/main.go).
Run `go run ./examples/plugins`, request `http://127.0.0.1:18080/hello` or
`/healthz`, and press Ctrl-C to drain requests and clean up plugins.
Startup, serving, or cleanup errors produce exit status 1. Normal SIGTERM/SIGINT
shutdown produces status 0. The example calls `os.Exit` only after `run` returns
(and its deferred plugin cleanup completes) and signal notification is stopped.

To reproduce the executable's exit behavior on a POSIX system with Go, Python 3,
and local port 18080 available, run:

```sh
python3 examples/plugins/smoke_test.py
```

The script holds the port open and checks startup exits with status 1, then starts
fresh processes, checks both HTTP endpoints, and verifies SIGTERM and SIGINT each
exit with status 0. The example's healthcheck Uninstall is a no-op; this process
check verifies shutdown and exit behavior, while lifecycle tests verify uninstall
invocation. Cleanup runs synchronously before `run` returns on both paths.

## Selection and configuration

The helper resolves names through Nirvana's `ConfigInstallerFor`. Import the
selected plugin packages so their installers register. This is Nirvana's plugin
registry, not a global configuration source. Register installers before use;
Nirvana's registry does not synchronize concurrent registration and lookup.

Before installing anything, the helper validates the entire list in order:

- A nil builder or config is an error, even with an empty list. Interface values
  must contain valid builders, not typed nil pointers.
- Duplicate names are errors, rather than repeated installations.
- Unknown names are errors, even if their configuration is absent.
- A selected plugin with `cfg.Config(name) == nil` is an error. No default
  configuration is synthesized. Use the plugin's own configurers to configure it.
- Configuration for unselected plugins is ignored. An empty list installs nothing.

The first validation error is returned without installation side effects. Plugin
configuration types are opaque; typed nil values or wrong types are subject to
that plugin's own contract and may panic. Panics are not converted to errors.

Only the supplied config is passed to plugins. The helper does not read files,
environment variables or old global configuration, or apply Config's server
settings, descriptors, filters, modifiers, or logger to the builder. Applications
set up their builder themselves. Plugins can mutate their own configuration while
installing, so the helper retains the original config rather than copying it.
Do not mutate or share that config with another lifecycle until cleanup finishes.
Each call is a separate lifecycle: duplicate detection is within one list, not
across calls. Do not also install/uninstall these plugins via Nirvana's Server.

When migrating an application that previously skipped missing configurations,
filter the application's ordered candidate list before calling the helper:

```go
var enabled []string
for _, name := range candidates {
    if cfg.Config(name) != nil {
        enabled = append(enabled, name)
    }
}
cleanup, err := humanirvana.InstallPlugins(builder, cfg, enabled)
```

## Ownership and cleanup

| Situation | Responsibility and order |
| --- | --- |
| Installation fails | The helper stops installing and uninstalls only previously successful plugins, in reverse order. It returns the installation error joined with every rollback error. The failing plugin or application must handle its partial resources. |
| Later initialization fails | The application calls cleanup, then propagates the initialization error joined with the cleanup error. Any resources created later by the application are released first. |
| Normal exit | The application stops HTTP traffic, waits for requests and other plugin resource users to finish, releases later application resources, then calls cleanup. |

The cleanup function is always non-nil, including on validation and install
errors. It attempts every successful plugin's Uninstall in reverse order even
when one returns an error. It is safe for repeated and concurrent calls: each
uninstall is attempted once, and every caller receives the cached cleanup result.
An uninstall error is not retried because retry safety is absent from the contract.
After automatic rollback, cleanup returns only the cached rollback errors; the
returned install error already includes them and need not be joined again.

Install and uninstall errors include plugin name and phase, wrap original errors
with `%w`, and are combined with `errors.Join`. `errors.Is` and `errors.As` can
inspect the original install error and every cleanup error. Validation errors
include the offending name; no exported validation error types are defined.

## Nirvana v0.3.1 contract limits

The upstream [ConfigInstaller contract](https://github.com/caicloud/nirvana/blob/v0.3.1/nirvana.go)
only provides `Name`, `Install`, and `Uninstall`. It supplies no partial-install
state, cancellation, transactional builder restoration, or retry guarantees.
The helper records a plugin only when Install returns nil. It never guesses
whether calling Uninstall after a failed Install would be safe.

For example, upstream [tracing](https://github.com/caicloud/nirvana/blob/v0.3.1/plugins/tracing/tracing.go)
can create a tracer before AddDescriptor returns an error. This helper cannot
guarantee recovery of that failed plugin's tracer. Use a plugin with self-cleaning
Install failure behavior, fix the plugin, or provide application-specific recovery
based on its documented resource ownership. Successful installation only permits
an Uninstall attempt; the helper cannot guarantee release if that attempt fails.

Uninstall also need not remove routes: healthcheck's Uninstall is a no-op. Discard
the builder and config after an installation failure or cleanup; they are not
restored snapshots. Plugin panics and hanging Uninstall calls are outside this
error-return contract. Cleanup must not recursively invoke itself.
