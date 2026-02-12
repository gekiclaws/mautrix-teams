package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"

	consumerclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

func (c *TeamsClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	if !c.IsLoggedIn() {
		return nil, bridgev2.ErrNotLoggedIn
	}
	if err := c.ensureValidSkypeToken(ctx); err != nil {
		return nil, err
	}
	if msg == nil || msg.Content == nil {
		return nil, bridgev2.ErrUnsupportedMessageType
	}
	threadID := strings.TrimSpace(string(msg.Portal.ID))
	if threadID == "" {
		return nil, errors.New("missing thread id")
	}

	consumer := c.newConsumer()
	if consumer == nil {
		return nil, errors.New("missing consumer client")
	}

	clientMessageID := consumerclient.GenerateClientMessageID()
	msg.AddPendingToIgnore(networkid.TransactionID(clientMessageID))

	now := time.Now().UTC()
	var err error
	switch msg.Content.MsgType {
	case event.MsgText:
		_, err = consumer.SendMessageWithID(ctx, threadID, msg.Content.Body, c.Meta.TeamsUserID, clientMessageID)
	case event.MsgImage:
		title, gifURL, ok := extractOutboundGIF(msg.Content)
		if !ok {
			return nil, bridgev2.ErrUnsupportedMessageType
		}
		_, err = consumer.SendGIFWithID(ctx, threadID, gifURL, title, c.Meta.TeamsUserID, clientMessageID)
	default:
		return nil, bridgev2.ErrUnsupportedMessageType
	}
	if err != nil {
		return nil, err
	}
	c.recordSelfMessage(clientMessageID)

	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        networkid.MessageID(clientMessageID),
			SenderID:  teamsUserIDToNetworkUserID(c.Meta.TeamsUserID),
			Timestamp: now,
		},
		StreamOrder:   now.UnixMilli(),
		RemovePending: networkid.TransactionID(clientMessageID),
	}, nil
}

var errUnsupportedReactionEmoji = bridgev2.WrapErrorInStatus(errors.New("unsupported reaction emoji")).
	WithErrorAsMessage().
	WithIsCertain(true).
	WithSendNotice(true).
	WithErrorReason(event.MessageStatusUnsupported)

func (c *TeamsClient) PreHandleMatrixReaction(ctx context.Context, msg *bridgev2.MatrixReaction) (bridgev2.MatrixReactionPreResponse, error) {
	if !c.IsLoggedIn() {
		return bridgev2.MatrixReactionPreResponse{}, bridgev2.ErrNotLoggedIn
	}
	if err := c.ensureValidSkypeToken(ctx); err != nil {
		return bridgev2.MatrixReactionPreResponse{}, err
	}
	if msg == nil || msg.Content == nil {
		return bridgev2.MatrixReactionPreResponse{}, errors.New("missing reaction content")
	}
	if strings.TrimSpace(c.Meta.TeamsUserID) == "" {
		return bridgev2.MatrixReactionPreResponse{}, errors.New("missing teams user id")
	}
	emoji := strings.TrimSpace(msg.Content.RelatesTo.Key)
	emotionKey, ok := MapEmojiToEmotionKey(emoji)
	if !ok {
		return bridgev2.MatrixReactionPreResponse{}, errUnsupportedReactionEmoji
	}
	return bridgev2.MatrixReactionPreResponse{
		SenderID: teamsUserIDToNetworkUserID(c.Meta.TeamsUserID),
		EmojiID:  networkid.EmojiID(emotionKey),
		Emoji:    emoji,
	}, nil
}

func (c *TeamsClient) HandleMatrixReaction(ctx context.Context, msg *bridgev2.MatrixReaction) (*database.Reaction, error) {
	if !c.IsLoggedIn() {
		return nil, bridgev2.ErrNotLoggedIn
	}
	if err := c.ensureValidSkypeToken(ctx); err != nil {
		return nil, err
	}
	if msg == nil || msg.Content == nil {
		return nil, errors.New("missing reaction content")
	}
	if msg.TargetMessage == nil || msg.TargetMessage.ID == "" {
		return nil, bridgev2.ErrTargetMessageNotFound
	}
	threadID := strings.TrimSpace(string(msg.Portal.ID))
	if threadID == "" {
		return nil, errors.New("missing thread id")
	}

	emotionKey := ""
	emoji := strings.TrimSpace(msg.Content.RelatesTo.Key)
	if msg.PreHandleResp != nil && msg.PreHandleResp.EmojiID != "" {
		emotionKey = string(msg.PreHandleResp.EmojiID)
		if msg.PreHandleResp.Emoji != "" {
			emoji = msg.PreHandleResp.Emoji
		}
	}
	if emotionKey == "" {
		var ok bool
		emotionKey, ok = MapEmojiToEmotionKey(emoji)
		if !ok {
			return nil, errUnsupportedReactionEmoji
		}
	}

	consumer := c.newConsumer()
	if consumer == nil {
		return nil, errors.New("missing consumer client")
	}

	teamsMessageID := NormalizeTeamsReactionTargetMessageID(string(msg.TargetMessage.ID))
	if teamsMessageID == "" {
		return nil, fmt.Errorf("missing teams message id for reaction target %s", msg.TargetMessage.ID)
	}
	_, err := consumer.AddReaction(ctx, threadID, teamsMessageID, emotionKey, time.Now().UTC().UnixMilli())
	if err != nil {
		return nil, err
	}

	return &database.Reaction{
		EmojiID: networkid.EmojiID(emotionKey),
		Emoji:   emoji,
	}, nil
}

func (c *TeamsClient) HandleMatrixReactionRemove(ctx context.Context, msg *bridgev2.MatrixReactionRemove) error {
	if !c.IsLoggedIn() {
		return bridgev2.ErrNotLoggedIn
	}
	if err := c.ensureValidSkypeToken(ctx); err != nil {
		return err
	}
	if msg == nil || msg.TargetReaction == nil {
		return nil
	}
	threadID := strings.TrimSpace(string(msg.Portal.ID))
	if threadID == "" {
		return errors.New("missing thread id")
	}

	emotionKey := strings.TrimSpace(string(msg.TargetReaction.EmojiID))
	if emotionKey == "" {
		var ok bool
		emotionKey, ok = MapEmojiToEmotionKey(msg.TargetReaction.Emoji)
		if !ok {
			return nil
		}
	}

	consumer := c.newConsumer()
	if consumer == nil {
		return errors.New("missing consumer client")
	}

	teamsMessageID := NormalizeTeamsReactionTargetMessageID(string(msg.TargetReaction.MessageID))
	if teamsMessageID == "" {
		return errors.New("missing teams message id for reaction removal")
	}
	_, err := consumer.RemoveReaction(ctx, threadID, teamsMessageID, emotionKey)
	return err
}

func (c *TeamsClient) HandleMatrixTyping(ctx context.Context, msg *bridgev2.MatrixTyping) error {
	if !c.IsLoggedIn() {
		return bridgev2.ErrNotLoggedIn
	}
	if err := c.ensureValidSkypeToken(ctx); err != nil {
		return err
	}
	if msg == nil || !msg.IsTyping {
		return nil
	}
	threadID := strings.TrimSpace(string(msg.Portal.ID))
	if threadID == "" {
		return errors.New("missing thread id")
	}
	consumer := c.newConsumer()
	if consumer == nil {
		return errors.New("missing consumer client")
	}
	_, err := consumer.SendTypingIndicator(ctx, threadID, c.Meta.TeamsUserID)
	return err
}

func (c *TeamsClient) HandleMatrixReadReceipt(ctx context.Context, msg *bridgev2.MatrixReadReceipt) error {
	if !c.IsLoggedIn() {
		return bridgev2.ErrNotLoggedIn
	}
	if err := c.ensureValidSkypeToken(ctx); err != nil {
		return err
	}
	if msg == nil {
		return nil
	}
	threadID := strings.TrimSpace(string(msg.Portal.ID))
	if threadID == "" {
		return errors.New("missing thread id")
	}
	if !c.shouldSendReceipt(threadID) {
		return nil
	}
	consumer := c.newConsumer()
	if consumer == nil {
		return errors.New("missing consumer client")
	}
	horizon := consumerclient.ConsumptionHorizonNow(time.Now().UTC())
	_, err := consumer.SetConsumptionHorizon(ctx, threadID, horizon)
	return err
}

func (c *TeamsClient) shouldSendReceipt(threadID string) bool {
	c.unreadMu.Lock()
	defer c.unreadMu.Unlock()
	if c.unreadSeen == nil {
		c.unreadSeen = make(map[string]bool)
	}
	if c.unreadSent == nil {
		c.unreadSent = make(map[string]bool)
	}
	if !c.unreadSeen[threadID] {
		return false
	}
	if c.unreadSent[threadID] {
		return false
	}
	c.unreadSent[threadID] = true
	return true
}

func (c *TeamsClient) markUnread(threadID string) {
	c.unreadMu.Lock()
	defer c.unreadMu.Unlock()
	if c.unreadSeen == nil {
		c.unreadSeen = make(map[string]bool)
	}
	if c.unreadSent == nil {
		c.unreadSent = make(map[string]bool)
	}
	c.unreadSeen[threadID] = true
	c.unreadSent[threadID] = false
}
