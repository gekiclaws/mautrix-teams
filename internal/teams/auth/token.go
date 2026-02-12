package auth

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
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	IDToken      string `json:"id_token"`
}

func (c *Client) ExchangeCode(ctx context.Context, code, verifier string) (*AuthState, error) {
	if code == "" {
		return nil, errors.New("missing authorization code")
	}
	if c.Log != nil {
		c.Log.Info().Str("redirect_uri", c.RedirectURI).Str("code_verifier", verifier).Msg("Exchanging authorization code")
	}
	values := url.Values{}
	values.Set("client_id", c.ClientID)
	values.Set("redirect_uri", c.RedirectURI)
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("code_verifier", verifier)
	return c.tokenRequest(ctx, values)
}

func (c *Client) RefreshAccessToken(ctx context.Context, refreshToken string) (*AuthState, error) {
	if refreshToken == "" {
		return nil, errors.New("missing refresh token")
	}
	values := url.Values{}
	values.Set("client_id", c.ClientID)
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	if len(c.Scopes) > 0 {
		values.Set("scope", strings.Join(c.Scopes, " "))
	}
	return c.tokenRequest(ctx, values)
}

func (c *Client) tokenRequest(ctx context.Context, values url.Values) (*AuthState, error) {
	body := bytes.NewBufferString(values.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenEndpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin := redirectOrigin(c.RedirectURI); origin != "" {
		req.Header.Set("Origin", origin)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 400 {
			snippet = snippet[:400] + "...(truncated)"
		}
		if c.Log != nil {
			c.Log.Error().Int("status", resp.StatusCode).Str("body", snippet).Msg("Token endpoint error")
		}
		if snippet == "" {
			return nil, fmt.Errorf("token endpoint returned non-2xx status: %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("token endpoint returned non-2xx status: %d body=%s", resp.StatusCode, snippet)
	}

	var payload tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.AccessToken == "" {
		return nil, errors.New("token response missing access_token")
	}
	state := &AuthState{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		IDToken:      payload.IDToken,
	}
	if payload.ExpiresIn > 0 {
		state.ExpiresAtUnix = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).UTC().Unix()
		if shouldPersistGraphToken(values.Get("scope")) {
			state.GraphAccessToken = payload.AccessToken
			state.GraphExpiresAt = state.ExpiresAtUnix
		}
	} else if shouldPersistGraphToken(values.Get("scope")) {
		state.GraphAccessToken = payload.AccessToken
	}
	return state, nil
}

func shouldPersistGraphToken(scope string) bool {
	trimmed := strings.TrimSpace(scope)
	if trimmed == "" {
		return true
	}
	for _, part := range strings.Fields(strings.ToLower(trimmed)) {
		switch {
		case strings.Contains(part, mbiAccessTokenMarker):
			return false
		case strings.Contains(part, "graph.microsoft.com"):
			return true
		case strings.HasSuffix(part, "files.readwrite"):
			return true
		}
	}
	return false
}

func redirectOrigin(redirectURI string) string {
	if strings.TrimSpace(redirectURI) == "" {
		return ""
	}
	parsed, err := url.Parse(redirectURI)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
