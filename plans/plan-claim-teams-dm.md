Open questions:
- None; please flag any additional expectations for DM claim behavior before implementation.

☑ Phase 1 – Implement idempotent DM claim helper and hook it to CreatePrivatePortal.
☐ Phase 2 – Unit test the claim helper with stubbed bridge user/bot behaviors.

Phase 1 – Implement idempotent DM claim helper and hook CreatePrivatePortal
Files:
- `main.go`: add an internal helper that joins the DM, persists the management room, and sends the confirmation message; have `CreatePrivatePortal` delegate to it so the logic stays centralized and Teams-specific.

Plan:
1. Create an unexported helper that accepts the bridge instance (`*TeamsBridge`), the bot intent (`*appservice.IntentAPI`), the management room ID, and the concrete `*User`. There is no need for an extra interface since the helper remains Teams-specific.
2. The helper should:
   - Return immediately if the provided room ID or user is empty.
   - Attempt to `JoinRoomByID(roomID)` and only log success after the join completes; any failure should be logged at warn level and short-circuit the helper so no persistence or messaging occurs.
   - After a successful join, log that the room is being claimed, call `user.SetManagementRoom(roomID)` to persist the management room, and send `Teams bridge ready. Use !login to activate.` via `SendMessageEvent`. Log any send failure (warn/debug) but still keep the management room persisted.
   - Be idempotent: repeated calls for the same room can safely rejoin and re-mark the room without additional guards—Matrix tolerates joining an already-joined room, and `SetManagementRoom` can be called repeatedly.
   - Strictly limit itself to joining, persisting the room, and announcing readiness; do not load auth.json, validate tokens, or start Teams consumers (that remains the job of `!login`).
3. Have `TeamsBridge.CreatePrivatePortal` type assert the incoming `bridge.User` to `*User`, and if successful, invoke the helper with `br.Bot`, the room ID, and that user.

Tests:
- None in this phase; helper tests live in Phase 2.

Phase 2 – Unit test the DM claiming helper
Files:
- `main_test.go` or a new `teams_private_portal_test.go`: add focused cases that exercise the helper with stubbed bot/user implementations.

Plan:
1. Create a lightweight fake implementation of the `bridge.User` interface that records the room passed to `SetManagementRoom`, exposes a deterministic `GetMXID`, and leaves all other methods as no-ops so we avoid any database interaction.
2. Create a fake bot implementing the helper's interface that records whether `JoinRoomByID` and `SendMessageEvent` were called, their arguments, and can simulate success or failure by toggling an injected error.
3. Write at least two unit tests:
   - Success path: the fake bot reports no errors, so the helper should join once, call `SetManagementRoom` once with the provided room, and send the confirmation message with the exact body text. Assert that these calls happened and that no extra join/send attempts occurred.
   - Join failure path: the fake bot returns an error for `JoinRoomByID`. Assert the helper skips persisting the management room and never calls `SendMessageEvent`, keeping the user/map unchanged. Logging can be ignored, but the helper should not panic or attempt the send.
4. Optionally, add a third case where `SendMessageEvent` fails even though the join succeeded; assert that the management room is still stored so `!login` remains possible and that the helper surfaces (i.e., logs) the failure without disrupting the stored state. The fake bot can record that the send method saw the expected room/message.

Tests should live alongside the helper so they can call it directly, and they must avoid touching the real database by relying exclusively on the fake user/bot implementations.
