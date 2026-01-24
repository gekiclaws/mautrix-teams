package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestAuthorizeURL(t *testing.T) {
	client := NewClient(nil)
	urlStr, err := client.AuthorizeURL("challenge", "state123")
	if err != nil {
		t.Fatalf("AuthorizeURL failed: %v", err)
	}
	parsed, err := url.Parse(urlStr)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	q := parsed.Query()
	if q.Get("client_id") != defaultClientID {
		t.Fatalf("unexpected client_id: %s", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != defaultRedirectURI {
		t.Fatalf("unexpected redirect_uri: %s", q.Get("redirect_uri"))
	}
	if q.Get("response_type") != "code" {
		t.Fatalf("unexpected response_type: %s", q.Get("response_type"))
	}
	if q.Get("response_mode") != "fragment" {
		t.Fatalf("unexpected response_mode: %s", q.Get("response_mode"))
	}
	if q.Get("code_challenge") != "challenge" {
		t.Fatalf("unexpected code_challenge: %s", q.Get("code_challenge"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Fatalf("unexpected code_challenge_method: %s", q.Get("code_challenge_method"))
	}
	if q.Get("state") != "state123" {
		t.Fatalf("unexpected state: %s", q.Get("state"))
	}
	if q.Get("scope") != "openid profile offline_access" {
		t.Fatalf("unexpected scope: %s", q.Get("scope"))
	}
}

func TestTokenExchange(t *testing.T) {
	var lastGrant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		lastGrant = r.Form.Get("grant_type")
		if r.Form.Get("client_id") == "" || r.Form.Get("redirect_uri") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600,"id_token":"id"}`))
	}))
	defer server.Close()

	client := NewClient(nil)
	client.TokenEndpoint = server.URL

	state, err := client.ExchangeCode(context.Background(), "code", "verifier")
	if err != nil {
		t.Fatalf("ExchangeCode failed: %v", err)
	}
	if lastGrant != "authorization_code" {
		t.Fatalf("unexpected grant_type after exchange: %s", lastGrant)
	}
	if state.AccessToken != "access" || state.RefreshToken != "refresh" || state.IDToken != "id" {
		t.Fatalf("unexpected tokens")
	}
	if state.ExpiresAtUnix == 0 {
		t.Fatalf("expected expiry timestamp")
	}

	before := time.Now().UTC().Add(10 * time.Second).Unix()
	if state.ExpiresAtUnix < before {
		t.Fatalf("expiry too soon")
	}
}
