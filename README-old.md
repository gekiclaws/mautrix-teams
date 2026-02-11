# mautrix-teams
A Matrix-Teams puppeting bridge.

## How to setup bridge
From repo root:
1. Run `bbctl login`, then `bbctl register sh-msteams`
2. Copy the registration file details verbatim into `registration.yaml`
3. Generate a config with `./mautrix-teams -c config.yaml -e`
4. Use the details from `bbctl register sh-msteams` log to update config.yaml fields:
- Homeserver URL -> homeserver.address
- Homeserver domain -> homeserver.domain
- Your user ID -> replace the "@admin:example.com" field under admin.permissions
  for example: "@admin:example.com": admin -> "@gekiclaws:beeper.com": admin
- as_token -> appservice.as_token
- hs_token -> appservice.hs_token

## How to run the bridge
From repo root:
1. Build the bridge via `./build.sh` (or `go build -o mautrix-teams ./cmd/mautrix-teams`).
2. Run the bridge via `./mautrix-teams -c config.yaml`.
3. Log in via bridgev2 provisioning (e.g. in the bridge management room, send `login` and follow the instructions).
