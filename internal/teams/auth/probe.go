package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
)

const maxProbeBytes = 1024

type ProbeResult struct {
	StatusCode  int
	BodySnippet string
	AuthHeaders map[string]string
}

func (c *Client) ProbeTeamsEndpoint(ctx context.Context, endpoint string, token string) (*ProbeResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Del("Authorization")
	req.Header.Del("authentication")
	if token != "" {
		c.AttachSkypeToken(req, token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxProbeBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}

	result := &ProbeResult{
		StatusCode:  resp.StatusCode,
		BodySnippet: string(body),
		AuthHeaders: filterAuthHeaders(resp.Header),
	}
	return result, nil
}

func filterAuthHeaders(headers http.Header) map[string]string {
	out := make(map[string]string)
	for key, values := range headers {
		if !isAuthHeader(key) {
			continue
		}
		out[key] = strings.Join(values, "; ")
	}
	return out
}

func isAuthHeader(key string) bool {
	lower := strings.ToLower(key)
	if lower == "set-cookie" || lower == "www-authenticate" {
		return true
	}
	if strings.HasPrefix(lower, "x-ms-") || strings.HasPrefix(lower, "x-azure-") {
		return true
	}
	return false
}
