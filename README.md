# mautrix-teams (Beeper / Teams Consumer)

A Matrix ↔ Microsoft Teams (Consumer) bridge designed to run **with Beeper via `bbctl`**.

This bridge is **not** a standalone appservice. It is intended to run locally, connected to Beeper using an appservice **websocket**, with Teams authentication handled externally.

---

## What this bridge does

* Connects Microsoft Teams (Consumer) to Matrix
* Runs as a Matrix appservice behind **Beeper**
* Uses an **external login helper** to acquire Teams auth
* Starts cleanly even without Teams auth (idle but healthy)
* Activates Teams polling only after successful login

---

## Prerequisites

* Go ≥ 1.21
* Beeper account
* `bbctl` installed and authenticated
* Access to this repository

---

## High-level architecture

* `mautrix-teams` runs locally
* Beeper connects to it via **appservice websocket**
* Teams auth is stored in `auth.json`
* The bridge loads auth on startup or when `!login` is issued

No browser, OAuth, or QR flows happen inside the bridge.

---

## Setup (from zero)

### 1. Clone and build

```bash
git clone https://github.com/your-org/mautrix-teams.git
cd mautrix-teams
go build ./cmd/mautrix-teams
```

---

### 2. Generate registration with bbctl

From the bridge directory:

```bash
bbctl login
bbctl register sh-teams
```

This generates a **Beeper-compatible appservice registration**.

Keep this file safe.

---

### 3. Create `config.yaml`

Start from `example-config.yaml` and fill in values from `registration.yaml`.

Key requirements:

* Appservice **websocket enabled**
* Tokens **must exactly match** the registration
* Config and registration must stay in sync

Example (simplified):

```yaml
homeserver:
  address: https://matrix.beeper.com
  domain: beeper.local
  websocket: false
  ws_proxy: []

appservice:
  id: 2d417984-0a3c-456e-8fd3-751b95da4632

  hostname: 127.0.0.1
  port: 29648

  database:
    type: sqlite3-fk-wal
    uri: file:mautrix-teams.db?_txlock=immediate

  as_token: hua_<>
  hs_token: huh_<>

  ephemeral_events: true

bridge:
  protocol: sh-teams
  permissions:
    "@<your_username>:beeper.com": admin

logging:
  level: debug
```

Do **not** invent tokens. Copy them.

---

### 4. Generate Teams auth (`auth.json`)

Teams authentication is handled by a separate helper.

From the same directory as `config.yaml`:

```bash
go run ./cmd/teams-login/
```

This will:

* Open a browser (unless disabled)
* Perform Microsoft login
* Write `auth.json` next to `config.yaml`

The bridge **only reads** this file.

---

### 5. Run the bridge

Terminal 1:

```bash
go build -o mautrix-teams
./mautrix-teams -n -c config.yaml
```

You should see logs indicating:

* Config loaded
* Appservice websocket started
* Bridge started

If `auth.json` is missing or expired, the bridge will stay idle.
This is expected.

---

### 6. Connect Beeper

Terminal 2:

```bash
bbctl proxy -r registration.yaml
```

You should see:

```
Appservice transaction websocket opened
Forwarding transaction ...
```

This means Beeper ↔ bridge is live.

---

### 7. Login from Matrix

In Beeper, DM the bridge bot and run:

```
!login
```

The bridge will:

* Reload `auth.json`
* Validate the Skype token
* Start Teams consumer loops
* Reply with success or a clear error

No browser interaction happens here.

---

## Expected behavior

| State          | Result                    |
| -------------- | ------------------------- |
| No `auth.json` | Bridge starts, idle       |
| Expired token  | `!login` reports error    |
| Valid token    | Teams polling starts      |
| Restart bridge | Auth reused automatically |

---

## Files you must preserve

These files are stateful and should not be regenerated casually:

* `config.yaml`
* `registration.yaml`
* `auth.json`
* Database file (if using sqlite)

Losing any of them will require re-setup.

---

## Common issues

**Nothing happens after `!login`**

* Check `auth.json` exists and is valid
* Check logs for token expiry
* Ensure bridge and `bbctl proxy` are both running

**Bridge starts but Beeper can’t connect**

* Tokens in `config.yaml` and `registration.yaml` must match
* Websocket must be enabled
* Port must match `bbctl proxy`

**Bridge panics on startup**

* Config is invalid
* Registration not installed correctly
* Using incompatible mautrix versions