package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAddReactionSuccess(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotQuery string
	var gotAuth string
	var gotContentType string
	var gotAccept string
	var payload map[string]map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("authentication")
		gotContentType = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.SendMessagesURL = server.URL + "/conversations"
	client.Token = "token123"

	status, err := client.AddReaction(context.Background(), "@19:abc@thread.v2", "msg/1", "like", 1700000000000)
	if err != nil {
		t.Fatalf("AddReaction failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("unexpected status: %d", status)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("unexpected method: %s", gotMethod)
	}
	if gotPath != "/conversations/%4019%3Aabc%40thread.v2/messages/msg%2F1/properties" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if gotQuery != "name=emotions" {
		t.Fatalf("unexpected query: %q", gotQuery)
	}
	if gotAuth != "skypetoken=token123" {
		t.Fatalf("unexpected auth: %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected content type: %q", gotContentType)
	}
	if gotAccept != "application/json" {
		t.Fatalf("unexpected accept: %q", gotAccept)
	}
	if payload["emotions"]["key"] != "like" {
		t.Fatalf("unexpected key: %v", payload["emotions"]["key"])
	}
	if payload["emotions"]["value"].(float64) != 1700000000000 {
		t.Fatalf("unexpected value: %v", payload["emotions"]["value"])
	}
}

func TestRemoveReactionSuccess(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotQuery string
	var payload map[string]map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.SendMessagesURL = server.URL + "/conversations"
	client.Token = "token123"

	status, err := client.RemoveReaction(context.Background(), "@19:abc@thread.v2", "msg/1", "heart")
	if err != nil {
		t.Fatalf("RemoveReaction failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("unexpected status: %d", status)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("unexpected method: %s", gotMethod)
	}
	if gotPath != "/conversations/%4019%3Aabc%40thread.v2/messages/msg%2F1/properties" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if gotQuery != "name=emotions" {
		t.Fatalf("unexpected query: %q", gotQuery)
	}
	if payload["emotions"]["key"] != "heart" {
		t.Fatalf("unexpected key: %v", payload["emotions"]["key"])
	}
}

func TestReactionNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("nope"))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.SendMessagesURL = server.URL + "/conversations"
	client.Token = "token123"

	_, err := client.AddReaction(context.Background(), "@19:abc@thread.v2", "msg/1", "like", 1)
	if err == nil {
		t.Fatalf("expected error")
	}
	var reactionErr ReactionError
	if !errors.As(err, &reactionErr) {
		t.Fatalf("expected ReactionError, got %T", err)
	}
	if reactionErr.Status != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", reactionErr.Status)
	}
	if reactionErr.BodySnippet == "" {
		t.Fatalf("expected body snippet")
	}
}
