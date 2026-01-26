package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAcquireSkypeTokenSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer msal" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"skypeToken":{"skypetoken":"jwt","expiresIn":10,"skypeid":"live:tester"}}`))
	}))
	defer server.Close()

	client := NewClient(nil)
	client.SkypeTokenEndpoint = server.URL

	start := time.Now().UTC()
	token, expiresAt, skypeID, err := client.AcquireSkypeToken(context.Background(), "msal")
	if err != nil {
		t.Fatalf("AcquireSkypeToken failed: %v", err)
	}
	if token != "jwt" {
		t.Fatalf("unexpected token: %s", token)
	}
	if skypeID != "live:tester" {
		t.Fatalf("unexpected skype id: %s", skypeID)
	}
	minExpiry := start.Add(10 * time.Second).Unix()
	maxExpiry := time.Now().UTC().Add(12 * time.Second).Unix()
	if expiresAt < minExpiry || expiresAt > maxExpiry {
		t.Fatalf("unexpected expiry: %d", expiresAt)
	}
}

func TestAcquireSkypeTokenMissingToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer msal" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"skypeToken":{"expiresIn":10}}`))
	}))
	defer server.Close()

	client := NewClient(nil)
	client.SkypeTokenEndpoint = server.URL

	_, _, _, err := client.AcquireSkypeToken(context.Background(), "msal")
	if err == nil {
		t.Fatalf("expected error for missing token")
	}
}

func TestAcquireSkypeTokenNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("nope"))
	}))
	defer server.Close()

	client := NewClient(nil)
	client.SkypeTokenEndpoint = server.URL

	_, _, _, err := client.AcquireSkypeToken(context.Background(), "msal")
	if err == nil {
		t.Fatalf("expected error for non-2xx")
	}
}

func TestAcquireSkypeTokenMissingAccessToken(t *testing.T) {
	client := NewClient(nil)
	client.SkypeTokenEndpoint = "https://example.invalid"

	_, _, _, err := client.AcquireSkypeToken(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error for missing access token")
	}
}

func TestHasValidSkypeToken(t *testing.T) {
	now := time.Now().UTC()
	state := &AuthState{
		SkypeToken:          "token",
		SkypeTokenExpiresAt: now.Add(70 * time.Second).Unix(),
	}
	if !state.HasValidSkypeToken(now) {
		t.Fatalf("expected token to be valid")
	}

	state.SkypeTokenExpiresAt = now.Add(50 * time.Second).Unix()
	if state.HasValidSkypeToken(now) {
		t.Fatalf("expected token to be invalid due to skew")
	}

	state.SkypeToken = ""
	if state.HasValidSkypeToken(now) {
		t.Fatalf("expected token to be invalid when empty")
	}
}
