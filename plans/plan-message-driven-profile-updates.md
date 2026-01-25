FLAGGED OPEN QUESTIONS
- None.

TASK CHECKLIST
Phase 1 — Message payload fields + model wiring
☑ Extend Teams message parsing to capture top-level `imdisplayname` and `fromDisplayNameInToken` in `model.RemoteMessage`
☑ Update client message parsing tests for the new fields and fallback ordering

Phase 2 — Profile update behavior + per-message metadata
☑ Add TeamsProfileQuery.UpdateDisplayName and surface it in the ProfileStore interface
☑ Update message ingest to detect `imdisplayname` changes, update DB + cache, and log name changes
☑ Ensure per-message profile metadata uses cached display name after updates, with display-only fallbacks
☑ Add unit tests for update-on-change and no-op cases, plus metadata selection

PHASE 1 — Message payload fields + model wiring
Files + changes
- internal/teams/model/message.go: add `IMDisplayName` and `TokenDisplayName` to `RemoteMessage` to carry top-level `imdisplayname` and `fromDisplayNameInToken` from the payload.
- internal/teams/client/messages.go: extend `remoteMessage` to parse top-level `imdisplayname` + `fromDisplayNameInToken` and populate `RemoteMessage.IMDisplayName` + `RemoteMessage.TokenDisplayName` (do not read `from` for names).
- internal/teams/client/messages_test.go: extend fixtures to include top-level `imdisplayname` + `fromDisplayNameInToken`, assert parsing, and verify separation when `imdisplayname` is missing.

Unit tests
- internal/teams/client/messages_test.go: add coverage that top-level `imdisplayname` and `fromDisplayNameInToken` are parsed and preserved; add a case with empty `imdisplayname` to verify it stays empty while the token fallback remains available.

PHASE 2 — Profile update behavior + per-message metadata
Files + changes
- database/teams_profile.go: add `UpdateDisplayName(teamsUserID string, displayName string, lastSeenTS time.Time) error` that updates `display_name` + `last_seen_ts` for an existing profile.
- internal/bridge/messages.go: extend `ProfileStore` with `UpdateDisplayName`; when a profile exists and `msg.IMDisplayName` is non-empty and different, call `UpdateDisplayName`, update the in-memory `existingProfile`, and log `teams_user_id` with old → new name.
- internal/bridge/messages.go: keep insert-only behavior for missing profiles, but store only `imdisplayname` (or empty) in the DB; do not persist token or derived fallback names.
- internal/bridge/messages.go: per-message profile selection order becomes (1) cached display_name after any update, (2) `msg.IMDisplayName`, (3) `msg.TokenDisplayName`, (4) sender ID; only the first is persisted.
- internal/bridge/messages_test.go: update fake profile store with `UpdateDisplayName` tracking; add unit tests for update-on-change, no-op when equal, and no-op when `imdisplayname` is empty; ensure metadata uses the updated name.

Unit tests
- internal/bridge/messages_test.go: new test for updating an existing profile when `imdisplayname` changes; assert DB update called, cached name used in metadata.
- internal/bridge/messages_test.go: new test for no-op when `imdisplayname` empty; metadata uses fallback without DB update.
- internal/bridge/messages_test.go: new test for no-op when `imdisplayname` matches cached name.
