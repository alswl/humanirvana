# Middleware, authentication and dependencies

`main.go` registers one existing Nirvana descriptor and one Huma operation on
the same Nirvana builder:

- `accessLog` is a Nirvana node middleware on the root descriptor. It derives
  the request context with a request ID, rejects requests without
  `Authorization: Bearer demo` with 401 before any handler runs, and logs the
  route template and final status, including Huma errors. When the middleware
  itself returns an error, Nirvana writes that response after the middleware
  returns, so it logs the error instead of a status.
- `Greeter` is created by the application and shared by both handlers. The
  library does not create or scope dependencies.
- The Huma handler reads the request ID that the Nirvana middleware put in the
  context.

```sh
go run ./examples/middleware -listen 127.0.0.1:18080
curl -i -H 'Authorization: Bearer demo' -H 'X-Request-ID: r1' http://127.0.0.1:18080/legacy/greeting
curl -i -H 'Authorization: Bearer demo' -H 'X-Request-ID: r2' http://127.0.0.1:18080/greetings/ada
curl -i http://127.0.0.1:18080/greetings/ada
```

For moving an operation between the original and migrated runtime and rolling
it back, see the coexistence example in [`examples/coexist`](../coexist/README.md)
and the [migration guide](../../docs/manual-migration.md).
