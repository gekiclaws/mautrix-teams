FLAGGED OPEN QUESTIONS
- What is the exact Teams messages API query parameter for “since last sequence” (e.g., `since`, `startTime`, or another name), and should we send it as a query string or header? (Non-blocking for A3; only add if confirmed by traffic capture.)

TASK CHECKLIST
Phase 1 — Teams incremental fetch (optional, non-blocking)
☐ Only if confirmed by traffic capture: add optional sinceSequence query parameter behind a constant
☐ Ensure client still orders messages via CompareSequenceID
☐ Add unit tests for optional parameter handling and mixed sequence ordering

Phase 2 — Sync engine
☑ Add SyncThread wrapper that loads last_sequence_id, sends via MessageIngestor, and persists once per thread
☑ Enforce stop-on-first-send-failure and skip empty-body messages with DEBUG log
☑ Add unit tests for no-resend, stop-on-failure, resume-after-failure, and empty-body skip

Phase 3 — teams-login wiring + logging
☑ Invoke sync per @thread.v2 thread after room discovery; skip non-@thread.v2 with DEBUG log
☑ Continue syncing other threads on per-thread failure; exit non-zero only on DB/persistence failures, Matrix client init failure, or global fetch failure (cannot list conversations)
☑ Add required structured logs for sync start/discovered/sent/complete and persistence errors

PHASE 1 — Teams incremental fetch (optional, non-blocking)
Files + changes
- internal/teams/client/messages.go: if the exact param name is confirmed, add optional sinceSequence query parameter behind a constant; keep sorting by CompareSequenceID.
- internal/teams/client/messages_test.go: add coverage for query parameter inclusion/omission and keep mixed numeric/string ordering tests aligned with CompareSequenceID.

Implementation notes
- This phase is optional and should be skipped entirely for A3 unless the parameter name is confirmed via captured traffic.
- If implemented, build the messages URL with url.Values so the optional sinceSequence parameter is encoded safely.
- If implemented, be explicit that server-side filtering may occur when the param is present.

Unit tests
- messages_test.go: verify when sinceSequence is non-empty, the request URL includes the query parameter; when empty, the URL has no since parameter.
- messages_test.go: ensure ordering uses CompareSequenceID when sequences mix numeric and string values.

PHASE 2 — Sync engine
Files + changes
- internal/bridge/sync.go: add a SyncThread function that wraps MessageIngestor to load last_sequence_id, send in order, and persist only after successful sends.
- internal/bridge/messages.go: keep MessageIngestor focused on fetch + filter + send (single owner for filtering); adjust return/error signaling so a send failure stops the thread without advancing sequence.
- internal/bridge/sync_test.go: add tests for persistence and restart-safe behavior.
- database/teams_thread.go: add a narrow UpdateLastSequenceID(threadID, seq) helper to persist progress atomically per thread.

Implementation notes
- Sync flow: log sync start with last_sequence_id, call MessageIngestor (which does ListMessages + filter + send), skip empty bodies with DEBUG, stop on first send failure, then persist highest successfully sent sequence once per thread.
- Persistence rule: only update last_sequence_id after at least one successful send; never update on fetch/parse/send failures.
- Use CompareSequenceID for all comparisons to keep numeric-vs-string ordering consistent.

Unit tests
- sync_test.go: no resend on second run (last_sequence_id persisted from first run and no sends on next run).
- sync_test.go: stop on failed send; later messages are not sent and last_sequence_id is unchanged.
- sync_test.go: resume correctly after failure (first run stops, second run resumes and persists).
- sync_test.go: empty-body messages are skipped (no sends) but do not block later messages.

PHASE 3 — teams-login wiring + logging
Files + changes
- cmd/teams-login/main.go: after DiscoverAndEnsureRooms, iterate threads, skip non-@thread.v2 with DEBUG log, invoke SyncThread per thread, log per-thread success/failure, and continue on per-thread errors.
- internal/bridge/sync.go or cmd/teams-login/main.go: add structured logging fields for sync lifecycle and persistence errors.

Implementation notes
- Logging (zerolog):
  - INFO teams sync start thread_id=<id> last_seq=<n>
  - INFO teams message discovered thread_id=<id> seq=<n>
  - INFO matrix message sent room_id=<id> seq=<n>
  - INFO teams sync complete thread_id=<id> new_last_seq=<n>
  - DEBUG teams message skipped empty body thread_id=<id> seq=<n>
  - ERROR failed to send matrix message thread_id=<id> room_id=<id> seq=<n> err=<err>
  - ERROR failed to persist last_sequence_id thread_id=<id> err=<err>
- Exit non-zero only on DB schema/migration failure, DB read/write failures for mapping/sequence, Matrix client init failure, or the agreed “global fetch failure” (cannot list conversations/threads at all).

Unit tests
- (No new tests in this phase; logging is covered by sync tests and behavior is verified via error handling paths.)
