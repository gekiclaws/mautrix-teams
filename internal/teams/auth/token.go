package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
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

func (c *Client) Refresh(ctx context.Context, refreshToken string) (*AuthState, error) {
	return nil, errors.New("refresh token exchange disabled for SPA-issued tokens")
}

func (c *Client) EnsureValidToken(ctx context.Context, state *AuthState) (*AuthState, bool, error) {
	if state == nil {
		return nil, false, errors.New("missing auth state")
	}
	return state, false, nil
}

func (c *Client) tokenRequest(ctx context.Context, values url.Values) (*AuthState, error) {
	body := bytes.NewBufferString(values.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenEndpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		if c.Log != nil {
			c.Log.Error().Int("status", resp.StatusCode).Str("body", string(body)).Msg("Token endpoint error")
		}
		return nil, errors.New("token endpoint returned non-2xx status")
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
	}
	return state, nil
}
