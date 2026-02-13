# Open Questions

1. If Graph download fails for one attachment (missing/expired Graph token, 403/404, etc.), should we:
   - fail the whole message conversion (drop the event), or
   - degrade for that attachment only (emit a link-style `m.text` line for the failed attachment) while still re-uploading others?
2. Teams “caption”: should this be treated as the Teams message body (text/HTML) that accompanies the attachment message, and sent once per message after all attachments (even if there are multiple files)?
3. When `properties.files` entries do not include a Drive item ID (`fileInfo.itemId`), is it acceptable to fall back to the existing share-link rendering, or should we treat that as an error?

# Task Checklist

Phase 1 — Attachment DTOs + Graph Content Download
- ☐ Extend attachment parsing to retain the Drive item ID used for Graph content downloads
- ☐ Add a Graph client helper to download `/me/drive/items/{id}/content` (with retry behavior consistent with other Graph calls)
- ☐ Add focused unit tests for the parser + Graph downloader

Phase 2 — Teams → Matrix Re-Upload + Message Parts
- ☐ Refresh Graph token on-demand when inbound attachments require Graph access
- ☐ Rework Teams message conversion to emit media events (`m.image`/`m.video`/`m.audio`/`m.file`) instead of share-link text
- ☐ Emit the Teams caption as a separate `m.text` part when present
- ☐ Add unit tests for msgtype selection and the multi-part conversion output

Phase 3 — Failure Semantics + Edge-Case Coverage
- ☐ Decide and implement consistent behavior for missing Drive item IDs and partial failures
- ☐ Add unit tests that lock in those semantics

# Phase 1 — Attachment DTOs + Graph Content Download

## Affected Files

- `internal/teams/model/attachment.go`
- `internal/teams/model/attachment_test.go`
- `internal/teams/graph/download_drive_item_content.go` (new)
- `internal/teams/graph/download_drive_item_content_test.go` (new)

## File-by-File Changes

- `internal/teams/model/attachment.go`
  - Extend `TeamsAttachment` with `DriveItemID string`.
  - Update `ParseAttachments` to parse Drive item ID from `properties.files` payload:
    - Prefer `fileInfo.itemId` (this is what outbound `files_builder.go` emits).
    - If needed, fall back to top-level `id` only when it matches the Drive item semantics used elsewhere (keep this conservative to avoid misusing listItem IDs).
  - Keep existing normalization behavior (trim strings; skip entries missing required fields).

- `internal/teams/model/attachment_test.go`
  - Update existing table tests to assert `DriveItemID` is parsed when present.
  - Add cases:
    - `fileInfo.itemId` present, `shareUrl` present, `fileName` present → attachment retained.
    - `fileInfo.itemId` missing → attachment still parses (so we can choose fallback behavior later).

- `internal/teams/graph/download_drive_item_content.go` (new)
  - Add `(*GraphClient) DownloadDriveItemContent(ctx, driveItemID string) (data []byte, contentType string, err error)` (or a small result struct).
  - Implement GET `https://graph.microsoft.com/v1.0/me/drive/items/{id}/content`.
  - Use `TeamsRequestExecutor.Do` with a response classifier that:
    - Treats 2xx as success.
    - Retries on 429 using `Retry-After` header and on 5xx (mirroring `get_drive_item.go` / `upload.go` behavior).
    - Returns a non-retryable error for other statuses and includes a small body snippet for debugging.
  - Enforce an in-memory attachment cap without introducing import cycles:
    - Add `GraphClient.MaxDownloadSize int64` (default: 100 MiB, matching existing attachment caps elsewhere).
    - Read at most `MaxDownloadSize+1` and error if exceeded.
  - Return `contentType` from the HTTP response header when present (leave MIME sniffing to the caller so we can combine header + sniff + extension).

- `internal/teams/graph/download_drive_item_content_test.go` (new)
  - Use `httptest.Server` to validate:
    - Correct path escaping and Authorization header.
    - Returns bytes and `Content-Type`.
    - 429 then 200 retries: override executor sleep/jitter to avoid time-based tests.

# Phase 2 — Teams → Matrix Re-Upload + Message Parts

## Affected Files

- `pkg/connector/handleteams.go`
- `pkg/connector/convert.go`
- `pkg/connector/convert_test.go`
- `internal/teams/client/messages_test.go` (update fixture payload to include `fileInfo.itemId`)

## File-by-File Changes

- `pkg/connector/handleteams.go`
  - Switch `simplevent.Message[model.RemoteMessage]{ ConvertMessageFunc: ... }` from the package-level `convertTeamsMessage` function to a `TeamsClient` method (e.g. `c.convertTeamsMessage`), so conversion has access to:
    - login metadata (refresh token + Graph token),
    - the shared consumer HTTP client,
    - and existing logging.

- `pkg/connector/handleteams.go` (or `pkg/connector/client.go`, whichever currently hosts token refresh helpers)
  - Add `(*TeamsClient) ensureValidGraphToken(ctx context.Context) error`:
    - If `c.Meta.GraphTokenValid(time.Now().UTC())`, no-op.
    - Otherwise, exchange `RefreshToken` for a new Graph token using the existing `refreshAccessTokenForGraphScope` helper (from `pkg/connector/storage_extractor.go`).
    - Persist updated `GraphAccessToken`/`GraphExpiresAt` (and refreshed refresh token if rotated) via `c.Login.Save(ctx)`.
    - Return a clear error if refresh token is missing or refresh fails.

- `pkg/connector/convert.go`
  - Replace `convertTeamsMessage(...)` with `func (c *TeamsClient) convertTeamsMessage(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, msg model.RemoteMessage) (*bridgev2.ConvertedMessage, error)`.
  - Parse attachments via `model.ParseAttachments(msg.PropertiesFiles)`.
  - If there are no attachments that can be re-uploaded, preserve the current behavior (text + rendered attachment links + GIF links).
  - If attachments exist:
    - Ensure a valid Graph token (`c.ensureValidGraphToken(ctx)`), then download each attachment via `graph.GraphClient.DownloadDriveItemContent`.
    - Determine MIME type using this precedence:
      1. HTTP `Content-Type` header (if non-empty and not obviously generic).
      2. `http.DetectContentType` on the first 512 bytes.
      3. `mime.TypeByExtension(filepath.Ext(filename))`.
      4. fallback `application/octet-stream`.
    - Map MIME type to Matrix msgtype:
      - `image/*` → `m.image`
      - `video/*` → `m.video`
      - `audio/*` → `m.audio`
      - otherwise → `m.file`
    - Upload to Matrix via `intent.UploadMedia(ctx, portal.MXID, data, filename, mimeType)` and use the returned MXC URL in the media event content.
    - Emit one `ConvertedMessagePart` per attachment:
      - `Type: event.EventMessage`
      - `Content: &event.MessageEventContent{ MsgType: ..., Body: filename, FileName: filename, URL: mxc, Info: &event.FileInfo{MimeType: mimeType, Size: len(data)} }`
      - carry over `Extra` per-message profile metadata (and any other existing extras) so all parts show the correct sender profile.
    - If caption is present:
      - Treat the Teams message body/formatted_body (and existing GIF rendering) as the caption.
      - Emit the caption as an additional final `m.text` part (do not embed caption into the media event).
      - Preserve HTML formatting when available (same logic as current `convertTeamsMessage` for `formatted_body` fallback).
  - Keep attachment-only messages from being dropped by ensuring the returned `ConvertedMessage` always has at least one part (media parts satisfy this; caption part optional).

- `pkg/connector/convert_test.go`
  - Update tests that assume attachments render as links:
    - Keep `renderInboundMessage*` tests for the “no reupload” path (e.g., when there are no Drive item IDs) if we keep that fallback.
  - Add unit tests for the new media conversion path:
    - A message with one attachment + caption produces 2 parts: first `m.file` (or `m.image` depending on MIME), second `m.text` caption.
    - A message with multiple attachments + no caption produces N parts, each media, with deterministic ordering matching `properties.files` order.
    - Per-message profile extra exists on all parts.
  - Use small fakes:
    - Fake `bridgev2.MatrixAPI` implementing `UploadMedia` to return deterministic MXC URLs without hitting a real homeserver.
    - `httptest.Server` for Graph download responses (or a small injected downloader if we choose to abstract it).

- `internal/teams/client/messages_test.go`
  - Update the JSON fixture for `properties.files` to include `fileInfo.itemId` so parser coverage matches the new reupload requirement.

# Phase 3 — Failure Semantics + Edge-Case Coverage

## Affected Files

- `pkg/connector/convert.go`
- `pkg/connector/convert_test.go`

## File-by-File Changes

- `pkg/connector/convert.go`
  - Implement the agreed behavior from Open Question #1 and #3:
    - If partial failure is allowed: emit media parts for successful attachments and add link-style lines (or a small failure notice) for failures in the caption text part.
    - If strict failure is required: return an error so the remote event fails conversion deterministically.
  - Ensure the behavior is value-oriented and composable:
    - Keep “download + mime detect + upload” in small helpers so tests can target them without end-to-end mocks.

- `pkg/connector/convert_test.go`
  - Add table-driven tests for the chosen failure semantics:
    - Missing Graph token.
    - Attachment entry missing `fileInfo.itemId`.
    - One of multiple attachments fails to download.
