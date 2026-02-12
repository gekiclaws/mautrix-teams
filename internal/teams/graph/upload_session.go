package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	teamsclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

var ErrMissingUploadSessionURL = errors.New("upload session response missing uploadUrl")

type GraphUploadSessionError struct {
	Status      int
	BodySnippet string
}

func (e GraphUploadSessionError) Error() string {
	return "graph upload session request failed"
}

func (c *GraphClient) CreateUploadSession(ctx context.Context, filename string) (string, error) {
	if c == nil || c.HTTP == nil {
		return "", ErrMissingGraphHTTPClient
	}
	if strings.TrimSpace(c.AccessToken) == "" {
		return "", ErrMissingGraphAccessToken
	}
	if strings.TrimSpace(filename) == "" {
		return "", ErrEmptyUploadFilename
	}

	endpoint, err := c.createUploadSessionURL(filename)
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(map[string]string{
		"@microsoft.graph.conflictBehavior": "rename",
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.AccessToken))
	req.Header.Set("Content-Type", "application/json")

	executor := c.executor()
	resp, err := executor.Do(ctx, req, classifyUploadSessionResponse)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return "", err
	}
	defer resp.Body.Close()

	var payload struct {
		UploadURL string `json:"uploadUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.UploadURL) == "" {
		return "", ErrMissingUploadSessionURL
	}
	return strings.TrimSpace(payload.UploadURL), nil
}

func (c *GraphClient) createUploadSessionURL(filename string) (string, error) {
	base := strings.TrimSpace(c.UploadBaseURL)
	if base == "" {
		base = defaultUploadEndpoint
	}
	escapedName := url.PathEscape(strings.TrimSpace(filename))
	endpoint := strings.TrimRight(base, "/") + "/" + escapedName + ":/createUploadSession"
	return endpoint, nil
}

func classifyUploadSessionResponse(resp *http.Response) error {
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
	return GraphUploadSessionError{
		Status:      resp.StatusCode,
		BodySnippet: string(snippet),
	}
}
