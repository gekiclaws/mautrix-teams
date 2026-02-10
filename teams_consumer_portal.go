package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
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
		portal.handleMatrixMessage(user, evt)
	case event.EventReaction:
		portal.handleMatrixReaction(user, evt)
	case event.EventRedaction:
		portal.handleMatrixRedaction(user, evt)
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

func (portal *TeamsConsumerPortal) handleMatrixMessage(sender bridge.User, evt *event.Event) {
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
	if portal.bridge.TeamsConsumerSender == nil {
		portal.bridge.ZLog.Warn().Msg("Teams consumer sender not configured")
		return
	}

	writer := func(ctx context.Context, status database.TeamsSendStatus, clientMessageID string, ts int64) error {
		return portal.writeMSSMetadata(evt, status, clientMessageID, ts)
	}

	var err error
	switch content.MsgType {
	case event.MsgText:
		err = portal.bridge.TeamsConsumerSender.SendMatrixText(context.Background(), portal.roomID, content.Body, evt.ID, portal.intentMXIDForMSS(sender), writer)
	case event.MsgImage:
		title, gifURL, ok := portal.extractOutboundGIF(content)
		if !ok {
			return
		}
		err = portal.bridge.TeamsConsumerSender.SendMatrixGIF(context.Background(), portal.roomID, gifURL, title, evt.ID, portal.intentMXIDForMSS(sender), writer)
	default:
		return
	}
	if err != nil {
		portal.bridge.ZLog.Warn().Err(err).Str("event_id", evt.ID.String()).Msg("Teams consumer send failed")
	}
}

func (portal *TeamsConsumerPortal) extractOutboundGIF(content *event.MessageEventContent) (title string, gifURL string, ok bool) {
	if content == nil {
		return "", "", false
	}
	if !looksLikeGIFMessage(content) {
		return "", "", false
	}

	title = strings.TrimSpace(content.FileName)
	if title == "" {
		title = strings.TrimSpace(content.Body)
	}
	if title == "" {
		title = "GIF"
	}

	rawURL := strings.TrimSpace(string(content.URL))
	if parsedGIFURL, ok := parseDirectGIFURL(rawURL); ok {
		return title, parsedGIFURL, true
	}
	return "", "", false
}

func looksLikeGIFMessage(content *event.MessageEventContent) bool {
	if content == nil {
		return false
	}
	if content.Info != nil && strings.EqualFold(strings.TrimSpace(content.Info.MimeType), "image/gif") {
		return true
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(content.FileName)), ".gif") {
		return true
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(content.Body)), ".gif") {
		return true
	}
	if _, ok := parseDirectGIFURL(strings.TrimSpace(string(content.URL))); ok {
		return true
	}
	return false
}

func parseDirectGIFURL(value string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if strings.HasSuffix(strings.ToLower(parsed.Path), ".gif") {
			return parsed.String(), true
		}
		return "", false
	default:
		return "", false
	}
}

func (portal *TeamsConsumerPortal) handleMatrixReaction(sender bridge.User, evt *event.Event) {
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

func (portal *TeamsConsumerPortal) handleMatrixRedaction(sender bridge.User, evt *event.Event) {
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

func (portal *TeamsConsumerPortal) writeMSSMetadata(evt *event.Event, status database.TeamsSendStatus, clientMessageID string, ts int64) error {
	intent := portal.intentForMSS(clientMessageID)
	if intent == nil {
		return errors.New("missing matrix intent for MSS")
	}
	if evt == nil || evt.ID == "" {
		return errors.New("missing event for MSS status")
	}

	mappedStatus := event.MessageStatusPending
	switch status {
	case database.TeamsSendStatusAccepted:
		mappedStatus = event.MessageStatusSuccess
	case database.TeamsSendStatusFailed:
		mappedStatus = event.MessageStatusRetriable
	}

	network := "teams"
	if portal != nil && portal.bridge != nil && portal.bridge.BeeperServiceName != "" {
		network = portal.bridge.BeeperServiceName
	}

	content := event.BeeperMessageStatusEventContent{
		Network: network,
		RelatesTo: event.RelatesTo{
			Type:    event.RelReference,
			EventID: evt.ID,
		},
		Status: mappedStatus,
	}
	if mappedStatus == event.MessageStatusRetriable {
		content.Reason = event.MessageStatusNetworkError
		content.Message = "Failed to send message to Teams"
	}

	_, err := intent.SendMessageEvent(portal.roomID, event.BeeperMessageStatus, &content)
	return err
}

func (portal *TeamsConsumerPortal) intentMXIDForMSS(sender bridge.User) id.UserID {
	if sender == nil {
		return ""
	}
	return sender.GetMXID()
}

func (portal *TeamsConsumerPortal) intentForMSS(clientMessageID string) *appservice.IntentAPI {
	if portal == nil {
		return nil
	}
	fallback := portal.MainIntent()
	if portal.bridge == nil || portal.bridge.DB == nil || portal.bridge.DB.TeamsSendIntent == nil {
		return fallback
	}
	intentEntry := portal.bridge.DB.TeamsSendIntent.GetByClientMessageID(clientMessageID)
	if intentEntry == nil || intentEntry.IntentMXID == "" {
		return fallback
	}
	puppet := portal.bridge.GetPuppetByCustomMXID(intentEntry.IntentMXID)
	if puppet == nil || puppet.CustomIntent() == nil {
		return fallback
	}
	return puppet.CustomIntent()
}
