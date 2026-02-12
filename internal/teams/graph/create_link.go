package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	teamsclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

const (
	defaultCreateLinkEndpoint = "https://graph.microsoft.com/v1.0"
	maxCreateLinkErrorBytes   = 1024
)

var (
	ErrEmptyListItemUniqueID      = errors.New("listItemUniqueID is empty")
	ErrMissingCreateLinkShareID   = errors.New("createLink response missing shareId")
	ErrMissingCreateLinkShareURL  = errors.New("createLink response missing link.webUrl")
	ErrMissingCreateLinkGraphResp = errors.New("missing response")
)

type CreatedShareLink struct {
	ShareID  string // u!...
	ShareURL string // https://1drv.ms/...
}

type GraphCreateLinkError struct {
	Status      int
	BodySnippet string
}

func (e GraphCreateLinkError) Error() string {
	return "graph createLink request failed"
}

func (c *GraphClient) CreateShareLink(ctx context.Context, listItemUniqueID string) (*CreatedShareLink, error) {
	if c == nil || c.HTTP == nil {
		return nil, ErrMissingGraphHTTPClient
	}
	if strings.TrimSpace(c.AccessToken) == "" {
		return nil, ErrMissingGraphAccessToken
	}
	if strings.TrimSpace(listItemUniqueID) == "" {
		return nil, ErrEmptyListItemUniqueID
	}

	endpoint := strings.TrimRight(defaultCreateLinkEndpoint, "/") + "/drive/items/" + listItemUniqueID + "/createLink"
	bodyBytes, err := json.Marshal(struct {
		Type string `json:"type"`
	}{
		Type: "edit",
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	// Ensure retries can re-read the body.
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyBytes)), nil
	}

	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.AccessToken))
	req.Header.Set("Content-Type", "application/json")

	executor := c.Executor
	if executor == nil {
		executor = &teamsclient.TeamsRequestExecutor{
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

	resp, err := executor.Do(ctx, req, classifyCreateLinkResponse)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		ShareID string `json:"shareId"`
		Link    struct {
			WebURL string `json:"webUrl"`
		} `json:"link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.ShareID) == "" {
		return nil, ErrMissingCreateLinkShareID
	}
	if strings.TrimSpace(payload.Link.WebURL) == "" {
		return nil, ErrMissingCreateLinkShareURL
	}
	return &CreatedShareLink{
		ShareID:  strings.TrimSpace(payload.ShareID),
		ShareURL: strings.TrimSpace(payload.Link.WebURL),
	}, nil
}

func classifyCreateLinkResponse(resp *http.Response) error {
	if resp == nil {
		return ErrMissingCreateLinkGraphResp
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return teamsclient.RetryableError{
			Status:     resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return teamsclient.RetryableError{Status: resp.StatusCode}
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxCreateLinkErrorBytes))
	return GraphCreateLinkError{
		Status:      resp.StatusCode,
		BodySnippet: string(snippet),
	}
}
