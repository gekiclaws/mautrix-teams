## Open Questions
- None.
  - Decisions:
    1. Always persist the advanced `latest_read_ts` even when there is no mapped inbound Teams message; skip the Matrix receipt but never roll back the horizon.
    2. If the horizon response includes more than one non-self participant (indicative of group chats), do not emit inbound read receipts for that thread.
    3. Emit Matrix read markers via plain `SetReadMarkers` with both `m.read` and `m.fully_read` equal to the same event ID (no `com.beeper.*.extra` metadata).

## Task Checklist
Phase 1
☑ Add consumption-horizon client + parser and persist Teams message metadata (sender/timestamp) needed for boundary lookups.
☑ Add DB schema/state for per-thread consumption horizon tracking and update upgrade coverage.
☑ Unit test horizon parsing/client behavior and message-map metadata writes.
Phase 2
☑ Implement read-receipt ingestion from consumption horizons with monotonic persistence and Matrix read markers.
☑ Wire inbound read-receipt ingestion into the Teams consumer polling loop without affecting message backoff.
☑ Unit test monotonicity, mapping selection, and no-op/error paths for inbound read receipts.

## Phase 1: Consumption Horizon Fetching + Message Metadata
Files
- `internal/teams/model/consumption_horizon.go` (new): define consumption horizon response types and `ParseConsumptionHorizonLatestReadTS`.
- `internal/teams/client/consumption_horizons.go` (new): add Teams client GET for `/threads/{thread_id}/consumptionhorizons`.
- `internal/teams/client/consumption_horizons_test.go` (new): validate endpoint path, headers, and response decoding/error classification.
- `database/teams_message.go`: add sender/timestamp columns + lookup by timestamp.
- `database/teams_consumption_horizon.go` (new): persist `last_read_ts` per thread + remote user.
- `database/database.go`: register the new query.
- `database/upgrades/33-teams-consumption-horizon.sql` (new): add schema for horizons + message metadata columns.
- `database/upgrades/00-latest-revision.sql`: include the new table/columns in the latest schema.
- `database/upgrades/upgrades_test.go`: assert the new table exists after upgrades.
- `internal/bridge/messages.go`: persist sender ID and timestamp into the message map.
- `internal/bridge/messages_test.go`: assert message-map entries include sender/timestamp.
Changes
- Add model helpers for consumption horizons:
  - `type ConsumptionHorizonsResponse` with `ID`, `Version`, and `Horizons []ConsumptionHorizon`.
  - `type ConsumptionHorizon` with `ID`, `ConsumptionHorizon`, and `MessageVisibilityTime`.
  - `ParseConsumptionHorizonLatestReadTS(horizon string) (int64, bool)` that extracts and parses the 2nd semicolon-delimited segment, ignoring all others.
- Implement `Client.GetConsumptionHorizons(ctx, threadID string)`:
  - Endpoint base `https://teams.live.com/api/chatsvc/consumer/v1/threads` (add `ConsumptionHorizonsURL` on client for overrides).
  - `GET {base}/{thread_id}/consumptionhorizons` with `skypetoken` auth.
  - Return a typed `ConsumptionHorizonsError` (or reuse `MessagesError` with a clear message) on non-2xx, preserving retryable handling for 429/5xx.
- Expand `teams_message_map` to store metadata required for timestamp boundary queries:
  - New columns: `message_ts BIGINT` and `sender_id TEXT` (nullable for existing rows).
  - Update `TeamsMessageMap` struct and `Upsert` to write `message_ts` and normalized `sender_id` when available.
  - Add query `GetLatestInboundBefore(threadID string, maxTS int64, selfUserID string) *TeamsMessageMap` that filters `message_ts <= maxTS`, `sender_id != selfUserID`, and non-empty IDs, ordering by `message_ts DESC LIMIT 1`.
  - Add an index on `(thread_id, message_ts)` to keep the boundary lookup cheap.
- Persist per-thread, per-remote-user horizon state:
  - New table `teams_consumption_horizon(thread_id TEXT, teams_user_id TEXT, last_read_ts BIGINT NOT NULL, PRIMARY KEY(thread_id, teams_user_id))`.
  - New query with `Get(threadID, teamsUserID)` and `UpsertLastRead(threadID, teamsUserID, lastReadTS)`.
- Record metadata during ingest:
  - In `MessageIngestor.IngestThread`, include `SenderID` and `Timestamp.UnixMilli()` in `TeamsMessageMap` writes so the boundary lookup is deterministic and restart-safe.
Tests
- `internal/teams/client/consumption_horizons_test.go`:
  - Assert `GET /api/chatsvc/consumer/v1/threads/{thread}/consumptionhorizons` path, auth header, and JSON decoding.
  - Non-2xx responses yield the typed error with status and snippet.
- `internal/teams/model/consumption_horizon_test.go` (if created):
  - `ParseConsumptionHorizonLatestReadTS` handles valid three-part strings, missing segments, and non-numeric 2nd segments.
- `internal/bridge/messages_test.go`:
  - When ingesting a message, verify the stored mapping includes `SenderID` and `MessageTS`.
- `database/upgrades/upgrades_test.go`:
  - Verify `teams_consumption_horizon` exists after applying upgrades.

## Phase 2: Inbound Read Receipt Ingestion + Wiring
Files
- `internal/bridge/consumption_horizons.go` (new): read-receipt ingestor that maps horizons to Matrix read markers.
- `internal/bridge/consumption_horizons_test.go` (new): unit tests for horizon comparison, mapping, and marker emission.
- `internal/bridge/teams_consumer_ingest.go`: invoke the horizon ingestor during polling.
- `teams_consumer_rooms.go`: wire the ingestor into the polling loop with the Teams client, bot sender, DB stores, and self user ID.
Changes
- Implement a dedicated ingestor to keep concerns separated:
  - `type TeamsConsumptionHorizonIngestor` with dependencies:
    - `Client interface { GetConsumptionHorizons(ctx context.Context, threadID string) (*model.ConsumptionHorizonsResponse, error) }`
    - `Messages interface { GetLatestInboundBefore(threadID string, maxTS int64, selfUserID string) *database.TeamsMessageMap }`
    - `State interface { Get(threadID, teamsUserID string) *database.TeamsConsumptionHorizon; UpsertLastRead(threadID, teamsUserID string, lastReadTS int64) error }`
    - `Sender interface { SetReadMarkers(roomID id.RoomID, eventID id.EventID) error }`
    - `SelfUserID string` (normalized `8:*`).
- `PollOnce(ctx, threadID string, roomID id.RoomID)` flow:
  - Fetch horizons; select the non-self entry (normalize IDs before compare).
  - Parse `latest_read_ts` from the 2nd segment; no-op on parse failure.
  - Load stored `last_read_ts`; if `latest_read_ts <= last_read_ts`, no-op.
  - Lookup latest inbound mapped message with `message_ts <= latest_read_ts` and `sender_id != self`.
  - If a mapping exists, call `SetReadMarkers` with both `m.read` and `m.fully_read` set to the same event ID.
  - Persist the new `last_read_ts` according to the answer to Open Question #1.
  - Log with `thread_id`, `room_id`, `teams_user_id`, and `latest_read_ts`.
  - Skip threads whose horizon response contains multiple non-self entries (treat them as out-of-scope group chats) so we never attempt deterministic inference.
  - Always persist the advanced `latest_read_ts` even when `GetLatestInboundBefore` returns nil so the horizon remains monotonic across restarts.
- Add a bot read-marker sender (if needed) similar to `BotMatrixSender`:
  - `SetReadMarkers(roomID, eventID)` wraps `mautrix.Client.SetReadMarkers` with `Read` and `FullyRead` set.
- Wire into polling without affecting message backoff:
  - Extend `TeamsConsumerIngestor` with an optional `ReadReceipts` field.
  - In `PollOnce`, call `ReadReceipts.PollOnce(...)` after `Syncer.SyncThread`, log any error, but always return the original `SyncResult`/error from message sync so backoff stays message-driven.
  - Instantiate the ingestor in `runTeamsConsumerMessageSync` using:
    - `consumer` client,
    - `br.Bot.Client` for read markers,
    - `br.DB.TeamsMessageMap` + `br.DB.TeamsConsumptionHorizon` for lookups/state,
    - `state.TeamsUserID` for self ID normalization.
Tests
- `internal/bridge/consumption_horizons_test.go`:
  - Horizon advances to a timestamp with a mapped inbound message → emits one read marker and persists `last_read_ts`.
  - Horizon equal to stored `last_read_ts` → no marker, no persistence changes.
  - Horizon advances but no inbound message mapping exists → no marker (and persistence behavior per Open Question #1).
  - Self horizon entry is ignored; only non-self horizon is used.
  - Sender errors are logged but do not alter message sync error behavior (verify by using a fake ingestor error return path).
