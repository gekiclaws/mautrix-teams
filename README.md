# mautrix-teams

`mautrix-teams` is an experimental Matrix ↔ Microsoft Teams bridge built on the mautrix `bridgev2` framework.

It currently targets the consumer Teams web stack at `teams.live.com`, using delegated user auth and polling-based sync.

## Current support

Supported:
- Teams login
- Text messages in both directions
- DM and group chat discovery
- Reactions
- Attachments in both directions
- GIF handling
- Matrix → Teams typing
- Read receipts in both directions
- Profile display names

Not yet supported:
- Teams → Matrix typing
- Message edits
- Message deletes / redactions
- Reply / thread semantics

## Architecture

```mermaid
flowchart LR
    A["Matrix homeserver"] --> B["mautrix bridgev2 runtime"]
    B --> C["Teams connector"]
    C --> D["Teams consumer chat APIs"]
    C --> E["Microsoft Graph file APIs"]
    F["Teams web login (teams.live.com)"] --> C
````

Main components:

* `pkg/connector`
* `internal/teams/auth`
* `internal/teams/client`
* `internal/teams/graph`
* `pkg/teamsdb`

## Docs

* [docs/setup.md](docs/setup.md)
* [docs/configuration.md](docs/configuration.md)
* [docs/architecture.md](docs/architecture.md)
* [docs/operations.md](docs/operations.md)
