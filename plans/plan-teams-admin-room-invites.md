## Open Questions
- None.

## Task Checklist
Phase 1
☑ Add explicit admin-invite plumbing as a room-ensure invariant, with idempotent and non-fatal invite handling.
Phase 2
☑ Wire startup/discovery flows so all ensured rooms (new and pre-mapped) execute the same admin-invite invariant.
Phase 3
☑ Add resolver and behavior tests for explicit-admin selection, single-warning semantics, and best-effort invite outcomes.

## Phase 1: Room-Level Admin Invite Primitive
Files
- `internal/bridge/rooms.go`: add admin invite dependencies (admin MXID set + invite sender), and run invite checks from `EnsureRoom` for both existing and newly created mappings.
- `teams_consumer_rooms.go`: pass configured admin MXID(s) into `RoomsService` during construction.
Changes
- Extend `RoomsService` with explicit invite configuration and collaborators, keeping room creation concerns separate from invite transport:
  - Add a small inviter interface in `internal/bridge/rooms.go` (e.g. `InviteUser(ctx, roomID, userID)` plus optional membership read helper if needed).
  - Add a deterministic list of target admin MXIDs resolved from `bridge.permissions` (explicit MXID keys with `admin` level only; ignore `*` and bare-domain keys).
- Update `EnsureRoom(thread)` behavior:
  - Existing mapping path: after confirming/storing room mapping, call `ensureAdminsInvited(roomID, thread.ID)`.
  - New room path: after create + persist mapping, call `ensureAdminsInvited(roomID, thread.ID)`.
  - This makes admin membership handling an invariant of room-ensure logic for every caller.
- Implement idempotency and best-effort behavior without introducing membership-sync complexity:
  - Prefer a lightweight membership check (`m.room.member` for target user) if available in current state store/client helper.
  - If membership check is unavailable/unreliable, rely on Matrix idempotent invite semantics and treat “already in room / already invited” as success.
  - Invite failures are logged and skipped per-admin; they do not fail `EnsureRoom`, discovery, or startup.
- Keep logging structured and low-noise (`room_id`, `thread_id`, `admin_mxid`, `result`) at debug level by default.
Tests
- In `internal/bridge/rooms_test.go`, add tests for `EnsureRoom` verifying:
  - Existing room mapping still triggers one invite attempt per configured explicit admin MXID.
  - Newly created room triggers invite after successful create/persist.
  - “already joined/invited” invite response is treated as success.
  - Generic invite errors are non-fatal and do not fail `EnsureRoom`.

## Phase 2: Startup + Discovery Coverage for Mapped Rooms
Files
- `teams_consumer_rooms.go`: add startup reconciliation for existing `teams_thread` mappings and keep discovery/new-thread registration behavior unchanged except for invite hook usage.
- `internal/bridge/store.go`: (if needed) expose/read existing room mappings in a form reusable by reconciliation without duplicating DB query logic.
- `internal/bridge/rooms.go`: add a small helper for reconciling admin invites across known mappings (separate from create/discover path).
Changes
- Keep one invitation code path by routing startup/discovery through room ensure:
  - For persisted mappings, run a startup reconciliation that executes the same `EnsureRoom`/`ensureAdminsInvited` invariant rather than a separate invite implementation.
  - For each row with non-empty `room_id`, ensure the room mapping and admin invites with shared logic.
- Keep discovery/refresh flow incremental and unchanged in shape:
  - `DiscoverAndEnsureRooms` and `RefreshAndRegisterThreads` continue to call `EnsureRoom`, which now always enforces admin invite semantics.
- Preserve scope boundaries from ticket:
  - No ghost/member sync loops.
  - No power-level mutations.
  - No Beeper/UI-specific branches.
Tests
- In `internal/bridge/rooms_test.go`, add/extend tests around discovery paths:
  - `DiscoverAndEnsureRooms` on already mapped threads still executes admin invite path (without recreating rooms).
  - Refresh registration for new threads creates room + invites admin; known threads do not recreate rooms.
- If reconciliation helper is added, add table-driven tests for:
  - Skipping empty room IDs.
  - Inviting each mapped room via the shared ensure path exactly once per run.

## Phase 3: Permission Resolution + Guardrails Tests
Files
- `teams_consumer_rooms.go` and/or a new small helper file under `internal/bridge/` for resolving admin MXIDs from `bridge.permissions`.
- `internal/bridge/rooms_test.go` (or a focused new test file like `internal/bridge/admin_invites_test.go`): validate resolver behavior with realistic permission maps.
Changes
- Add a narrow resolver that converts `bridge.permissions` into invite targets:
  - Include all explicit Matrix user IDs whose permission level is `admin`.
  - Exclude wildcard and domain entries.
  - Produce stable ordering for deterministic tests/logs.
- If resolver returns no explicit admin MXIDs, log a clear warning once per startup and skip invite attempts rather than failing room creation/discovery.
Tests
- Add resolver unit tests covering:
  - Single explicit admin MXID.
  - Multiple explicit admin MXIDs.
  - Domain/wildcard-only config yields empty invite target set.
  - Mixed config returns only explicit MXIDs, in stable order.
  - No explicit admins emits one startup warning, not one warning per room.
