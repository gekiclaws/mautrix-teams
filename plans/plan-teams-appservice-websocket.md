Task Checklist
- Phase 1: Config alignment for websocket appservice
  ☑ Update `config.yaml` homeserver/appservice values to mirror iMessage websocket mode (hungryserv base, websocket enabled, sh-msteams bot/id).
  ☑ Replace `config.yaml` `appservice.as_token`/`appservice.hs_token` with exact `bbctl register sh-msteams` values.
- Phase 2: Documentation sync
  ☑ Reflect the finalized websocket appservice config in `example-config.yaml` as documentation.

Phase 1: Config alignment for websocket appservice
Files:
- `config.yaml`: mirror iMessage appservice websocket settings; point homeserver to hungryserv; wire appservice tokens and bot localpart from bbctl output.

Changes:
- Set `homeserver.address` to `https://matrix.beeper.com/_hungryserv/gekiclaws`, `homeserver.software: hungry`, and `homeserver.websocket: true`.
- Enable websocket mode by setting `homeserver.websocket: true`. Do not add `homeserver.websocket_proxy` (iMessage relies on the base URL and default websocket derivation).
- Keep `appservice.hostname`/`appservice.port` intact but rely on websocket mode to skip HTTP listener at runtime.
- Update `appservice.id`/`appservice.bot.username` to `sh-msteams`/`sh-msteamsbot` and replace `appservice.as_token`/`appservice.hs_token` with the exact `bbctl register sh-msteams` values from the registration file.

Unit tests:
- No new unit tests; configuration-only changes with no new logic.

Phase 2: Documentation sync
Files:
- `example-config.yaml`: document the websocket appservice setup once Phase 1 values are finalized.

Changes:
- Mirror the `config.yaml` homeserver/appservice websocket fields and hungryserv address shape without copying unrelated comments or sections.

Unit tests:
- No new unit tests; documentation-only updates.
