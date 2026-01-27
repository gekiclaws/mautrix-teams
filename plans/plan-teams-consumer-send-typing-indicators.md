## Open Questions
- None.

## Task Checklist
Phase 1
☐ Add Teams client support for outbound typing indicators via the existing messages endpoint and executor.
☐ Unit test the typing endpoint URL, headers, payload, and typed error behavior.
Phase 2
☐ Add a Teams consumer typing sender and wire it into `TeamsConsumerPortal.HandleMatrixTyping`.
☐ Initialize the typing sender alongside the existing Teams consumer sender/reactor.
☐ Unit test typing sender behavior with simple fakes.

## Phase 1: Typing Client Endpoint
Files
- `internal/teams/client/typing.go` (new): implement the Control/Typing message send endpoint.
- `internal/teams/client/typing_test.go` (new): validate typing endpoint URL, headers, payload, and typed errors.
Changes
- Implement `Client.SendTypingIndicator(ctx context.Context, threadID string, fromUserID string) (int, error)`:
  - Reuse `SendMessagesURL` (defaulting to the existing consumer send base) and `TeamsRequestExecutor`.
  - Endpoint: `POST {SendMessagesURL}/{thread_id}/messages`.
  - Payload: a minimal control message routed through the same send path:
    - `type: "Message"`
    - `conversationid: <thread_id>`
    - `messagetype: "Control/Typing"`
    - `contenttype: "Text"`
    - `from: <from_user_id>`
    - `fromUserId: <from_user_id>`
    - `clientmessageid: <generated>`
    - `composetime` / `originalarrivaltime`: RFC3339Nano now.
  - Use `WithRequestMeta(ctx, RequestMeta{ThreadID: threadID, ClientMessageID: clientMessageID, Operation: "teams typing"})` so retry/backoff logs remain consistent but distinct.
  - Classification: same retry rules as existing send/reactions (429 + 5xx retryable), with a typed `TypingError` for other non-2xx responses.
Tests
- `internal/teams/client/typing_test.go`:
  - Use `httptest.Server` with a custom `SendMessagesURL`.
  - Assert method `POST`, path `/consumer/v1/users/ME/conversations/{escaped_thread}/messages`, and required headers.
  - Assert payload includes `messagetype: Control/Typing`, correct conversation/from fields, and a non-empty `clientmessageid`.
  - Non-2xx responses return `TypingError` with status and body snippet.

## Phase 2: Portal Wiring + Typing Sender
Files
- `internal/bridge/typing.go` (new): Teams consumer typing sender that resolves thread IDs and sends Control/Typing messages.
- `internal/bridge/typing_test.go` (new): unit test typing sender behavior and thread resolution guardrails.
- `teams_consumer_portal.go`: implement typing handling and route to the typing sender.
- `teams_consumer_rooms.go`: initialize the typing sender alongside the existing sender/reactor.
- `main.go`: add a `TeamsConsumerTyper` field to `DiscordBridge`.
Changes
- Add a thin, best-effort typing sender:
  - New type `TeamsConsumerTyper` with dependencies:
    - `Client interface { SendTypingIndicator(ctx context.Context, threadID, fromUserID string) (int, error) }`
    - `Threads ThreadLookup`
    - `UserID string`
    - `Log zerolog.Logger`
  - Method `SendTyping(ctx context.Context, roomID id.RoomID) error`:
    - Resolve `threadID` via `Threads.GetThreadID`.
    - Call `Client.SendTypingIndicator`.
    - Log attempt/response status; return errors for observability, but portal handlers should swallow them (best-effort).
- Teach `TeamsConsumerPortal` to handle typing indicators:
  - Implement `HandleMatrixTyping(newTyping []id.UserID)`.
  - Track `currentlyTyping` inside `TeamsConsumerPortal` and compute started users using the same minimal `typingDiff` logic already used in `Portal`.
  - For each newly typing user, call `TeamsConsumerTyper.SendTyping(context.Background(), portal.roomID)`.
  - Do not persist typing state.
- Wire initialization:
  - Add `TeamsConsumerTyper *teamsbridge.TeamsConsumerTyper` to `DiscordBridge`.
  - In `initTeamsConsumerSender`, initialize the typer with the existing consumer client, thread store, and `state.TeamsUserID`.
  - `GetIPortal` can continue returning `TeamsConsumerPortal` when the consumer sender/store exist; the portal should guard internally if the typer is nil.
Tests
- `internal/bridge/typing_test.go`:
  - With a resolved thread ID, `SendTyping` calls the client exactly once with the resolved thread and configured user ID.
  - An unmapped room returns an error and does not call the client.
