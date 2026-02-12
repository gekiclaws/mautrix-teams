package connector

import (
	"context"
	"strings"
	"testing"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

func TestRenderInboundMessageTextOnly(t *testing.T) {
	rendered := renderInboundMessage("hello", "", nil)
	if rendered.Body != "hello" {
		t.Fatalf("unexpected body: %q", rendered.Body)
	}
	if rendered.FormattedBody != "" {
		t.Fatalf("unexpected formatted body: %q", rendered.FormattedBody)
	}
}

func TestRenderInboundMessageAttachmentOnly(t *testing.T) {
	rendered := renderInboundMessage("", "", []model.TeamsAttachment{
		{Filename: "spec.pdf", ShareURL: "https://example.test/share"},
	})
	if !strings.Contains(rendered.Body, "Attachment: spec.pdf - https://example.test/share") {
		t.Fatalf("unexpected body: %q", rendered.Body)
	}
	if !strings.Contains(rendered.FormattedBody, `<a href="https://example.test/share">spec.pdf</a>`) {
		t.Fatalf("unexpected formatted body: %q", rendered.FormattedBody)
	}
}

func TestRenderInboundMessageTextAndAttachment(t *testing.T) {
	rendered := renderInboundMessage("hello", "", []model.TeamsAttachment{
		{Filename: "spec.pdf", ShareURL: "https://example.test/share"},
	})
	if !strings.Contains(rendered.Body, "hello") {
		t.Fatalf("body missing original text: %q", rendered.Body)
	}
	if !strings.Contains(rendered.Body, "Attachment: spec.pdf - https://example.test/share") {
		t.Fatalf("body missing attachment line: %q", rendered.Body)
	}
	if !strings.Contains(rendered.FormattedBody, "hello") {
		t.Fatalf("formatted body missing original text: %q", rendered.FormattedBody)
	}
	if !strings.Contains(rendered.FormattedBody, `<a href="https://example.test/share">spec.pdf</a>`) {
		t.Fatalf("formatted body missing attachment link: %q", rendered.FormattedBody)
	}
}

func TestRenderInboundMessageGIFOnly(t *testing.T) {
	rendered := renderInboundMessageWithGIFs("", "", nil, []model.TeamsGIF{
		{Title: "Football GIF", URL: "https://media4.giphy.com/media/test/giphy.gif"},
	})
	if !strings.Contains(rendered.Body, "GIF: Football GIF - https://media4.giphy.com/media/test/giphy.gif") {
		t.Fatalf("unexpected body: %q", rendered.Body)
	}
	if !strings.Contains(rendered.FormattedBody, `<a href="https://media4.giphy.com/media/test/giphy.gif">Football GIF</a>`) {
		t.Fatalf("unexpected formatted body: %q", rendered.FormattedBody)
	}
}

func TestConvertTeamsMessageAddsPerMessageProfile(t *testing.T) {
	msg := model.RemoteMessage{
		Body:       "hello",
		SenderID:   "8:live:me",
		SenderName: "Alice",
	}
	converted, err := convertTeamsMessage(context.Background(), nil, nil, msg)
	if err != nil {
		t.Fatalf("convertTeamsMessage failed: %v", err)
	}
	if converted == nil || len(converted.Parts) != 1 || converted.Parts[0].Extra == nil {
		t.Fatalf("expected message part with extra metadata")
	}
	extra := converted.Parts[0].Extra
	perMessage, ok := extra["com.beeper.per_message_profile"].(map[string]any)
	if !ok {
		t.Fatalf("expected per_message_profile map, got %#v", extra["com.beeper.per_message_profile"])
	}
	if perMessage["id"] != "8:live:me" || perMessage["displayname"] != "Alice" {
		t.Fatalf("unexpected per_message_profile: %#v", perMessage)
	}
}
