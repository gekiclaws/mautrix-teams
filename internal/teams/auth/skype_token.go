package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

const (
	skypeTokenErrorSnippetLimit = 2048
	SkypeTokenExpirySkew        = 60 * time.Second
)

type skypeTokenResponse struct {
	SkypeToken struct {
		SkypeToken string `json:"skypetoken"`
		ExpiresIn  int64  `json:"expiresIn"`
		SkypeID    string `json:"skypeid"`
		SignInName string `json:"signinname"`
		IsBusiness bool   `json:"isBusinessTenant"`
	} `json:"skypeToken"`
}

func (c *Client) AcquireSkypeToken(ctx context.Context, accessToken string) (string, int64, error) {
	if c.SkypeTokenEndpoint == "" {
		return "", 0, errors.New("skype token endpoint not configured")
	}
	if accessToken == "" {
		return "", 0, errors.New("missing access token for skypetoken acquisition")
	}
	if c.Log != nil {
		c.Log.Info().Msg("Acquiring Teams skypetoken")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.SkypeTokenEndpoint, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		snippet := readBodySnippet(resp.Body, skypeTokenErrorSnippetLimit)
		if c.Log != nil {
			c.Log.Error().Int("status", resp.StatusCode).Str("body_snippet", snippet).Msg("Failed to acquire skypetoken")
		}
		return "", 0, errors.New("skypetoken endpoint returned non-2xx status")
	}

	var payload skypeTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", 0, err
	}

	token := payload.SkypeToken.SkypeToken
	if token == "" {
		return "", 0, errors.New("skypetoken response missing token")
	}

	var expiresAt int64
	if payload.SkypeToken.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(payload.SkypeToken.ExpiresIn) * time.Second).Unix()
	}
	return token, expiresAt, nil
}

func (a *AuthState) HasValidSkypeToken(now time.Time) bool {
	if a == nil || a.SkypeToken == "" || a.SkypeTokenExpiresAt == 0 {
		return false
	}
	expiresAt := time.Unix(a.SkypeTokenExpiresAt, 0).UTC()
	return now.UTC().Add(SkypeTokenExpirySkew).Before(expiresAt)
}

func readBodySnippet(r io.Reader, limit int64) string {
	if r == nil || limit <= 0 {
		return ""
	}
	limited := io.LimitReader(r, limit)
	body, _ := io.ReadAll(limited)
	return string(body)
}
