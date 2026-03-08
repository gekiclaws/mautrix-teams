package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.mau.fi/mautrix-teams/internal/teams/auth"
)

func TestExtractTeamsLoginMetadataFromLocalStorage_PersistsGraphToken(t *testing.T) {
	skypeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer mbi-access-token" {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		_, _ = w.Write([]byte(`{"skypeToken":{"skypetoken":"skype-token","expiresIn":600,"skypeid":"live:tester"}}`))
	}))
	defer skypeServer.Close()

	origFactory := newAuthClient
	newAuthClient = func(store *auth.CookieStore) *auth.Client {
		client := auth.NewClient(store)
		client.SkypeTokenEndpoint = skypeServer.URL
		return client
	}
	defer func() {
		newAuthClient = origFactory
	}()

	storage := map[string]string{
		"msal.token.keys." + auth.DefaultClientID: `{"refreshToken":["rt"],"idToken":["id"],"accessToken":["mbi","graph"]}`,
		"rt":    `{"secret":"refresh-token","expiresOn":"1700000000"}`,
		"id":    `{"secret":"id-token"}`,
		"mbi":   `{"secret":"mbi-access-token","expiresOn":"1700000100","target":"service::api.fl.spaces.skype.com::MBI_SSL"}`,
		"graph": `{"secret":"graph-access-token","expiresOn":"1700000200","target":"https://graph.microsoft.com/Files.ReadWrite User.Read"}`,
	}
	payload, err := json.Marshal(storage)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	meta, err := ExtractTeamsLoginMetadataFromLocalStorage(context.Background(), string(payload), auth.DefaultClientID)
	if err != nil {
		t.Fatalf("unexpected extraction error: %v", err)
	}
	if meta.GraphAccessToken != "graph-access-token" {
		t.Fatalf("unexpected graph token: %s", meta.GraphAccessToken)
	}
	if meta.GraphExpiresAt != 1700000200 {
		t.Fatalf("unexpected graph expiry: %d", meta.GraphExpiresAt)
	}
	if meta.SkypeToken != "skype-token" {
		t.Fatalf("unexpected skype token: %s", meta.SkypeToken)
	}
	if meta.TeamsUserID != "8:live:tester" {
		t.Fatalf("unexpected teams user id: %s", meta.TeamsUserID)
	}
}

func TestExtractTeamsLoginMetadataFromLocalStorage_NoGraphTokenStillSucceeds(t *testing.T) {
	skypeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"skypeToken":{"skypetoken":"skype-token","expiresIn":600,"skypeid":"live:tester"}}`))
	}))
	defer skypeServer.Close()

	origFactory := newAuthClient
	newAuthClient = func(store *auth.CookieStore) *auth.Client {
		client := auth.NewClient(store)
		client.SkypeTokenEndpoint = skypeServer.URL
		return client
	}
	defer func() {
		newAuthClient = origFactory
	}()

	storage := map[string]string{
		"msal.token.keys." + auth.DefaultClientID: `{"refreshToken":["rt"],"accessToken":["mbi"]}`,
		"rt":  `{"secret":"refresh-token","expiresOn":"1700000000"}`,
		"mbi": `{"secret":"mbi-access-token","expiresOn":"1700000100","target":"service::api.fl.spaces.skype.com::MBI_SSL"}`,
	}
	payload, err := json.Marshal(storage)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	meta, err := ExtractTeamsLoginMetadataFromLocalStorage(context.Background(), string(payload), auth.DefaultClientID)
	if err != nil {
		t.Fatalf("unexpected extraction error: %v", err)
	}
	if meta.GraphAccessToken != "" {
		t.Fatalf("expected empty graph token, got %s", meta.GraphAccessToken)
	}
	if meta.GraphExpiresAt != 0 {
		t.Fatalf("expected empty graph expiry, got %d", meta.GraphExpiresAt)
	}
	if meta.SkypeToken != "skype-token" {
		t.Fatalf("unexpected skype token: %s", meta.SkypeToken)
	}
}

func TestExtractTeamsLoginMetadataFromLocalStorage_RefreshesGraphWhenMissingInStorage(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		scope := r.Form.Get("scope")
		switch {
		case scope == "service::api.fl.spaces.skype.com::MBI_SSL offline_access":
			_, _ = w.Write([]byte(`{"access_token":"mbi-from-refresh","refresh_token":"refresh-updated","expires_in":3600}`))
		case scope == "https://graph.microsoft.com/Files.ReadWrite offline_access":
			_, _ = w.Write([]byte(`{"access_token":"graph-from-refresh","refresh_token":"refresh-updated-2","expires_in":7200}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer tokenServer.Close()

	skypeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer mbi-from-refresh" {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		_, _ = w.Write([]byte(`{"skypeToken":{"skypetoken":"skype-token","expiresIn":600,"skypeid":"live:tester"}}`))
	}))
	defer skypeServer.Close()

	origFactory := newAuthClient
	newAuthClient = func(store *auth.CookieStore) *auth.Client {
		client := auth.NewClient(store)
		client.TokenEndpoint = tokenServer.URL
		client.SkypeTokenEndpoint = skypeServer.URL
		return client
	}
	defer func() {
		newAuthClient = origFactory
	}()

	storage := map[string]string{
		"msal.token.keys." + auth.DefaultClientID: `{"refreshToken":["rt"],"accessToken":["openid-only"]}`,
		"rt":          `{"secret":"refresh-token","expiresOn":"1700000000"}`,
		"openid-only": `{"secret":"openid-token","expiresOn":"1700000100","target":"openid profile"}`,
	}
	payload, err := json.Marshal(storage)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	meta, err := ExtractTeamsLoginMetadataFromLocalStorage(context.Background(), string(payload), auth.DefaultClientID)
	if err != nil {
		t.Fatalf("unexpected extraction error: %v", err)
	}
	if meta.GraphAccessToken != "graph-from-refresh" {
		t.Fatalf("unexpected graph token: %s", meta.GraphAccessToken)
	}
	if meta.GraphExpiresAt == 0 {
		t.Fatalf("expected graph expiry to be set")
	}
	if meta.RefreshToken != "refresh-updated-2" {
		t.Fatalf("unexpected refresh token: %s", meta.RefreshToken)
	}
	if meta.SkypeToken != "skype-token" {
		t.Fatalf("unexpected skype token: %s", meta.SkypeToken)
	}
}

func TestResolveClientID_DefaultsByAccountType(t *testing.T) {
	if got := resolveClientID(nil, auth.AccountTypeConsumer); got != auth.DefaultClientID {
		t.Fatalf("unexpected consumer client id: %s", got)
	}
	if got := resolveClientID(nil, auth.AccountTypeEnterprise); got != auth.EnterpriseClientID {
		t.Fatalf("unexpected enterprise client id: %s", got)
	}
}

func TestResolveClientID_PrefersConfiguredClientID(t *testing.T) {
	main := &TeamsConnector{Config: TeamsConfig{ClientID: "configured-client-id"}}
	if got := resolveClientID(main, auth.AccountTypeConsumer); got != "configured-client-id" {
		t.Fatalf("unexpected consumer configured client id: %s", got)
	}
	// Enterprise always uses the fixed enterprise client ID, ignoring operator config.
	if got := resolveClientID(main, auth.AccountTypeEnterprise); got != auth.EnterpriseClientID {
		t.Fatalf("unexpected enterprise client id: %s", got)
	}
}

func TestRefreshAccessTokenForEnterpriseScope_FallbackOnPrimaryFailure(t *testing.T) {
	var receivedScopes []string
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		scope := r.Form.Get("scope")
		receivedScopes = append(receivedScopes, scope)

		switch scope {
		case "https://api.spaces.skype.com/.default offline_access":
			w.WriteHeader(http.StatusBadRequest)
		case "openid profile offline_access https://api.spaces.skype.com/.default":
			_, _ = w.Write([]byte(`{"access_token":"enterprise-access","refresh_token":"refresh-updated","expires_in":3600}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer tokenServer.Close()

	client := auth.NewEnterpriseClient("common", nil)
	client.TokenEndpoint = tokenServer.URL

	state, err := refreshAccessTokenForEnterpriseScope(context.Background(), client, "refresh-token")
	if err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}
	if state.AccessToken != "enterprise-access" {
		t.Fatalf("unexpected access token: %s", state.AccessToken)
	}
	if len(receivedScopes) < 2 {
		t.Fatalf("expected primary and fallback refresh attempts, got %d", len(receivedScopes))
	}
}
