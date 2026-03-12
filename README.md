# mautrix-teams

A Matrix ↔ Microsoft Teams puppeting bridge built using the mautrix bridge framework.

This bridge enables full-fidelity messaging between Matrix and Microsoft Teams by reverse engineering the Teams web client APIs and translating events between the two protocols.

The implementation is referenced in the [Beeper engineering blog](https://blog.beeper.com/2025/10/28/build-a-beeper-bridge/).

## Features

- Matrix ↔ Teams message bridging
- Puppeting (messages appear from the correct user)
- Support for edits, reactions, and attachments
- Real-time event translation between protocols

## Architecture

The bridge connects the Teams web client APIs to the Matrix protocol using the mautrix bridge framework.

High-level flow:

```
Microsoft Teams
     │
     ▼
Teams Web Client APIs
     │
     ▼
mautrix-teams bridge
     │
     ▼
Matrix homeserver
```

The bridge acts as a translation layer between the two messaging systems, mapping Teams message events to Matrix room events and synchronizing user identities through puppeting.

## Running the bridge

From the repository root:

1. Run `bbctl --env prod config sh-msteams`
2. Select `bridgev2` for bridge type
3. Copy the generated config into `config.yaml`

Build and start the bridge:

```

./build.sh
./mautrix-teams

```

Then in Beeper Desktop:

1. Go to the **Bridges** tab
2. Click the three dots
3. Select **Experimental → Add an account**

## Notes

This project reverse engineers the Teams web client APIs and may break if Microsoft changes the client protocol.
