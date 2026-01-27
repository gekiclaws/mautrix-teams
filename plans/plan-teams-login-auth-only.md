Open Questions
- Resolved: add an explicit `--no-probe` flag (default probe ON); probe is a sanity check only, not ingest.
- Resolved: do not require `config.yaml`; fall back to a minimal structured logger if missing/invalid.
- Resolved: any log implying threads/messages/reactions/rooms/Matrix is bridge-only (e.g., mentions `thread_id`, `seq`, `room_id`, `matrix_event_id`).

Task Checklist
- Phase 1: Strip ingest/bootstrap from teams-login and make the auth-only exit path explicit.
  - ☑ Remove the one-shot ingest bootstrap from `teams-login` and ensure it exits immediately after auth/probe.
  - ☑ Add a `--no-probe` flag (default ON) and a pure helper if needed; add unit tests for the helper only.
- Phase 2: Make the auth-only contract obvious in CLI help/logging, and keep ingest exclusively in the bridge entrypoint.
  - ☑ Update CLI help/logging strings to describe auth-only behavior, probe intent, and an explicit “auth-only exit” log line.
  - ☑ Confirm bridge-side consumer sync still owns discovery/ingest/reaction wiring (no new tests unless logic changes).

Phase 1: Strip ingest/bootstrap from teams-login
Files
- `cmd/teams-login/main.go`: Remove any bridge/ingest setup (DB open, room discovery, message/reaction ingest); keep only auth, state persistence, and optional probe; exit immediately after completing auth work.

Changes
- Delete `runRoomBootstrap` and `isConversationsError`, along with imports for `dbutil`, `database`, `internal/bridge`, `internal/teams/client`, and `mautrix` that were only needed for ingest/bridge setup.
- Add `--no-probe` flag (default probe ON). Gate `runProbe` behind the flag in both the “skypetoken already valid” and “new skypetoken acquired” paths, then return immediately without any discovery or sync.
- After acquiring and saving a new skypetoken, log a final “Authentication complete. Exiting (auth-only mode).” line and exit.

Unit tests
- Add a small unit test in `cmd/teams-login/main_test.go` for any new pure helper used to gate the probe flag (e.g., `shouldRunProbe(flag)`), avoiding network or auth client mocking.

Phase 2: Make the auth-only contract explicit
Files
- `cmd/teams-login/main.go`: Update CLI help/logging text to clarify the auth-only purpose and probe intent; allow logging to fall back when config is missing/invalid.
- `teams_consumer_rooms.go`: Add a brief comment in `startTeamsConsumerRoomSync` or `runTeamsConsumerRoomSync` stating this is the sole entrypoint for Teams → Matrix ingest in the bridge process.

Changes
- Adjust `flag.SetHelpTitles` or log messages so the tool clearly states it performs authentication only, runs an optional probe sanity check, and exits immediately after persisting auth state.
- Allow `teams-login` to proceed when `config.yaml` is missing/invalid by falling back to a minimal structured logger; only use config logging when the config loads cleanly.
- Keep all thread discovery / message / reaction ingest wiring exclusively under the bridge runtime (the `go run .` path), with no calls from `teams-login`.

Unit tests
- No new tests expected unless any bridge logic changes; if a comment-only change is made, no tests are added.
