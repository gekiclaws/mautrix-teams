package connector

// Teams -> Matrix ingest and sync loops.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/bridgev2/status"

	"go.mau.fi/mautrix-teams/internal/teams/auth"
	"go.mau.fi/mautrix-teams/internal/teams/model"
	"go.mau.fi/mautrix-teams/pkg/teamsdb"
	"go.mau.fi/mautrix-teams/pkg/teamsid"
)

const (
	threadDiscoveryInterval = 30 * time.Second
	selfMessageTTL          = 5 * time.Minute
)

func (c *TeamsClient) startSyncLoop() {
	if c == nil || c.Login == nil {
		return
	}
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	if c.syncCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(c.Login.Log.WithContext(context.Background()))
	c.syncCancel = cancel
	c.syncDone = make(chan struct{})
	go func() {
		defer close(c.syncDone)
		c.syncLoop(ctx)
	}()
}

func (c *TeamsClient) stopSyncLoop(timeout time.Duration) {
	c.syncMu.Lock()
	cancel := c.syncCancel
	done := c.syncDone
	c.syncCancel = nil
	c.syncDone = nil
	c.syncMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done == nil {
		return
	}
	if timeout <= 0 {
		<-done
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

func (c *TeamsClient) syncLoop(ctx context.Context) {
	log := zerolog.Ctx(ctx)
	// Run once immediately to seed portals.
	err := c.syncOnce(ctx)
	if err != nil {
		log.Err(err).Msg("Teams sync loop initial run failed")
	}

	// Poll threads continuously with per-thread scheduling until canceled.
	if err := c.pollDueThreads(ctx, err == nil); err != nil && !errors.Is(err, context.Canceled) {
		log.Err(err).Msg("Teams polling loop exited")
	}
}

func (c *TeamsClient) syncOnce(ctx context.Context) error {
	if err := c.refreshThreads(ctx); err != nil {
		return err
	}
	// Poll once over all known threads.
	return c.pollAllThreadsOnce(ctx)
}

func (c *TeamsClient) ensureValidSkypeToken(ctx context.Context) error {
	if c == nil || c.Login == nil {
		return errors.New("missing client/login")
	}
	if c.Meta == nil {
		if meta, ok := c.Login.Metadata.(*teamsid.UserLoginMetadata); ok {
			c.Meta = meta
		}
	}
	if c.Meta == nil {
		return errors.New("missing login metadata")
	}

	now := time.Now().UTC()
	if c.Meta.SkypeToken != "" && c.Meta.SkypeTokenExpiresAt != 0 {
		expiresAt := time.Unix(c.Meta.SkypeTokenExpiresAt, 0).UTC()
		if now.Add(auth.SkypeTokenExpirySkew).Before(expiresAt) {
			return nil
		}
	}
	refresh := strings.TrimSpace(c.Meta.RefreshToken)
	if refresh == "" {
		return errors.New("missing refresh token, re-login required")
	}

	authClient := auth.NewClient(nil)
	if c.Main != nil && strings.TrimSpace(c.Main.Config.ClientID) != "" {
		authClient.ClientID = strings.TrimSpace(c.Main.Config.ClientID)
	}

	state, err := authClient.RefreshAccessToken(ctx, refresh)
	if err != nil {
		return err
	}
	if strings.TrimSpace(state.RefreshToken) != "" {
		c.Meta.RefreshToken = strings.TrimSpace(state.RefreshToken)
	}

	skypeToken, skypeExpiresAt, skypeID, err := authClient.AcquireSkypeToken(ctx, state.AccessToken)
	if err != nil {
		return err
	}

	c.Meta.AccessTokenExpiresAt = state.ExpiresAtUnix
	c.Meta.SkypeToken = skypeToken
	c.Meta.SkypeTokenExpiresAt = skypeExpiresAt
	c.Meta.TeamsUserID = auth.NormalizeTeamsUserID(skypeID)
	c.Login.RemoteName = c.Meta.TeamsUserID

	if err := c.Login.Save(ctx); err != nil {
		c.Login.Log.Err(err).Msg("Failed to persist refreshed login metadata")
	}
	return nil
}

func (c *TeamsClient) refreshThreads(ctx context.Context) error {
	if c == nil || c.Main == nil || c.Main.DB == nil || c.Login == nil {
		return nil
	}
	if err := c.ensureValidSkypeToken(ctx); err != nil {
		c.Login.BridgeState.Send(status.BridgeState{StateEvent: status.StateBadCredentials, Message: err.Error(), UserAction: status.UserActionRelogin})
		return err
	}

	consumer := c.newConsumer()
	if consumer == nil {
		return errors.New("missing consumer client")
	}
	convs, err := consumer.ListConversations(ctx, c.Meta.SkypeToken)
	if err != nil {
		return err
	}

	for _, conv := range convs {
		thread, ok := conv.NormalizeForSelf(c.Meta.TeamsUserID)
		if !ok || strings.TrimSpace(thread.ID) == "" || strings.TrimSpace(thread.ConversationID) == "" {
			continue
		}
		_ = c.Main.DB.ThreadState.Upsert(ctx, &teamsdb.ThreadState{
			BridgeID:     c.Main.Bridge.ID,
			UserLoginID:  c.Login.ID,
			ThreadID:     thread.ID,
			Conversation: thread.ConversationID,
			IsOneToOne:   thread.IsOneToOne,
			Name:         thread.RoomName,
		})

		name := thread.RoomName
		roomType := ptrRoomType(thread.IsOneToOne)
		chatInfo := &bridgev2.ChatInfo{Name: &name, Type: roomType}
		c.Login.QueueRemoteEvent(&simplevent.ChatResync{
			EventMeta: simplevent.EventMeta{
				Type:         bridgev2.RemoteEventChatResync,
				PortalKey:    c.portalKey(thread.ID),
				CreatePortal: true,
				Timestamp:    time.Now().UTC(),
			},
			ChatInfo: chatInfo,
		})
	}
	return nil
}

func ptrRoomType(isOneToOne bool) *database.RoomType {
	t := database.RoomTypeDefault
	if isOneToOne {
		t = database.RoomTypeDM
	}
	return &t
}

type pollState struct {
	backoff  PollBackoff
	nextPoll time.Time
}

func (c *TeamsClient) pollAllThreadsOnce(ctx context.Context) error {
	threads, err := c.Main.DB.ThreadState.ListForLogin(ctx, c.Login.ID)
	if err != nil {
		return err
	}
	for _, th := range threads {
		_, _ = c.pollThread(ctx, th, time.Now().UTC())
	}
	return nil
}

func (c *TeamsClient) pollDueThreads(ctx context.Context, initialDiscoverySucceeded bool) error {
	if c == nil || c.Main == nil || c.Main.DB == nil || c.Login == nil {
		return nil
	}
	log := zerolog.Ctx(ctx)
	threads, err := c.Main.DB.ThreadState.ListForLogin(ctx, c.Login.ID)
	if err != nil {
		return err
	}

	states := make(map[string]*pollState, len(threads))
	for _, th := range threads {
		if th == nil || th.ThreadID == "" {
			continue
		}
		states[th.ThreadID] = &pollState{backoff: PollBackoff{Delay: pollBaseDelay}, nextPoll: time.Now().UTC()}
	}

	nextDiscovery := time.Now().UTC().Add(threadDiscoveryInterval)
	if !initialDiscoverySucceeded {
		nextDiscovery = time.Time{}
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		now := time.Now().UTC()
		if nextDiscovery.IsZero() || !now.Before(nextDiscovery) {
			if err := c.refreshThreads(ctx); err != nil {
				log.Err(err).Msg("Teams thread discovery refresh failed")
			}
			nextDiscovery = now.Add(threadDiscoveryInterval)
		}
		nextWake := now.Add(5 * time.Second)

		threads, err := c.Main.DB.ThreadState.ListForLogin(ctx, c.Login.ID)
		if err != nil {
			return err
		}
		for _, th := range threads {
			if th == nil || th.ThreadID == "" {
				continue
			}
			ps := states[th.ThreadID]
			if ps == nil {
				ps = &pollState{backoff: PollBackoff{Delay: pollBaseDelay}, nextPoll: now}
				states[th.ThreadID] = ps
			}
			if now.Before(ps.nextPoll) {
				if ps.nextPoll.Before(nextWake) {
					nextWake = ps.nextPoll
				}
				continue
			}

			ingested, err := c.pollThread(ctx, th, now)
			delay, _ := ApplyPollBackoff(&ps.backoff, ingested, err)
			ps.nextPoll = now.Add(delay)
			if ps.nextPoll.Before(nextWake) {
				nextWake = ps.nextPoll
			}
		}

		sleep := time.Until(nextWake)
		if sleep < 100*time.Millisecond {
			sleep = 100 * time.Millisecond
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *TeamsClient) pollThread(ctx context.Context, th *teamsdb.ThreadState, now time.Time) (int, error) {
	if c == nil || th == nil {
		return 0, nil
	}
	if err := c.ensureValidSkypeToken(ctx); err != nil {
		c.Login.BridgeState.Send(status.BridgeState{StateEvent: status.StateBadCredentials, Message: err.Error(), UserAction: status.UserActionRelogin})
		return 0, err
	}
	consumer := c.newConsumer()
	if consumer == nil {
		return 0, errors.New("missing consumer client")
	}

	msgs, err := consumer.ListMessages(ctx, th.Conversation, th.LastSequenceID)
	if err != nil {
		return 0, err
	}

	lastSeq := strings.TrimSpace(th.LastSequenceID)
	var maxSeq string
	var maxTS int64
	ingested := 0
	selfID := ""
	if c.Meta != nil {
		selfID = model.NormalizeTeamsUserID(c.Meta.TeamsUserID)
	}

	for _, msg := range msgs {
		if strings.TrimSpace(msg.MessageID) == "" {
			continue
		}
		// Filter already-seen messages in case the remote API returns history.
		if lastSeq != "" && model.CompareSequenceID(strings.TrimSpace(msg.SequenceID), lastSeq) <= 0 {
			// Still process reactions on older messages for sync parity.
			c.queueReactionSyncForMessage(ctx, th, msg, "")
			continue
		}

		senderID := model.NormalizeTeamsUserID(msg.SenderID)
		displayName := strings.TrimSpace(msg.IMDisplayName)
		if displayName == "" {
			displayName = strings.TrimSpace(msg.TokenDisplayName)
		}
		if displayName == "" {
			displayName = senderID
		}
		_ = c.Main.DB.Profile.Upsert(ctx, senderID, displayName, now)
		msg.SenderName = displayName

		effectiveMessageID := NormalizeTeamsReactionMessageID(msg.MessageID)
		if effectiveMessageID == "" {
			effectiveMessageID = NormalizeTeamsReactionMessageID(msg.SequenceID)
		}

		es := bridgev2.EventSender{Sender: teamsUserIDToNetworkUserID(senderID)}
		if senderID != "" && selfID != "" && senderID == selfID {
			es.IsFromMe = true
			es.SenderLogin = c.Login.ID
			if strings.TrimSpace(msg.ClientMessageID) != "" {
				effectiveMessageID = NormalizeTeamsReactionMessageID(msg.ClientMessageID)
			}
		}
		if effectiveMessageID != "" && len(msg.Reactions) > 0 {
			c.markReactionSeen(effectiveMessageID, true)
		}
		c.queueReactionSyncForMessage(ctx, th, msg, effectiveMessageID)

		clientMessageID := strings.TrimSpace(msg.ClientMessageID)
		isSelfEcho := senderID != "" && selfID != "" && senderID == selfID && c.consumeSelfMessage(clientMessageID)
		if maxSeq == "" || model.CompareSequenceID(strings.TrimSpace(msg.SequenceID), maxSeq) > 0 {
			maxSeq = strings.TrimSpace(msg.SequenceID)
		}
		if ts := msg.Timestamp.UnixMilli(); ts > maxTS {
			maxTS = ts
		}
		ingested++
		if isSelfEcho {
			continue
		}

		eventMessageID := effectiveMessageID
		if eventMessageID == "" {
			eventMessageID = strings.TrimSpace(msg.MessageID)
		}
		evt := &simplevent.Message[model.RemoteMessage]{
			EventMeta: simplevent.EventMeta{
				Type:         bridgev2.RemoteEventMessage,
				PortalKey:    c.portalKey(th.ThreadID),
				Sender:       es,
				CreatePortal: true,
				Timestamp:    msg.Timestamp,
				StreamOrder:  msg.Timestamp.UnixMilli(),
			},
			Data:               msg,
			ID:                 networkid.MessageID(eventMessageID),
			TransactionID:      networkid.TransactionID(clientMessageID),
			ConvertMessageFunc: convertTeamsMessage,
		}
		c.Login.QueueRemoteEvent(evt)
		if !es.IsFromMe && strings.TrimSpace(th.ThreadID) != "" {
			c.markUnread(th.ThreadID)
		}
	}

	if maxSeq != "" {
		_ = c.Main.DB.ThreadState.UpdateCursor(ctx, c.Login.ID, th.ThreadID, maxSeq, maxTS)
		th.LastSequenceID = maxSeq
		th.LastMessageTS = maxTS
	}
	_ = c.pollConsumptionHorizons(ctx, th, now)
	return ingested, nil
}

const receiptPollInterval = 30 * time.Second

func (c *TeamsClient) pollConsumptionHorizons(ctx context.Context, th *teamsdb.ThreadState, now time.Time) error {
	if c == nil || c.Main == nil || c.Main.DB == nil || c.Login == nil || th == nil {
		return nil
	}
	threadID := strings.TrimSpace(th.ThreadID)
	if threadID == "" {
		return nil
	}
	if !c.shouldPollReceipts(threadID, now) {
		return nil
	}

	consumer := c.newConsumer()
	if consumer == nil {
		return errors.New("missing consumer client")
	}

	resp, err := consumer.GetConsumptionHorizons(ctx, threadID)
	if err != nil || resp == nil {
		return err
	}

	selfID := model.NormalizeTeamsUserID(c.Meta.TeamsUserID)
	if selfID == "" {
		return nil
	}

	var remoteID string
	var remoteHorizon *model.ConsumptionHorizon
	nonSelfCount := 0
	for idx := range resp.Horizons {
		entry := &resp.Horizons[idx]
		entryID := model.NormalizeTeamsUserID(entry.ID)
		if entryID == "" || entryID == selfID {
			continue
		}
		nonSelfCount++
		if nonSelfCount > 1 {
			return nil
		}
		remoteID = entryID
		remoteHorizon = entry
	}
	if remoteHorizon == nil || remoteID == "" {
		return nil
	}

	latestReadTS, ok := model.ParseConsumptionHorizonLatestReadTS(remoteHorizon.ConsumptionHorizon)
	if !ok || latestReadTS <= 0 {
		return nil
	}

	state, err := c.Main.DB.ConsumptionHorizon.Get(ctx, c.Login.ID, threadID, remoteID)
	if err != nil {
		return err
	}
	if state != nil && latestReadTS <= state.LastReadTS {
		return nil
	}

	readUpTo := time.UnixMilli(latestReadTS).UTC()
	portalKey := c.portalKey(threadID)
	target, err := c.Main.Bridge.DB.Message.GetLastPartAtOrBeforeTime(ctx, portalKey, readUpTo)
	if err != nil {
		return err
	}

	receipt := &simplevent.Receipt{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventReadReceipt,
			PortalKey: portalKey,
			Sender: bridgev2.EventSender{
				Sender: teamsUserIDToNetworkUserID(remoteID),
			},
			Timestamp: readUpTo,
		},
		ReadUpTo: readUpTo,
	}
	if target != nil && target.ID != "" {
		receipt.LastTarget = target.ID
		receipt.Targets = []networkid.MessageID{target.ID}
	}
	c.Login.QueueRemoteEvent(receipt)

	return c.Main.DB.ConsumptionHorizon.UpsertLastRead(ctx, c.Login.ID, threadID, remoteID, latestReadTS)
}

func (c *TeamsClient) shouldPollReceipts(threadID string, now time.Time) bool {
	c.receiptPollMu.Lock()
	defer c.receiptPollMu.Unlock()
	if c.receiptPoll == nil {
		c.receiptPoll = make(map[string]time.Time)
	}
	last := c.receiptPoll[threadID]
	if !last.IsZero() && now.Sub(last) < receiptPollInterval {
		return false
	}
	c.receiptPoll[threadID] = now
	return true
}

func (c *TeamsClient) queueReactionSyncForMessage(ctx context.Context, th *teamsdb.ThreadState, msg model.RemoteMessage, messageID string) {
	if c == nil || c.Login == nil || th == nil {
		return
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		messageID = NormalizeTeamsReactionMessageID(msg.MessageID)
	}
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
