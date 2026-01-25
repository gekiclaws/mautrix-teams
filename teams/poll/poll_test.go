package poll

import (
	"testing"
	"time"

	"go.mau.fi/mautrix-teams/teams"
)

func TestNormalizeMessage(t *testing.T) {
	message := teams.GraphMessage{
		ID:              "msg-1",
		CreatedDateTime: time.Unix(123, 0).UTC(),
		From: &teams.GraphMessageFrom{
			User: &teams.GraphMessageUser{
				ID:          "user-1",
				DisplayName: "User One",
			},
		},
		Body: teams.GraphMessageBody{
			ContentType: "html",
			Content:     "<b>Hello</b> world",
		},
	}

	normalized := normalizeMessage("chat-1", message)
	if normalized.ChatID != "chat-1" {
		t.Fatalf("ChatID mismatch: %s", normalized.ChatID)
	}
	if normalized.MessageID != "msg-1" {
		t.Fatalf("MessageID mismatch: %s", normalized.MessageID)
	}
	if normalized.SenderID != "user-1" {
		t.Fatalf("SenderID mismatch: %s", normalized.SenderID)
	}
	if normalized.SenderName != "User One" {
		t.Fatalf("SenderName mismatch: %s", normalized.SenderName)
	}
	if normalized.BodyText != "Hello world" {
		t.Fatalf("BodyText mismatch: %s", normalized.BodyText)
	}
	if !normalized.CreatedAt.Equal(message.CreatedDateTime) {
		t.Fatalf("CreatedAt mismatch: %s", normalized.CreatedAt)
	}
}

func TestCollectPolledMessagesCursor(t *testing.T) {
	messages := []teams.GraphMessage{
		{ID: "1"},
		{ID: "2"},
		{ID: "3"},
	}

	polled, found := collectPolledMessages("chat-1", messages, "2")
	if !found {
		t.Fatalf("expected cursor to be found")
	}
	if len(polled) != 1 || polled[0].MessageID != "3" {
		t.Fatalf("unexpected polled messages: %#v", polled)
	}

	polled, found = collectPolledMessages("chat-1", messages, "9")
	if found {
		t.Fatalf("expected cursor to be missing")
	}
	if len(polled) != 0 {
		t.Fatalf("expected no messages when cursor missing: %#v", polled)
	}

	polled, found = collectPolledMessages("chat-1", messages, "")
	if !found {
		t.Fatalf("expected cursor to be treated as found for empty cursor")
	}
	if len(polled) != 3 {
		t.Fatalf("expected all messages for empty cursor, got %d", len(polled))
	}
}
