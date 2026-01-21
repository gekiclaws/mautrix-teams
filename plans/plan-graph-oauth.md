Task Checklist
Phase 1: Teams auth test helper
☑ Add a `teams/auth` helper that loads `.env`, reads Azure env vars, performs client-credentials auth, calls Graph, and logs a redacted JSON payload.
☑ Add unit tests for `.env` loading only.

Phase 2: Manual entry point + env samples
☑ Add explicit opt-in trigger (`--teams-auth-test` and `GO_TEAMS_AUTH_TEST=1`) to run the auth test without touching the bridge lifecycle otherwise.
☑ Add `.env.sample` and update `.gitignore` to exclude `.env`.

Phase 1: Teams auth test helper
Files
- `teams/auth/auth.go`: Add env loading, client-credentials token acquisition, Graph user request, and redacted logging using zerolog.
- `teams/auth/auth_test.go`: Add tests for `.env` parsing.
Details
- Implement `RunGraphAuthTest(ctx, log)` that loads `.env` from repo root if present, reads `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`, and `AZURE_GRAPH_USER_ID`, and errors clearly on missing required values.
- Use raw HTTP with `application/x-www-form-urlencoded` to `https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token` and `scope=https://graph.microsoft.com/.default`.
- Parse token response into a typed struct, never log secrets, and fail if access token is missing.
- Call `GET https://graph.microsoft.com/v1.0/users/{id}` with `Authorization: Bearer <token>`.
- Marshal a redacted response payload (e.g., `{ "id": "...", "displayName": "..." }`) and log via zerolog as JSON alongside HTTP status and endpoint.
- Keep helper functions unexported unless needed by tests.
Tests
- `TestLoadDotEnvLoadsExpectedKeys`: writes a temp `.env`, loads it, and asserts env vars are set without clobbering existing values.

Phase 2: Manual entry point + env samples
Files
- `main.go`: Add explicit opt-in trigger that runs the auth test (and only then) using a zerolog logger consistent with the bridge style.
- `.gitignore`: Add `.env`.
- `.env.sample`: Add placeholder Azure env vars with non-secret values.
Details
-- Detect `--teams-auth-test` in `os.Args` and/or `GO_TEAMS_AUTH_TEST=1`; when set, run the auth test helper, log errors clearly, and exit immediately after completion.
- Ensure the bridge lifecycle runs untouched on normal startup when neither trigger is set.
Tests
- None (flag/env gating is trivial and exercised via unit tests in Phase 1).
