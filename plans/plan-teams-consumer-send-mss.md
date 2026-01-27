## Open Questions
- None.

## Task Checklist
Phase 1
☐ Add a Teams Consumer send client that builds the minimal payload, warns on malformed thread IDs, and records response details with unit tests.
Phase 2
☐ Add a persisted Teams send-intent store (DB schema + query helpers) for MSS transitions.
Phase 3
☐ Wire Matrix → Teams Consumer sending with provisional MSS metadata (bridge-local blob) and logging, using the send-intent store.

## Phase 1: Consumer Send API + Payload Validation
Files
- `internal/teams/client/messages.go`: add a send API and supporting request/response helpers.
- `internal/teams/auth/state.go`: store the current Teams user ID (derived from skypetoken response/JWT) for outbound payloads.
- `internal/teams/auth/skype_token.go`: capture and return `skypeid` alongside the token so it can be persisted in state.
- `cmd/teams-login/main.go`: persist the Teams user ID when refreshing the skypetoken.
- `internal/teams/client/messages_test.go`: add send request tests (URL escaping, headers, payload, non-2xx handling).
- `internal/teams/auth/skype_token_test.go`: update tests for the extra `skypeid` extraction.
Changes
- Add a `defaultSendMessagesURL` constant: `https://teams.live.com/api/chatsvc/consumer/v1/users/ME/conversations` and a `SendMessagesURL` field on `client.Client` (defaulted in `NewClient`).
- Implement `Client.SendMessage(ctx, threadID, text, fromUserID string) (clientMessageID string, err error)` that:
  - Hard-fails on missing HTTP client, missing token, missing/whitespace `threadID`, or empty message text.
  - Uses `url.PathEscape(threadID)` when building `POST {SendMessagesURL}/{thread_id}/messages`.
  - Sets `authentication: skypetoken=<token>` (exact casing/value), `Accept: application/json`, and `Content-Type: application/json`.
  - Builds the minimal JSON payload with required fields:
    - `type: "Message"`
    - `conversationid: <thread_id>`
    - `content: "<p>{text}</p>"` (escape text and normalize newlines safely before wrapping in `<p>`; avoid double-escaping).
    - `messagetype: "RichText/Html"`
    - `contenttype: "Text"`
    - `clientmessageid: <generated>` (numeric by default; add regex test)
    - `composetime` and `originalarrivaltime`: `time.Now().UTC().Format(time.RFC3339Nano)`
    - `from` and `fromUserId`: the normalized Teams user ID (`8:*`).
  - On non-2xx, read and return a truncated body snippet (2KB) in a typed error (mirroring `MessagesError`).
- Warn on malformed thread IDs by checking for `@thread.v2` (no hard block).
- Extend `AuthState` with `TeamsUserID` (string) and persist it in `auth.json` (store normalized `8:*` only); if missing during login refresh, keep any previously persisted value as fallback.
- Update `AcquireSkypeToken` to return `skypeid` so `cmd/teams-login` can normalize once and persist; update tests accordingly.
Tests
- `SendMessage` URL-escapes the thread ID and hits `/conversations/{escaped}/messages`.
- `SendMessage` sets the `authentication` header and JSON content type.
- `SendMessage` payload includes required fields only and uses the generated `clientmessageid`.
- `SendMessage` uses the exact escaped path and `clientmessageid` matches a numeric regex.
- `SendMessage` returns a typed error with status + body snippet on non-2xx.

## Phase 2: Persisted Send-Intent Store
Files
- `database/teams_send_intent.go`: new query + model for send-intents.
- `database/database.go`: register `TeamsSendIntent` on `Database`.
- `database/upgrades/28-teams-send-intent.sql`: add the send-intent table.
- `database/upgrades/00-latest-revision.sql`: include the new table in the latest schema snapshot.
Changes
- Add table `teams_send_intent` with columns:
  - `thread_id TEXT NOT NULL`
  - `client_message_id TEXT PRIMARY KEY`
  - `timestamp BIGINT NOT NULL` (Unix millis)
  - `status TEXT NOT NULL` (values: `pending`, `accepted`, `failed`)
- Implement query helpers:
  - `Insert(intent *TeamsSendIntent) error`
  - `UpdateStatus(clientMessageID string, status string) error`
  - `GetByClientMessageID(clientMessageID string) *TeamsSendIntent` (optional, for diagnostics).
- Keep the status as a string enum (validate to the three allowed values on insert/update).
Tests
- Optional: add a lightweight schema smoke test that applies the new upgrade in-memory to validate migration shape.

## Phase 3: Matrix → Teams Consumer Send + MSS Wiring
Files
- `internal/bridge/store.go`: extend the Teams thread store with reverse lookup (room → thread ID).
- `internal/bridge/send.go` (new): add a Teams consumer sender that creates send-intents, sends messages, and updates MSS.
- `main.go` (or a new `teams_consumer_portal.go`): wire Matrix event handling for Teams Consumer rooms.
Changes
- Extend `TeamsThreadStore` with `GetThreadID(roomID id.RoomID) (string, bool)` by maintaining a `byRoomID` map updated in `LoadAll()` and `Put()`.
- Add a `TeamsConsumerSender` with dependencies:
  - `Client *client.Client` (skypetoken-aware),
  - `SendIntents *database.TeamsSendIntentQuery`,
  - `Threads *teamsbridge.TeamsThreadStore`,
  - `UserID string` (current Teams user ID),
  - `Log zerolog.Logger`.
- Implement `SendMatrixText(ctx, roomID, body, eventID)` that:
  - Resolves `thread_id` via the thread store; hard-fails if missing or malformed.
  - Creates a send-intent with `pending` status before the HTTP request.
  - Calls `Client.SendMessage(...)` and transitions MSS to `accepted` (2xx) or `failed` (non-2xx/error).
  - Logs request attempt, response status, and status transitions.
  - Emits provisional MSS metadata on the Matrix event immediately using `com.beeper.teams.mss` with minimal fields:
    - `status`: `pending` | `accepted` | `failed`
    - `client_message_id`: numeric ID used for the send-intent
    - `ts`: Unix millis for the MSS write time
- Wire event handling for Teams consumer rooms:
  - If a Matrix message arrives in a room mapped in `teams_thread`, route only `m.text` messages to `TeamsConsumerSender`.
  - Ensure errors are logged and do not crash the daemon.
Tests
- Unit test for `TeamsConsumerSender.SendMatrixText` using an `httptest` server to assert pending → accepted/failed transitions and intent persistence.
- Unit test for `TeamsThreadStore` reverse lookup (room → thread ID) consistency after `LoadAll()` and `Put()`.
