package connector

import (
	"context"
	"strings"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"

	"go.mau.fi/mautrix-teams/internal/teams/model"
	"go.mau.fi/mautrix-teams/pkg/teamsdb"
)

func (c *TeamsClient) queueReactionSyncForMessage(ctx context.Context, th *teamsdb.ThreadState, msg model.RemoteMessage) {
	if c == nil || c.Login == nil || th == nil {
		return
	}
	messageID := NormalizeTeamsReactionMessageID(msg.MessageID)
	if messageID == "" {
		messageID = NormalizeTeamsReactionMessageID(msg.SequenceID)
	}
	if messageID == "" {
		return
	}

	data, hasReactions := c.buildReactionSyncData(msg.Reactions)
	if !hasReactions && !c.shouldSendEmptyReactionSync(ctx, th.ThreadID, messageID) {
		return
	}
	if data == nil {
		data = &bridgev2.ReactionSyncData{Users: map[networkid.UserID]*bridgev2.ReactionSyncUser{}, HasAllUsers: true}
	}
	evt := &simplevent.ReactionSync{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventReactionSync,
			PortalKey: c.portalKey(th.ThreadID),
			Timestamp: msg.Timestamp,
		},
		TargetMessage: networkid.MessageID(messageID),
		Reactions:     data,
	}
	c.Login.QueueRemoteEvent(evt)
}

func (c *TeamsClient) buildReactionSyncData(reactions []model.MessageReaction) (*bridgev2.ReactionSyncData, bool) {
	if len(reactions) == 0 {
		return nil, false
	}
	users := make(map[networkid.UserID]*bridgev2.ReactionSyncUser)
	selfID := model.NormalizeTeamsUserID("")
	if c != nil && c.Meta != nil {
		selfID = model.NormalizeTeamsUserID(c.Meta.TeamsUserID)
	}

	for _, reaction := range reactions {
		emotionKey := strings.TrimSpace(reaction.EmotionKey)
		if emotionKey == "" {
			continue
		}
		emoji, ok := MapEmotionKeyToEmoji(emotionKey)
		if !ok {
			continue
		}
		for _, user := range reaction.Users {
			userID := model.NormalizeTeamsUserID(user.MRI)
			if userID == "" {
				continue
			}
			sender := bridgev2.EventSender{
				Sender: teamsUserIDToNetworkUserID(userID),
			}
			if selfID != "" && userID == selfID {
				sender.IsFromMe = true
				if c != nil && c.Login != nil {
					sender.SenderLogin = c.Login.ID
				}
			}
			rsu := users[sender.Sender]
			if rsu == nil {
				rsu = &bridgev2.ReactionSyncUser{HasAllReactions: true}
				users[sender.Sender] = rsu
			}
			br := &bridgev2.BackfillReaction{
				Sender:  sender,
				EmojiID: networkid.EmojiID(emotionKey),
				Emoji:   emoji,
			}
			if user.TimeMS != 0 {
				br.Timestamp = time.UnixMilli(user.TimeMS).UTC()
			}
			rsu.Reactions = append(rsu.Reactions, br)
		}
	}
	if len(users) == 0 {
		return nil, false
	}
	return &bridgev2.ReactionSyncData{
		Users:       users,
		HasAllUsers: true,
	}, true
}

func (c *TeamsClient) shouldSendEmptyReactionSync(ctx context.Context, threadID string, messageID string) bool {
	if c == nil {
		return false
	}
	if c.markReactionSeen(messageID, false) {
		return true
	}
	if c.Main == nil || c.Main.Bridge == nil || c.Main.Bridge.DB == nil {
		return false
	}
	existing, err := c.Main.Bridge.DB.Reaction.GetAllToMessage(ctx, c.portalKey(threadID).Receiver, networkid.MessageID(messageID))
	if err != nil {
		return false
	}
	return len(existing) > 0
}

func (c *TeamsClient) markReactionSeen(messageID string, seen bool) bool {
	c.reactionSeenMu.Lock()
	defer c.reactionSeenMu.Unlock()
	if c.reactionSeen == nil {
		c.reactionSeen = make(map[string]struct{})
	}
	_, exists := c.reactionSeen[messageID]
	if seen {
		c.reactionSeen[messageID] = struct{}{}
	} else if exists {
		delete(c.reactionSeen, messageID)
	}
	return exists
}
