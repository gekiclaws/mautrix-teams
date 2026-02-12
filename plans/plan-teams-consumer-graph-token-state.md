Open Questions
- None.

Task Checklist
Phase 1 — Persisted auth state + Graph token extraction
☑ Extend Teams consumer persisted auth metadata with `graph_access_token` and `graph_expires_at` unix timestamp fields.
☑ Update delegated OAuth scope set to include `Files.ReadWrite` and `offline_access` (no `Files.ReadWrite.All`).
☑ Extract a Graph-scoped access token (and absolute expiry) from MSAL localStorage payloads during login metadata extraction.
☑ Add unit tests for Graph token selection precedence and expiry parsing.

Phase 2 — Graph token accessors + login/refresh persistence
☑ Add `GraphTokenValid(now)` and `GetGraphAccessToken()` helpers on the persisted Teams consumer auth state with explicit missing/expired errors.
☑ Persist Graph token + expiry on both initial login extraction and refresh-token exchanges used by `ensureValidSkypeToken`.
☑ Add unit tests for accessor error paths and successful retrieval.

Phase 3 — Startup/login logging + no-regression coverage
☑ Add debug/info logs that explicitly report Graph token presence and expiry at login success and connect/startup validation.
☑ Add/extend connector tests to verify metadata JSON round-trip includes Graph fields and existing skypetoken flows remain unaffected.

Phase 1 — Persisted auth state + Graph token extraction
Files
- `pkg/teamsid/dbmeta.go`: Extend `UserLoginMetadata` with `GraphAccessToken string` and `GraphExpiresAt int64` JSON fields; keep additive schema style.
- `internal/teams/auth/state.go`: Add matching Graph fields to `AuthState` so localStorage extraction and token exchanges carry Graph token data consistently.
- `internal/teams/auth/client.go`: Extend default delegated scopes to include `Files.ReadWrite` and retain `offline_access` (exclude `Files.ReadWrite.All`).
- `internal/teams/auth/msal.go`: Add Graph token selection from MSAL `accessToken` entries and populate `AuthState.GraphAccessToken` + `AuthState.GraphExpiresAt`.
- `internal/teams/auth/msal_test.go`: Add Graph-focused selection tests (prefer Graph-token target over MBI/openid, choose latest valid expiry).
- `internal/teams/auth/flow_test.go`: Extend extraction assertions to validate Graph token + absolute expiry propagation from localStorage payloads.

Plan
- Keep `UserLoginMetadata` as the persisted Teams consumer auth state and add Graph fields with `omitempty` JSON tags.
- In MSAL parsing, match Graph tokens by explicit Graph resource target (`graph.microsoft.com`) in token target/resource metadata, not by generic substring heuristics.
- Preserve current skypetoken bootstrap behavior: MBI token selection logic remains unchanged; Graph extraction is additive.
- Update delegated OAuth scopes to include `Files.ReadWrite` plus `offline_access`, and do not include `Files.ReadWrite.All`.
- Normalize expiry to absolute unix seconds at extraction time (from `expiresOn`), never storing relative `expires_in` in persisted metadata.

Tests
- `internal/teams/auth/msal_test.go`:
  - Add a case with mixed access tokens (`openid`, `MBI_SSL`, Graph) and assert Graph token is extracted into new Graph fields without changing existing MBI expectations.
  - Add a case where Graph token is absent and assert Graph fields remain empty/zero while extraction still succeeds.
- `internal/teams/auth/flow_test.go`:
  - Extend current extraction success test to assert Graph token + expiry values.

Phase 2 — Graph token accessors + login/refresh persistence
Files
- `pkg/teamsid/dbmeta.go`: Add helper methods `GraphTokenValid(now time.Time) bool` and `GetGraphAccessToken() (string, error)` plus explicit sentinel errors.
- `pkg/connector/storage_extractor.go`: Persist Graph token + expiry from extracted state into returned `UserLoginMetadata`.
- `pkg/connector/handleteams.go`: When refreshing via `RefreshAccessToken`, update/persist Graph token + expiry from returned OAuth payload before saving login metadata.
- `internal/teams/auth/token.go`: Ensure token endpoint responses populate Graph fields in `AuthState` (`access_token` + `expires_in` converted to absolute unix expiry).
- `internal/teams/auth/client_test.go`: Extend token endpoint tests to assert Graph field population and expiry conversion.
- `pkg/teamsid/dbmeta_test.go`: Add unit tests for `GraphTokenValid`/`GetGraphAccessToken` covering valid, missing, and expired states.

Plan
- Implement Graph accessor helpers on `UserLoginMetadata` (the persisted Teams consumer auth state) so callers can fetch a Graph bearer token with explicit errors:
  - missing token error
  - expired token error (includes expiry timestamp in message/context)
- `GraphTokenValid(now)` should use the same 60-second skew as skypetoken validity checks.
- Update login extraction path to store Graph fields from parsed MSAL state in metadata alongside existing skypetoken fields.
- Update refresh path (`ensureValidSkypeToken`) to refresh Graph fields only when OAuth response contains a Graph token and expiry; do not clear an existing valid stored Graph token when refresh payload omits Graph fields.
- Keep refresh behavior non-invasive: no proactive Graph refresh loop, only persistence of whatever token/expiry arrives in the existing refresh flow.

Tests
- `pkg/teamsid/dbmeta_test.go`:
  - `TestGraphTokenValid` boundary cases (valid future expiry, zero expiry, expired, and near-expiry invalid with 60s skew).
  - `TestGetGraphAccessToken` returns token when valid, explicit error on missing token, explicit error on expired token.
- `internal/teams/auth/client_test.go`:
  - Assert `tokenRequest` maps `access_token` and `expires_in` into Graph fields (absolute unix).
  - Preserve existing assertions for refresh/id token behavior.

Phase 3 — Startup/login logging + no-regression coverage
Files
- `pkg/connector/login.go`: Log Graph token presence at login success and Graph expiry timestamp at debug level.
- `pkg/connector/client.go`: On connect/startup, log whether persisted Graph token is valid or expired using new accessor/validity helper.
- `pkg/connector/storage_extractor_test.go`: Add focused tests for `ExtractTeamsLoginMetadataFromLocalStorage` ensuring Graph fields are persisted and skypetoken acquisition path still succeeds.

Plan
- Add login success logging that explicitly reports Graph token presence (`true/false`) and emits `graph_expires_at` at debug level without logging token contents.
- Add startup/connect logging after metadata load that reports Graph token status (`missing`, `valid`, `expired`) with timestamp context.
- Keep all message-send code paths unchanged in this ticket; verify plan-level no-regression by covering metadata extraction and existing token bootstrap behavior in unit tests.

Tests
- `pkg/connector/storage_extractor_test.go`:
  - Success case: localStorage includes Graph token and skypetoken bootstrap succeeds; returned metadata has both skype and Graph fields.
  - Regression case: no Graph token in storage still returns metadata and skypetoken fields as before.
