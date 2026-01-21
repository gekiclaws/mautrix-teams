package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

const (
	envTenantID     = "AZURE_TENANT_ID"
	envClientID     = "AZURE_CLIENT_ID"
	envClientSecret = "AZURE_CLIENT_SECRET"
	envGraphUserID  = "AZURE_GRAPH_USER_ID"
)

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type graphUser struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// RunGraphAuthTest performs a client-credentials flow and calls one Graph endpoint.
func RunGraphAuthTest(ctx context.Context, log zerolog.Logger) error {
	if err := loadDotEnv(".env"); err != nil {
		return err
	}

	tenantID := os.Getenv(envTenantID)
	clientID := os.Getenv(envClientID)
	clientSecret := os.Getenv(envClientSecret)
	userID := os.Getenv(envGraphUserID)
	if tenantID == "" || clientID == "" || clientSecret == "" || userID == "" {
		return fmt.Errorf("missing required env vars: %s %s %s %s", envTenantID, envClientID, envClientSecret, envGraphUserID)
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	token, err := fetchToken(ctx, httpClient, tenantID, clientID, clientSecret)
	if err != nil {
		return err
	}

	user, status, endpoint, err := fetchGraphUser(ctx, httpClient, token, userID)
	if err != nil {
		return err
	}

	log.Info().
		Str("endpoint", endpoint).
		Int("status", status).
		Str("graph_user_id", user.ID).
		Str("display_name", user.DisplayName).
		Msg("Teams Graph auth test succeeded")
	return nil
}

func fetchToken(ctx context.Context, client *http.Client, tenantID, clientID, clientSecret string) (string, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("grant_type", "client_credentials")
	form.Set("scope", "https://graph.microsoft.com/.default")

	endpoint := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", errors.New("token response missing access_token")
	}
	return token.AccessToken, nil
}

func fetchGraphUser(ctx context.Context, client *http.Client, token, userID string) (graphUser, int, string, error) {
	endpoint := fmt.Sprintf("https://graph.microsoft.com/v1.0/users/%s", url.PathEscape(userID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return graphUser{}, 0, endpoint, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return graphUser{}, 0, endpoint, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return graphUser{}, resp.StatusCode, endpoint, err
	}
	if resp.StatusCode != http.StatusOK {
		return graphUser{}, resp.StatusCode, endpoint, fmt.Errorf("graph request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var user graphUser
	if err := json.Unmarshal(body, &user); err != nil {
		return graphUser{}, resp.StatusCode, endpoint, err
	}
	return user, resp.StatusCode, endpoint, nil
}

func loadDotEnv(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		value = strings.Trim(value, `"'`)
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}
