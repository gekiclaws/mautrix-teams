Open Questions
- None.

Task Checklist
Phase 1: Shared Graph client primitives
☐ Extract token acquisition + env loading into a reusable Graph client used by auth test and poller.
☐ Update unit tests for `.env` loading to cover the new helper surface.

Phase 2: Teams poller package
☐ Add `teams/poll` with `Poller`, `PolledMessage`, and a single-run polling method that logs normalized messages and advances the in-memory cursor.
☐ Add unit tests for normalization and cursor advancement.

Phase 3: Poll-test entrypoint
☐ Add `--teams-poll-test` / `GO_TEAMS_POLL_TEST=1` gating that runs the poller once and exits without starting the bridge.

Phase 1: Shared Graph client primitives
Files
- `teams/graph.go`: Introduce `GraphCredentials`, env loading, token acquisition, and a JSON GET helper on a `GraphClient`.
- `teams/auth/auth.go`: Update auth test to use `GraphClient` instead of direct HTTP helpers.
- `teams/auth/auth_test.go` or `teams/graph_test.go`: Adjust `.env` loader tests to match new helpers.
Details
- Move `loadDotEnv` into a shared helper and add `LoadGraphCredentialsFromEnv(path)` that validates required env vars.
- Implement `NewGraphClient(ctx, creds)` that performs the client-credentials flow and stores the token for subsequent requests.
- Provide a minimal `GetUser(ctx, userID)` method so `RunGraphAuthTest` remains a thin wrapper.
Tests
- Extend `.env` loader tests to verify missing env vars are reported clearly and pre-set env vars are not overridden.

Phase 2: Teams poller package
Files
- `teams/poll/poll.go`: Implement `PolledMessage`, `Poller`, and `RunOnce` (or equivalent) for chat + message polling.
- `teams/graph_messages.go` (or `teams/graph.go`): Add `ListChats` and `ListChatMessages` Graph helpers with `$top` + `$orderby` + optional `$filter`.
- `teams/poll/poll_test.go`: Add unit tests for normalization + cursor behavior.
Details
- Graph request flow: `GET /users/{user-id}/chats` then `GET /chats/{chat-id}/messages`.
- Use `AZURE_GRAPH_USER_ID` for polling; fail fast with a clear error if unset.
- Use `$top` and `$orderby=createdDateTime`; do not use `$filter=createdDateTime gt` yet.
- Cursor is `map[chatID]lastMessageID` only; skip older messages by ID and update cursor only after logging.
- Normalize to `PolledMessage` fields, log at INFO with `chat_id`, `message_id`, `sender`, `created_at`, and a 120-char truncated body.
- Prefer `contentType == "text"`; otherwise do best-effort tag stripping before truncation (no full HTML parsing).
- Log rate-limit headers if present, but do not act on them.
Tests
- Normalize: feed a sample Graph message struct and assert `PolledMessage` fields and truncation behavior.
- Cursor: feed ordered messages and verify only messages after the last-seen ID are emitted and that the cursor advances to the latest logged message.

Phase 3: Poll-test entrypoint
Files
- `main.go`: Add a new gating function mirroring auth-test to run the poller once and exit.
Details
- Detect `--teams-poll-test` and `GO_TEAMS_POLL_TEST=1`, instantiate `GraphClient` + `Poller`, run once, log errors, and `os.Exit` before bridge startup.
- Single poll run only (no loops, timers, retries, or backoff).
Tests
- None (flag/env gating is trivial and covered by unit tests in earlier phases).
