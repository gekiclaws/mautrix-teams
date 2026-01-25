package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

func TestListMessagesSuccess(t *testing.T) {
	var gotAuth []string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("authentication"))
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[{"id":"m1","sequenceId":2,"from":{"id":"u1","displayName":"User One"},"createdTime":"2024-01-01T00:00:00Z","content":{"text":"hello"}},{"id":"m2","sequenceId":"1","content":{"text":""}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.MessagesURL = server.URL + "/conversations"
	client.Token = "token123"

	msgs, err := client.ListMessages(context.Background(), "@oneToOne.skype", "")
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(gotAuth) != 1 {
		t.Fatalf("expected one request, got %d", len(gotAuth))
	}
	for _, auth := range gotAuth {
		if auth != "skypetoken=token123" {
			t.Fatalf("unexpected authorization header: %q", auth)
		}
	}
	if !strings.Contains(gotPath, "/conversations/") || !strings.Contains(gotPath, "/messages") {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if !strings.Contains(gotPath, "@oneToOne.skype") && !strings.Contains(gotPath, "%40oneToOne.skype") {
		t.Fatalf("unexpected conversation id in path: %q", gotPath)
	}
	if len(msgs) != 2 {
		t.Fatalf("unexpected messages length: %d", len(msgs))
	}
	if msgs[0].MessageID != "m2" || msgs[1].MessageID != "m1" {
		t.Fatalf("unexpected ordering: %#v", msgs)
	}
	if msgs[1].SenderID != "u1" {
		t.Fatalf("unexpected sender id: %q", msgs[1].SenderID)
	}
	if msgs[1].SenderName != "User One" {
		t.Fatalf("unexpected sender name: %q", msgs[1].SenderName)
	}
	if msgs[1].Body != "hello" {
		t.Fatalf("unexpected body: %q", msgs[1].Body)
	}
	if msgs[1].Timestamp.IsZero() {
		t.Fatalf("expected parsed timestamp")
	}
	if msgs[1].Timestamp.Format(time.RFC3339) != "2024-01-01T00:00:00Z" {
		t.Fatalf("unexpected timestamp: %s", msgs[1].Timestamp.Format(time.RFC3339))
	}
}

func TestListMessagesMissingOptionalFields(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[{"id":"m1","sequenceId":"1"}]}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.MessagesURL = server.URL + "/conversations"
	client.Token = "token123"

	msgs, err := client.ListMessages(context.Background(), "@oneToOne.skype", "")
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("unexpected messages length: %d", len(msgs))
	}
	if !strings.Contains(gotPath, "/conversations/") || !strings.Contains(gotPath, "/messages") {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if msgs[0].SenderID != "" {
		t.Fatalf("expected empty sender id")
	}
	if msgs[0].SenderName != "" {
		t.Fatalf("expected empty sender name")
	}
	if !msgs[0].Timestamp.IsZero() {
		t.Fatalf("expected zero timestamp")
	}
}

func TestListMessagesContentVariants(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[` +
			`{"id":"m1","sequenceId":"1","content":"hey how&apos;ve u been?"},` +
			`{"id":"m2","sequenceId":"2","content":{"text":"hello"}},` +
			`{"id":"m3","sequenceId":"3","content":""},` +
			`{"id":"m4","sequenceId":"4","content":123}` +
			`]}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.MessagesURL = server.URL + "/conversations"
	client.Token = "token123"

	msgs, err := client.ListMessages(context.Background(), "@oneToOne.skype", "")
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("unexpected messages length: %d", len(msgs))
	}
	if !strings.Contains(gotPath, "/conversations/") || !strings.Contains(gotPath, "/messages") {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if msgs[0].Body != "hey how've u been?" {
		t.Fatalf("unexpected body for string content: %q", msgs[0].Body)
	}
	if msgs[1].Body != "hello" {
		t.Fatalf("unexpected body for object content: %q", msgs[1].Body)
	}
	if msgs[2].Body != "" {
		t.Fatalf("expected empty body for empty content")
	}
	if msgs[3].Body != "" {
		t.Fatalf("expected empty body for unsupported content")
	}
}

func TestListMessagesFromVariants(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations/%40oneToOne.skype/messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[` +
			`{"id":"m1","sequenceId":"1","from":"https://msgapi.teams.live.com/v1/users/ME/contacts/8:live:mattckwong","content":{"text":"hello"}},` +
			`{"id":"m2","sequenceId":"2","from":{"id":"8:live:mattckwong","displayName":"Matt"},"content":{"text":"hi"}},` +
			`{"id":"m3","sequenceId":"3","from":""},` +
			`{"id":"m4","sequenceId":"4","from":123}` +
			`]}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.MessagesURL = server.URL + "/conversations"
	client.Token = "token123"

	msgs, err := client.ListMessages(context.Background(), "@oneToOne.skype", "")
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("unexpected messages length: %d", len(msgs))
	}
	if msgs[0].SenderID != "8:live:mattckwong" {
		t.Fatalf("unexpected sender id for URL: %q", msgs[0].SenderID)
	}
	if msgs[0].SenderName != "" {
		t.Fatalf("expected empty sender name for URL: %q", msgs[0].SenderName)
	}
	if msgs[1].SenderID != "8:live:mattckwong" {
		t.Fatalf("unexpected sender id for object: %q", msgs[1].SenderID)
	}
	if msgs[1].SenderName != "Matt" {
		t.Fatalf("unexpected sender name for object: %q", msgs[1].SenderName)
	}
	if msgs[2].SenderID != "" {
		t.Fatalf("expected empty sender id for empty from")
	}
	if msgs[2].SenderName != "" {
		t.Fatalf("expected empty sender name for empty from")
	}
	if msgs[3].SenderID != "" {
		t.Fatalf("expected empty sender id for malformed from")
	}
	if msgs[3].SenderName != "" {
		t.Fatalf("expected empty sender name for malformed from")
	}
}

func TestListMessagesNon2xx(t *testing.T) {
	body := strings.Repeat("a", maxErrorBodyBytes+10)
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.MessagesURL = server.URL + "/conversations"
	client.Token = "token123"

	_, err := client.ListMessages(context.Background(), "@oneToOne.skype", "")
	if err == nil {
		t.Fatalf("expected error")
	}
	var msgErr MessagesError
	if !errors.As(err, &msgErr) {
		t.Fatalf("expected MessagesError, got %T", err)
	}
	if msgErr.Status != http.StatusForbidden {
		t.Fatalf("unexpected status: %d", msgErr.Status)
	}
	if !strings.Contains(gotPath, "/conversations/") || !strings.Contains(gotPath, "/messages") {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if len(msgErr.BodySnippet) != maxErrorBodyBytes {
		t.Fatalf("unexpected body snippet length: %d", len(msgErr.BodySnippet))
	}
}

func TestListMessagesMissingConversationID(t *testing.T) {
	client := NewClient(http.DefaultClient)
	client.Token = "token123"

	_, err := client.ListMessages(context.Background(), "", "")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "missing conversation id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompareSequenceIDFallback(t *testing.T) {
	if model.CompareSequenceID("10", "2") <= 0 {
		t.Fatalf("expected numeric comparison to order 10 after 2")
	}
	if model.CompareSequenceID("A2", "10") <= 0 {
		t.Fatalf("expected lexicographic comparison when parse fails")
	}
}
