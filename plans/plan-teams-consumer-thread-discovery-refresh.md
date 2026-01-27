Open Questions
- None. Resolved decisions:
- Discovery refresh runs only inside `runTeamsConsumerMessageSync`.
- Threads with empty `RoomID` are treated as “known” and are not repaired by refresh.

Task Checklist
Phase 1
☑ Extract thread discovery into a reusable, pure discoverer that returns normalized threads with no DB writes.
☑ Add unit tests for discovery normalization and missing-thread filtering.

Phase 2
☑ Add a thread refresh helper that diffs against the thread store and ensures rooms only for new threads.
☑ Add unit tests that verify only new threads are registered and that unchanged threads do not trigger room creation.

Phase 3
☑ Wire a periodic discovery refresh loop into `runTeamsConsumerMessageSync`.
☑ Feed newly registered threads into the existing polling loop via a channel and initialize their backoff state.
☑ Add the required refresh and registration logs without per-thread spam for unchanged threads.

Phase 1: Reusable Thread Discovery
Files
- `internal/bridge/teams_consumer_threads.go` (new): introduce `TeamsThreadDiscoverer` and its `Discover` method.
- `internal/bridge/rooms.go`: refactor `DiscoverAndEnsureRooms` to use the new discoverer.
- `internal/bridge/rooms_test.go`: add focused tests for discovery behavior via fakes.

Changes
- Add a small, value-oriented discoverer that only performs remote discovery + normalization:

```go
type TeamsThreadDiscoverer struct {
	Lister ConversationLister
	Token  string
	Log    zerolog.Logger
}

func (d *TeamsThreadDiscoverer) Discover(ctx context.Context) ([]model.Thread, error)
```

- `Discover` should:
- Call `Lister.ListConversations(ctx, Token)`.
- Normalize each conversation via `conv.Normalize()`.
- Skip entries with missing thread IDs.
- Return the slice of normalized `model.Thread` values.
- Keep existing `ConversationsError` logging behavior by reusing the same error classification currently in `DiscoverAndEnsureRooms`.
- Refactor `DiscoverAndEnsureRooms` to:
- Instantiate a `TeamsThreadDiscoverer`.
- Call `Discover(ctx)`.
- Keep its responsibility limited to “ensure rooms for discovered threads.”

Tests
- In `internal/bridge/rooms_test.go`, add:
- `TestTeamsThreadDiscovererSkipsMissingThreadID`: fake lister returns one missing ID and one valid ID; assert only the valid thread is returned.
- `TestTeamsThreadDiscovererNormalizesThreadFields`: fake lister returns a conversation with whitespace-padded IDs and a known thread type; assert IDs are trimmed and `IsOneToOne` is set as expected.

Phase 2: Diff + Register New Threads
Files
- `internal/bridge/thread_refresh.go` (new): implement a pure-ish refresh helper that composes discovery, diffing, and room ensuring for new threads only.
- `internal/bridge/rooms_test.go`: add refresh unit tests using existing fakes.

Changes
- Introduce a refresh helper that keeps concerns side-by-side and easy to test:

```go
type ThreadRegistration struct {
	Thread model.Thread
	RoomID id.RoomID
}

func RefreshAndRegisterThreads(ctx context.Context, discoverer *TeamsThreadDiscoverer, store ThreadStore, rooms *RoomsService, log zerolog.Logger) ([]ThreadRegistration, error)
```

- Behavior:
- Call `discoverer.Discover(ctx)` and record `discovered` count.
- For each discovered thread:
- Use `store.Get(thread.ID)` to detect known threads.
- Only for unknown threads:
- Call `rooms.EnsureRoom(thread)` to persist the thread and obtain a room ID.
- Append `{Thread: thread, RoomID: roomID}` to the result slice.
- Emit required summary logs at the refresh call site (Phase 3), not here, so the helper remains composable.
- Add an explicit idempotency comment in `RefreshAndRegisterThreads`:
- `// RefreshAndRegisterThreads is idempotent. It only registers threads that do not already exist in the store.`

Tests
- In `internal/bridge/rooms_test.go`, add:
- `TestRefreshAndRegisterThreadsRegistersOnlyNew`: pre-seed the fake store with `thread-1`, discover `thread-1` and `thread-2`, assert only `thread-2` is returned and the creator is called once.
- `TestRefreshAndRegisterThreadsNoNewThreadsNoCreates`: pre-seed the fake store with all discovered threads, assert zero registrations and zero creator calls.

Phase 3: Periodic Refresh + Polling Integration
Files
- `teams_consumer_rooms.go`: add the refresh loop, integrate new threads into polling, and add required logging.

Changes
- Keep the polling loop single-threaded and un-braided by introducing a channel that feeds new DB threads into the poller:

```go
newThreadsCh := make(chan *database.TeamsThread, 32)
```

- Extract refresh concerns into a separate goroutine and keep token + discovery logic alongside message sync setup:
- Hardcode the refresh interval to `10 * time.Minute`.
- Use a ticker, and also run one refresh immediately before entering the poll loop.
- Suggested structure inside `runTeamsConsumerMessageSync` (and only here; do not add refresh to `runTeamsConsumerRoomSync`):
1. Build shared dependencies once: `store`, `rooms`, `discoverer`.
2. Define a `refreshOnce(ctx)` closure that:
- Logs `"teams thread discovery refresh start"`.
- Calls `teamsbridge.RefreshAndRegisterThreads(...)`.
- For each registration:
- Load the DB row via `br.DB.TeamsThread.GetByThreadID(reg.Thread.ID)`.
- If non-nil, send it on `newThreadsCh`.
- Log `"teams thread registered"` with `thread_id` and `room_id`.
- Logs `"teams thread discovery refresh complete"` with `discovered` and `new` counts.
- Emit this completion log unconditionally, including when `new == 0`, to make refresh liveness and Teams API reachability observable.
3. Start a goroutine that runs `refreshOnce(ctx)` immediately, then on each ticker tick.

- Update the polling loop to accept dynamic thread additions without sharing mutable slices across goroutines:
- Replace the static `threads := br.DB.TeamsThread.GetAll()` with a mutable slice local to the poll loop, e.g. `threads := br.DB.TeamsThread.GetAll()` followed by `threadsByID := map[string]*database.TeamsThread{...}`.
- When receiving a thread from `newThreadsCh`:
- If the thread ID already exists in `threadsByID`, ignore it.
- Otherwise, store it in the map, append to the slice, and initialize its `threadPollState` with base delay and `NextPollAt = time.Now().UTC()`.
- Ensure the poll loop’s main select can wake for either the sleep timer or new threads:
- After computing `sleepFor`, create a timer and `select` on `ctx.Done()`, `timer.C`, and `newThreadsCh`.
- If a new thread arrives, stop the timer (drain if needed) and continue the loop to re-evaluate due work immediately.

Tests
- Do not add time-based loop tests. Rely on the Phase 1 and Phase 2 unit tests plus existing poll backoff tests.
