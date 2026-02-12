package graph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	teamsclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

var ErrEmptyDriveItemID = errors.New("drive item id is empty")

type GraphDriveItemError struct {
	Status      int
	BodySnippet string
}

func (e GraphDriveItemError) Error() string {
	return "graph drive item request failed"
}

func (c *GraphClient) GetDriveItem(ctx context.Context, driveItemID string) (*UploadedDriveItem, error) {
	if c == nil || c.HTTP == nil {
		return nil, ErrMissingGraphHTTPClient
	}
	if strings.TrimSpace(c.AccessToken) == "" {
		return nil, ErrMissingGraphAccessToken
	}
	if strings.TrimSpace(driveItemID) == "" {
		return nil, ErrEmptyDriveItemID
	}

	// This uses /me/drive since uploads are to the current user's OneDrive.
	endpoint := "https://graph.microsoft.com/v1.0/me/drive/items/" + url.PathEscape(strings.TrimSpace(driveItemID))
	q := url.Values{}
	q.Set("$select", "id,name,size,sharepointIds,parentReference")
	endpoint = endpoint + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.AccessToken))

	executor := c.executor()
	resp, err := executor.Do(ctx, req, classifyGetDriveItemResponse)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	}
	defer resp.Body.Close()

	return parseUploadedDriveItem(resp.Body)
}

func classifyGetDriveItemResponse(resp *http.Response) error {
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
	return GraphDriveItemError{Status: resp.StatusCode, BodySnippet: string(snippet)}
}
