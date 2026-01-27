package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSendTypingIndicatorRequestShape(t *testing.T) {
	threadID := "19:abc/def@thread.v2"
	fromUserID := "8:live:me"
	escapedThreadID := url.PathEscape(threadID)

	var gotMethod string
	var gotPath string
	var gotAuth string
	var gotAccept string
	var gotContentType string
	var gotBody map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("authentication")
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	consumer := NewClient(server.Client())
	consumer.SendMessagesURL = server.URL + "/consumer/v1/users/ME/conversations"
	consumer.Token = "token123"

	status, err := consumer.SendTypingIndicator(context.Background(), threadID, fromUserID)
	if err != nil {
		t.Fatalf("SendTypingIndicator failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", status, http.StatusOK)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("unexpected method: got %s want %s", gotMethod, http.MethodPost)
	}
	expectedPath := "/consumer/v1/users/ME/conversations/" + escapedThreadID + "/messages"
	if gotPath != expectedPath {
		t.Fatalf("unexpected path: got %s want %s", gotPath, expectedPath)
	}
	if gotAuth != "skypetoken=token123" {
		t.Fatalf("unexpected authentication header: %q", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Fatalf("unexpected Accept header: %q", gotAccept)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected Content-Type header: %q", gotContentType)
	}

	if gotBody["messagetype"] != "Control/Typing" {
		t.Fatalf("unexpected messagetype: %q", gotBody["messagetype"])
	}
	if gotBody["conversationid"] != threadID {
		t.Fatalf("unexpected conversationid: %q", gotBody["conversationid"])
	}
	if gotBody["from"] != fromUserID || gotBody["fromUserId"] != fromUserID {
		t.Fatalf("unexpected from fields: from=%q fromUserId=%q", gotBody["from"], gotBody["fromUserId"])
	}
	if strings.TrimSpace(gotBody["clientmessageid"]) == "" {
		t.Fatalf("clientmessageid is empty")
	}
}

func TestSendTypingIndicatorNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad typing"))
	}))
	defer server.Close()

	consumer := NewClient(server.Client())
	consumer.SendMessagesURL = server.URL + "/conversations"
	consumer.Token = "token123"

	status, err := consumer.SendTypingIndicator(context.Background(), "19:abc@thread.v2", "8:live:me")
	if err == nil {
		t.Fatalf("expected error")
	}
	if status != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d", status, http.StatusBadRequest)
	}
	var typingErr TypingError
	if !strings.Contains(err.Error(), "typing request failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !errors.As(err, &typingErr) {
		t.Fatalf("expected TypingError, got %T", err)
	}
	if typingErr.Status != http.StatusBadRequest {
		t.Fatalf("unexpected typing error status: got %d want %d", typingErr.Status, http.StatusBadRequest)
	}
	if !strings.Contains(typingErr.BodySnippet, "bad typing") {
		t.Fatalf("unexpected body snippet: %q", typingErr.BodySnippet)
	}
}
