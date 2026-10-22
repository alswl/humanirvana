# Compatibility reference

Baseline: Huma v2.35.0 on unmodified Nirvana v0.3.1, through the public
descriptor API. No Nirvana fork, RawHandler extension, `replace` directive or
Go workspace is needed.

Nirvana owns routing and media admission. Admitted requests enter the Huma
middleware, binding, validation, handler and response chain.

## Support

| Capability | Status | Boundary |
| --- | --- | --- |
| GET, HEAD, POST, PUT, PATCH, DELETE | Supported | Other methods panic at registration. Nirvana's global method registry is not changed. |
| Typed path/query/header and JSON input, defaults, validation | Tested subset | Tests compare accepted/rejected requests, status, JSON and Content-Type. Not every Huma type/tag combination is covered. |
| Typed output, response headers/status, errors, OpenAPI | Tested subset | Only the compatibility-suite scenarios are verified. Media admission can disagree with generated OpenAPI. |
| Huma API/operation middleware and request context | Tested subset | Order, short-circuit and context cases are covered. Nirvana admission can reject a request before Huma middleware runs. |
| Nirvana Filters, node Middleware, legacy descriptors | Preserved, tested cases | Builder DefinitionModifiers also apply to Huma bridge definitions; they must keep the single prefab context parameter and empty result list. |
| Nirvana middleware context in Huma | Tested | Values, deadline and cancellation set by Nirvana middleware reach Huma middleware and handlers. |
| Huma and Nirvana routes on one router | Supported | Each method/path has one owner. Legacy routes do not gain Huma validation or OpenAPI. |
| CBOR (default Nirvana media registry) | Unsupported | 415 before the Huma callback, even when CBOR is enabled in Huma's format configuration. |
| Media absent from Nirvana's registry, e.g. `application/merge-patch+json`, `application/x-raw`, `application/x-test` | Unsupported for those media | 415 before AutoPatch, RawBody handling or a custom decoder runs. Other RawBody or PATCH uses are not affected. |
| Same media-admission error as native Huma | Unsupported | Unknown media returns 415 with Nirvana's JSON error body, not Huma's problem response. Unsupported `Accept` can return 406. |
| All Huma capabilities; real-application lossless migration | Not established | Of 60 tracked Huma capabilities, 56 are partial and 2 unsupported. Synthetic examples do not prove a real migration. |

The adapter does not change Nirvana's global consumer/producer registries or
rewrite request headers to hide media limits. Do not advertise CBOR or other
unadmitted media in your Huma configuration or OpenAPI. Generated OpenAPI is
not evidence that Nirvana admits a format.

## Registration and request-time errors

Registration panics on:

- an unsupported HTTP method;
- invalid Nirvana route syntax;
- a conflicting route or other registration error;
- a nil builder, operation or callback.

Register all operations before you start the HTTP server. If registration
fails, discard the instance. Registration is not a transaction and does not
roll back earlier registrations.

Unsupported media does **not** fail at startup. It is rejected per request with
415 or 406. The adapter only receives an operation and a callback, not the full
Huma format configuration, and handlers can choose media dynamically. A
successful startup therefore does not prove that every configured format works.
A startup media preflight is not implemented.
