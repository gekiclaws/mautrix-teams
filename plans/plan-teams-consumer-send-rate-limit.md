## Open Questions
- None.

## Task Checklist
Phase 1
☑ Add a reusable Teams request executor with bounded retry/backoff, including retry classification, logging, and unit tests for retry behavior.
Phase 2
☑ Wire SendMessageWithID to the executor and add send-specific classification tests.

## Phase 1: Teams Request Executor
Files
- `internal/teams/client/executor.go`: introduce the executor type, retry/backoff logic, and helper types/errors.
- `internal/teams/client/executor_test.go`: add unit tests for retry rules, backoff, and context cancellation.
Changes
- Add `TeamsRequestExecutor` with fields `HTTP *http.Client`, `Log zerolog.Logger`, `MaxRetries int`, `BaseBackoff time.Duration`, `MaxBackoff time.Duration`, and a small internal `sleep func(ctx context.Context, d time.Duration) error` to avoid real delays in tests.
- Implement `Do(ctx, req, classify)` that:
  - Clones/rebuilds the request for each attempt (using `req.GetBody` when present) so bodies can be resent safely.
  - Calls `HTTP.Do` and passes the response to `classify`.
  - Retries only when the classifier returns a retryable error (see below) or when `http.Client.Do` returns a narrow retryable network error (see below), up to `MaxRetries` total retries; stop immediately on `ctx.Done()`.
  - Uses `Retry-After` for 429 when provided; otherwise exponential backoff with jitter and an upper bound (`MaxBackoff`).
  - Ensures response bodies are always drained/closed before retrying or returning.
- Define retry-related errors/types (package `client`) such as:
  - `type RetryableError struct { Status int; RetryAfter time.Duration; Cause error }` (or similar), with a helper `IsRetryable(err error) bool`.
  - `var ErrPermanent = errors.New("teams request permanent failure")` if you want a sentinel for classifier convenience.
- Define retryable network error handling for `http.Client.Do` errors:
  - Retry only when `errors.As(err, &netErr)` and `(netErr.Timeout() || netErr.Temporary())`.
  - Treat `context.DeadlineExceeded` as retryable only when it originates inside `Do` (not `ctx.Done()`).
  - Never retry `context.Canceled`, TLS errors, or DNS resolution errors.
- Add context-scoped request metadata for logging (e.g. `type RequestMeta struct{ ThreadID, ClientMessageID string }` + `WithRequestMeta(ctx, meta)` + `requestMetaFromContext(ctx)`), used by the executor to attach `thread_id` and `client_message_id` to retry/backoff logs without hardcoding send-specific logic.
- Logging (INFO/WARN):
  - On retry decision: `teams send retry` with `attempt`, `status`, `retry_after` (if any), `thread_id`, `client_message_id`.
  - On backoff: `teams send backoff` with `duration`, `attempt`, `thread_id`, `client_message_id`.
  - On success after retries: `teams send succeeded` with `attempts`, `thread_id`, `client_message_id`.
  - On final failure: `teams send failed` with `attempts`, `error`, `thread_id`, `client_message_id`.
- Ensure send requests set `req.GetBody` explicitly when building the request body so retries can safely re-send the JSON payload.
- Explicitly drain response bodies before retrying (`io.Copy(io.Discard, resp.Body)` and `resp.Body.Close()`).
Tests
- 429 with `Retry-After: 2` leads to a retry and uses a 2s backoff duration (verify via injected sleep recorder).
- 429 without `Retry-After` uses exponential backoff (verify durations grow and are capped by `MaxBackoff`).
- 500 causes a retry up to `MaxRetries` and then returns the classifier error.
- 400 returns immediately with no retries.
- Context cancellation stops retries early (use a context canceled before the backoff sleep).
- Temporary/timeout network error from `http.Client.Do` retries; DNS or TLS errors do not retry.

## Phase 2: Send Message Wiring + Classification
Files
- `internal/teams/client/messages.go`: add the send response classifier and route SendMessageWithID through the executor.
- `internal/teams/client/messages_test.go`: add/adjust tests for retry classification and error behavior.
Changes
- Add `classifyTeamsSendResponse(resp *http.Response) error`:
  - 2xx → `nil`.
  - 429 → `RetryableError` (populate `RetryAfter` from header if present).
  - 5xx → `RetryableError`.
  - other 4xx → `SendMessageError` (preserve status/body snippet behavior).
- Extend `Client` to hold an `Executor *TeamsRequestExecutor`, and initialize it in `NewClient` with defaults (`MaxRetries: 4`, `BaseBackoff: 500ms`, `MaxBackoff: 10s`) using the same `HTTP` client and a `zerolog.Nop()` logger when `Client.Log` is nil. Ensure the executor uses the client’s logger when set so logs include request metadata.
- In `SendMessageWithID`, wrap the `ctx` with request metadata (`thread_id`, `client_message_id`) before calling `Executor.Do(...)`.
- Replace direct `c.HTTP.Do(req)` with `c.Executor.Do(ctx, req, classifyTeamsSendResponse)` and keep existing success/error semantics (non-2xx should still yield `SendMessageError` with status/snippet).
Tests
- Add a test that simulates 429 then 200 to ensure SendMessageWithID succeeds after retry and does not change the payload or client message id.
- Add a test that simulates 400 to ensure no retry is attempted and `SendMessageError` is returned (reuse existing error expectations).
- If logger wiring affects tests, add a tiny test to ensure executor uses the client’s logger (optional, only if there’s existing logging verification).
