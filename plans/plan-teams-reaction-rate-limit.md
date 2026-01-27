## Open Questions
- None.

## Task Checklist
Phase 1
☑ Extend executor request metadata to support reaction log labeling and `teams_message_id` (and optionally `emotion_key`) without changing retry behavior.
Phase 2
☑ Route reaction add/remove through `TeamsRequestExecutor` with retry classification and request metadata plumbing.
Phase 3
☑ Add reaction retry tests for 429/500/400/cancel flows using the executor, including a metadata/payload stability check for `emotion_key`.

## Phase 1: Executor Metadata + Log Labeling
Files
- `internal/teams/client/executor.go`: extend request metadata for reactions and apply it to executor logs.
Changes
- Extend `RequestMeta` with fields for reaction logging (`TeamsMessageID` and optional `EmotionKey`) plus a small label/operation string to allow reaction-specific log messages while keeping executor ownership of retry logs.
- Update `logWithMeta` to attach `teams_message_id` (and `emotion_key` if set) alongside existing `thread_id`/`client_message_id` fields; attach both IDs when present (no preference/overwriting).
- Teach logging helpers to use a message prefix derived from metadata (defaulting to the existing `teams send ...` strings when no operation label is set), so reactions can log `teams reaction retry/backoff/failed/succeeded` without new logging paths.
Tests
- None (existing executor tests cover retry mechanics; log label changes are string-only and can be validated in reaction tests if desired).

## Phase 2: Reaction Executor Wiring + Classification
Files
- `internal/teams/client/reactions.go`: wire reactions through executor and add response classifier.
Changes
- Add `classifyTeamsReactionResponse(*http.Response) error` mirroring send semantics: 2xx → nil, 429/5xx → `RetryableError` (respecting `Retry-After`), other 4xx → `NewReactionError`/`ReactionError`.
- Add `NewReactionError(resp *http.Response) ReactionError` to preserve reaction-specific error typing while centralizing body-snippet extraction.
- Update `sendReaction` to set `req.GetBody` for retryable requests, and route `Executor.Do` with request metadata: `thread_id`, `teams_message_id` (parent message id), and `emotion_key` when available.
- Mirror send message error handling: if `Executor.Do` returns `(resp, err)`, close the body when non-nil, return status + error; on success, close response body and return status.
- Ensure `Client.Executor` is initialized and configured similarly to `SendMessageWithID` (fallback defaults, inherit `HTTP` client and logger).
Tests
- None (retry behavior is covered in the next phase with reactions tests).

## Phase 3: Reaction Retry Tests
Files
- `internal/teams/client/reactions_test.go`: add executor-driven retry tests for reactions.
Changes
- Add tests that install a custom `TeamsRequestExecutor` on the client (with deterministic `sleep`/`jitter`) to validate retry behavior without delays.
- Cover required scenarios:
  - 429 → retry → success (assert two attempts and final success).
  - 429 → retry → exhausted (assert attempts == `MaxRetries+1` and `RetryableError`).
  - 500 → retry (assert retry occurs and succeeds or exhausts as configured).
  - 400 → no retry (assert single attempt and `ReactionError`).
  - Context cancellation stops retries (cancel in `sleep` and assert `context.Canceled` with only one attempt).
- Add a metadata/payload stability assertion in one retry test: `emotion_key` remains unchanged across attempts (same request body, same metadata fields in executor logs if captured).
Tests
- Use per-test `httptest.Server` counters to assert attempt counts and validate request payloads remain stable across retries.
