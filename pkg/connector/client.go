package connector

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-teams/internal/teams/auth"
	consumerclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

type TeamsClient struct {
	Main  *TeamsConnector
	Login *bridgev2.UserLogin
	Meta  *TeamsUserLoginMetadata

	loggedIn atomic.Bool

	consumerMu sync.Mutex
	consumer   *consumerclient.Client

	syncMu     sync.Mutex
	syncCancel context.CancelFunc
	syncDone   chan struct{}
}

var (
	_ bridgev2.NetworkAPI                  = (*TeamsClient)(nil)
	_ bridgev2.BackgroundSyncingNetworkAPI = (*TeamsClient)(nil)
)

func (c *TeamsClient) Connect(ctx context.Context) {
	if c == nil || c.Login == nil || c.Main == nil {
		return
	}
	if c.Meta == nil {
		if meta, ok := c.Login.Metadata.(*TeamsUserLoginMetadata); ok {
			c.Meta = meta
		} else {
			c.Meta = &TeamsUserLoginMetadata{}
			c.Login.Metadata = c.Meta
		}
	}

	c.Login.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnecting})

	if err := c.ensureValidSkypeToken(ctx); err != nil {
		c.loggedIn.Store(false)
		c.Login.Log.Err(err).Msg("Failed to ensure valid Teams tokens")
		c.Login.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Message:    err.Error(),
			UserAction: status.UserActionRelogin,
		})
		return
	}

	c.loggedIn.Store(true)
	c.Login.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
	c.startSyncLoop()
}

func (c *TeamsClient) Disconnect() {
	c.stopSyncLoop(5 * time.Second)
}

func (c *TeamsClient) IsLoggedIn() bool {
	if c == nil {
		return false
	}
	if c.loggedIn.Load() {
		return true
	}
	if c.Meta == nil {
		if meta, ok := c.Login.Metadata.(*TeamsUserLoginMetadata); ok {
			c.Meta = meta
		}
	}
	if c.Meta == nil || c.Meta.SkypeToken == "" || c.Meta.SkypeTokenExpiresAt == 0 {
		return false
	}
	expiresAt := time.Unix(c.Meta.SkypeTokenExpiresAt, 0).UTC()
	return time.Now().UTC().Add(auth.SkypeTokenExpirySkew).Before(expiresAt)
}

func (c *TeamsClient) LogoutRemote(ctx context.Context) {
	if c == nil || c.Login == nil {
		return
	}
	c.stopSyncLoop(5 * time.Second)
	if meta, ok := c.Login.Metadata.(*TeamsUserLoginMetadata); ok && meta != nil {
		*meta = TeamsUserLoginMetadata{}
	}
	_ = c.Login.Save(ctx)
	c.loggedIn.Store(false)
}

func (c *TeamsClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	_ = ctx
	if c == nil || c.Meta == nil {
		return false
	}
	return strings.TrimSpace(string(userID)) != "" &&
		teamsUserIDToNetworkUserID(c.Meta.TeamsUserID) == userID
}

func (c *TeamsClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	if c == nil || c.Login == nil || c.Main == nil || c.Main.DB == nil {
		return nil, bridgev2.ErrNotLoggedIn
	}
	threadID := strings.TrimSpace(string(portal.ID))
	if threadID == "" {
		return nil, errors.New("missing thread id")
	}
	row, err := c.Main.DB.ThreadState.Get(ctx, c.Login.ID, threadID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		// Portal can exist before we have a discovery row; return minimal info.
		name := "Chat"
		return &bridgev2.ChatInfo{Name: &name}, nil
	}
	name := row.Name
	var roomType database.RoomType
	if row.IsOneToOne {
		roomType = database.RoomTypeDM
	} else {
		roomType = database.RoomTypeDefault
	}
	return &bridgev2.ChatInfo{
		Name: &name,
		Type: &roomType,
	}, nil
}

func (c *TeamsClient) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	if c == nil || c.Main == nil || c.Main.DB == nil || ghost == nil {
		return nil, bridgev2.ErrNotLoggedIn
	}
	profile, err := c.Main.DB.Profile.GetByTeamsUserID(ctx, string(ghost.ID))
	if err != nil {
		return nil, err
	}
	if profile == nil || strings.TrimSpace(profile.DisplayName) == "" {
		return &bridgev2.UserInfo{Name: ptrString(string(ghost.ID))}, nil
	}
	return &bridgev2.UserInfo{Name: &profile.DisplayName}, nil
}

func (c *TeamsClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *event.RoomFeatures {
	_ = ctx
	_ = portal
	return &event.RoomFeatures{}
}

func (c *TeamsClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	if !c.IsLoggedIn() {
		return nil, bridgev2.ErrNotLoggedIn
	}
	if msg == nil || msg.Content == nil {
		return nil, bridgev2.ErrUnsupportedMessageType
	}
	if msg.Content.MsgType != event.MsgText {
		return nil, bridgev2.ErrUnsupportedMessageType
	}
	threadID := strings.TrimSpace(string(msg.Portal.ID))
	if threadID == "" {
		return nil, errors.New("missing thread id")
	}

	consumer := c.getConsumer()
	if consumer == nil {
		return nil, errors.New("missing consumer client")
	}
	consumer.Token = c.Meta.SkypeToken

	clientMessageID := consumerclient.GenerateClientMessageID()
	msg.AddPendingToIgnore(networkid.TransactionID(clientMessageID))

	now := time.Now().UTC()
	_, err := consumer.SendMessageWithID(ctx, threadID, msg.Content.Body, c.Meta.TeamsUserID, clientMessageID)
	if err != nil {
		return nil, err
	}

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

func (c *TeamsClient) ConnectBackground(ctx context.Context, _ *bridgev2.ConnectBackgroundParams) error {
	// For now, background sync just runs one discovery+poll cycle and returns.
	if c == nil {
		return nil
	}
	if err := c.ensureValidSkypeToken(ctx); err != nil {
		return err
	}
	return c.syncOnce(ctx)
}

func (c *TeamsClient) getConsumer() *consumerclient.Client {
	if c == nil || c.Main == nil {
		return nil
	}
	c.consumerMu.Lock()
	defer c.consumerMu.Unlock()
	if c.consumer != nil {
		return c.consumer
	}
	authClient := auth.NewClient(nil)
	c.consumer = consumerclient.NewClient(authClient.HTTP)
	return c.consumer
}

func ptrString(v string) *string { return &v }
