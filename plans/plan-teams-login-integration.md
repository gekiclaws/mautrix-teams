Resolved questions
1. Auth state can be overridden via the `MAUTRIX_TEAMS_AUTH_PATH` env var, then `bridge.teams_auth_path` in `config.yaml`, and otherwise falls back to `dirname(config.yaml)/auth.json`.
2. The Discord-style command set should be removed entirely; `RegisterCommands()` now only exposes Teams-related helpers.

Task checklist
Phase 1 – Remove Discord-specific legacy
☑ Rename the bridge/user abstractions to Teams-centric names, drop Discord-only command handlers/imports, and prune `RegisterCommands()` to its Teams subset.
☑ Clean up any user/session metadata and provisioning references that are purely Discord constructs.
Phase 2 – Implement Teams login command
☑ Add a `cmdTeamsLogin` handler that loads/validates `auth.json`, reports actionable errors, and stores the successful auth state in the bridge.
☑ Exercise the new helper with targeted unit tests for missing file, expired token, and success cases.
Phase 3 – Wire Teams auth into lifecycle
☑ Load/ cache `auth.json` during `Start()`, gate the Teams consumer loops on a valid Skype token, and reuse the cached state from the `!login` handler (including restarting loops after login).
☑ Add unit tests that cover missing auth, valid auth, and log expectations around loop startup.
Phase 4 – Add guardrails and logging
☐ Surround auth loading/polling with descriptive logs/errors (including expiry handling) and ensure client init paths fail fast without panicking.
☐ Add unit tests that assert the new logging/guardrail helpers (e.g., `resolveAuthPath`, auth validation) behave correctly.

Phase 1 – Remove Discord-specific legacy
Files & summaries
- `main.go`: Rename the `DiscordBridge` type to `TeamsBridge`, strip out Discord-specific fields/commands, and prepare the struct for the new Teams-focused lifecycle helpers.
- `commands.go`: Eliminate `login-token`, `login-qr`, `disconnect`, `reconnect`, and guild-related command handlers, remove their imports (`discordgo`, `remoteauth`), and ensure only Teams-relevant handlers are registered.
- `user.go`/`provisioning.go`: Drop Discord-only session fields/ helpers (`DiscordID`, `Session`, `Connected()`, `wasDisconnected`, provisioning fields) or rename them to describe the Teams state that remains.
- `teams_consumer_rooms.go` + related helpers: Remove the ad-hoc `loadTeamsConsumerAuth` calls from each poller and prepare them to rely on a single cached auth state managed by the bridge.

Details
1. Rename the bridge and related constructors (e.g., `DiscordBridge`, `NewDiscordBridge`, any type assertions in commands/events) to `TeamsBridge` so the naming matches the Teams focus; update `main.go`/`commands.go` references accordingly.
2. Delete the Discord login helpers (`cmdLoginToken`, `cmdLoginQR` and their helpers like `decodeToken`, `sendQRCode`) plus the `remoteauth` dependency; `RegisterCommands` should now only add the Teams-related handlers (e.g., the new `cmdTeamsLogin`, remaining admin helpers, and any non-Discord utilities still needed).
3. Remove or repurpose Discord-specific user fields: collapse `DiscordID` into a more generic `RemoteID` if anything remains, eliminate `Session` plus the `Connected()` method and `wasDisconnected/wasLoggedOut` flags, and adjust provisioning/status helpers so they no longer refer to Discord events/states.
4. Update `teams_consumer_rooms.go` so it no longer loads auth state from disk itself; instead rely on a bridge helper (to be added in Phase 3) so the poller loops can be replayed when the login command succeeds.

Unit tests
- Write a small table test for `RegisterCommands()` (or a new helper it calls) that ensures only the expected command names remain in the processor, covering the removal of `login-token`/`login-qr`/`reconnect`.
- Add focused tests for the renamed user/session helpers (if they remain exposed) to ensure they panic or error fast when old Discord state is missing (e.g., verifying `Connected()` now reflects the Teams state). 

Phase 2 – Implement Teams login command
Files & summaries
- `commands.go`: Introduce `cmdTeamsLogin`/`fnTeamsLogin` plus any shared helper (e.g., `loadAuthStateFromConfigPath`) and wire the handler into `RegisterCommands()`; remove lingering Discord command scaffolding at the bottom of the file.
- `teams_consumer_rooms.go`: Add a shared helper (or method on `TeamsBridge`) that reads `auth.json` relative to `ConfigPath` and validates `SkypeToken`/expiry so the command and startup logic can reuse it.
- `main.go`: Add fields to cache the last-loaded auth state and a mutex to guard it so the command can update the bridge and the pollers can reuse it.

Details
1. Implement a helper that derives the `auth.json` path from `br.ConfigPath` and instantiates `auth.NewStateStore`; keep this logic testable (e.g., make the helper return the path instead of loading directly) so tests can inject a fake store.
2. Build `fnTeamsLogin` to:
   * Call the helper to load the stored state.
   * Reply with clear remediation steps when the file is missing, when the token is absent, or when `state.HasValidSkypeToken(now)` is false (include a pointer to run `teams-login`).
   * On success, persist the state into the bridge (see Phase 3) and call a new bridge helper to start/refresh the Teams pollers, then reply `Teams auth OK`.
   * Avoid any browser/OAuth logic; simply re-read the token produced by `teams-login`.
3. Ensure the command is registered with the prefix that matches existing UX (`!login` in bot DM) and document in `HelpMeta` that the user must run `teams-login` externally.

Unit tests
- Create table-driven tests for the helper that resolves the auth path and loads the state; simulate missing file, invalid JSON, valid expired token, and valid token scenarios.
- Test `fnTeamsLogin` by injecting stubbed `StateStore` behavior and a fake `commands.Event` (we already wrap these events) to assert the bot replies with the right error messages and that the bridge cache is updated when `HasValidSkypeToken` returns true.

Phase 3 – Wire Teams auth into lifecycle
Files & summaries
- `main.go`: Add fields to cache `*auth.AuthState`, a `teamsAuthLock`, and a boolean tracking whether the consumer loops are running; use these in `Start()` to gate poller launches.
- `teams_consumer_rooms.go`: Update the `start*` helpers to accept the cached auth token rather than reloading from disk and guard them from starting when no auth is present. Reuse a central method that initializes the `teamsbridge` clients once per valid state.
- `teams_consumer_rooms.go` (or a new file): Add a `setTeamsAuth(state *auth.AuthState)` helper on the bridge to store state and, if the bridge is already running, restart/refresh the pollers without duplicating goroutines.

Details
1. In `Start()`, call the `resolveAuthState` helper from Phase 2 and, if a valid state exists, store it and invoke the poller startup helper; otherwise log a warning and keep the bridge idle (do not attempt to start the consumers or panic).
2. Refactor the three `startTeamsConsumer*` goroutines so they rely on the cached state rather than re-reading `auth.json`, return early with logged errors when state is missing, and share a single initialization entrypoint (e.g., `runTeamsConsumers(ctx, state)` that sets up the `TeamsConsumerSender`, `TeamsConsumerReactor`, etc.) so they can be restarted when login command succeeds.
3. After `fnTeamsLogin` updates the cached state, call the same helper so the pollers begin running immediately; guard against double-starts by tracking a `teamsConsumersStarted` flag or comparable atomic.
4. Make sure the helper that starts pollers passes descriptive logs (component name, state present/absent) so administrators can tell when auth is missing or renewal is required.

Unit tests
- Add a test for `Start()` that injects a fake state loader returning `nil` and another returning a valid token, asserting the presence/absence of the log message and whether the consumer-start helper was invoked (mocked via an interface or a test spy).
- Write unit tests for the helper that launches consumer goroutines, verifying that it bails out with an error when `state` is nil and initializes the Teams clients when a valid token is provided (e.g., by passing in a fake `teamsbridge` clients factory).

Phase 4 – Add guardrails and logging
Files & summaries
- `main.go` / `teams_consumer_rooms.go`: Inject logging around auth load/expiry decisions, Teams client initialization, and consumer loop startups; ensure helper methods return descriptive errors rather than panics.
- `internal/teams/auth`: Confirm the `StateStore` helpers now clearly report why a token is invalid (missing file, expired, missing token) and surface that information to the command/startup path.

Details
1. Wrap every auth-loading step with `br.Log.Info()/Warn()` calls that include the path, expiry timestamp, and whether the state is usable; fail fast with returned errors instead of relying on default panics if e.g. `ConfigPath` is empty.
2. When running consumer loops, log that Teams polling is starting (include thread counts or key metrics) only after confirming the token is valid, and log a warning when the token expires so the operator notices the need to rerun `!login`.
3. Ensure existing helpers like `fetchMediaConfig` or `/versions` (reviewed in `teamsbridge` or `internal/teams`) do not expect uninitialized spec versions; add nil checks or fallbacks where they previously assumed clients were ready.

Unit tests
- Add a unit test around the auth-state helper that asserts it returns descriptive errors for an empty `ConfigPath`, missing auth file, and expired token, and that the log output contains the path/expiry where applicable.
- Add a unit test for the consumer-start helper to ensure it logs both success and failure paths (e.g., using a test logger and checking for log messages when auth is missing or present).
