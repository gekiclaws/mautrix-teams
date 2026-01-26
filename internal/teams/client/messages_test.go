package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
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
		_, _ = w.Write([]byte(`{"messages":[{"id":"m1","clientmessageid":"c1","sequenceId":2,"from":{"id":"u1"},"imdisplayname":"User One","fromDisplayNameInToken":"Token User","createdTime":"2024-01-01T00:00:00Z","content":{"text":"hello"}},{"id":"m2","sequenceId":"1","content":{"text":""}}]}`))
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
	if msgs[1].ClientMessageID != "c1" {
		t.Fatalf("unexpected clientmessageid: %q", msgs[1].ClientMessageID)
	}
	if msgs[1].SenderID != "u1" {
		t.Fatalf("unexpected sender id: %q", msgs[1].SenderID)
	}
	if msgs[1].SenderName != "" {
		t.Fatalf("unexpected sender name: %q", msgs[1].SenderName)
	}
	if msgs[1].IMDisplayName != "User One" {
		t.Fatalf("unexpected imdisplayname: %q", msgs[1].IMDisplayName)
	}
	if msgs[1].TokenDisplayName != "Token User" {
		t.Fatalf("unexpected token display name: %q", msgs[1].TokenDisplayName)
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
	if msgs[0].IMDisplayName != "" {
		t.Fatalf("expected empty imdisplayname")
	}
	if msgs[0].TokenDisplayName != "" {
		t.Fatalf("expected empty token display name")
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
		if r.URL.Path != "/conversations/%40oneToOne.skype/messages" && r.URL.Path != "/conversations/@oneToOne.skype/messages" {
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
	if msgs[1].SenderName != "" {
		t.Fatalf("expected empty sender name for object: %q", msgs[1].SenderName)
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

func TestListMessagesEmotionsParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[` +
			`{"id":"m1","sequenceId":"1","content":{"text":"hello"},"properties":{"emotions":[` +
			`{"key":"like","users":[{"mri":"8:one","time":1700000000000},{"mri":"8:two","time":"1700000000123"},{"mri":"8:three","time":"bad"}]},` +
			`{"key":"heart","users":[]},` +
			`{"key":" ","users":[{"mri":"8:skip"}]}` +
			`],"annotationsSummary":[{"key":"like","count":2}]}}` +
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
	if len(msgs) != 1 {
		t.Fatalf("unexpected messages length: %d", len(msgs))
	}
	if len(msgs[0].Reactions) != 1 {
		t.Fatalf("unexpected reactions length: %d", len(msgs[0].Reactions))
	}
	reaction := msgs[0].Reactions[0]
	if reaction.EmotionKey != "like" {
		t.Fatalf("unexpected emotion key: %q", reaction.EmotionKey)
	}
	if len(reaction.Users) != 3 {
		t.Fatalf("unexpected reaction users length: %d", len(reaction.Users))
	}
	if reaction.Users[0].MRI != "8:one" || reaction.Users[0].TimeMS != 1700000000000 {
		t.Fatalf("unexpected first user: %#v", reaction.Users[0])
	}
	if reaction.Users[1].MRI != "8:two" || reaction.Users[1].TimeMS != 1700000000123 {
		t.Fatalf("unexpected second user: %#v", reaction.Users[1])
	}
	if reaction.Users[2].MRI != "8:three" || reaction.Users[2].TimeMS != 0 {
		t.Fatalf("unexpected third user: %#v", reaction.Users[2])
	}
}

func TestSendMessageSuccess(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotContentType string
	var gotAccept string
	var payload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
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

	threadID := "@19:abc@thread.v2"
	clientMessageID, err := client.SendMessage(context.Background(), threadID, "Hello <world>\nLine", "8:live:me")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if gotPath != "/conversations/%4019%3Aabc%40thread.v2/messages" && gotPath != "/conversations/@19:abc@thread.v2/messages" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if gotAuth != "skypetoken=token123" {
		t.Fatalf("unexpected authentication header: %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected content type: %q", gotContentType)
	}
	if gotAccept != "application/json" {
		t.Fatalf("unexpected accept header: %q", gotAccept)
	}
	if payload["type"] != "Message" {
		t.Fatalf("unexpected type: %q", payload["type"])
	}
	if payload["conversationid"] != threadID {
		t.Fatalf("unexpected conversationid: %q", payload["conversationid"])
	}
	if payload["content"] != "<p>Hello &lt;world&gt;<br>Line</p>" {
		t.Fatalf("unexpected content: %q", payload["content"])
	}
	if payload["messagetype"] != "RichText/Html" {
		t.Fatalf("unexpected messagetype: %q", payload["messagetype"])
	}
	if payload["contenttype"] != "Text" {
		t.Fatalf("unexpected contenttype: %q", payload["contenttype"])
	}
	if payload["from"] != "8:live:me" || payload["fromUserId"] != "8:live:me" {
		t.Fatalf("unexpected from fields: %q %q", payload["from"], payload["fromUserId"])
	}
	if payload["composetime"] == "" || payload["composetime"] != payload["originalarrivaltime"] {
		t.Fatalf("unexpected compose/original arrival time: %q %q", payload["composetime"], payload["originalarrivaltime"])
	}
	if payload["clientmessageid"] != clientMessageID {
		t.Fatalf("clientmessageid mismatch: %q vs %q", payload["clientmessageid"], clientMessageID)
	}
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(clientMessageID) {
		t.Fatalf("clientmessageid is not numeric: %q", clientMessageID)
	}
}

func TestSendMessageNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("nope"))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.SendMessagesURL = server.URL + "/conversations"
	client.Token = "token123"

	_, err := client.SendMessage(context.Background(), "@19:abc@thread.v2", "hello", "8:live:me")
	if err == nil {
		t.Fatalf("expected error for non-2xx")
	}
	var sendErr SendMessageError
	if !errors.As(err, &sendErr) {
		t.Fatalf("expected SendMessageError, got %T", err)
	}
	if sendErr.Status != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", sendErr.Status)
	}
	if sendErr.BodySnippet == "" {
		t.Fatalf("expected body snippet")
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
