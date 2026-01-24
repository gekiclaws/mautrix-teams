## Open Questions
- None.

## Task Checklist
Phase 1
☑ Add auth state + persistence types (tokens + cookies) and PKCE helpers under `internal/teams/auth/`.
☑ Implement cookie jar persistence utilities using `net/http/cookiejar` + JSON serialization.
☑ Add unit tests for PKCE and cookie persistence.
Phase 2
☑ Implement OAuth URL builder, token exchange, refresh logic, and token validity checks.
☑ Add unit tests for authorize URL construction and token exchange/refresh via `httptest`.
Phase 3
☑ Add `cmd/teams-login` CLI wired to existing config/logging and state paths.
☑ Implement local-listener flow with manual fallback and persist auth state on success.

## Phase 1: Auth State, PKCE, Cookie Persistence
Files
- internal/teams/auth/state.go: add token structs, JSON marshal/unmarshal with `expires_at` unix timestamp, and atomic disk save/load helpers.
- internal/teams/auth/pkce.go: generate `code_verifier` + derive `code_challenge` (base64url SHA-256) helpers.
- internal/teams/auth/cookies.go: create/load/save cookie jar using `cookiejar.New(publicsuffix.List)` and JSON serialization with an allowlist for `login.live.com`, `login.microsoftonline.com`, `teams.live.com`, `*.skype.com`, and `*.teams.live.com`.
- internal/teams/auth/cookies_test.go: round-trip jar persistence tests with known domains.
- internal/teams/auth/pkce_test.go: verify code_challenge derivation + verifier length/base64url format.
Changes
- Define `TokenSet` and `AuthState` with `AccessToken`, `RefreshToken`, `ExpiresAtUnix`, and `IDToken` fields; keep verifier only in memory.
- Add `Store` helper that writes `auth.json` atomically (temp file + rename) and loads it on startup.
- Implement cookie persistence by serializing cookies for an explicit domain allowlist; load by calling `jar.SetCookies` for each domain URL.
Tests
- PKCE tests that validate deterministic `code_challenge` outputs for fixed `code_verifier` inputs.
- Cookie jar tests that store a cookie for `https://login.live.com/`, save to JSON, reload, and assert the cookie is returned for that URL.

## Phase 2: OAuth Client Core (Authorize, Exchange, Refresh)
Files
- internal/teams/auth/client.go: add `Client` with HTTP client, endpoints, scopes, and auth flow methods.
- internal/teams/auth/token.go: implement exchange/refresh helpers and `EnsureValidToken` with expiry leeway.
- internal/teams/auth/client_test.go: test authorize URL and token exchange/refresh via `httptest` server.
Changes
- Build authorize URL using fixed `client_id`, `redirect_uri`, `scope`, `response_type=code`, `response_mode=fragment`, `code_challenge`, `code_challenge_method=S256`, and a random `state` (scope space-separated but URL-encoded as `+` or `%20`).
- Implement `ExchangeCode` and `Refresh` methods posting `application/x-www-form-urlencoded` to the consumer token endpoint, parsing `access_token`, `refresh_token`, `expires_in`, and `id_token`.
- Add `EnsureValidToken(ctx)` that refreshes when `time.Until(expires_at) < skew` and atomically replaces persisted tokens.
Tests
- Authorize URL test asserts required query parameters and accepts scope encoding as `+` or `%20` separators.
- Token exchange/refresh tests validate form fields and stored expiry timestamp using a local `httptest` token endpoint.

## Phase 3: CLI Bootstrapper + Wiring
Files
- cmd/teams-login/main.go: new CLI entrypoint with config/logging init, login flow, and persistence.
- config/config.go: if needed, expose helper(s) to parse config and compile logging consistently with main.
- internal/teams/auth/flow.go: local-listener helper page + manual fallback orchestration.
Changes
- Parse `-c/--config` using `mauflag` for consistency, load `config.Config` from the same config path, and compile logging via `config.Config.Logging`.
- Derive state directory from the config file path (use `filepath.Dir(configPath)`), then set `auth.json` and `cookies.json` in that directory.
- Implement local-listener flow: start a loopback server that serves a helper page prompting for the redirected URL, use JS to extract `#code=...` and POST it back, then shut down; if listener fails or `--manual` is set, prompt for pasted URL and parse `#code=...`.
- Persist updated tokens + cookies on success; on subsequent runs, load state, refresh tokens if needed, and avoid re-login.
Tests
- Add unit tests for URL fragment parsing from manual input and for the local listener handler (if it uses a simple HTTP handler function).
