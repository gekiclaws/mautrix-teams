FLAGGED OPEN QUESTIONS
- Sequence comparison: implement numeric when both parse as uint64, otherwise fall back to strings.Compare.
- Failed Matrix send: stop processing the thread on the first send failure; do not advance last_sequence_id.
- Empty-body messages: log at DEBUG and skip.

TASK CHECKLIST
Phase 1 — Teams messages client + normalization
☑ Add Teams consumer messages client with error handling and ordering
☑ Add minimal message model normalization + parsing
☑ Add unit tests for parsing, missing fields, ordering, and non-2xx errors

Phase 2 — Deduplication + persistence
☑ Add sequence comparison + filtering using last_sequence_id
☑ Persist last_sequence_id updates only after successful sends
☑ Add unit tests for dedup sequence handling and persistence decisions

Phase 3 — Matrix send + teams-login wiring + logging
☑ Send messages into Matrix rooms via bot client
☑ Wire one-shot ingestion into teams-login after room bootstrap
☑ Add structured logging for discovery, send, persist, and errors

PHASE 1 — Teams messages client + normalization
Files + changes
- internal/teams/client/messages.go: add ListMessages(ctx, threadID, sinceSequence) to fetch consumer messages with Authorization: Bearer, parse payload, sort by SequenceID asc, and return RemoteMessage slice.
- internal/teams/client/messages_test.go: add httptest coverage for success parse, missing optional fields, non-2xx error with truncated body, and ordering by SequenceID.
- internal/teams/model/message.go: add RemoteMessage model (MessageID, SequenceID, SenderID, Timestamp, Body) and normalization helpers (timestamp parsing, body extraction) used by the client.

Implementation notes
- Mirror patterns from internal/teams/client/conversations.go: default endpoint constant, ErrMissingHTTPClient, error type with Status + BodySnippet, and 2xx handling.
- JSON parsing: define lightweight response struct with `messages` array and nested `content.text` + `sequenceId`/`id`/`from`/`createdTime` fields; normalization should tolerate missing fields.
- Timestamp parsing: try time.RFC3339Nano then time.RFC3339; leave zero value on failure.
- Sorting: after normalization, sort by SequenceID ascending using CompareSequenceID (numeric when possible, lexicographic fallback).
- Do not filter by sinceSequence inside the client; keep filtering in Phase 2.

Unit tests
- messages_test.go: success response returns one normalized message, empty body filtered, timestamp parsed, and auth header is "Bearer <token>".
- messages_test.go: missing optional fields (sender, timestamp) do not panic and yield zero values.
- messages_test.go: non-2xx response returns error type with status and body snippet length == maxErrorBodyBytes.
- messages_test.go: unordered input returns ascending SequenceID output.
- messages_test.go: mixed SequenceID parse success/failure uses numeric compare when both parse; otherwise strings.Compare.

PHASE 2 — Deduplication + persistence
Files + changes
- internal/bridge/messages.go: add a small ingestion service that filters by last_sequence_id, skips empty-body messages with DEBUG logging, sends messages, and returns the last successfully-sent SequenceID when the thread completes without send failures.
- internal/bridge/messages_test.go: add tests for sequence filtering, highest-seen tracking, and failure handling (no advance on failed send).
- database/teams_thread.go: add helper methods for updating last_sequence_id in a TeamsThread record (or use existing Upsert with updated fields).

Implementation notes
- Keep sequence comparison isolated in a helper (e.g., CompareSequenceID(a, b string) int) so filtering + sorting share logic.
- Ingestion flow for a thread: load last_sequence_id, call ListMessages, skip <= last_sequence_id, skip empty body with DEBUG log, send in order, stop on first send failure, track last successful SequenceID, persist updated last_sequence_id after successful sends.
- Ensure last_sequence_id is only advanced when a send succeeds; do not update on fetch or parse errors.

Unit tests
- messages_test.go (bridge): given last_sequence_id, filter out older/equal sequence IDs.
- messages_test.go (bridge): send failures do not advance last_sequence_id; verify returned updated sequence is unchanged and later messages are not sent.
- messages_test.go (bridge): when all sends succeed, highest SequenceID is returned and persisted.

PHASE 3 — Matrix send + teams-login wiring + logging
Files + changes
- internal/bridge/messages.go: add Matrix sender implementation using mautrix client SendMessageEvent with event.MessageEventContent{MsgType: m.text, Body: <text>}.
- cmd/teams-login/main.go: after DiscoverAndEnsureRooms, iterate teams_thread records, fetch messages since last_sequence_id, send into mapped room, and persist updated last_sequence_id.

Implementation notes
- Use the existing bot client from runRoomBootstrap (mautrix.NewClient) for sends; avoid impersonation.
- Logging with zerolog:
  - INF teams message discovered thread_id=<id> seq=<n>
  - INF matrix message sent room_id=<id> seq=<n>
  - ERR failed to send message thread_id=<id> room_id=<id> seq=<n> err=<err>
- Exit non-zero on message fetch failure or DB write failure; stop processing a thread on the first send failure and continue with other threads.

Unit tests
- messages_test.go (bridge): stub Matrix sender to capture sent bodies; verify one event per message, msgtype m.text, body matches.
- messages_test.go (bridge): ensure logs are emitted for discovery/sent/error (if logging is asserted in existing tests).
