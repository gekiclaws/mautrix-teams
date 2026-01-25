Open Questions
- None.

Task Checklist
Phase 1 — Skype token acquisition + parsing
☐ Add `AcquireSkypeToken(ctx)` to fetch and parse the Teams consumer skypetoken via cookies only, with error logging on non-2xx.
☐ Add unit tests for response parsing and expiry calculation using `httptest`.

Phase 2 — Persistence + validity helpers
☐ Extend `AuthState` with `SkypeToken` and `SkypeTokenExpiresAt`, update load/save logic to treat skype-only state as valid, and mark OAuth/MSAL fields as bootstrap-only/legacy.
☐ Add `HasValidSkypeToken(now)` helper with skewed expiry check and unit tests for time boundary cases.

Phase 3 — Usage in requests + CLI wiring
☐ Attach `Authorization: Bearer <skypetoken>` to Teams API requests when token is present.
☐ Update `teams-login` flow to acquire/persist the skypetoken after cookie login and reuse it on subsequent runs; probe uses skypetoken immediately when present.

Phase 1 — Skype token acquisition + parsing
Files
- `internal/teams/auth/skype_token.go`: Add `AcquireSkypeToken(ctx)` and response structs, parse `skypeToken.skypetoken` + `expiresIn`, compute expiry timestamp, and return token + expiry.
- `internal/teams/auth/skype_token_test.go`: Add unit tests for parsing success cases and expiry math using a fixed clock.

Plan
- Create `AcquireSkypeToken(ctx)` on `*Client` that POSTs to `https://teams.live.com/api/auth/v1.0/authz/consumer` using the existing HTTP client with cookies, no body, and no OAuth headers.
- Decode the JSON payload into a typed struct, validate `skypeToken.skypetoken` presence, and compute `expiresAt` as `now + expiresIn` (seconds).
- On non-2xx, read a small body snippet (e.g., 2KB) and log `status` + `body_snippet`, then return a descriptive error.
- Unit tests (httptest):
  - Success response with `expiresIn` sets `expiresAt` correctly.
  - Missing `skypeToken.skypetoken` returns an error.
  - Non-2xx returns an error and preserves body snippet length.

Phase 2 — Persistence + validity helpers
Files
- `internal/teams/auth/state.go`: Add `SkypeToken` and `SkypeTokenExpiresAt` fields; update `Load()` to accept skype-only state; keep existing OAuth fields intact and add a bootstrap-only/legacy comment.
- `internal/teams/auth/skype_token.go`: Add `HasValidSkypeToken(now)` helper on `AuthState` (or as a standalone function) using `SkypeTokenExpiresAt` and a 60s skew constant.
- `internal/teams/auth/skype_token_test.go`: Add boundary tests for `HasValidSkypeToken` (expired, near-expiry with skew, valid).

Plan
- Extend `AuthState` JSON struct with `SkypeToken` and `SkypeTokenExpiresAt` (unix seconds).
- Add a comment on OAuth/MSAL fields in `AuthState` marking them bootstrap-only/legacy; do not remove them from disk schema.
- Adjust `StateStore.Load()` to return state if either OAuth tokens are present or a `SkypeToken` is present; avoid discarding skype-only state.
- Implement `HasValidSkypeToken(now)` to return false when token is empty or expiring within the 60s skew window; keep the helper pure and time-parameterized.
- Unit tests for validity checks using fixed timestamps to avoid real time.

Phase 3 — Usage in requests + CLI wiring
Files
- `internal/teams/auth/client.go`: Add a helper to attach the skypetoken Authorization header to requests (e.g., `AttachSkypeToken(req, state)` or `AttachSkypeToken(req, token)`), leaving it a no-op when token missing.
- `internal/teams/auth/probe.go`: Use the new helper and include the skypetoken immediately when present; no retry logic.
- `cmd/teams-login/main.go`: Replace OAuth `EnsureValidToken` use with `HasValidSkypeToken` + `AcquireSkypeToken`, persist `SkypeToken` + `SkypeTokenExpiresAt`, and log acquisition success/expiry (OAuth fields remain bootstrap-only).
- `internal/teams/auth/probe_test.go`: Update tests to cover Authorization header usage when a skypetoken is present.

Plan
- Add a small helper on `Client` to set `Authorization: Bearer <skypetoken>` and use it in Teams API requests (starting with the probe).
- Update `ProbeTeamsEndpoint` to include Authorization when a valid skypetoken is present; if no skypetoken, send the request without Authorization.
- In `teams-login`, after loading auth state:
  - If `HasValidSkypeToken(now)` is false, log `INF Acquiring Teams skypetoken`, call `AcquireSkypeToken`, store token + expiry in state, and persist.
  - If valid, reuse without browser or OAuth; skip `EnsureValidToken`.
  - Log `INF Skype token acquired` with `expires_at` timestamp when acquisition succeeds.
- Update `internal/teams/auth/probe_test.go` to cover Authorization header usage when a skypetoken is present, and unauthenticated requests when not.
