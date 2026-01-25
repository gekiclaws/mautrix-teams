package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListConversationsSuccess(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("authentication")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"conversations":[{"threadProperties":{"originalThreadId":"abc","productThreadType":"OneToOneChat"}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.ConversationsURL = server.URL

	convos, err := client.ListConversations(context.Background(), "token123")
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}
	if gotAuth != "skypetoken=token123" {
		t.Fatalf("unexpected authentication header: %q", gotAuth)
	}
	if len(convos) != 1 {
		t.Fatalf("unexpected conversations length: %d", len(convos))
	}
}

func TestListConversationsNon2xx(t *testing.T) {
	body := strings.Repeat("a", maxErrorBodyBytes+10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.ConversationsURL = server.URL

	_, err := client.ListConversations(context.Background(), "token123")
	if err == nil {
		t.Fatalf("expected error")
	}
	var convErr ConversationsError
	if !errors.As(err, &convErr) {
		t.Fatalf("expected ConversationsError, got %T", err)
	}
	if convErr.Status != http.StatusForbidden {
		t.Fatalf("unexpected status: %d", convErr.Status)
	}
	if len(convErr.BodySnippet) != maxErrorBodyBytes {
		t.Fatalf("unexpected body snippet length: %d", len(convErr.BodySnippet))
	}
}
