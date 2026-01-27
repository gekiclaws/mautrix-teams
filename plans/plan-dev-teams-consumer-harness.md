## Open Questions
- None.

## Task Checklist
Phase 1
☑ Add a dev-only `dev-send` harness that loads config/DB/auth, constructs a Matrix-shaped text event, resolves the Teams thread, and calls `TeamsConsumerSender.SendMatrixText` directly with dev-only logging (no Matrix writes).
Phase 2
☑ Add targeted unit tests for dev-send arg parsing and Matrix event construction, and tighten send-path logging to surface pending intent creation + room→thread resolution.

## Phase 1: Dev-Send Harness Core
Files
- `main.go`: add a dev-only entrypoint (`dev-send`) that short-circuits before `br.Main()` and logs invocation.
- `dev_send.go`: implement arg parsing, config/DB bootstrap, synthetic Matrix event construction, thread resolution, and send invocation.
- `teams_consumer_rooms.go`: reuse or expose sender init so the harness can initialize `TeamsConsumerSender` without Matrix startup (if a small helper is needed).
Changes
- Add a `runDevSendIfRequested(args []string)` gate in `main.go` (similar to `runTeamsAuthTestIfRequested`) that detects the `dev-send` subcommand, runs the harness, and exits with a non-zero status on error.
- Implement `parseDevSendArgs(args []string) (DevSendOptions, error)` to accept `--room`, `--sender` (Teams user ID `8:*` only), `--text`, optional `--event-id`, and optional `--config` (default `config.yaml`).
- Add `buildDevMatrixTextEvent(opts DevSendOptions) *event.Event` that sets `RoomID`, `Sender`, `ID`, `Type: m.room.message`, and `Content` with `msgtype=m.text` and `body`.
- Do not write MSS metadata to Matrix; only log MSS transitions in the harness.
- Load config with `configupgrade.Do` + `config.Config` (no validation) to read `AppService.Database`, open the DB via `dbutil.NewFromConfig`, and create `database.New(...)`.
- Initialize a minimal `DiscordBridge` instance with `ConfigPath`, `DB`, and `ZLog`, then call `initTeamsConsumerSender` (or a small extracted helper) and `TeamsThreadStore.LoadAll()`.
- Resolve the Teams thread ID for the room before sending to log `room_id` → `thread_id` mapping explicitly; fail fast if missing.
- Invoke `TeamsConsumerSender.SendMatrixText(ctx, roomID, body, eventID, devMSSWriter)` directly (no portal/Matrix ingress); the dev MSS writer should log transitions without Matrix HTTP.
Tests
- None in this phase (tests are in Phase 2).

## Phase 2: Dev Harness Tests + Send-Path Logging
Files
- `dev_send_test.go`: add pure unit tests for arg parsing and event construction.
- `internal/bridge/send.go`: add structured logs for pending intent creation and explicit room→thread resolution, if not already covered by Phase 1 logging.
Changes
- Add tests that verify `parseDevSendArgs` enforces required flags and defaults the config path.
- Add tests that verify `buildDevMatrixTextEvent` produces `m.room.message` with `m.text` content and preserves room/sender/event IDs.
- Log the pending send-intent creation with `client_message_id`, `thread_id`, and `status=pending`.
- Log room→thread resolution in the send path (or confirm the harness log is sufficient) to satisfy observability requirements.
Tests
- `TestParseDevSendArgsRequiredAndDefaults`
- `TestBuildDevMatrixTextEvent`
