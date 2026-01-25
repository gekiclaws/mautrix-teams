package teams

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type GraphChat struct {
	ID string `json:"id"`
}

type GraphMessage struct {
	ID              string            `json:"id"`
	CreatedDateTime time.Time         `json:"createdDateTime"`
	From            *GraphMessageFrom `json:"from"`
	Body            GraphMessageBody  `json:"body"`
}

type GraphMessageFrom struct {
	User *GraphMessageUser `json:"user"`
}

type GraphMessageUser struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type GraphMessageBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type graphListResponse[T any] struct {
	Value []T `json:"value"`
}

func (c *GraphClient) ListChats(ctx context.Context, userID string, top int) ([]GraphChat, GraphResponse, error) {
	path := fmt.Sprintf("users/%s/chats", url.PathEscape(userID))
	query := url.Values{}
	if top > 0 {
		query.Set("$top", strconv.Itoa(top))
	}
	var payload graphListResponse[GraphChat]
	resp, err := c.getJSON(ctx, path, query, &payload)
	if err != nil {
		return nil, resp, err
	}
	return payload.Value, resp, nil
}

func (c *GraphClient) ListChatMessages(ctx context.Context, chatID string, top int) ([]GraphMessage, GraphResponse, error) {
	path := fmt.Sprintf("chats/%s/messages", url.PathEscape(chatID))
	query := url.Values{}
	if top > 0 {
		query.Set("$top", strconv.Itoa(top))
	}
	query.Set("$orderby", "createdDateTime")
	var payload graphListResponse[GraphMessage]
	resp, err := c.getJSON(ctx, path, query, &payload)
	if err != nil {
		return nil, resp, err
	}
	return payload.Value, resp, nil
}
