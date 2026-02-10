## Open Questions
- None.
  - Decisions:
    1. Outbound Matrix→Teams reactions key `teams_user_id` as the single logged-in consumer Teams account from `auth.json` for now.
    2. Store canonical `msg/<id>` `teams_message_id` everywhere in DB/internal state and strip `msg/` only at Teams API call boundaries.

## Task Checklist
Phase 1
☑ Replace split reaction persistence (`teams_reaction_map` + `teams_reaction`) with one `reaction_map` table keyed by `(thread_id, teams_message_id, teams_user_id, reaction_key)`.
☑ Refactor DB query interfaces and bridge dependencies to use one reaction mapping API for add/lookup/diff/remove.
☑ Add unit tests for schema presence and reaction key/query idempotency.

Phase 2
☑ Send Teams→Matrix reactions via Teams-user ghost intents (not bot), persist `matrix_reaction_event_id`, and no-op when key already exists.
☑ Implement Teams poll removal diff that redacts stored Matrix reaction events as the original ghost sender and deletes mappings only when safe.
☑ Add unit tests for ghost attribution, idempotent add, removal diff, and redaction-failure retry semantics.

Phase 3
☑ On Matrix→Teams reaction add/remove, upsert/delete the same reaction-map rows so poll-based ingest suppresses echo duplicates.
☑ Remove raw-content echo marker dependency (`com.beeper.teams.ingested_reaction`) from correctness path; retain only as optional diagnostic metadata.
☑ Add unit tests for outbound echo suppression and reaction toggle/change convergence.

## Phase 1: Unified Reaction Mapping Persistence
Files
- `database/upgrades/36-reaction-map.sql` (new): create `reaction_map` with required PK/index and drop obsolete reaction tables.
- `database/upgrades/00-latest-revision.sql`: replace `teams_reaction_map`/`teams_reaction` snapshot entries with `reaction_map` and index.
- `database/upgrades/upgrades_test.go`: assert `reaction_map` table and `(matrix_room_id, matrix_reaction_event_id)` index existence.
- `database/reaction_map.go` (new): define `ReactionMapQuery` + `ReactionMap` model with typed lookup/upsert/delete methods.
- `database/database.go`: register `ReactionMapQuery` and remove `TeamsReactionMapQuery`/`TeamsReactionStateQuery` wiring.
- `database/teams_reaction.go`: delete file after moving callers to `reaction_map.go`.
- `database/teams_reaction_state.go`: delete file after moving callers to `reaction_map.go`.
- `internal/bridge/reactions.go`: switch `TeamsConsumerReactor` store dependency to unified reaction-map interface.
- `internal/bridge/reactions_ingest.go`: switch `TeamsReactionIngestor` store dependency to unified reaction-map interface.
Changes
- Add `reaction_map` schema:
  - `thread_id TEXT NOT NULL`
  - `teams_message_id TEXT NOT NULL`
  - `teams_user_id TEXT NOT NULL`
  - `reaction_key TEXT NOT NULL`
  - `matrix_room_id TEXT NOT NULL`
  - `matrix_target_event_id TEXT NOT NULL`
  - `matrix_reaction_event_id TEXT NOT NULL`
  - `updated_ts_ms BIGINT NOT NULL`
  - `PRIMARY KEY (thread_id, teams_message_id, teams_user_id, reaction_key)`
  - `INDEX reaction_map_matrix_event_idx (matrix_room_id, matrix_reaction_event_id)`
- Define one stable value object in bridge code:
  - `type ReactionKey struct { ThreadID, TeamsMessageID, TeamsUserID, ReactionKey string }`.
  - `NewReactionKey(threadID, teamsMessageID, teamsUserID, reactionKey string) (ReactionKey, bool)` normalizes/validates IDs and returns empty/false when any part is missing.
  - Use `NewReactionKey(...)` in both Teams→Matrix ingest and Matrix→Teams outbound paths so key composition cannot drift.
- Expose query methods needed by both directions without braided concerns:
  - `GetByKey(key ReactionKey) *ReactionMap`
  - `ListByMessage(threadID, teamsMessageID string) ([]*ReactionMap, error)`
  - `Upsert(row *ReactionMap) error`
  - `DeleteByKey(key ReactionKey) error`
  - `GetByMatrixReaction(roomID id.RoomID, reactionEventID id.EventID) *ReactionMap`
- Keep Teams API ID normalization localized (`msg/<id>` canonical in storage, stripped only when invoking Teams endpoints).
Tests
- `database/upgrades/upgrades_test.go` verifies `reaction_map` table and index exist after full upgrade.
- New `database/reaction_map_test.go` (or equivalent DB query tests) validates:
  - PK uniqueness/idempotent upsert for identical ReactionKey.
  - `GetByMatrixReaction` resolves stored row.
  - `ListByMessage` returns all rows for one message and excludes other messages.

## Phase 2: Teams → Matrix Ghost-Attributed Reaction Ingest + Removals
Files
- `teams_consumer_reaction_sender.go` (new): appservice intent sender that emits/redacts reactions as Teams virtual users.
- `teams_consumer_read_receipts.go`: reuse `intentForTeamsVirtualUser` / membership profile logic from a shared helper surface.
- `internal/bridge/reactions_ingest.go`: replace bot-sender flow with ghost sender that receives Teams user ID; add set-diff against unified `reaction_map`.
- `internal/bridge/messages.go`: pass canonical `teams_message_id`/target MXID consistently into reaction ingest path.
- `teams_consumer_rooms.go`: wire `MessageIngestor.ReactionIngestor` with ghost-capable sender and unified reaction store.
- `internal/bridge/reactions_ingest_test.go`: update fakes and assertions for ghost user send/redact and map-based idempotency/removal.
- `teams_consumer_read_receipts_test.go`: add coverage for shared virtual-user helper behavior reused by reaction sender.
Changes
- Introduce a ghost-capable sender contract for inbound poll mirroring:
  - `SendReactionAsTeamsUser(roomID, targetEventID, emoji, teamsUserID) (reactionEventID, error)`
  - `RedactReactionAsTeamsUser(roomID, reactionEventID, teamsUserID) error`
- Implement sender in `main` package using appservice intents:
  - Resolve ghost MXID from `teams_user_id`.
  - `EnsureRegistered`/`EnsureJoined` room.
  - Ensure `m.room.member` display name is set before send/redact so Beeper renders user names instead of raw MXIDs.
  - Send `m.reaction` and redact via that ghost intent.
- Update Teams poll ingest algorithm:
  - Build current ReactionKeys from `msg.Reactions` (`teams_user_id` from normalized `user.mri`, `reaction_key` as mapped emoji key).
  - Load existing keys via `ListByMessage`.
  - Additions: if key exists -> noop; else resolve target event, map emotion→emoji, send as ghost, persist full row.
  - Removals: for stored keys missing from current set, redact stored `matrix_reaction_event_id` as same ghost; delete row only on confirmed success.
- Safe failure behavior:
  - Missing target message map: warn and skip, no panic/crash.
  - Redaction failure: keep row for retry on next poll for transient errors; delete only on successful redaction or confirmed missing event (`M_NOT_FOUND`).
Tests
- `internal/bridge/reactions_ingest_test.go`:
  - New reaction sends once as expected `teams_user_id` ghost and persists map row.
  - Existing key causes no send (idempotent).
  - Missing target map logs/returns nil with no send.
  - Removal diff redacts matching `matrix_reaction_event_id` and deletes row on success.
  - Redaction error preserves row for retry; `M_NOT_FOUND` path deletes row.
  - Multiple users on same emoji only remove the departing user’s reaction.

## Phase 3: Matrix → Teams Outbound Echo Suppression + Toggle Safety
Files
- `internal/bridge/reactions.go`: write/unwrite unified reaction-map rows during add/remove; resolve outbound row by matrix reaction event for removals.
- `teams_consumer_rooms.go`: pass local Teams user ID into `TeamsConsumerReactor` so outbound rows get stable `teams_user_id`.
- `teams_consumer_portal.go`: keep routing unchanged; no additional behavioral branching in portal layer.
- `internal/bridge/reactions_test.go`: migrate to unified map fakes and add outbound echo/toggle test cases.
- `internal/bridge/messages_test.go`: add/update assertions that poll ingest noops when outbound map row already exists.
Changes
- On `AddMatrixReaction` success (Matrix→Teams):
  - Build canonical ReactionKey via shared `NewReactionKey(...)` with `thread_id`, canonical `teams_message_id`, configured outbound `teams_user_id` (consumer identity), and `reaction_key`.
  - Upsert unified row with `matrix_room_id`, `matrix_target_event_id`, and outbound `matrix_reaction_event_id` (`evt.ID`).
  - This guarantees next Teams poll sees existing key and skips Matrix re-send.
- On `RemoveMatrixReaction` (Matrix redaction):
  - Resolve row by `(room_id, redacted_event_id)`.
  - Call Teams remove using row’s key fields.
  - Delete by PK after successful Teams remove.
- Toggle/change semantics:
  - Remove old reaction row when redacted.
  - Add new reaction row for replacement emoji.
  - Teams poll set-diff converges to current set without looping.
- Keep `com.beeper.teams.ingested_reaction` optional for observability, but correctness must come from `reaction_map` PK+lookups.
Tests
- `internal/bridge/reactions_test.go`:
  - Add path persists unified map row with expected key fields.
  - Remove path resolves via `(room_id, redacts)` and deletes on success.
  - Duplicate suppression: outbound add followed by simulated poll with same key triggers zero inbound Matrix sends.
  - Toggle case: old emoji redaction + new emoji add results in one delete and one insert with distinct keys.
  - Remove failure leaves row for retry.
