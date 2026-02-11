package connector

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
)

const (
	threadDiscoveryInterval = 10 * time.Minute
	basePollInterval        = 2 * time.Second
	maxPollInterval         = 30 * time.Second
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
	if err := c.syncOnce(ctx); err != nil {
		log.Err(err).Msg("Teams sync loop initial run failed")
	}

	// Keep refreshing thread list in the background.
	go func() {
		ticker := time.NewTicker(threadDiscoveryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.refreshThreads(ctx); err != nil {
					log.Err(err).Msg("Teams thread discovery refresh failed")
				}
			}
		}
	}()

	// Poll threads continuously with per-thread scheduling until canceled.
	if err := c.pollDueThreads(ctx); err != nil && !errors.Is(err, context.Canceled) {
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
		if meta, ok := c.Login.Metadata.(*TeamsUserLoginMetadata); ok {
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

	consumer := c.getConsumer()
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
	delay    time.Duration
	nextPoll time.Time
}

func (c *TeamsClient) pollAllThreadsOnce(ctx context.Context) error {
	threads, err := c.Main.DB.ThreadState.ListForLogin(ctx, c.Login.ID)
	if err != nil {
		return err
	}
	for _, th := range threads {
		_ = c.pollThread(ctx, th, time.Now().UTC())
	}
	return nil
}

func (c *TeamsClient) pollDueThreads(ctx context.Context) error {
	if c == nil || c.Main == nil || c.Main.DB == nil || c.Login == nil {
		return nil
	}
	threads, err := c.Main.DB.ThreadState.ListForLogin(ctx, c.Login.ID)
	if err != nil {
		return err
	}

	states := make(map[string]*pollState, len(threads))
	for _, th := range threads {
		if th == nil || th.ThreadID == "" {
			continue
		}
		states[th.ThreadID] = &pollState{delay: basePollInterval, nextPoll: time.Now().UTC()}
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		now := time.Now().UTC()
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
				ps = &pollState{delay: basePollInterval, nextPoll: now}
				states[th.ThreadID] = ps
			}
			if now.Before(ps.nextPoll) {
				if ps.nextPoll.Before(nextWake) {
					nextWake = ps.nextPoll
				}
				continue
			}

			err := c.pollThread(ctx, th, now)
			if err != nil {
				ps.delay *= 2
				if ps.delay > maxPollInterval {
					ps.delay = maxPollInterval
				}
				ps.nextPoll = now.Add(ps.delay)
			} else {
				ps.delay = basePollInterval
				ps.nextPoll = now.Add(basePollInterval)
			}
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

func (c *TeamsClient) pollThread(ctx context.Context, th *teamsdb.ThreadState, now time.Time) error {
	if c == nil || th == nil {
		return nil
	}
	if err := c.ensureValidSkypeToken(ctx); err != nil {
		c.Login.BridgeState.Send(status.BridgeState{StateEvent: status.StateBadCredentials, Message: err.Error(), UserAction: status.UserActionRelogin})
		return err
	}
	consumer := c.getConsumer()
	if consumer == nil {
		return errors.New("missing consumer client")
	}
	consumer.Token = c.Meta.SkypeToken

	msgs, err := consumer.ListMessages(ctx, th.Conversation, th.LastSequenceID)
	if err != nil {
		return err
	}

	lastSeq := strings.TrimSpace(th.LastSequenceID)
	var maxSeq string
	var maxTS int64

	for _, msg := range msgs {
		if strings.TrimSpace(msg.MessageID) == "" {
			continue
		}
		// Filter already-seen messages in case the remote API returns history.
		if lastSeq != "" && model.CompareSequenceID(strings.TrimSpace(msg.SequenceID), lastSeq) <= 0 {
			continue
		}

		senderID := model.NormalizeTeamsUserID(msg.SenderID)
		displayName := strings.TrimSpace(msg.TokenDisplayName)
		if displayName == "" {
			displayName = strings.TrimSpace(msg.IMDisplayName)
		}
		if displayName == "" {
			displayName = senderID
		}
		_ = c.Main.DB.Profile.Upsert(ctx, senderID, displayName, now)

		es := bridgev2.EventSender{Sender: teamsUserIDToNetworkUserID(senderID)}
		if senderID != "" && strings.TrimSpace(c.Meta.TeamsUserID) != "" && senderID == c.Meta.TeamsUserID {
			es.IsFromMe = true
			es.SenderLogin = c.Login.ID
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
			ID:                 networkid.MessageID(msg.MessageID),
			TransactionID:      networkid.TransactionID(strings.TrimSpace(msg.ClientMessageID)),
			ConvertMessageFunc: convertTeamsMessage,
		}
		c.Login.QueueRemoteEvent(evt)

		if maxSeq == "" || model.CompareSequenceID(strings.TrimSpace(msg.SequenceID), maxSeq) > 0 {
			maxSeq = strings.TrimSpace(msg.SequenceID)
		}
		if ts := msg.Timestamp.UnixMilli(); ts > maxTS {
			maxTS = ts
		}
	}

	if maxSeq != "" {
		_ = c.Main.DB.ThreadState.UpdateCursor(ctx, c.Login.ID, th.ThreadID, maxSeq, maxTS)
		th.LastSequenceID = maxSeq
		th.LastMessageTS = maxTS
	}
	return nil
}
