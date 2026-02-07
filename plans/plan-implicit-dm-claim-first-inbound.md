FLAGGED OPEN QUESTIONS
- None.

TASK CHECKLIST
Phase 1 — Extract a shared management-room claim primitive
☑ Rename/generalize invite-only claim helper to a trigger-agnostic helper
☑ Keep invite-trigger behavior unchanged by routing existing invite paths through the new helper
☑ Add unit tests covering already-joined handling and unchanged invite behavior

Phase 2 — Add implicit DM claim pre-handler on inbound `m.room.message`
☑ Register a bridge-local pre-handler for `event.EventMessage` using `EventProcessor.PrependHandler`
☑ Enforce trigger conditions: real user sender, no existing management room, and validated 1:1 membership `{sender, bot}`
☑ Persist management room + readiness message via the shared helper, with idempotent no-op on subsequent messages
☑ Add unit tests for success, non-DM skip, join failure skip, and membership-fetch failure skip

Phase 3 — Tighten observability and regression coverage
☑ Emit one activation log per user claim path with trigger metadata (`trigger=implicit_dm` vs `trigger=invite`)
☑ Add a unit test asserting no repeated activation when management room already exists
☑ Run focused Go tests for management-room claim flows

PHASE 1 — Extract a shared management-room claim primitive
Files + changes
- `main.go`: replace `claimManagementRoomOnInvite` with a neutral helper (e.g. `claimManagementRoom`) that accepts trigger context, joins by room ID, persists management room, and sends readiness message.
- `main.go`: keep `CreatePrivatePortal` and `handleBotInviteManagementRoomClaim` behavior unchanged by delegating into the shared helper.
- `teams_private_portal_test.go`: update existing invite-claim tests to target the shared helper name and add coverage for “already joined” join errors being treated as success.

Implementation notes
- Keep the helper narrowly scoped: join room, set management room, send `Teams bridge ready. Use !login to activate.`.
- Preserve existing semantics:
  - Join failure aborts persistence/message.
  - Send failure logs warning but keeps persisted management room.
- Add a small join-error classifier (e.g. `isAlreadyJoinedJoinError(err)`) so idempotent claim attempts continue when Matrix returns an already-joined response.
- Include a `trigger` field in claim logs to distinguish invite vs implicit DM activation without duplicating logic.

Unit tests
- `teams_private_portal_test.go`: existing success/join-failure/send-failure invite tests remain green against the shared helper.
- `teams_private_portal_test.go`: new case where join returns an already-joined error; assert management room is persisted and readiness message is attempted.
- `teams_private_portal_test.go`: invite-handler direct invite case still claims room; non-direct invite remains no-op.

PHASE 2 — Add implicit DM claim pre-handler on inbound `m.room.message`
Files + changes
- `main.go`: in `Init()`, register a new handler with `br.EventProcessor.PrependHandler(event.EventMessage, br.handleImplicitDMManagementRoomClaim)`.
- `main.go`: add `handleImplicitDMManagementRoomClaim(evt *event.Event)` and small helpers for trigger checks and DM membership validation.
- `teams_private_portal_test.go`: extend HTTP test server behavior with `/joined_members` responses and add pre-handler tests.

Implementation notes
- Trigger checks in handler (all required):
  - `evt.Type == event.EventMessage`.
  - Sender is neither bot nor ghost.
  - Sender resolves to a local `*User` with sufficient permission level.
  - `user.GetManagementRoomID() == ""`.
- Activation flow:
  - Attempt bot join by room ID through shared claim helper path (already-joined tolerated).
  - Validate DM membership using `bot.JoinedMembers(roomID)` after join.
  - Accept only rooms with exactly 2 joined users and exact membership `{evt.Sender, br.Bot.UserID}`.
  - If valid, claim/persist management room and send readiness message via shared helper.
  - If invalid or membership fetch fails, abort without persistence.
  - If membership is invalid after a successful join, do not auto-leave; keep the bot joined and skip claim.
- Keep this pre-handler bridge-local and early, without modifying mautrix core.
- Preserve the same ordering guarantee in both `appservice.async_transactions=true` and `false`: this handler must execute before command and portal routing for each `m.room.message` by using `EventProcessor.PrependHandler`.
- Keep portal resolution and command routing untouched; this pre-handler only establishes the missing lifecycle edge.

Unit tests
- `teams_private_portal_test.go`: implicit claim success from first inbound message in a 2-member room (`sender + bot`) joins, persists room, sends readiness message.
- `teams_private_portal_test.go`: room with extra members does not claim management room and does not send readiness.
- `teams_private_portal_test.go`: hard join failure aborts claim.
- `teams_private_portal_test.go`: joined-members fetch failure aborts claim after join attempt.

PHASE 3 — Tighten observability and regression coverage
Files + changes
- `main.go`: add structured log fields on activation attempts/results to make implicit activation visible in Beeper logs.
- `teams_private_portal_test.go`: add regression case for idempotency when management room already set.

Implementation notes
- Log once at successful claim with consistent fields: `user`, `room_id`, `trigger`.
- Skip log spam on subsequent messages by short-circuiting early when management room already exists.

Unit tests
- `teams_private_portal_test.go`: when user already has `ManagementRoom`, implicit handler should no-op (no join/send/persist mutation).

Validation commands
- `go test ./...`
- `go test ./... -run 'Test(ClaimManagementRoom|HandleBotInviteManagementRoomClaim|HandleImplicitDMManagementRoomClaim)'`
