Open Questions
- None.

Task checklist
Phase 1 – Bridge auth wiring
☑ Add `TeamsBridge.LoadTeamsAuth` plus logging, call it from `Start()`, and refresh the cached state on success.
☑ Adjust `!login` so it calls `LoadTeamsAuth`, replies with actionable guidance, caches the valid state, and triggers the consumer startup helper.
Phase 2 – Teams consumer reactor activation
☑ Introduce `StartTeamsConsumers(ctx, state)` that can run once per valid auth and only starts the read-only Teams consumer reactor (the current message-sync loop).
Phase 3 – Guardrails and observability
☑ Harden the new auth/consumer helpers so missing or expired auth objects are logged/reported, no panics occur, and tests cover both success and failure paths.

Phase 1 – Bridge-level Teams auth loader
Files & summaries
 - `main.go`: replace the inline auth load in `Start()` with a call to the new `LoadTeamsAuth`, cache the resulting state, log the success message, and delegate activation to the consumer helper instead of `ensureTeamsConsumersRunning`.
 - `commands.go`: rewrite `fnTeamsLogin` to call `LoadTeamsAuth`, interpret the sentinel errors into actionable replies, update the logging around auth loading, and reuse the cached state when posting success.
 - `teams_auth_state.go`: keep `resolveTeamsAuthPath`, expose the path of the loaded file, and add `func (br *TeamsBridge) LoadTeamsAuth(now time.Time) (*auth.AuthState, error)` so callers see the path, expiration, and descriptive sentinel errors (missing file, missing token, expired token, bad JSON). Build the helper for bridge lifecycle only; CLI/dev helpers continue using `loadTeamsConsumerAuth`.
 - `teams_auth_state_test.go`: expand the loader tests with a table that exercises success, missing file, expired token, and invalid JSON so we can assert the helper returns the right sentinel errors and passes back expiry metadata.

Details
- Have `LoadTeamsAuth` call `loadTeamsConsumerAuth(br.ConfigPath, br.Config.Bridge.TeamsAuthPath)` to reuse path resolution, but add logging around the returned `authPath` and unwrap the concrete `auth` errors with `errors.Is` so callers can differentiate missing vs expired tokens while surfacing expiry metadata via `ErrTeamsAuthExpiredToken`.
- When `Start()` calls `LoadTeamsAuth`, cache the resulting state via `br.setTeamsAuthState` only on success, log the `auth_path` + expiration timestamp, log “Teams auth OK” on success, and skip consumer startup (logging a warning) when the helper returns a descriptive error.
- `fnTeamsLogin` should now only reply after a single `LoadTeamsAuth` call, translating `ErrTeamsAuthMissingState`/`ErrTeamsAuthMissingToken` into “run `teams-login`” guidance, the expired token error into “rerun `teams-login` because the token is stale”, and the success case into “Teams auth OK” plus a call to `StartTeamsConsumers` (reusing the cached state). Limit this handling to bridge runtime commands; CLI helpers retain their existing load path.

Unit tests
 - Add table-driven tests in `teams_auth_state_test.go` that cover resolving the path and exercising the new `LoadTeamsAuth` helper (missing config path, missing file, expired token, valid token) to ensure the sentinel errors carry expiry metadata and the path is passed back for logging.
 - Add a new command test (e.g., `commands_test.go`) that injects a fake bridge/event pair, fakes `LoadTeamsAuth` outcomes (missing auth, expired token with structured expiry, valid auth), and asserts the replies produced by `fnTeamsLogin` match the guidance text and that the bridge cache is refreshed on success.

Phase 2 – Teams consumer reactor activation
Files & summaries
- `teams_lifecycle.go`: replace `ensureTeamsConsumersRunning` with `StartTeamsConsumers`, keeping the `teamsRunLock`/`teamsRunning` guard but ensuring it only spins up the message-sync loop described as the “Teams consumer reactor.”
- `teams_consumer_rooms.go`: refactor the existing message-sync setup so `StartTeamsConsumers` can share the initialization (context creation, logger, `TeamsConsumerIngestor`) without duplicating logic; expose a helper that accepts `context.Context`, logger, and the cached state and returns any error so the lifecycle helper can log/report it.
- `teams_lifecycle_test.go`: adjust the tests to cover the new helper (guarding nil auth, idempotency, and success paths) and to assert `teamsRunning` is only set when the reactor start completes.

Details
- Build `StartTeamsConsumers(ctx context.Context, state *auth.AuthState)` to check for a nil state, validate it, log “Starting Teams consumer reactor”, log `Teams auth missing` / `expired token` when skipping, re-use the message-sync bootstrapping logic (context background + `runTeamsConsumerMessageSync`) so the HTTP polling loop fires, and set `teamsRunning = true` when the goroutine is launched.
- Guard against double starts by returning early (and logging “Teams consumers already running”) when `teamsRunning` is already true, and return an error when the state validation fails so callers can bubble it up instead of panicking.
- Ensure the consumer goroutine uses `br.WaitWebsocketConnected()` as before plus the same `runTeamsConsumerMessageSync(context.Background(), log, state)` logic so we get the low-blast-radius read-only behavior the spec demands; capture the context cancellation/failure path for later phases.
- Keep the existing `TeamsConsumerReactor` field and other send-related hooks untouched for now, since only the read-only reactor is in scope.

Unit tests
- Add tests in `teams_lifecycle_test.go` for `StartTeamsConsumers` that stub out `runTeamsConsumerMessageSync` (via a package-level variable or helper) so we can simulate success/failure without firing the real poll loop; verify that nil auth returns a descriptive error and that the `teamsRunning` flag never flips in that case.
- Test that calling `StartTeamsConsumers` twice leaves `teamsRunning` true but only starts the reactor once, and that the second call returns early with a log indicating it was already running.
- Add a test that ensures when `runTeamsConsumerMessageSync` returns an error immediately, `StartTeamsConsumers` surfaces that error and keeps `teamsRunning` false so the command can report the failure.

Phase 3 – Guardrails and safety
Files & summaries
- `teams_auth_state.go`: make sure every error returned by `LoadTeamsAuth` indicates the failure mode (missing path/file vs expired) and enhance logging around the expiry timestamps so operators see why auth is unusable.
- `teams_lifecycle.go`: add early returns and logs (e.g., “Teams consumers skipped: auth missing”) whenever `StartTeamsConsumers` is asked to run without a valid cached state; ensure all helpers now return errors instead of panics on nil `br`/`Config`/`DB`.
- `teams_lifecycle_test.go` and `teams_auth_state_test.go`: add guardrail-focused tests that assert the new logs/errors when auth is missing/expired and when consumers would have started with nil auth.

Details
- Have `LoadTeamsAuth` log both the path and expiry timestamp whenever it successfully loads a state, and log the specific sentinel error (missing file vs expired token) before returning so the caller has context for the warning message it emits.
- Update `Start()` and the login command to interpret the error type (e.g., `errors.Is(err, ErrTeamsAuthExpiredToken)`) so they log/request the precise remediation (“rerun teams-login”) and do not attempt to call `StartTeamsConsumers` when the auth is unusable.
- Ensure all consumer startup helpers `validateTeamsAuthState`; if validation fails, propagate the error back to the caller so both `Start()` and `!login` can reply/log the reason rather than panicking (this will satisfy the “no panic” requirement in Phase 3).

Unit tests
- Add a guardrail test in `teams_lifecycle_test.go` that ensures `StartTeamsConsumers` returns early (with an error message) when the cached state expires while the bridge is running, and that the log contains “auth expired” so operators can see why consumers were skipped.
- In `teams_auth_state_test.go`, assert that `LoadTeamsAuth` surfaces the expiry timestamp in the error message and does not wrap it in an opaque `json.Unmarshal` failure when the token is expired but the JSON is valid.
- Add a test covering the logging/return path when `LoadTeamsAuth` is called without a config path so the resulting `ErrTeamsAuthMissingCfgPath` bubbles up in both logging and the login reply.
