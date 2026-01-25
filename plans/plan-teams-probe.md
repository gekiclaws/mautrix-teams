## Open Questions
- None.

## Task Checklist
Phase 1
☑ Add a reusable Teams probe helper that uses the existing HTTP client and cookie jar.
☑ Add unit tests for probe response handling (status/header/body truncation).
Phase 2
☑ Wire the probe into `cmd/teams-login` with clear logging and no OAuth token usage.

## Phase 1: Authenticated Probe Helper
Files
- internal/teams/auth/probe.go: implement a small helper to perform one GET request using `Client.HTTP` with cookies, no OAuth headers.
- internal/teams/auth/probe_test.go: unit tests using `httptest` for status/body truncation and auth-related header selection.
Changes
- Add `ProbeResult` struct capturing `StatusCode`, `BodySnippet`, and `AuthHeaders` (filtered map).
- Implement `Client.ProbeTeamsEndpoint(ctx, url)` that:
  - Builds a GET request without any Authorization header.
  - Uses `Client.HTTP` (cookie jar already attached).
  - Reads and logs the first 1–2KB of the body (truncate at 2048 bytes).
  - Extracts auth-related headers (e.g., `set-cookie`, `www-authenticate`, `x-ms-*`, `x-azure-*`).
  - Returns the `ProbeResult` for caller logging.
Tests
- `ProbeTeamsEndpoint` returns truncated body and status from an `httptest` server.
- Auth-related headers are filtered and surfaced correctly.

## Phase 2: CLI Wiring and Logging
Files
- cmd/teams-login/main.go: call the probe after auth state is loaded or after login succeeds.
Changes
- Standardize on `https://teams.live.com/api/mt/Me` with no CLI override.
- After confirming auth state exists (no login required), run the probe once and log:
  - Status code
  - Body snippet (first 1–2KB)
  - Auth-related headers
  - A short interpretation line: `200/JSON -> cookies OK`, `401/403 -> expected (missing Teams-native token)`.
- On first login, run the probe after saving cookies/state.
- Do not add retries or recurring checks.
Tests
- None beyond Phase 1 (CLI wiring is thin; avoid integration tests).
