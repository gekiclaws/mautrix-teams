## Open Questions
- None. `ConsumptionHorizonNow(now)` now returns `<now_ms>;<now_ms>;0`.

## Task Checklist
Phase 1
☑ Add Teams client support for outbound consumption-horizon read receipts using the existing executor.
☑ Add a small in-memory unread-cycle tracker and mark rooms unread only on inbound (non-send-intent) ingested messages.
☑ Unit test the receipts endpoint, unread tracker rules, and unread marking during ingest.
Phase 2
☑ Add a Teams consumer read-receipt sender and wire it into `TeamsConsumerPortal.HandleMatrixReadReceipt`.
☑ Ensure read receipts fire only once per unread cycle with no persistence or reconciliation.
☑ Unit test one-shot unread-cycle gating and thread resolution behavior.
Phase 3
☑ Add a dev hook to trigger a single outbound read receipt for a mapped room.

## Phase 1: Receipt Endpoint + Unread Cycle Tracking
Files
- `internal/teams/client/receipts.go` (new): implement consumption-horizon read receipt endpoint.
- `internal/teams/client/receipts_test.go` (new): validate receipts endpoint URL, headers, payload, helper, and typed errors.
- `internal/bridge/unread_cycle.go` (new): add an in-memory unread-cycle tracker with a narrow interface.
- `internal/bridge/unread_cycle_test.go` (new): unit test unread-cycle transitions and one-shot receipt gating.
- `internal/bridge/messages.go`: mark rooms unread only when ingesting inbound messages that are not send-intent echoes.
- `internal/bridge/messages_test.go`: add a focused test that inbound ingest marks unread while send-intent echoes do not.
Changes
- Implement consumption-horizon read receipts via the existing M3 send host and executor:
  - `Client.SetConsumptionHorizon(ctx context.Context, threadID string, horizon string) (int, error)`.
  - Endpoint: `PUT {SendMessagesURL}/{thread_id}/properties?name=consumptionhorizon`.
  - Payload: `{ "consumptionhorizon": <horizon> }`.
  - Add helper: `func ConsumptionHorizonNow(now time.Time) string` returning `<unix_ms>;<unix_ms>;0`.
  - Use `WithRequestMeta(ctx, RequestMeta{ThreadID: threadID, Operation: "teams receipt"})` so retry/backoff logs stay consistent and easy to filter.
  - Classification: same retry rules as existing send/reactions (429 + 5xx retryable), with a typed `ReceiptError` for other non-2xx responses.
- Add an in-memory unread-cycle tracker with no persistence:
  - New type `UnreadCycleTracker` with narrow interface:
    - `MarkUnread(roomID id.RoomID)`.
    - `ShouldSendReadReceipt(roomID id.RoomID) bool`.
  - Behavior:
    - `MarkUnread` sets `unread=true` and `receiptSent=false`.
    - `ShouldSendReadReceipt` returns true exactly once per unread cycle by flipping to `receiptSent=true` and `unread=false` on the first call.
  - Implement with `map[id.RoomID]unreadCycleState` guarded by `sync.Mutex`.
- Mark rooms unread only for inbound ingested messages:
  - Extend `MessageIngestor` with optional dependency:
    - `UnreadTracker interface { MarkUnread(roomID id.RoomID) }`.
  - In `IngestThread`, when a new Matrix message is sent (`intentMXID == ""` and `SendText` succeeds), call `UnreadTracker.MarkUnread(roomID)`.
  - Do not mark unread for send-intent echoes (`intentMXID != ""`), with an explicit comment documenting why.
Tests
- `internal/teams/client/receipts_test.go`:
  - Assert `PUT /conversations/{thread}/properties?name=consumptionhorizon`, required headers, and `{consumptionhorizon: ...}` body.
  - Verify `ConsumptionHorizonNow` returns three semicolon-separated parts with millisecond timestamps.
  - Non-2xx responses return `ReceiptError` with status and body snippet.
- `internal/bridge/unread_cycle_test.go`:
  - `MarkUnread` then `ShouldSendReadReceipt` returns true once, then false until another `MarkUnread`.
  - Repeated `MarkUnread` before read remains one-shot.
- `internal/bridge/messages_test.go`:
  - Use a small fake unread tracker that records rooms.
  - Inbound message without send intent marks unread.
  - Message with a matching send intent does not mark unread.

## Phase 2: Portal Wiring + Receipt Sender
Files
- `internal/bridge/read_receipts.go` (new): Teams consumer read-receipt sender that uses the unread-cycle tracker and consumption horizon endpoint.
- `internal/bridge/read_receipts_test.go` (new): unit test receipt sender one-shot unread-cycle gating.
- `teams_consumer_portal.go`: implement read-receipt handling and route to the receipt sender.
- `teams_consumer_rooms.go`: initialize the unread-cycle tracker and receipt sender, wiring unread tracking into ingest.
- `main.go`: add bridge fields for the tracker and receipt sender.
Changes
- Add a thin read-receipt sender that keeps state/time concerns separated:
  - New type `TeamsConsumerReceiptSender` with dependencies:
    - `Client interface { SetConsumptionHorizon(ctx context.Context, threadID, horizon string) (int, error) }`
    - `Threads ThreadLookup`
    - `Unread interface { ShouldSendReadReceipt(roomID id.RoomID) bool }`
    - `Log zerolog.Logger`
  - Method `SendReadReceipt(ctx context.Context, roomID id.RoomID, now time.Time) error`:
    - First consult `Unread.ShouldSendReadReceipt(roomID)`; if false, return nil without calling Teams.
    - Resolve `threadID` via `Threads.GetThreadID`.
    - Build horizon via `client.ConsumptionHorizonNow(now)`.
    - Call `SetConsumptionHorizon` and log attempt/response/error with `thread_id` and `room_id`.
- Teach `TeamsConsumerPortal` to handle read receipts:
  - Implement `HandleMatrixReadReceipt(brUser bridge.User, eventID id.EventID, receipt event.ReadReceipt)`.
  - Do not inspect per-message data; this is room-level only.
  - Call `TeamsConsumerReceiptSender.SendReadReceipt(context.Background(), portal.roomID, time.Now().UTC())`.
  - Swallow errors after logging (best-effort).
- Wire initialization:
  - Add bridge fields:
    - `TeamsUnreadCycles *teamsbridge.UnreadCycleTracker`
    - `TeamsConsumerReceipt *teamsbridge.TeamsConsumerReceiptSender`
  - In `runTeamsConsumerMessageSync`, initialize `TeamsUnreadCycles = teamsbridge.NewUnreadCycleTracker()` once per process and pass it into `MessageIngestor.UnreadTracker`.
  - In `initTeamsConsumerSender` (or a small extracted helper), initialize `TeamsConsumerReceiptSender` using the existing consumer client, thread store, logger, and unread tracker.
Tests
- `internal/bridge/read_receipts_test.go`:
  - With unread marked, first `SendReadReceipt` triggers exactly one client call.
  - A second `SendReadReceipt` without a new `MarkUnread` makes no additional client calls.
  - If unread was never marked, `SendReadReceipt` is a no-op.
  - Unmapped room returns an error without calling the client even when unread was marked.

## Phase 3: Dev Hook
Files
- `dev_read_receipt.go` (new): add a dev-only hook that marks a room unread in-memory and sends a single read receipt.
- `main.go`: add a `dev-read-receipt` gate before other dev hooks.
Changes
- Implement `dev-read-receipt --room <room_id> [--config <path>]`:
  - Load config, DB, and Teams consumer auth the same way as `dev-send`/`dev-react`.
  - Initialize `TeamsThreadStore`, `UnreadCycleTracker`, and `TeamsConsumerReceiptSender`.
  - Force an unread cycle with `unread.MarkUnread(roomID)` to make the dev hook deterministic.
  - Send a single receipt via `SendReadReceipt(context.Background(), roomID, time.Now().UTC())`.
