package main

import (
	"testing"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestParseDevSendArgsRequiredAndDefaults(t *testing.T) {
	_, err := parseDevSendArgs([]string{})
	if err == nil {
		t.Fatalf("expected error for missing args")
	}

	opts, err := parseDevSendArgs([]string{
		"--room", "!room:example.org",
		"--sender", "8:live:someone",
		"--text", "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.ConfigPath != "config.yaml" {
		t.Fatalf("expected default config path, got %q", opts.ConfigPath)
	}
	if opts.RoomID != id.RoomID("!room:example.org") {
		t.Fatalf("unexpected room id: %s", opts.RoomID)
	}
	if opts.Sender != "8:live:someone" {
		t.Fatalf("unexpected sender: %s", opts.Sender)
	}
	if opts.Text != "hello" {
		t.Fatalf("unexpected text: %s", opts.Text)
	}
}

func TestBuildDevMatrixTextEvent(t *testing.T) {
	opts := DevSendOptions{
		RoomID:  "!room:example.org",
		Sender:  "8:live:someone",
		Text:    "hello",
		EventID: "$dev-event",
	}
	evt := buildDevMatrixTextEvent(opts)
	if evt == nil {
		t.Fatalf("expected event")
	}
	if evt.RoomID != id.RoomID("!room:example.org") {
		t.Fatalf("unexpected room id: %s", evt.RoomID)
	}
	if evt.Sender != id.UserID("8:live:someone") {
		t.Fatalf("unexpected sender: %s", evt.Sender)
	}
	if evt.ID != id.EventID("$dev-event") {
		t.Fatalf("unexpected event id: %s", evt.ID)
	}
	if evt.Type != event.EventMessage {
		t.Fatalf("unexpected event type: %v", evt.Type)
	}
	content, ok := evt.Content.Parsed.(*event.MessageEventContent)
	if !ok || content == nil {
		t.Fatalf("expected message content")
	}
	if content.MsgType != event.MsgText {
		t.Fatalf("unexpected msgtype: %s", content.MsgType)
	}
	if content.Body != "hello" {
		t.Fatalf("unexpected body: %s", content.Body)
	}
}
