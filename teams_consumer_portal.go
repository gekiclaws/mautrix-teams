package main

import (
	"context"
	"errors"

	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
)

type TeamsConsumerPortal struct {
	bridge *DiscordBridge
	roomID id.RoomID
}

func (portal *TeamsConsumerPortal) IsEncrypted() bool {
	if portal == nil || portal.bridge == nil || portal.bridge.AS == nil || portal.bridge.AS.StateStore == nil {
		return false
	}
	return portal.bridge.AS.StateStore.IsEncrypted(portal.roomID)
}

func (portal *TeamsConsumerPortal) IsPrivateChat() bool {
	return false
}

func (portal *TeamsConsumerPortal) MarkEncrypted() {}

func (portal *TeamsConsumerPortal) MainIntent() *appservice.IntentAPI {
	if portal == nil || portal.bridge == nil {
		return nil
	}
	return portal.bridge.Bot
}

func (portal *TeamsConsumerPortal) ReceiveMatrixEvent(user bridge.User, evt *event.Event) {
	if portal == nil || portal.bridge == nil || evt == nil || user == nil {
		return
	}
	if user.GetPermissionLevel() < bridgeconfig.PermissionLevelUser {
		return
	}
	portal.handleMatrixMessage(user.(*User), evt)
}

func (portal *TeamsConsumerPortal) UpdateBridgeInfo() {}

func (portal *TeamsConsumerPortal) handleMatrixMessage(sender *User, evt *event.Event) {
	if sender == nil || evt == nil {
		return
	}
	if evt.Type != event.EventMessage {
		return
	}
	if evt.Content.Parsed == nil {
		_ = evt.Content.ParseRaw(evt.Type)
	}
	content, ok := evt.Content.Parsed.(*event.MessageEventContent)
	if !ok || content == nil {
		return
	}
	if content.MsgType != event.MsgText {
		return
	}
	if portal.bridge.TeamsConsumerSender == nil {
		portal.bridge.ZLog.Warn().Msg("Teams consumer sender not configured")
		return
	}

	writer := func(ctx context.Context, status database.TeamsSendStatus, clientMessageID string, ts int64) error {
		return portal.writeMSSMetadata(sender, evt, status, clientMessageID, ts)
	}

	err := portal.bridge.TeamsConsumerSender.SendMatrixText(context.Background(), portal.roomID, content.Body, evt.ID, writer)
	if err != nil {
		portal.bridge.ZLog.Warn().Err(err).Str("event_id", evt.ID.String()).Msg("Teams consumer send failed")
	}
}

func (portal *TeamsConsumerPortal) writeMSSMetadata(sender *User, evt *event.Event, status database.TeamsSendStatus, clientMessageID string, ts int64) error {
	intent := portal.intentForMSS(sender)
	if intent == nil {
		return errors.New("missing matrix intent for MSS")
	}
	if evt.Content.Parsed == nil {
		_ = evt.Content.ParseRaw(evt.Type)
	}
	content, ok := evt.Content.Parsed.(*event.MessageEventContent)
	if !ok || content == nil {
		return errors.New("missing message content")
	}
	edited := *content
	edited.SetEdit(evt.ID)
	edited.Body = content.Body
	if content.Format != "" {
		edited.FormattedBody = content.FormattedBody
	}

	mss := map[string]any{
		"status":            string(status),
		"client_message_id": clientMessageID,
		"ts":                ts,
	}
	extra := map[string]any{
		"com.beeper.teams.mss": mss,
	}

	wrapped := event.Content{Parsed: &edited, Raw: extra}
	_, err := intent.SendMessageEvent(portal.roomID, event.EventMessage, &wrapped)
	return err
}

func (portal *TeamsConsumerPortal) intentForMSS(sender *User) *appservice.IntentAPI {
	if sender == nil {
		return nil
	}
	dp := sender.GetIDoublePuppet()
	if dp == nil || dp.CustomIntent() == nil {
		return nil
	}
	return dp.CustomIntent()
}
