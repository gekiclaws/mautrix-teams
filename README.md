# mautrix-teams
A Matrix-Teams puppeting bridge.

## How to setup bridge
From repo root:
1. Run `bbctl login`, then `bbctl register sh-msteams`
2. Copy the registration file details verbatim into `registration.yaml`
3. Copy `example-config.yaml` into `config.yaml`
4. Use the details from `bbctl register sh-msteams` log to update config.yaml fields:
- Homeserver URL -> homeserver.address
- Homeserver domain -> homeserver.domain
- Your user ID -> replace the "@admin:example.com" field under admin.permissions
  for example: "@admin:example.com": admin -> "@gekiclaws:beeper.com": admin
- as_token -> appservice.as_token
- hs_token -> appservice.hs_token

## How to run the bridge
From repo root:
1. Get Teams auth credentials via `go run ./cmd/teams-login/`
2. Build the bridge via `go build -o mautrix-teams`
3. Run the bridge via `./mautrix-teams`
