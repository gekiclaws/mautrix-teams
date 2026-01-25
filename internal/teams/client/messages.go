package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

const defaultMessagesURL = "https://msgapi.teams.live.com/v1/users/ME/conversations"

var ErrMissingToken = errors.New("consumer client missing skypetoken")

type MessagesError struct {
	Status      int
	BodySnippet string
}

func (e MessagesError) Error() string {
	return "messages request failed"
}

type remoteMessage struct {
	ID          string          `json:"id"`
	SequenceID  json.RawMessage `json:"sequenceId"`
	CreatedTime string          `json:"createdTime"`
	From        json.RawMessage `json:"from"`
	Content     json.RawMessage `json:"content"`
}

func (c *Client) ListMessages(ctx context.Context, conversationID string, sinceSequence string) ([]model.RemoteMessage, error) {
	if c == nil || c.HTTP == nil {
		return nil, ErrMissingHTTPClient
	}
	if c.Token == "" {
		return nil, ErrMissingToken
	}
	if conversationID == "" {
		return nil, errors.New("missing conversation id")
	}

	var payload struct {
		Messages []remoteMessage `json:"messages"`
	}
	baseURL := c.MessagesURL
	if baseURL == "" {
		baseURL = defaultMessagesURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	messagesURL := fmt.Sprintf("%s/%s/messages", baseURL, url.PathEscape(conversationID))
	if err := c.fetchJSON(ctx, messagesURL, &payload); err != nil {
		return nil, err
	}

	result := make([]model.RemoteMessage, 0, len(payload.Messages))
	for _, msg := range payload.Messages {
		sequenceID, err := normalizeSequenceID(msg.SequenceID)
		if err != nil {
			return nil, err
		}
		senderID := model.ExtractSenderID(msg.From)
		if senderID == "" && c.Log != nil {
			c.Log.Debug().
				Str("message_id", msg.ID).
				Msg("teams message missing sender id")
		}
		result = append(result, model.RemoteMessage{
			MessageID:  msg.ID,
			SequenceID: sequenceID,
			SenderID:   senderID,
			Timestamp:  model.ParseTimestamp(msg.CreatedTime),
			Body:       model.ExtractBody(msg.Content),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return model.CompareSequenceID(result[i].SequenceID, result[j].SequenceID) < 0
	})

	return result, nil
}

func normalizeSequenceID(value json.RawMessage) (string, error) {
	if len(value) == 0 {
		return "", nil
	}
	var asString string
	if err := json.Unmarshal(value, &asString); err == nil {
		return asString, nil
	}
	var asNumber json.Number
	if err := json.Unmarshal(value, &asNumber); err == nil {
		return asNumber.String(), nil
	}
	return "", errors.New("invalid sequenceId")
}

func (c *Client) fetchJSON(ctx context.Context, endpoint string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("authentication", "skypetoken="+c.Token)
	req.Header.Set("Accept", "application/json")
	c.debugRequest("teams messages request", endpoint, req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return MessagesError{
			Status:      resp.StatusCode,
			BodySnippet: string(snippet),
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func (c *Client) debugRequest(message string, endpoint string, req *http.Request) {
	if c == nil || c.Log == nil {
		return
	}
	headers := map[string][]string{}
	for key, values := range req.Header {
		if strings.EqualFold(key, "authentication") || strings.EqualFold(key, "authorization") {
			headers[key] = []string{"REDACTED"}
		} else {
			headers[key] = values
		}
	}
	c.Log.Debug().
		Str("url", endpoint).
		Interface("headers", headers).
		Msg(message)
}
