# Migration checklist

Use one copy of this list per migrated operation. Every item needs evidence
(a test, a recorded request/response, or a count), not a statement.

## Versions and support

- [ ] Go 1.23 or later (CI uses 1.23.12); Huma v2.35.0; Nirvana v0.3.1
      without `replace` directives or a Go workspace.
- [ ] Every capability the operation uses is listed as supported or a tested
      subset in the [README](../README.md#supported-behavior-and-verification-scope),
      and the exact combination is covered by a comparison test.
- [ ] The operation does not depend on CBOR, merge-patch, or other media that
      Nirvana's registry does not admit (these return 415/406 before Huma).
- [ ] HTTP method is GET, HEAD, POST, PUT, PATCH or DELETE.

## Errors, authentication and side effects

- [ ] Filters and node Middlewares are installed identically on both builders;
      a rejected request (403/401) never reaches the handler on either side.
- [ ] Validation, binding, handler and routing errors return the same status,
      `Content-Type` and complete body on both runtimes, or the difference is
      a documented, accepted contract change.
- [ ] Request ID, logging and other context values set by Nirvana middleware
      are visible to the Huma handler.
- [ ] Each request produces the same number of writes and handler calls on
      both runtimes, including for rejected requests (zero).
- [ ] Dependencies are created by the application and shared, not rebuilt per
      request by the migrated handler.

## Route ownership

- [ ] The operation's `METHOD /path` has exactly one owner at any time.
- [ ] Undeclared routes go to the original runtime and still work.
- [ ] The ownership map rejects duplicates, unknown owners and overlapping
      patterns before it is published.
- [ ] No route is registered twice on the same builder (registration panics).

## Cutover and rollback

- [ ] A switch affects only requests that arrive after it; in-flight requests
      finish on their original owner and are not retried or replayed.
- [ ] Rolling back restores the original owner, and the next write lands in the
      original runtime with the expected count.
- [ ] Restart-based switching is understood to reset in-memory state.
- [ ] The rollback path was exercised before the cutover went live.
