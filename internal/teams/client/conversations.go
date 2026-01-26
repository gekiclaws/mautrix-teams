package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

const (
	defaultConversationsURL = "https://teams.live.com/api/chatsvc/consumer/v1/users/ME/conversations"
	maxErrorBodyBytes       = 2048
)

var ErrMissingHTTPClient = errors.New("consumer client missing http client")

type ConversationsError struct {
	Status      int
	BodySnippet string
}

func (e ConversationsError) Error() string {
	return "conversations request failed"
}

type Client struct {
	HTTP             *http.Client
	ConversationsURL string
	MessagesURL      string
	SendMessagesURL  string
	Token            string
	Log              *zerolog.Logger
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{
		HTTP:             httpClient,
		ConversationsURL: defaultConversationsURL,
		SendMessagesURL:  defaultSendMessagesURL,
	}
}

func (c *Client) ListConversations(ctx context.Context, token string) ([]model.RemoteConversation, error) {
	if c == nil || c.HTTP == nil {
		return nil, ErrMissingHTTPClient
	}
	endpoint := c.ConversationsURL
	if endpoint == "" {
		endpoint = defaultConversationsURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authentication", "skypetoken="+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, ConversationsError{
			Status:      resp.StatusCode,
			BodySnippet: string(snippet),
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Conversations []model.RemoteConversation `json:"conversations"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Conversations, nil
}
