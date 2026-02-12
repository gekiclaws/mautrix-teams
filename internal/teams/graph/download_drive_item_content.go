package graph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"

	teamsclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

var ErrEmptyDownloadDriveItemID = errors.New("drive item id is empty")

type GraphDriveItemContentError struct {
	Status      int
	BodySnippet string
}

func (e GraphDriveItemContentError) Error() string {
	return "graph drive item content request failed"
}

type DriveItemContent struct {
	Bytes       []byte
	ContentType string
}

func (c *GraphClient) DownloadDriveItemContent(ctx context.Context, driveItemID string) (*DriveItemContent, error) {
	if c == nil || c.HTTP == nil {
		return nil, ErrMissingGraphHTTPClient
	}
	if strings.TrimSpace(c.AccessToken) == "" {
		return nil, ErrMissingGraphAccessToken
	}
	driveItemID = strings.TrimSpace(driveItemID)
	if driveItemID == "" {
		return nil, ErrEmptyDownloadDriveItemID
	}

	endpoint := "https://graph.microsoft.com/v1.0/me/drive/items/" + url.PathEscape(driveItemID) + "/content"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.AccessToken))

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

	resp, err := executor.Do(ctx, req, classifyDriveItemContentResponse)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	}
	defer resp.Body.Close()

	max := int64(defaultMaxUploadBytes)
	if c.MaxUploadSize > 0 {
		max = int64(c.MaxUploadSize)
	}
	// Read at most max+1 to enforce a hard cap.
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if readErr != nil {
		return nil, readErr
	}
	if int64(len(data)) > max {
		return nil, ErrUploadContentTooLarge
	}

	return &DriveItemContent{
		Bytes:       data,
		ContentType: strings.TrimSpace(resp.Header.Get("Content-Type")),
	}, nil
}

func classifyDriveItemContentResponse(resp *http.Response) error {
	if resp == nil {
		return errors.New("missing response")
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
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxUploadErrorBytes))
	return GraphDriveItemContentError{Status: resp.StatusCode, BodySnippet: string(snippet)}
}

