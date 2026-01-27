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

type ReceiptError struct {
	Status      int
	BodySnippet string
}

func (e ReceiptError) Error() string {
	return "receipt request failed"
}

// ConsumptionHorizonNow builds a thread-level consumption horizon using timestamps only.
// Teams consumer treats this as a conversation watermark, not a per-message ack.
func ConsumptionHorizonNow(now time.Time) string {
	ms := now.UTC().UnixMilli()
	return fmt.Sprintf("%d;%d;0", ms, ms)
}

func (c *Client) SetConsumptionHorizon(ctx context.Context, threadID string, horizon string) (int, error) {
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
	horizon = strings.TrimSpace(horizon)
	if horizon == "" {
		return 0, errors.New("missing consumption horizon")
	}

	baseURL := c.SendMessagesURL
	if baseURL == "" {
		baseURL = defaultSendMessagesURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	endpoint := fmt.Sprintf("%s/%s/properties?name=consumptionhorizon", baseURL, url.PathEscape(threadID))

	payload := map[string]string{
		"consumptionhorizon": horizon,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.Header.Set("authentication", "skypetoken="+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	c.debugRequest("teams receipt request", endpoint, req)

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
		ThreadID:  threadID,
		Operation: "teams receipt",
	})

	resp, err := executor.Do(ctx, req, classifyTeamsReceiptResponse)
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

func classifyTeamsReceiptResponse(resp *http.Response) error {
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
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	return ReceiptError{
		Status:      resp.StatusCode,
		BodySnippet: string(snippet),
	}
}
