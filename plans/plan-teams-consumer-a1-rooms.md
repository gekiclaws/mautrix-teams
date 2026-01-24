## Open Questions
- None.

## Task Checklist
Phase 1
☐ Add a Teams consumer conversations client with normalization models and unit tests.
Phase 2
☐ Add persistent Teams thread ↔ Matrix room mapping storage and upgrade schema.
Phase 3
☐ Add room-ensure logic and wire discovery into `teams-login` and bridge startup with required logging.

## Phase 1: Consumer API + Normalization
Files
- `internal/teams/client/conversations.go`: add a consumer client that performs the conversations GET with Bearer skypetoken, body truncation, and structured error details.
- `internal/teams/model/conversation.go`: define `RemoteConversation`, `ThreadProperties`, and `Thread` with `Normalize()` for required fields.
- `internal/teams/client/conversations_test.go`: test success decode, Authorization header, and non-2xx body truncation handling via `httptest`.
- `internal/teams/model/conversation_test.go`: test `Normalize()` behavior for missing `originalThreadId`, one-to-one detection, and created-at passthrough.
Changes
- Implement `Client.ListConversations(ctx) ([]model.RemoteConversation, error)` that:
  - Uses an injected `*http.Client` (from `auth.Client.HTTP`) and the same cookie jar.
  - Sets `Authorization: Bearer <skypetoken>` and `Accept: application/json`.
  - On non-2xx, reads the first 2048 bytes of the body, logs `status` + `body_snippet`, and returns an error containing both.
- Define `RemoteConversation` JSON with only the needed nested fields (`threadProperties.originalThreadId`, `threadProperties.productThreadType`, `threadProperties.createdat`, `threadProperties.isCreator`).
- Add `Thread` struct with fields: `ID`, `Type`, `CreatedAtRaw` (string or nullable), `IsCreator`, `IsOneToOne`.
- `Normalize()` returns `(Thread, bool)`; returns `false` if `originalThreadId` is missing so callers can log a warning and skip.
Tests
- `ListConversations` sends the correct Authorization header and parses `conversations[]` on 200.
- `ListConversations` truncates error body to 2048 bytes and returns a non-nil error on 4xx/5xx.
- `Normalize()` sets `IsOneToOne` for `productThreadType == "OneToOneChat"` and skips entries with empty IDs.

## Phase 2: Persistent Thread ↔ Room Mapping
Files
- `database/teams_thread.go`: add a `TeamsThread` model and query helpers.
- `database/database.go`: register `TeamsThread` query in `Database`.
- `database/upgrades/25-teams-thread-room.sql`: add new table for mappings and future metadata.
- `database/upgrades/00-latest-revision.sql`: include the new table in the latest schema snapshot.
Changes
- Add table `teams_thread` (or `teams_thread_room`) with:
  - `thread_id TEXT PRIMARY KEY`
  - `room_id TEXT UNIQUE`
  - `last_sequence_id TEXT NULL`
  - `last_message_ts BIGINT NULL`
  - `last_message_id TEXT NULL`
- Implement query helpers:
  - `GetByThreadID(threadID string) *TeamsThread`
  - `GetAll() []*TeamsThread` (for cache warm-up)
  - `Upsert()` to store room IDs and keep future nullable fields.
Tests
- None (DB helpers follow existing patterns; avoid integration tests here).

## Phase 3: Room Ensure + Wiring
Files
- `internal/bridge/store.go`: add a small Teams thread store that loads all rows on startup and persists updates.
- `internal/bridge/rooms.go`: implement `EnsureRoom(thread)` using a `RoomCreator` interface and structured logging.
- `cmd/teams-login/main.go`: run discovery + room creation in fail-fast mode after login/probe.
- `main.go` and/or `user.go`: wire guarded discovery on bridge startup (non-fatal on error).
Changes
- Add `TeamsThreadStore` with `LoadAll()`, `Get(threadID)`, and `Put(threadID, roomID)` backed by `database.TeamsThread`.
- Add `RoomsService` with dependencies (`TeamsThreadStore`, `RoomCreator`, `zerolog.Logger`) and `EnsureRoom(ctx, thread)`:
  - If mapping exists, log `DBG matrix room exists thread_id=<id> room_id=<id>` and return.
  - Otherwise call `RoomCreator.CreateRoom(...)`, persist mapping, and log `INF matrix room created room_id=<id> thread_id=<id>`.
- Define a `RoomCreator` implementation for the bridge bot that:
  - Uses existing bridge config defaults for encryption/room version (add `m.room.encryption` only if config enables it).
  - Forces `Visibility: private`, `CreationContent["m.federate"] = false`.
  - Sets `Preset` to `private_chat` for one-to-one, `private` for group.
  - Uses placeholder names ("Chat" or empty) per the ticket.
  - Ensures only the bot joins (no invites).
- Add a `DiscoverAndEnsureRooms(ctx, token, httpClient, store, creator, log)` helper that:
  - Calls `ListConversations`, normalizes threads, logs `INF teams thread discovered thread_id=<id> type=<type>`.
  - Skips threads without IDs (logs warn).
  - Calls `EnsureRoom` per thread.
- `teams-login`:
  - Load shared `auth.json`/`cookies.json` from the config directory, validate skypetoken, and call `DiscoverAndEnsureRooms`.
  - Initialize a minimal bot-only Matrix client using existing appservice registration helpers (avoid full `bridge.Bridge` startup).
  - On any error from listing or room creation, log `ERR ...` and exit non-zero (fail fast).
- Main bridge startup:
  - Load the same shared auth state/cookies once during `Start()` (or immediately after `Init()`), and run discovery in a goroutine.
  - Log and continue on errors; do not crash.
Tests
- Unit test `RoomsService.EnsureRoom` with a fake `RoomCreator` to verify idempotence and mapping persistence behavior.
- Unit test `DiscoverAndEnsureRooms` with a fake `ListConversations`/client and store to ensure skips + logging on missing IDs.
