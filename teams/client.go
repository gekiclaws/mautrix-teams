package teams

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
)

var ErrNotImplemented = errors.New("teams stub: not implemented")

// Client is a no-op Teams adapter stub that mirrors the small surface used by the bridge.
type Client struct {
	State *discordgo.State

	IsUser   bool
	Identify discordgo.Identify

	Client *http.Client

	LastHeartbeatAck  time.Time
	LastHeartbeatSent time.Time

	HeartbeatSession discordgo.HeartbeatSession

	LogLevel     int
	Logger       func(msgL, caller int, format string, a ...interface{})
	EventHandler func(any)
}

func NewClient() *Client {
	state := discordgo.NewState()
	state.User = &discordgo.User{
		Discriminator: "0",
	}
	return &Client{
		State:             state,
		IsUser:            true,
		Client:            &http.Client{},
		LastHeartbeatAck:  time.Unix(0, 0).UTC(),
		LastHeartbeatSent: time.Unix(0, 0).UTC(),
	}
}

func (c *Client) Open() error {
	return nil
}

func (c *Client) Close() error {
	return nil
}

func (c *Client) LoadMainPage(ctx context.Context) error {
	return ErrNotImplemented
}

func (c *Client) MarkViewing(channelID string) error {
	return ErrNotImplemented
}

func (c *Client) SubscribeGuild(dat discordgo.GuildSubscribeData) error {
	return ErrNotImplemented
}

func (c *Client) User(userID string, options ...discordgo.RequestOption) (*discordgo.User, error) {
	return nil, ErrNotImplemented
}

func (c *Client) UserChannelPermissions(userID, channelID string, options ...discordgo.RequestOption) (int64, error) {
	return 0, ErrNotImplemented
}

func (c *Client) GuildMember(guildID, userID string, options ...discordgo.RequestOption) (*discordgo.Member, error) {
	return nil, ErrNotImplemented
}

func (c *Client) Channel(channelID string, options ...discordgo.RequestOption) (*discordgo.Channel, error) {
	return nil, ErrNotImplemented
}

func (c *Client) ChannelMessage(channelID, messageID string, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	return nil, ErrNotImplemented
}

func (c *Client) ChannelMessages(channelID string, limit int, beforeID, afterID, aroundID string, options ...discordgo.RequestOption) ([]*discordgo.Message, error) {
	return nil, ErrNotImplemented
}

func (c *Client) ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	return nil, ErrNotImplemented
}

func (c *Client) ChannelMessageEdit(channelID, messageID, content string, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	return nil, ErrNotImplemented
}

func (c *Client) ChannelMessageDelete(channelID, messageID string, options ...discordgo.RequestOption) error {
	return ErrNotImplemented
}

func (c *Client) ChannelTyping(channelID string, options ...discordgo.RequestOption) error {
	return ErrNotImplemented
}

func (c *Client) ChannelMessageAckNoToken(channelID, messageID string, options ...discordgo.RequestOption) (*discordgo.PtrAck, error) {
	return nil, ErrNotImplemented
}

func (c *Client) ChannelAttachmentCreate(channelID string, data *discordgo.ReqPrepareAttachments, options ...discordgo.RequestOption) (*discordgo.RespPrepareAttachments, error) {
	return nil, ErrNotImplemented
}

func (c *Client) WebhookCreate(channelID, name, avatar string, options ...discordgo.RequestOption) (*discordgo.Webhook, error) {
	return nil, ErrNotImplemented
}

func (c *Client) MessageThreadStartComplex(channelID, messageID string, data *discordgo.ThreadStart, options ...discordgo.RequestOption) (*discordgo.Channel, error) {
	return nil, ErrNotImplemented
}

func (c *Client) ThreadJoin(id string, options ...discordgo.RequestOption) error {
	return ErrNotImplemented
}

func (c *Client) ApplicationCommandsSearch(channelID, query string, options ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error) {
	return nil, ErrNotImplemented
}

func (c *Client) SendInteractions(guildID, channelID string, cmd *discordgo.ApplicationCommand, options []*discordgo.ApplicationCommandOptionInput, nonce string, reqOptions ...discordgo.RequestOption) error {
	return ErrNotImplemented
}

func (c *Client) MessageReactionAddUser(guildID, channelID, messageID, emojiID string, options ...discordgo.RequestOption) error {
	return ErrNotImplemented
}

func (c *Client) MessageReactionRemoveUser(guildID, channelID, messageID, emojiID, userID string, options ...discordgo.RequestOption) error {
	return ErrNotImplemented
}
