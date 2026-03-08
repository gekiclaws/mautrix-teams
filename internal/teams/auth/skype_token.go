package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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

func (c *Client) AcquireSkypeToken(ctx context.Context, accessToken string) (string, int64, string, error) {
	if c.SkypeTokenEndpoint == "" {
		return "", 0, "", errors.New("skype token endpoint not configured")
	}
	if accessToken == "" {
		return "", 0, "", errors.New("missing access token for skypetoken acquisition")
	}
	if c.Log != nil {
		c.Log.Info().Msg("Acquiring Teams skypetoken")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.SkypeTokenEndpoint, nil)
	if err != nil {
		return "", 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		snippet := strings.TrimSpace(readBodySnippet(resp.Body, skypeTokenErrorSnippetLimit))
		if len(snippet) > 400 {
			snippet = snippet[:400] + "...(truncated)"
		}
		if c.Log != nil {
			c.Log.Error().Int("status", resp.StatusCode).Str("body_snippet", snippet).Msg("Failed to acquire skypetoken")
		}
		if snippet == "" {
			return "", 0, "", fmt.Errorf("skypetoken endpoint returned non-2xx status: %d", resp.StatusCode)
		}
		return "", 0, "", fmt.Errorf("skypetoken endpoint returned non-2xx status: %d body=%s", resp.StatusCode, snippet)
	}

	var payload skypeTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", 0, "", err
	}

	token := payload.SkypeToken.SkypeToken
	if token == "" {
		return "", 0, "", errors.New("skypetoken response missing token")
	}

	var expiresAt int64
	if payload.SkypeToken.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(payload.SkypeToken.ExpiresIn) * time.Second).Unix()
	}
	return token, expiresAt, payload.SkypeToken.SkypeID, nil
}

type enterpriseAuthzResponse struct {
	Tokens struct {
		SkypeToken string `json:"skypeToken"`
		ExpiresIn  int64  `json:"expiresIn"`
	} `json:"tokens"`
	Region     string                 `json:"region"`
	Partition  string                 `json:"partition"`
	RegionGTMs map[string]interface{} `json:"regionGtms"`
}

// AcquireEnterpriseSkypeToken exchanges an Enterprise access token for a Skype token
// via the Enterprise authz endpoint. It returns the token, expiry, and region GTMs.
func (c *Client) AcquireEnterpriseSkypeToken(ctx context.Context, accessToken string) (string, int64, map[string]interface{}, error) {
	if c.SkypeTokenEndpoint == "" {
		return "", 0, nil, errors.New("skype token endpoint not configured")
	}
	if accessToken == "" {
		return "", 0, nil, errors.New("missing access token for enterprise skypetoken acquisition")
	}
	if c.Log != nil {
		c.Log.Info().Msg("Acquiring Enterprise Teams skypetoken")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.SkypeTokenEndpoint, strings.NewReader("{}"))
	if err != nil {
		return "", 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ms-teams-authz-type", "TokenRefresh")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", 0, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		snippet := strings.TrimSpace(readBodySnippet(resp.Body, skypeTokenErrorSnippetLimit))
		if len(snippet) > 400 {
			snippet = snippet[:400] + "...(truncated)"
		}
		if c.Log != nil {
			c.Log.Error().Int("status", resp.StatusCode).Str("body_snippet", snippet).Msg("Failed to acquire enterprise skypetoken")
		}
		if snippet == "" {
			return "", 0, nil, fmt.Errorf("enterprise skypetoken endpoint returned non-2xx status: %d", resp.StatusCode)
		}
		return "", 0, nil, fmt.Errorf("enterprise skypetoken endpoint returned non-2xx status: %d body=%s", resp.StatusCode, snippet)
	}

	var payload enterpriseAuthzResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", 0, nil, err
	}

	token := payload.Tokens.SkypeToken
	if token == "" {
		return "", 0, nil, errors.New("enterprise skypetoken response missing token")
	}

	var expiresAt int64
	if payload.Tokens.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(payload.Tokens.ExpiresIn) * time.Second).Unix()
	}
	return token, expiresAt, payload.RegionGTMs, nil
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
