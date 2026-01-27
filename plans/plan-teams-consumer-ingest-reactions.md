## Open Questions
- None.

## Task Checklist
Phase 1
☑ Parse Teams `properties.emotions` into `model.RemoteMessage` reactions.
Phase 2
☑ Add Teams reaction state persistence + Teams message reverse lookup (Teams ID → Matrix event ID).
Phase 3
☑ Implement Teams→Matrix reaction ingest with set-diff, logging, and MessageIngestor wiring.

## Phase 1: Parse Teams Reaction State From Messages
Files
- `internal/teams/model/message.go`: add reaction model types + parsing helpers for `properties.emotions`.
- `internal/teams/client/messages.go`: parse `properties.emotions` into `RemoteMessage`.
- `internal/teams/client/messages_test.go`: cover emotions parsing and malformed entry handling.
Changes
- Extend `model.RemoteMessage` with a `Reactions []MessageReaction` field (name aligned with other model structs).
- Add types in `internal/teams/model/message.go`:
  - `MessageReaction { EmotionKey string; Users []MessageReactionUser }`
  - `MessageReactionUser { MRI string; TimeMS int64 }`
- Add `ExtractReactions(properties json.RawMessage) []MessageReaction`:
  - Decode `properties.emotions[]` with `key` and `users`.
  - Ignore `annotationsSummary` entirely.
  - Normalize/trim `key` and `mri`.
  - Parse `users[].time` as optional ms (handle string or number); ignore `value`.
  - Skip malformed entries and emotions with empty user lists; never panic.
- Update `internal/teams/client/messages.go`:
  - Add `Properties json.RawMessage \\`json:"properties"\\`` to `remoteMessage`.
  - Populate `RemoteMessage.Reactions = model.ExtractReactions(msg.Properties)`.
Tests
- `ListMessages` parses `properties.emotions` into `RemoteMessage.Reactions` with normalized keys and MRIs.
- Malformed `properties` / invalid `time` values are ignored without returning an error.
- Emotions with empty `users` arrays are skipped.

## Phase 2: Persist Teams Reaction State + Message Lookup
Files
- `database/teams_reaction_state.go` (new): Teams reaction persistence (Teams → Matrix ingest state).
- `database/teams_message.go`: add lookup by Teams message ID.
- `database/database.go`: register the new query helper.
- `database/upgrades/32-teams-reaction-state.sql` (new): create `teams_reaction` table.
- `database/upgrades/00-latest-revision.sql`: include `teams_reaction` in the latest schema snapshot.
- `database/upgrades/upgrades_test.go`: assert `teams_reaction` table exists after upgrades.
Changes
- Add `teams_reaction` table:
  - `thread_id TEXT NOT NULL`
  - `teams_message_id TEXT NOT NULL`
  - `emotion_key TEXT NOT NULL`
  - `user_mri TEXT NOT NULL`
  - `matrix_event_id TEXT NOT NULL`
  - `PRIMARY KEY (thread_id, teams_message_id, emotion_key, user_mri)`
- Add `TeamsReactionStateQuery` with helpers:
  - `ListByMessage(threadID, teamsMessageID string) ([]*TeamsReactionState, error)` for per-message diff.
  - `Insert(state *TeamsReactionState) error` (use `ON CONFLICT DO NOTHING`).
  - `Delete(threadID, teamsMessageID, emotionKey, userMRI string) error`.
- Add `TeamsMessageMapQuery.GetByTeamsMessageID(threadID, teamsMessageID string)` for reaction ingest when a message was mapped earlier.
Tests
- `upgrades_test.go` verifies `teams_reaction` table exists after upgrades.

## Phase 3: Teams → Matrix Reaction Ingest
Files
- `internal/bridge/reactions_ingest.go` (new): reaction set-diff + Matrix emission.
- `internal/bridge/reactions.go`: add `MapEmotionKeyToEmoji` (reuse existing mapping table).
- `internal/bridge/messages.go`: wire reaction ingest into message fetch flow.
- `internal/bridge/reactions_ingest_test.go` (new): diff/add/remove/idempotency tests with fakes.
- `internal/bridge/messages_test.go`: verify message ingest invokes reaction ingest with correct inputs.
Changes
- Add `MapEmotionKeyToEmoji(emotionKey string) (string, bool)` next to `MapEmojiToEmotionKey` with a single source-of-truth mapping.
- Introduce `MatrixReactionSender`:
  - `SendReaction(roomID id.RoomID, target id.EventID, key string) (id.EventID, error)`.
  - `Redact(roomID id.RoomID, eventID id.EventID) (id.EventID, error)`.
  - Implement `BotMatrixReactionSender` using `mautrix.Client.SendMessageEvent` and `RedactEvent`.
- Add `TeamsReactionStateStore` + `TeamsMessageMapLookup` interfaces for DB access.
- Implement `TeamsReactionIngestor` with method:
  - `IngestMessageReactions(ctx, threadID string, roomID id.RoomID, msg model.RemoteMessage, targetMXID id.EventID) error`.
  - Build `current` set from `msg.Reactions` (`thread_id`, `teams_message_id`, `emotion_key`, `user_mri`).
  - Load `existing` via `ListByMessage` and compute set diff (per message only).
  - For additions:
    - Resolve target MXID (use `targetMXID` if set; else lookup via `GetByTeamsMessageID`).
    - Map `emotion_key` → emoji; on unmapped keys, log and skip.
    - Send `m.reaction` event with bot sender; use `users[].time` if parseable for logging, otherwise `time.Now()`; on success, insert `teams_reaction` row with reaction MXID.
  - For removals:
    - Redact the stored `matrix_event_id`; on success, delete row.
  - Log adds/removals with `thread_id`, `teams_message_id`, `emotion_key`, `user_mri`, and matrix IDs.
  - Never fail the message ingest on malformed reaction payloads; log and continue.
- Wire into `MessageIngestor`:
  - Add optional `ReactionIngestor MessageReactionIngestor` field.
  - Ensure reaction ingest runs for every fetched message, regardless of sequence filtering or empty body skips.
  - After message processing (and after `TeamsMessageMap` upsert when sending new messages), call `ReactionIngestor.IngestMessageReactions` with the message and the best-known target MXID.
Tests
- Reaction ingest adds new reactions and persists them with returned reaction MXID.
- Reaction ingest removes stale reactions and deletes state after successful redaction.
- Idempotency: identical current/existing sets result in no Matrix calls.
- Unmapped emotion keys are logged and skipped without emitting events.
- `MessageIngestor` passes the correct target MXID for freshly-sent messages and still invokes reaction ingest for messages where only reactions change.
