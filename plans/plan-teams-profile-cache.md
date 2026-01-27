FLAGGED OPEN QUESTIONS
- None.

TASK CHECKLIST
Phase 1 — Profile identity + persistence
☐ Add teams_profile table and upgrade entry (latest revision + new upgrade file)
☐ Add TeamsProfile query type + DB handle with insert-if-missing semantics
☐ Add normalization helper for Teams user IDs + last-seen timestamp helper with unit test

Phase 2 — Ingest + profile population + Matrix display name
☐ Extend RemoteMessage with SenderName and parse display name from Teams messages payload
☐ Populate profile cache during ingest (insert-if-missing, log new profile) and use cached/display name for Matrix events
☐ Update Matrix sender to attach per-message profile metadata when display name is available
☐ Update/extend unit tests for client parsing, ingestor behavior, and sender metadata wiring

Phase 3 — Wiring
☐ Wire TeamsProfile store into teams-login message ingestion

PHASE 1 — Profile identity + persistence
Files + changes
- database/upgrades/00-latest-revision.sql: add the `teams_profile` table definition alongside other persistent tables.
- database/upgrades/27-teams-profile-cache.sql: create the new table with `teams_user_id` primary key, `display_name`, and `last_seen_ts` (BIGINT), no update logic.
- database/database.go: add `TeamsProfile *TeamsProfileQuery` to the Database struct and initialize it in New().
- database/teams_profile.go: add TeamsProfileQuery with `GetByTeamsUserID` and `InsertIfMissing` (INSERT ... ON CONFLICT DO NOTHING) plus TeamsProfile model.
- internal/teams/model/message.go: add `NormalizeTeamsUserID(string) string` to canonicalize sender IDs (trim/normalize) for consistent storage and lookup; add `ChooseLastSeenTS(time.Time, time.Time) time.Time` to centralize timestamp fallback.
- internal/teams/model/message_test.go: add unit tests for NormalizeTeamsUserID (empty/whitespace/normal IDs) and ChooseLastSeenTS (message timestamp vs fallback to now).

Implementation notes
- Use `teams_user_id` as the stable key (normalized once via NormalizeTeamsUserID) and store `last_seen_ts` in Unix millis.
- Use message timestamp when valid; fall back to current time only when missing/zero (via ChooseLastSeenTS).
- Insert-only semantics: `InsertIfMissing` must not update existing rows; use `RowsAffected` to detect first insert for logging later.

Unit tests
- internal/teams/model/message_test.go: verify normalization behavior for empty/whitespace IDs and already-normalized IDs; verify last_seen_ts selection behavior.

PHASE 2 — Ingest + profile population + Matrix display name
Files + changes
- internal/teams/model/message.go: add `SenderName` to RemoteMessage and `ExtractSenderName(json.RawMessage)` to parse display name from the `from` field (e.g., displayName/name variants).
- internal/teams/client/messages.go: populate RemoteMessage.SenderName via ExtractSenderName and normalize SenderID with NormalizeTeamsUserID.
- internal/teams/client/messages_test.go: extend fixtures to include displayName and assert SenderName parsing; add coverage for missing displayName.
- internal/bridge/messages.go: add a ProfileStore interface, store lookup/insert logic, and per-message profile construction for Matrix sends; keep insert-only behavior and log new profiles (teams_user_id + display_name + empty name flag).
- internal/bridge/messages_test.go: add tests for profile insertion (insert on first seen, no insert on existing), display name selection (cache vs payload), and ensure messages still stop on send failure.
- internal/bridge/sync_test.go: update fake sender to the new interface/signature and keep existing behavior assertions.
- internal/bridge/messages.go: update BotMatrixSender to send `event.Content` with `Raw` metadata containing `com.beeper.per_message_profile` when display name is present; omit otherwise.

Implementation notes
- Profile selection: prefer cached display_name when present; otherwise fall back to message SenderName; for display fallback, use sender ID only in the Matrix metadata (do not write back to DB).
- Insert path: build profile from normalized sender ID + display name + message timestamp, call InsertIfMissing, and log only on successful insert.
- Per-message profile payload should include stable `id` (teams_user_id) + `displayname`; avoid avatar fields (out of scope).

Unit tests
- internal/teams/client/messages_test.go: verify SenderName parsing for object payloads with displayName; ensure missing displayName yields empty SenderName.
- internal/bridge/messages_test.go: add a test that ingestor inserts the profile once and passes display name into the sender metadata; add a test that existing profile prevents insert and uses cached name.
- internal/bridge/messages_test.go / internal/bridge/sync_test.go: update fakes to accept optional profile metadata while preserving sequence/stop-on-failure assertions.

PHASE 3 — Wiring
Files + changes
- cmd/teams-login/main.go: create TeamsProfile store (from teamsDB) and pass it into MessageIngestor; no other behavior changes.

Implementation notes
- Keep wiring minimal: only pass the profile store into ingestor construction and rely on the new ingest logic for population + display name metadata.

Unit tests
- No new unit tests; wiring is covered by updated ingestor tests.
