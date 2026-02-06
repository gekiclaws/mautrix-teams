package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

const defaultConsumptionHorizonsURL = "https://teams.live.com/api/chatsvc/consumer/v1/threads"

type ConsumptionHorizonsError struct {
	Status      int
	BodySnippet string
}

func (e ConsumptionHorizonsError) Error() string {
	return "consumption horizons request failed"
}

func (c *Client) GetConsumptionHorizons(ctx context.Context, threadID string) (*model.ConsumptionHorizonsResponse, error) {
	if c == nil || c.HTTP == nil {
		return nil, ErrMissingHTTPClient
	}
	if c.Token == "" {
		return nil, ErrMissingToken
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, errors.New("missing thread id")
	}

	baseURL := c.ConsumptionHorizonsURL
	if baseURL == "" {
		baseURL = defaultConsumptionHorizonsURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	endpoint := fmt.Sprintf("%s/%s/consumptionhorizons", baseURL, url.PathEscape(threadID))

	var payload model.ConsumptionHorizonsResponse
	if err := c.fetchConsumptionHorizonsJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (c *Client) fetchConsumptionHorizonsJSON(ctx context.Context, endpoint string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("authentication", "skypetoken="+c.Token)
	req.Header.Set("Accept", "application/json")
	c.debugRequest("teams consumption horizons request", endpoint, req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
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
		return ConsumptionHorizonsError{
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
