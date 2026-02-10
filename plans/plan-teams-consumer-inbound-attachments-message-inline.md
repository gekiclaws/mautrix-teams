## FLAGGED OPEN QUESTIONS
- None. Decisions locked:
  - Use `formatted_body` + plain body attachment links only (no Beeper attachment metadata yet).
  - Add `internal/bridge/matrix_messages.go` helper and keep `internal/bridge/messages.go` focused on ingest/send orchestration.

## Task Checklist
Phase 1
☑ Add attachment value types and `ParseAttachments(raw string) ([]TeamsAttachment, bool)` with strict malformed/empty handling.
☑ Add unit tests for empty, `[]`, single, multiple, and malformed payloads.

Phase 2
☑ Parse `properties.files` from Teams message payloads and carry it in the internal message model.
☑ Parse attachments during ingest and keep ordering/sequence/unread/receipt behavior unchanged.
☑ Add/extend ingest tests to prove attachment metadata is attached without state regressions.

Phase 3
☑ Render inbound attachments into a single Matrix event payload (text-only, attachment-only, text+attachment).
☑ Preserve original text body while appending usable attachment links.
☑ Add emission tests asserting one Matrix send per Teams message and correct combined content.

## Phase 1: Parsing + Normalization
Files
- `internal/teams/model/attachment.go` (new): define attachment DTOs and parsing helpers for Teams `properties.files`.
- `internal/teams/model/attachment_test.go` (new): table-driven parser coverage for valid/invalid payloads.

Changes
- Add `TeamsAttachment`:
  - `Filename string`
  - `ShareURL string`
  - `DownloadURL string`
  - `FileType string`
- Implement `ParseAttachments(raw string) ([]TeamsAttachment, bool)`:
  - Return `(nil, false)` when `strings.TrimSpace(raw)` is empty or exactly `"[]"`.
  - Decode JSON array of lightweight raw entries that read only:
    - `fileName`
    - `fileInfo.shareUrl`
    - `fileInfo.fileUrl`
    - `fileType`
  - Skip entries missing `fileName` or `fileInfo.shareUrl`.
  - Return `(nil, false)` for malformed JSON or if all entries are skipped.
  - Return `(attachments, true)` only when at least one normalized attachment remains.
- Keep parser value-oriented and side-effect free (no network, no DB, no logging in parser path).

Tests
- Empty string returns `(nil, false)`.
- `"[]"` returns `(nil, false)`.
- Single valid item produces one normalized `TeamsAttachment`.
- Multiple mixed entries parse and skip invalid ones.
- Malformed JSON returns `(nil, false)` without panic.

## Phase 2: Message Ingest Integration
Files
- `internal/teams/model/message.go`: extend `RemoteMessage` with raw files payload field for ingest-time parsing.
- `internal/teams/client/messages.go`: extract `properties.files` string from message properties into `RemoteMessage`.
- `internal/teams/client/messages_test.go`: cover `properties.files` extraction (present/missing/malformed properties object).
- `internal/bridge/messages.go`: call `model.ParseAttachments(msg.PropertiesFiles)` during ingest and attach result to the outbound payload-building path.
- `internal/bridge/messages_test.go`: verify attachments flow through ingest while sequence/state semantics are unchanged.

Changes
- Extend `model.RemoteMessage` with `PropertiesFiles string` (raw JSON string from `properties.files`).
- In Teams client message decode:
  - Add a small extractor for `properties.files` that safely handles absent/malformed `properties`.
  - Keep fetch ordering unchanged (`CompareSequenceID`) and keep reaction parsing untouched.
- In `MessageIngestor.IngestThread`:
  - Parse attachments from `msg.PropertiesFiles` with `model.ParseAttachments`.
  - Keep attachment metadata available for emission alongside existing body/formatted-body/profile metadata.
  - Preserve invariants:
    - no change to `last_seq` advancement rules
    - no change to unread marker behavior
    - no change to receipts/reactions ordering
    - no additional Teams/Graph/API calls
- Adjust the empty-message skip condition so attachment-only messages are no longer dropped (`skip only when body is empty AND no parsed attachments`).

Tests
- Message with no `properties.files` behaves exactly as before.
- Message with valid `properties.files` exposes parsed attachments to emission path.
- Attachment presence does not change sequence filtering, `MessagesIngested`, `Advanced`, or unread marking behavior.
- Attachment-only message is ingested (single send) rather than skipped.

## Phase 3: Matrix Emission (Single Event)
Files
- `internal/bridge/matrix_messages.go` (new, if approved): pure helper(s) to render body + `formatted_body` for text/attachments in one event.
- `internal/bridge/messages.go`: use rendering helper (or inline equivalent) before `SendText`.
- `internal/bridge/matrix_messages_test.go` (new): unit tests for rendering combinations.
- `internal/bridge/messages_test.go`: assert one `SendText` call and payload correctness for attachment-only and text+attachment cases.

Changes
- Implement a deterministic renderer that keeps one Teams message -> one Matrix event:
  - Text-only: unchanged message body/formatted body behavior.
  - Attachment-only: emit a text event with human-readable attachment lines and share links.
  - Text + attachments: preserve original text, then append attachment lines.
- Rendering format:
  - Plain `body`: append lines like `Attachment: <filename> - <share URL>`.
  - `formatted_body`: append HTML list/line links (`<a href="...">filename</a>`), while preserving existing formatted message HTML.
- Keep transport unchanged (`SendText`) to avoid introducing upload flows or extra events.
- Optional extension point in renderer for future Beeper attachment metadata, but do not emit extra events or call external APIs.

Tests
- Text-only message: one send, unchanged body semantics.
- Attachment-only message: one send with usable link content in body/`formatted_body`.
- Text + attachment message: one send containing original text plus rendered attachment link block.
- Multiple attachments: still one send, deterministic line ordering from input array order.
