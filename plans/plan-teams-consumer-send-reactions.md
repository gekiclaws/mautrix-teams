## Open Questions
- None.

## Task Checklist
Phase 1
☑ Add Teams Consumer reaction client methods (add/remove) with request/response tests.
Phase 2
☑ Add Teams message/reaction mapping stores (DB + queries) plus upgrade assertions.
Phase 3
☑ Handle Matrix reaction + redaction events for Teams Consumer rooms with emoji mapping, logging, and removal lookup (reaction redactions only).

## Phase 1: Teams Consumer Reaction Client
Files
- `internal/teams/client/reactions.go`: add add/remove reaction API methods and typed errors.
- `internal/teams/client/reactions_test.go`: validate URL building, headers, payloads, and non-2xx error handling.
Changes
- Implement `Client.AddReaction(ctx, threadID, teamsMessageID, emotionKey string, appliedAtMS int64) (int, error)`:
  - Validate HTTP client, token, thread ID, message ID, and emotion key (non-empty).
  - Build `PUT {SendMessagesURL}/{thread_id}/messages/{teams_message_id}/properties?name=emotions` using `url.PathEscape` for both IDs.
  - Payload: `{ "emotions": { "key": <emotion_key>, "value": <unix_ms> } }`.
  - Headers: `authentication: skypetoken=<token>`, `Accept: application/json`, `Content-Type: application/json`.
  - On non-2xx, return a typed `ReactionError` with status + truncated body snippet (2KB).
- Implement `Client.RemoveReaction(ctx, threadID, teamsMessageID, emotionKey string) (int, error)`:
  - Same validation and endpoint as Add, but `DELETE` and payload `{ "emotions": { "key": <emotion_key> } }`.
- Reuse existing `SendMessagesURL` default base so reactions share the same host/root as message send.
Tests
- `AddReaction` uses escaped thread/message IDs and hits `/conversations/{thread}/messages/{message}/properties?name=emotions`.
- `AddReaction` sets `authentication`, `Accept`, and JSON `Content-Type` headers and sends the expected JSON body.
- `RemoveReaction` mirrors the endpoint and headers and sends the key-only JSON body.
- Non-2xx responses return `ReactionError` with status + body snippet.

## Phase 2: Teams Message + Reaction Mapping Stores
Files
- `database/teams_message.go` (new): Teams message mapping table + query helpers.
- `database/teams_reaction.go` (new): Teams reaction mapping table + query helpers.
- `database/database.go`: register new query helpers.
- `database/upgrades/29-teams-message-map.sql` (new): add `teams_message_map` table.
- `database/upgrades/30-teams-reaction-map.sql` (new): add `teams_reaction_map` table.
- `database/upgrades/00-latest-revision.sql`: include both tables in the latest schema snapshot.
- `database/upgrades/upgrades_test.go`: add upgrade assertions for the new tables.
Changes
- Add `teams_message_map` table with columns:
  - `mxid TEXT PRIMARY KEY`
  - `thread_id TEXT NOT NULL`
  - `teams_message_id TEXT NOT NULL`
- Add query helpers:
  - `GetByMXID(mxid id.EventID) *TeamsMessageMap`
  - `Upsert(map *TeamsMessageMap) error` (idempotent on `mxid`).
- Add `teams_reaction_map` table with columns:
  - `reaction_mxid TEXT PRIMARY KEY`
  - `target_mxid TEXT NOT NULL`
  - `emotion_key TEXT NOT NULL`
- Add query helpers:
  - `GetByReactionMXID(mxid id.EventID) *TeamsReactionMap`
  - `Insert(map *TeamsReactionMap) error`
  - `Delete(reactionMXID id.EventID) error`
Tests
- `upgrades_test.go`: assert `teams_message_map` and `teams_reaction_map` tables exist after running upgrades.

## Phase 3: Matrix → Teams Reaction Handling
Files
- `internal/bridge/reactions.go` (new): emoji→emotion mapping, add/remove flow, and logging.
- `internal/bridge/reactions_test.go` (new): validate mapping, add/remove calls, and redaction lookup behavior with fakes.
- `teams_consumer_portal.go`: route `m.reaction` and redaction events to the reaction handler.
- `teams_consumer_rooms.go`: initialize a Teams Consumer reaction handler alongside the sender.
- `main.go`: add a `TeamsConsumerReactor` field to `DiscordBridge`.
Changes
- Add a centralized mapping function `MapEmojiToEmotionKey(emoji string) (string, bool)` with the minimal table:
  - ❤️ → `heart`
  - 👍 → `like`
  - 😂 → `laugh`
  - 😮 → `surprised`
  - 😢 → `sad`
  - 😡 → `angry`
  - Unmapped emojis are logged and ignored.
- Implement `TeamsConsumerReactor` (teamsbridge) with dependencies:
  - `Client *client.Client`
  - `Threads ThreadLookup` (room → thread ID)
  - `Messages TeamsMessageMapStore` (target mxid → teams message ID)
  - `Reactions TeamsReactionMapStore` (reaction mxid → target mxid + emotion key)
  - `Log zerolog.Logger`
- Reaction add flow:
  - Validate `m.reaction` and `m.annotation` relation type.
  - Resolve `thread_id` via `TeamsThreadStore` and `teams_message_id` via `teams_message_map` for `relates_to.event_id`.
  - Map emoji → emotion key; if unmapped or missing mapping, log and return.
  - Call `Client.AddReaction` with `time.Now().UnixMilli()`.
  - Log attempt + status (`teams reaction add attempt`, `teams reaction response`); on non-2xx log `teams reaction error` and return.
  - On success, persist `teams_reaction_map` with reaction event ID, target event ID, and emotion key.
- Reaction remove flow (redaction):
  - Look up the redacted reaction in `teams_reaction_map`; if missing, ignore to avoid changing generic redaction behavior.
  - Resolve the target Teams message ID from `teams_message_map`.
  - Call `Client.RemoveReaction` and log attempt/response/error with the same fields.
  - On success, delete the reaction map entry.
- Update `TeamsConsumerPortal.ReceiveMatrixEvent` to switch on event type (`m.room.message`, `m.reaction`, `m.room.redaction`) and call the appropriate handler (existing message send vs. new reaction add/remove).
Tests
- Emoji mapping returns expected keys and rejects unmapped emoji.
- Add flow sends a PUT with mapped emotion key and records reaction mapping on success.
- Redaction flow uses stored reaction mapping to issue DELETE and removes mapping on success.
- Missing message mapping or unmapped emoji logs and exits without calling the client.
