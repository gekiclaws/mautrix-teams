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
)

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

	consumer := c.getConsumer()
	if consumer == nil {
		return nil, errors.New("missing consumer client")
	}
	consumer.Token = c.Meta.SkypeToken

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

	consumer := c.getConsumer()
	if consumer == nil {
		return errors.New("missing consumer client")
	}
	consumer.Token = c.Meta.SkypeToken

	teamsMessageID := NormalizeTeamsReactionTargetMessageID(string(msg.TargetReaction.MessageID))
	if teamsMessageID == "" {
		return errors.New("missing teams message id for reaction removal")
	}
	_, err := consumer.RemoveReaction(ctx, threadID, teamsMessageID, emotionKey)
	return err
}
