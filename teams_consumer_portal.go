package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
)

type TeamsConsumerPortal struct {
	bridge *TeamsBridge
	roomID id.RoomID

	currentlyTyping     []id.UserID
	currentlyTypingLock sync.Mutex
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
	switch evt.Type {
	case event.EventMessage:
		portal.handleMatrixMessage(user.(*User), evt)
	case event.EventReaction:
		portal.handleMatrixReaction(user.(*User), evt)
	case event.EventRedaction:
		portal.handleMatrixRedaction(user.(*User), evt)
	}
}

func (portal *TeamsConsumerPortal) UpdateBridgeInfo() {}

func teamsTypingDiff(prev, new []id.UserID) (started []id.UserID) {
OuterNew:
	for _, userID := range new {
		for _, previousUserID := range prev {
			if userID == previousUserID {
				continue OuterNew
			}
		}
		started = append(started, userID)
	}
	return
}

func (portal *TeamsConsumerPortal) HandleMatrixTyping(newTyping []id.UserID) {
	if portal == nil || portal.bridge == nil {
		return
	}
	portal.currentlyTypingLock.Lock()
	startedTyping := teamsTypingDiff(portal.currentlyTyping, newTyping)
	portal.currentlyTyping = newTyping
	portal.currentlyTypingLock.Unlock()

	typer := portal.bridge.TeamsConsumerTyper
	if typer == nil {
		return
	}
	for range startedTyping {
		if err := typer.SendTyping(context.Background(), portal.roomID); err != nil {
			portal.bridge.ZLog.Warn().Err(err).Str("room_id", portal.roomID.String()).Msg("Teams consumer typing failed")
		}
	}
}

func (portal *TeamsConsumerPortal) HandleMatrixReadReceipt(brUser bridge.User, eventID id.EventID, receipt event.ReadReceipt) {
	if portal == nil || portal.bridge == nil {
		return
	}
	sender := portal.bridge.TeamsConsumerReceipt
	if sender == nil {
		return
	}
	if err := sender.SendReadReceipt(context.Background(), portal.roomID, time.Now().UTC()); err != nil {
		portal.bridge.ZLog.Warn().
			Err(err).
			Str("room_id", portal.roomID.String()).
			Str("event_id", eventID.String()).
			Str("sender", brUser.GetMXID().String()).
			Msg("Teams consumer read receipt failed")
	}
}

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

func (portal *TeamsConsumerPortal) handleMatrixReaction(sender *User, evt *event.Event) {
	if sender == nil || evt == nil {
		return
	}
	if portal.bridge.TeamsConsumerReactor == nil {
		portal.bridge.ZLog.Warn().Msg("Teams consumer reactor not configured")
		return
	}

	if err := portal.bridge.TeamsConsumerReactor.AddMatrixReaction(context.Background(), portal.roomID, evt); err != nil {
		portal.bridge.ZLog.Warn().Err(err).Str("event_id", evt.ID.String()).Msg("Teams consumer reaction add failed")
	}
}

func (portal *TeamsConsumerPortal) handleMatrixRedaction(sender *User, evt *event.Event) {
	if sender == nil || evt == nil {
		return
	}
	if portal.bridge.TeamsConsumerReactor == nil {
		portal.bridge.ZLog.Warn().Msg("Teams consumer reactor not configured")
		return
	}
	if err := portal.bridge.TeamsConsumerReactor.RemoveMatrixReaction(context.Background(), portal.roomID, evt); err != nil {
		portal.bridge.ZLog.Warn().Err(err).Str("event_id", evt.ID.String()).Msg("Teams consumer reaction remove failed")
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
