package teams

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
)

const (
	EnvTenantID     = "AZURE_TENANT_ID"
	EnvClientID     = "AZURE_CLIENT_ID"
	EnvClientSecret = "AZURE_CLIENT_SECRET"
	EnvGraphUserID  = "AZURE_GRAPH_USER_ID"
)

type GraphCredentials struct {
	TenantID     string
	ClientID     string
	ClientSecret string
}

type GraphClient struct {
	HTTP    *http.Client
	BaseURL string
	Token   string
}

type GraphResponse struct {
	Status   int
	Header   http.Header
	Endpoint string
}

type graphTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type GraphUser struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

func LoadGraphCredentialsFromEnv(path string) (GraphCredentials, error) {
	if err := loadDotEnv(path); err != nil {
		return GraphCredentials{}, err
	}

	tenantID := os.Getenv(EnvTenantID)
	clientID := os.Getenv(EnvClientID)
	clientSecret := os.Getenv(EnvClientSecret)
	if tenantID == "" || clientID == "" || clientSecret == "" {
		return GraphCredentials{}, fmt.Errorf("missing required env vars: %s %s %s", EnvTenantID, EnvClientID, EnvClientSecret)
	}
	return GraphCredentials{
		TenantID:     tenantID,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}

func NewGraphClient(ctx context.Context, creds GraphCredentials) (*GraphClient, error) {
	httpClient := &http.Client{Timeout: 15 * time.Second}
	token, err := fetchGraphToken(ctx, httpClient, creds)
	if err != nil {
		return nil, err
	}
	return &GraphClient{
		HTTP:    httpClient,
		BaseURL: "https://graph.microsoft.com/v1.0",
		Token:   token,
	}, nil
}

func (c *GraphClient) GetUser(ctx context.Context, userID string) (GraphUser, GraphResponse, error) {
	path := fmt.Sprintf("users/%s", url.PathEscape(userID))
	var user GraphUser
	resp, err := c.getJSON(ctx, path, nil, &user)
	return user, resp, err
}

func (c *GraphClient) getJSON(ctx context.Context, path string, query url.Values, out any) (GraphResponse, error) {
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		endpoint = endpoint + "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return GraphResponse{Endpoint: endpoint}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return GraphResponse{Endpoint: endpoint}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return GraphResponse{Status: resp.StatusCode, Header: resp.Header, Endpoint: endpoint}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return GraphResponse{Status: resp.StatusCode, Header: resp.Header, Endpoint: endpoint}, fmt.Errorf("graph request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return GraphResponse{Status: resp.StatusCode, Header: resp.Header, Endpoint: endpoint}, err
	}
	return GraphResponse{Status: resp.StatusCode, Header: resp.Header, Endpoint: endpoint}, nil
}

func fetchGraphToken(ctx context.Context, client *http.Client, creds GraphCredentials) (string, error) {
	form := url.Values{}
	form.Set("client_id", creds.ClientID)
	form.Set("client_secret", creds.ClientSecret)
	form.Set("grant_type", "client_credentials")
	form.Set("scope", "https://graph.microsoft.com/.default")

	endpoint := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", creds.TenantID)
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

	var token graphTokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", errors.New("token response missing access_token")
	}
	return token.AccessToken, nil
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
