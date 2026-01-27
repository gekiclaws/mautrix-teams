Open Questions
- Should the idle growth be linear (`+base`) or multiplicative (e.g. `*1.5`)? This plan assumes linear growth for simpler, more predictable artifacts.
- For non-429 4xx during polling, should we treat it as “idle” (slow but steady) or as a capped failure backoff? This plan treats it as capped-idle (30s) and resets failures.

Task Checklist
Phase 1
☑ Refactor polling outcomes to return typed results and propagate Teams fetch errors.
☑ Update polling-related unit tests to assert the new result/error behavior.

Phase 2
☑ Add `PollBackoff` as a value-oriented helper and unit test its delay rules.
☑ Add a small classification helper that maps poll outcomes/errors to backoff updates.

Phase 3
☑ Rework the inbound polling loop to use per-thread backoff state and per-thread scheduling (no per-thread sleeps).
☑ Add structured backoff/sleep logs at the polling loop level without per-message noise.

Phase 1: Surface Poll Outcomes and Retry Signals
Files
- `internal/teams/client/messages.go`: classify polling responses so 429/5xx return `RetryableError` (with `RetryAfter` when present) instead of an opaque `MessagesError`.
- `internal/teams/client/messages_test.go`: add focused tests for 429 (`Retry-After` respected) and 5xx retryable classification, and keep existing non-retryable 4xx coverage.
- `internal/bridge/messages.go`: return a typed ingest result that includes how many messages were ingested and whether the cursor advanced.
- `internal/bridge/sync.go`: return a typed sync result plus error; do not swallow Teams fetch errors.
- `internal/bridge/sync_test.go`: update existing tests to assert the new result shape, and add one new test that ensures Teams fetch errors propagate.
- `internal/bridge/teams_consumer_ingest.go`: change `PollOnce` to return the sync result (and error) so the poll loop can classify outcomes without re-deriving state.

Changes
- In `internal/teams/client/messages.go`, update `fetchJSON`’s non-2xx handling:
- For `429`, return `RetryableError{Status: 429, RetryAfter: parseRetryAfter(...)}`.
- For `5xx`, return `RetryableError{Status: resp.StatusCode}`.
- For other `4xx`, keep returning `MessagesError` with a bounded body snippet.
- In `internal/bridge/messages.go`, introduce a small value type:

```go
type IngestResult struct {
	MessagesIngested int
	LastSequenceID   string
	Advanced         bool
}
```

- Change `MessageIngestor.IngestThread(...)` to return `(IngestResult, error)`:
- Increment `MessagesIngested` only when a Matrix message send succeeds.
- Preserve existing cursor semantics: only advance/persist on successful ingestion.
- Keep Matrix send failures non-fatal to polling cadence (return zero-value result and `nil`) unless you want them to drive backoff.
- In `internal/bridge/sync.go`, introduce a value type and return it with errors:

```go
type SyncResult struct {
	MessagesIngested int
	LastSequenceID   string
	Advanced         bool
}
```

- `ThreadSyncer.SyncThread(...)` should:
- Call `IngestThread` and return its error directly (do not swallow Teams fetch failures).
- Persist and update in-memory cursor only when `Advanced` is true.
- Return a populated `SyncResult` on success (including idle polls where `MessagesIngested == 0`).
- Update `TeamsConsumerIngestor.PollOnce(...)` to return `(SyncResult, error)` and remain a thin delegator.

Tests
- `internal/teams/client/messages_test.go`:
- Add `TestListMessages429ReturnsRetryableWithRetryAfter` using an `httptest.Server` that responds with `429` and `Retry-After: 2`, then assert `errors.As(err, &RetryableError)` and `RetryAfter == 2*time.Second`.
- Add `TestListMessages5xxReturnsRetryable` with a `500` response and assert `errors.As(err, &RetryableError)` with the correct status.
- `internal/bridge/sync_test.go`:
- Update existing tests to assert the returned `SyncResult` (especially `MessagesIngested` and `Advanced`) rather than only side effects.
- Add `TestSyncThreadPropagatesListerError` with a fake lister that returns a sentinel error and assert `SyncThread` returns it.

Phase 2: Add PollBackoff and Outcome Classification
Files
- `internal/bridge/poll_backoff.go` (new): define `PollBackoff`, the bounded delay rules, and a small, pure classification helper that applies poll outcomes to a backoff instance.
- `internal/bridge/poll_backoff_test.go` (new): unit test the delay rules and classification behavior using simple, typed errors (no complex mocking).

Changes
- Add a small, value-oriented helper:

```go
type PollBackoff struct {
	Failures int
	Delay    time.Duration
}
```

- Use fixed, local constants (no config knobs in this ticket):
- `pollBaseDelay = 2 * time.Second`
- `pollIdleCap = 30 * time.Second`
- `pollFailureCap = 60 * time.Second`
- Implement delay transitions without sleeping:
- `OnSuccess()`: reset failures and set `Delay = pollBaseDelay`.
- `OnIdle()`: reset failures, and increase delay by `pollBaseDelay` up to `pollIdleCap`.
- `OnRetryAfter(d)`: if `d > 0`, set `Delay = d` and increment failures; otherwise fall back to `OnFailure()`.
- `OnFailure()`: increment failures and use exponential backoff from `pollBaseDelay`, capped at `pollFailureCap`.
- Add a pure classification helper that keeps concerns un-braided:

```go
type PollBackoffReason string

const (
	PollBackoffSuccess    PollBackoffReason = "success"
	PollBackoffIdle       PollBackoffReason = "idle"
	PollBackoffRetryAfter PollBackoffReason = "retry_after"
	PollBackoffFailure    PollBackoffReason = "failure"
	PollBackoffClient4xx  PollBackoffReason = "client_4xx"
)

func ApplyPollBackoff(b *PollBackoff, res SyncResult, err error) (delay time.Duration, reason PollBackoffReason)
```

- Classification rules in `ApplyPollBackoff`:
- `err == nil && res.MessagesIngested > 0` → `OnSuccess`.
- `err == nil && res.MessagesIngested == 0` → `OnIdle`.
- `RetryableError` with `Status == 429` and `RetryAfter > 0` → `OnRetryAfter`.
- Other `RetryableError` (including 5xx / retryable network error) → `OnFailure`.
- `MessagesError` with `4xx` (non-429) → set `Delay = pollIdleCap`, reset failures, reason `client_4xx`.
- Any other error → `OnFailure`.

Tests
- `internal/bridge/poll_backoff_test.go`:
- `TestPollBackoffSuccessResetsDelay`: start from a larger delay/failures, call `OnSuccess`, assert base delay and zero failures.
- `TestPollBackoffIdleIncreasesToCap`: call `OnIdle` repeatedly and assert linear growth capped at `pollIdleCap`.
- `TestPollBackoffRetryAfterOverrides`: set a large failure delay, call `OnRetryAfter(10*time.Second)`, assert delay equals 10s.
- `TestPollBackoffFailureExponentialCapped`: call `OnFailure` repeatedly and assert exponential growth capped at `pollFailureCap`.
- `TestApplyPollBackoffClassification`:
- Success with messages → `success`.
- Success with no messages → `idle`.
- `RetryableError{Status: 429, RetryAfter: 7s}` → `retry_after` with 7s delay.
- `RetryableError{Status: 503}` → `failure`.
- `MessagesError{Status: 403}` → `client_4xx` with `pollIdleCap`.

Phase 3: Per-Thread Scheduling in the Poll Loop
Files
- `teams_consumer_rooms.go`: replace the fixed-interval loop with per-thread backoff state and per-thread scheduling that computes a single global sleep based on the next due thread.

Changes
- Keep polling single-threaded, but separate per-thread state from loop timing:

```go
type threadPollState struct {
	Backoff    teamsbridge.PollBackoff
	NextPollAt time.Time
}
```

- Initialize one state per thread with `NextPollAt = time.Now().UTC()` and `Backoff.Delay = pollBaseDelay`.
- Replace the fixed “tick then sleep” loop with:
- For each thread, skip polling when `now.Before(state.NextPollAt)`.
- When a thread is due, call `consumerIngestor.PollOnce(ctx, thread)` to get `(SyncResult, error)`.
- Apply backoff via `teamsbridge.ApplyPollBackoff(&state.Backoff, res, err)`.
- Set `state.NextPollAt = now.Add(state.Backoff.Delay)`.
- Compute the earliest `NextPollAt` across all states and sleep until that time (with a small floor such as 200ms to avoid busy looping).
- Add the required structured logs (loop level only, no per-message logs):
- `teams poll backoff updated` at INFO/WARN with `thread_id`, `reason`, `delay`, and (when available) `status` / `retry_after`.
- `teams poll sleeping` at INFO with the computed global sleep duration and the next due timestamp (or next due thread id if cheap to include).
- Error handling:
- Teams polling errors must not kill the loop.
- When errors occur, still update that thread’s backoff and continue processing other due threads.

Tests
- Update `internal/bridge/teams_consumer_ingest.go` tests if present, otherwise rely on Phase 1 + Phase 2 unit tests for outcome correctness and backoff behavior.
- Keep loop timing logic simple and value-driven to avoid fragile time-based tests.
