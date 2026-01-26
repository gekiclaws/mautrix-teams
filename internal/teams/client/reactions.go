package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type ReactionError struct {
	Status      int
	BodySnippet string
}

func (e ReactionError) Error() string {
	return "reaction request failed"
}

func (c *Client) AddReaction(ctx context.Context, threadID string, teamsMessageID string, emotionKey string, appliedAtMS int64) (int, error) {
	if c == nil || c.HTTP == nil {
		return 0, ErrMissingHTTPClient
	}
	if c.Token == "" {
		return 0, ErrMissingToken
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return 0, errors.New("missing thread id")
	}
	teamsMessageID = strings.TrimSpace(teamsMessageID)
	if teamsMessageID == "" {
		return 0, errors.New("missing teams message id")
	}
	if strings.TrimSpace(emotionKey) == "" {
		return 0, errors.New("missing emotion key")
	}
	if appliedAtMS == 0 {
		return 0, errors.New("missing applied timestamp")
	}

	payload := map[string]map[string]interface{}{
		"emotions": {
			"key":   emotionKey,
			"value": appliedAtMS,
		},
	}
	return c.sendReaction(ctx, http.MethodPut, threadID, teamsMessageID, payload)
}

func (c *Client) RemoveReaction(ctx context.Context, threadID string, teamsMessageID string, emotionKey string) (int, error) {
	if c == nil || c.HTTP == nil {
		return 0, ErrMissingHTTPClient
	}
	if c.Token == "" {
		return 0, ErrMissingToken
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return 0, errors.New("missing thread id")
	}
	teamsMessageID = strings.TrimSpace(teamsMessageID)
	if teamsMessageID == "" {
		return 0, errors.New("missing teams message id")
	}
	if strings.TrimSpace(emotionKey) == "" {
		return 0, errors.New("missing emotion key")
	}

	payload := map[string]map[string]string{
		"emotions": {
			"key": emotionKey,
		},
	}
	return c.sendReaction(ctx, http.MethodDelete, threadID, teamsMessageID, payload)
}

func (c *Client) sendReaction(ctx context.Context, method string, threadID string, teamsMessageID string, payload interface{}) (int, error) {
	baseURL := c.SendMessagesURL
	if baseURL == "" {
		baseURL = defaultSendMessagesURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	endpoint := fmt.Sprintf("%s/%s/messages/%s/properties?name=emotions", baseURL, url.PathEscape(threadID), url.PathEscape(teamsMessageID))

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("authentication", "skypetoken="+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	c.debugRequest("teams reaction request", endpoint, req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return resp.StatusCode, ReactionError{
			Status:      resp.StatusCode,
			BodySnippet: string(snippet),
		}
	}

	return resp.StatusCode, nil
}
