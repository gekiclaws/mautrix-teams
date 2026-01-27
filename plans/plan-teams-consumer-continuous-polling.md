Open Questions
- Resolved: Poll only the threads present at startup for this ticket, and add a TODO to periodically re-run discovery to pick up new conversations.
- Resolved: Do not add `sync_state`; treat `last_sequence_id` as the only persisted cursor.
- Resolved: Use a 30s interval with `time.Sleep` (no ticker).

Task Checklist
Phase 1
☑ Update `internal/bridge/sync.go` to keep the in-memory thread cursor in sync (set `thread.LastSequenceID` when persistence succeeds).
☑ Add `internal/bridge/teams_consumer_ingest.go` with `TeamsConsumerIngestor.PollOnce(ctx, thread)` to wrap `ThreadSyncer.SyncThread` and centralize per-thread ingest logging.
☑ Add a unit test in `internal/bridge/sync_test.go` that asserts `SyncThread` updates `thread.LastSequenceID` on success.

Phase 2
☑ Update `teams_consumer_rooms.go` to run a continuous polling loop (single loop over threads) with a fixed 30s `time.Sleep`, logging loop start, tick, per-thread fetched count, reactions ingested, and sleep duration; keep running across per-thread errors and add a TODO for periodic re-discovery.
☑ Update `internal/bridge/messages.go` to log `teams messages fetched` (count) and `teams reactions ingested` once per thread poll, without per-message noise.

Phase 1: Extract per-thread poll + cursor updates
Files
- `internal/bridge/sync.go`: set `thread.LastSequenceID` after a successful `UpdateLastSequenceID` to keep in-memory state aligned with the DB cursor.
- `internal/bridge/teams_consumer_ingest.go`: introduce `TeamsConsumerIngestor` with dependencies (`Syncer`, `Log`) and a `PollOnce` method that validates the thread and delegates to `SyncThread`.
- `internal/bridge/sync_test.go`: add `TestSyncThreadUpdatesThreadState` (or similar) to verify `thread.LastSequenceID` is updated after a successful sync.

Changes
- Keep `ThreadSyncer.SyncThread`’s responsibilities limited: ingest, persist cursor, update in-memory cursor; avoid adding loop logic here.
- `TeamsConsumerIngestor.PollOnce` should only do lightweight validation/logging and call `SyncThread` to keep concerns separated.

Tests
- `internal/bridge/sync_test.go`: new test uses existing fakes to assert `thread.LastSequenceID` is set to the persisted value after a successful sync.

Phase 2: Continuous polling loop + structured logs
Files
- `teams_consumer_rooms.go`: replace the one-shot `for` loop with a continuous polling loop (30s `time.Sleep`) that iterates threads each tick and calls `TeamsConsumerIngestor.PollOnce` per thread; add a TODO for periodic thread discovery refresh.
- `internal/bridge/messages.go`: log `teams messages fetched` with count immediately after `ListMessages`, and log `teams reactions ingested` once per poll when the `ReactionIngestor` is present.

Changes
- Build the `MessageIngestor`, `ThreadSyncer`, and `TeamsConsumerIngestor` once, then run the poll loop without exiting after the first pass.
- Emit required logs: loop start, poll tick, messages fetched count, reactions ingested (per thread), and sleep duration; avoid per-message logs.
- Keep polling single-threaded per tick; no per-thread goroutines for this ticket.
- Handle per-thread errors by logging and continuing to the next thread/tick so the loop remains live.

Tests
- No new unit tests required beyond Phase 1 (poll loop is time-based; correctness of per-thread ingest is covered by existing `ThreadSyncer` and `MessageIngestor` tests).
