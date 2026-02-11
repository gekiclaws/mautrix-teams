# Session Snapshot: mautrix-teams Bridgev2 Login + Multi-Login

Timestamp (UTC): 2026-02-11T00:28:12Z

## Transcript

### User
We want to implement proper bridgev2 login flows and multiple bridge logins to this MS Teams bridge. Check /Users/batuhan/Projects/texts/beeper-workspac e/mautrix/whatsapp /Users/batuhan/Projects/texts/be eper-workspace/mautrix/signal for how bridges work.

### Assistant
I’m going to inspect the current `mautrix-teams` auth/provisioning code, then compare it to the Bridgev2/multi-login patterns in `mautrix/whatsapp` and `mautrix/signal`, and implement the same structure here (DB models, provisioning API, and login lifecycle).

### Assistant
I’ve confirmed `mautrix-teams` is currently built on the legacy `mautrix/bridge` (v1) stack, while the WhatsApp/Signal references you pointed to are fully on `mautrix/bridgev2`. Next I’ll inspect `main.go` + `internal/bridge` to see what auth/login/provisioning exists today, then decide whether we can layer bridgev2 login/provisioning in-place or need a connector-style migration like Signal’s.

### User
also check mautrix/meta in the same directory as whatsapp and signal for browser logins

### Assistant
I’m going to inspect `mautrix/meta`’s `bridgev2` login implementation (it has the browser/cookie style flows) and map that onto Teams’ existing `teams-login` OAuth helper approach so we can implement a proper `/v2/*` provisioning flow and support multiple concurrent logins.

### Assistant
Implemented Bridgev2 provisioning login flows and multi-login plumbing (modeled after `mautrix/meta`’s browser-based flows).

- New bridgev2 entrypoint: `/Users/batuhan/Projects/texts/mautrix-teams/cmd/mautrix-teams/main.go`
- Connector + DB metadata + multi-login support:
  - `/Users/batuhan/Projects/texts/mautrix-teams/pkg/connector/connector.go`
  - `/Users/batuhan/Projects/texts/mautrix-teams/pkg/connector/dbmeta.go`
  - `/Users/batuhan/Projects/texts/mautrix-teams/pkg/connector/client.go` (refreshes access token via refresh token, re-acquires skypetoken, persists metadata per-login)
- Login flows:
  - `/Users/batuhan/Projects/texts/mautrix-teams/pkg/connector/login.go`
  - Flow `webview_localstorage`: embedded browser (`LoginStepTypeCookies`) with `ExtractJS` that auto-dumps `localStorage`
  - Flow `msal_localstorage`: manual paste (`LoginStepTypeUserInput`) for the same `localStorage` dump

Notes:
- Multiple logins are now native bridgev2 `user_login` rows (login ID = normalized Teams user ID like `8:live:...`), so one Matrix user can add multiple Teams accounts.
- I removed the legacy v1 bridge code from this repo because `maunium.net/go/mautrix` v0.26.x no longer contains `mautrix/bridge` (v1), and keeping it (even behind build tags) breaks `go mod tidy`.

### User

did we implement Migrate mautrix-teams to Bridgev2 Multi-Login Architecture (Split-By-Login Portals)
Summary
Rewrite /Users/batuhan/Projects/texts/mautrix-teams to match the beeper-workspace mautrix-whatsapp / mautrix-signal patterns:

Switch from mautrix/bridge (bridgev1) to mautrix/bridgev2 with mxmain.BridgeMain.
Implement multiple Teams accounts per Matrix user using bridgev2.UserLogin.
Make all portals split by login (every portal key always includes Receiver = UserLogin.ID), avoiding cross-login room sharing.
Replace the global auth.json + cmd/teams-login flow with an in-bridge LoginFlow that uses a web helper + token capture, and persist tokens in UserLogin.Metadata.
Add token refresh so logins stay alive without rerunning an external tool.
Decisions Locked In (Per Your Answers)
Architecture: migrate to bridgev2
Logins: multiple Teams accounts per Matrix user
Portals: split by login (no shared group portals)
Scope
In scope (must ship for success)
bridgev2 skeleton: mxmain.BridgeMain, connector/client scaffolding, config loading, provisioning router usage.
A Teams login flow that:
Presents an “open helper page” step
Captures MSAL localStorage JSON
Extracts access/refresh tokens
Acquires/refreshes SkypeToken
Creates a UserLogin
Per-login background sync loop:
Thread discovery -> simplevent.ChatResync with CreatePortal: true
Message polling -> simplevent.Message[...] for text messages (minimum viable)
Teams-specific DB tables keyed by user_login_id where needed.
Explicitly out of scope (can follow later)
Full parity feature set: reactions/read receipts/typing/media uploads, Spaces, relay mode, etc.
Preserving/migrating the current discord-like DB schema/data.
Repository Restructure
Add bridgev2 entrypoint

New: main.go
Replace current main.go bridgev1 entrypoint (either delete or keep temporarily behind a build tag; final state should be bridgev2-only).
Add connector package (bridgev2-style)

New: connector.go
type TeamsConnector struct { Bridge *bridgev2.Bridge; Config …; DB …; loginSessions … }
Implements bridgev2.NetworkConnector.
New: client.go
type TeamsClient struct { Main *TeamsConnector; UserLogin *bridgev2.UserLogin; consumer *consumerclient.Client; … }
Implements bridgev2.NetworkAPI (and bridgev2.BackgroundSyncingNetworkAPI if needed for long-poll style behavior).
Add Teams ID helpers (mirror signalid, waid)

New: ids.go
MakeUserLoginID(teamsUserID string) networkid.UserLoginID
MakeUserID(teamsUserID string) networkid.UserID
MakePortalKey(threadID string, loginID networkid.UserLoginID) networkid.PortalKey (always sets Receiver)
Normalize IDs using existing auth.NormalizeTeamsUserID.
Login Flow Design (bridgev2-style)
Metadata schema
Create:

metadata.go
type UserLoginMetadata struct {
TeamsUserID string
AccessToken string
AccessTokenExpiresAt int64
RefreshToken string
SkypeToken string
SkypeTokenExpiresAt int64
// any future fields: SignInName, IsBusiness, etc
}
Token refresh support (required)
Extend /Users/batuhan/Projects/texts/mautrix-teams/internal/teams/auth:

Add refresh-token grant support:

New method: (*auth.Client).RefreshToken(ctx, refreshToken string) (*auth.AuthState, error)
Use grant_type=refresh_token, client_id, and the existing TokenEndpoint.
Add an abstraction in TeamsClient:

EnsureValidSkypeToken(ctx):
If SkypeToken valid: return
Else if AccessToken still valid: call AcquireSkypeToken
Else if RefreshToken present: call RefreshToken, update metadata access token + expiry, then AcquireSkypeToken
Else: send BridgeStateBadCredentials and require relogin.
Login UX (modeled after whatsapp/signal login processes)
Implement in login.go:

GetLoginFlows() returns one flow: { ID: "msal-localstorage", Name: "Web login", Description: "Log in via teams.live.com and paste localStorage JSON" }
CreateLogin() returns a stateful TeamsMSALLogin that implements bridgev2.LoginProcessDisplayAndWait.
Helper web endpoints (public, per-login-session)
Because flow.go currently runs a localhost listener, rewrite it into “router handlers” (not a listener):

Add to connector a global in-memory map:
map[string]chan *auth.AuthState keyed by session_id (uuid)
Register routes on provisioning router at startup:
GET /login/teams/{session_id}: serve helper HTML (reuse helperPageHTML, but adjust POST URL)
POST /login/teams/{session_id}/capture: accept {storage: "...json..."} and push AuthState to the session channel
Where to register routes:

In main.go set m.PostStart = func(){ … } like whatsapp/signal and call connector.RegisterProvisioningRoutes(m.Matrix.Provisioning.Router).
Login process state machine
Start(ctx):

Generate session_id
Create channel, store in connector map
Return LoginStepTypeDisplayAndWait with:
Display type: QR (data = full helper URL) OR Code (data = helper URL). Prefer QR like whatsapp/signal patterns.
Instructions: include the console snippet:
copy(JSON.stringify(Object.fromEntries(Object.entries(localStorage))))
Wait(ctx) waits on channel for captured AuthState:
Extract Access/Refresh tokens via ExtractTokensFromMSALLocalStorage (already done by handler)
Call AcquireSkypeToken(ctx, accessToken)
Normalize TeamsUserID (8: prefix)
Create new bridgev2.UserLogin:
ID = teamsid.MakeUserLoginID(teamsUserID)
RemoteName = teamsUserID (until we add profile lookup)
Metadata = &teamsid.UserLoginMetadata{…}
Use DeleteOnConflict: true so relogin replaces old login cleanly
Return LoginStepTypeComplete
Cancel behavior:

Cancel() should remove the session_id mapping and close the channel.
Security/abuse controls:

Session IDs must be unguessable (uuid).
Rate-limit capture per session: first successful capture wins (like once.Do in current helper listener).
Reject payloads > some sane size (e.g., 512KB) to avoid memory abuse.
Per-Login Sync Model
Portal key rules (split by login)
Every portal key:

PortalKey.ID = threadID
PortalKey.Receiver = UserLogin.ID
This guarantees:

Multiple logins never share a Matrix room accidentally.
Multiple Matrix users logging into the same Teams thread still get separate portals per-login (by design).
Thread discovery -> portals
In sync_threads.go:

On TeamsClient.Connect(ctx):
EnsureValidSkypeToken(ctx)
Fetch conversations using existing internal/teams/client list API.
Normalize each to model.Thread via NormalizeForSelf(metadata.TeamsUserID)
For each thread:
Queue a simplevent.ChatResync:
PortalKey = teamsid.MakePortalKey(thread.ID, login.ID)
ChatInfo at minimum:
Name = thread.RoomName
DM vs group inferred from thread.IsOneToOne
CreatePortal = true
Message polling -> simplevent.Message
In poll_messages.go:

Maintain per-(user_login_id, thread_id) poll state in Teams DB (below).
Poll messages using the existing consumer poller logic (teams/poll + consumer client), and for each new message:
Queue simplevent.Message[*model.NormalizedMessage] with:
PortalKey = (thread_id, receiver=login)
Sender = bridgev2.EventSender{ Sender: teamsid.MakeUserID(msg.SenderID), IsFromMe: msg.SenderID == metadata.TeamsUserID }
ID = Teams message ID (scoped by portal, OK)
Timestamp = msg time
ConvertMessageFunc:
MVP: text-only -> event.MessageEventContent{MsgType: m.text, Body: msg.BodyText}
Later: attachments, edits, etc.
Ordering:

Set StreamOrder monotonically per thread if available; otherwise use timestamp.UnixMilli().
Failure handling:

If token invalid: push BadCredentials state and stop loops for that login.
If one thread fails polling: backoff per thread (reuse existing backoff pattern, but store state per user_login_id).
Teams-Specific DB (Like whatsapp’s wadb)
Create a separate DB “section” similar to whatsapp:

New package: /Users/batuhan/Projects/texts/mautrix-teams/pkg/teamsdb/
Initialize from connector using the bridgev2 DB handle (pattern-match whatsapp wadb.New(bridge.ID, bridge.DB.Database, ...)).
Tables (minimum)
teams_thread_state

user_login_id TEXT NOT NULL
thread_id TEXT NOT NULL
conversation_id TEXT
last_sequence_id TEXT
last_message_ts BIGINT
last_message_id TEXT
Primary key: (user_login_id, thread_id)
Index: (user_login_id, last_message_ts)
Any existing Teams-only state you still need can be ported later, but must include user_login_id if it’s account-scoped.

Migration strategy:

New DB schema versioning for this section (like whatsapp upgrades). No attempt to migrate the current discord-derived schema.
Config + Runtime
Replace the current config/ and example-config.yaml (discord-derived) with bridgev2-style config (use whatsapp/signal as template).
Keep any Teams-specific config under connector config struct (poll intervals, backfill toggles, etc.).
Deprecate cmd/teams-login:
Either delete it or leave it as a developer utility; production flow should be via bridge provisioning/login.
Acceptance Criteria
One Matrix user can add 2+ Teams accounts, and each becomes a distinct UserLogin visible to bridgev2.
Each login gets its own portals (receiver always set), and messages appear only under the correct login.
If SkypeToken expires, the bridge refreshes tokens automatically using RefreshToken and continues syncing without manual re-login.
Bad/expired refresh token causes a clean BadCredentials bridge state and requires re-login.
Concurrent login attempts do not cross-talk (session_id isolates captures).
Test Plan
Unit tests (required)
Login capture handler

POST valid {storage: ...} => unblocks waiting login process exactly once
POST invalid JSON => 400
Second POST after success => does not overwrite first result
Token refresh logic

Skype valid => no refresh calls
Skype expired + access valid => AcquireSkypeToken called
Access expired + refresh present => RefreshToken called then AcquireSkypeToken called
Missing refresh => BadCredentials
ID/key normalization

NormalizeTeamsUserID adds 8: prefix
Portal key receiver always equals login ID
Manual test scenarios (required)
Add first login via helper page, confirm portals created.
Add second login, confirm separate rooms (no overlap).
Simulate token expiry (force metadata expiry), confirm refresh path works.
Re-login same Teams account, confirm DeleteOnConflict replaces old login cleanly.
Assumptions / Defaults
Teams mode targeted here is the existing consumer auth path (teams.live.com + SkypeToken).
Initial message bridging MVP is text-only; other message types are follow-up work.
No compatibility/migration of the current discord-derived DB is required; new deployments should start with a fresh DB for the bridgev2 rewrite.

### Assistant
No. We migrated **login/provisioning and multi-login storage** to bridgev2, but we did **not** implement the full “split-by-login portals + per-login sync + Teams DB section” architecture you outlined.

**Implemented**
- Bridgev2 entrypoint using `mxmain.BridgeMain`: `/Users/batuhan/Projects/texts/mautrix-teams/cmd/mautrix-teams/main.go`
- bridgev2 connector skeleton + multi-login via `bridgev2.UserLogin` metadata:
  - `/Users/batuhan/Projects/texts/mautrix-teams/pkg/connector/connector.go`
  - `/Users/batuhan/Projects/texts/mautrix-teams/pkg/connector/dbmeta.go`
  - `/Users/batuhan/Projects/texts/mautrix-teams/pkg/connector/client.go`
- In-bridge login flows (modeled after `mautrix/meta`’s browser/cookie approach, but for Teams MSAL localStorage):
  - Embedded webview auto-extract (`LoginStepTypeCookies`, `ExtractJS`)
  - Manual paste (`LoginStepTypeUserInput`)
  - `/Users/batuhan/Projects/texts/mautrix-teams/pkg/connector/login.go`
- Refresh-token grant support for keeping logins alive:
  - `/Users/batuhan/Projects/texts/mautrix-teams/internal/teams/auth/token.go` (`RefreshAccessToken`)
  - Used in `/Users/batuhan/Projects/texts/mautrix-teams/pkg/connector/client.go` to refresh access token and reacquire skypetoken, persisting back into `UserLogin.Metadata`.

**Not implemented (still missing from your spec)**
- Split-by-login portals (portal key `Receiver = UserLogin.ID`) and any portal creation at all (no `ChatResync` / `CreatePortal` wiring yet).
- Per-login background sync loop (thread discovery + message polling) and simplevent emission.
- Teams-specific DB “section” like whatsapp’s/signal’s (`teams_thread_state` keyed by `user_login_id`, upgrade scripts, etc.).
- Provisioning-router “helper page” endpoints (`GET /login/teams/{session_id}`, `POST /capture`) and the `DisplayAndWait` session-based flow (we used webview extraction instead).
- Config rewrite to bridgev2-style full config (right now it’s only a minimal connector config).
- Unit tests for the login capture handler, refresh logic matrix, and portal key receiver invariants.

If you want, I can continue by implementing the missing pieces in this order:
1. `pkg/connector/ids.go` + enforce `PortalKey.Receiver = login.ID` everywhere.
2. `pkg/teamsdb/` with `teams_thread_state (user_login_id, thread_id, ...)` + upgrades.
3. `TeamsClient.Connect()` thread discovery and `simplevent.ChatResync` with `CreatePortal: true`.
4. Per-login poll loop that emits text-only `simplevent.Message`.
5. The session-based provisioning helper routes + `LoginProcessDisplayAndWait` (if you still prefer that UX over the webview ExtractJS flow).

### User
can you take a snapshot of the messages i sent you and add it to .temporary-sessions directory as Markdown file. incldue your responses too. and add tiemstamp
