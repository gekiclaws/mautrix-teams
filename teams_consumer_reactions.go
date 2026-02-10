package main

import (
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/event"
)

// HandleTeamsConsumerReaction routes reaction events for Teams consumer rooms without
// requiring a legacy Discord login session.
func (br *DiscordBridge) HandleTeamsConsumerReaction(evt *event.Event) {
	if br == nil || evt == nil || evt.Type != event.EventReaction {
		return
	}
	if br.TeamsConsumerReactor == nil || br.TeamsThreadStore == nil {
		return
	}
	// Legacy portal reactions are handled in the default mautrix bridge handler.
	if br.GetPortalByMXID(evt.RoomID) != nil {
		return
	}
	if _, ok := br.TeamsThreadStore.GetThreadID(evt.RoomID); !ok {
		return
	}
	if evt.Sender == br.Bot.UserID || br.IsGhost(evt.Sender) {
		return
	}

	user := br.GetIUser(evt.Sender, true)
	if user == nil || user.GetPermissionLevel() < bridgeconfig.PermissionLevelUser {
		return
	}
	if val, ok := evt.Content.Raw[appservice.DoublePuppetKey]; ok && val == br.Name && user.GetIDoublePuppet() != nil {
		return
	}

	portal := &TeamsConsumerPortal{bridge: br, roomID: evt.RoomID}
	portal.ReceiveMatrixEvent(user, evt)
}
