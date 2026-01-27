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
	"time"

	"github.com/rs/zerolog"
)

type ReactionError struct {
	Status      int
	BodySnippet string
}

func (e ReactionError) Error() string {
	return "reaction request failed"
}

func NewReactionError(resp *http.Response) ReactionError {
	if resp == nil {
		return ReactionError{}
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	return ReactionError{
		Status:      resp.StatusCode,
		BodySnippet: string(snippet),
	}
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
	return c.sendReaction(ctx, http.MethodPut, threadID, teamsMessageID, emotionKey, payload)
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
	return c.sendReaction(ctx, http.MethodDelete, threadID, teamsMessageID, emotionKey, payload)
}

func (c *Client) sendReaction(ctx context.Context, method string, threadID string, teamsMessageID string, emotionKey string, payload interface{}) (int, error) {
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
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.Header.Set("authentication", "skypetoken="+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	c.debugRequest("teams reaction request", endpoint, req)

	executor := c.Executor
	if executor == nil {
		executor = &TeamsRequestExecutor{
			HTTP:        c.HTTP,
			Log:         zerolog.Nop(),
			MaxRetries:  4,
			BaseBackoff: 500 * time.Millisecond,
			MaxBackoff:  10 * time.Second,
		}
		c.Executor = executor
	}
	if executor.HTTP == nil {
		executor.HTTP = c.HTTP
	}
	if c.Log != nil {
		executor.Log = *c.Log
	}

	ctx = WithRequestMeta(ctx, RequestMeta{
		ThreadID:       threadID,
		TeamsMessageID: teamsMessageID,
		EmotionKey:     emotionKey,
		Operation:      "teams reaction",
	})

	resp, err := executor.Do(ctx, req, classifyTeamsReactionResponse)
	if err != nil {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}
		return statusCode, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func classifyTeamsReactionResponse(resp *http.Response) error {
	if resp == nil {
		return errors.New("missing response")
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return RetryableError{
			Status:     resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return RetryableError{Status: resp.StatusCode}
	}
	return NewReactionError(resp)
}
