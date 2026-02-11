package connector

import (
	"context"
	"errors"
	"strings"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"

	"go.mau.fi/mautrix-teams/internal/teams/client"
	"go.mau.fi/mautrix-teams/internal/teams/model"
	"go.mau.fi/mautrix-teams/pkg/teamsdb"
)

const (
	receiptPollInterval = 30 * time.Second
	receiptSendCooldown = 5 * time.Second
)

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
	if !c.shouldSendReceipt(threadID, time.Now().UTC()) {
		return nil
	}
	consumer := c.getConsumer()
	if consumer == nil {
		return errors.New("missing consumer client")
	}
	consumer.Token = c.Meta.SkypeToken
	horizon := client.ConsumptionHorizonNow(time.Now().UTC())
	_, err := consumer.SetConsumptionHorizon(ctx, threadID, horizon)
	return err
}

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

	consumer := c.getConsumer()
	if consumer == nil {
		return errors.New("missing consumer client")
	}
	consumer.Token = c.Meta.SkypeToken

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

func (c *TeamsClient) shouldSendReceipt(threadID string, now time.Time) bool {
	c.receiptSendMu.Lock()
	defer c.receiptSendMu.Unlock()
	if c.receiptLastSent == nil {
		c.receiptLastSent = make(map[string]time.Time)
	}
	last := c.receiptLastSent[threadID]
	if !last.IsZero() && now.Sub(last) < receiptSendCooldown {
		return false
	}
	c.receiptLastSent[threadID] = now
	return true
}
