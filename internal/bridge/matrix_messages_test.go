package teamsbridge

import (
	"strings"
	"testing"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

func TestRenderInboundMessageTextOnly(t *testing.T) {
	rendered := RenderInboundMessage("hello", "", nil)
	if rendered.Body != "hello" {
		t.Fatalf("unexpected body: %q", rendered.Body)
	}
	if rendered.FormattedBody != "" {
		t.Fatalf("unexpected formatted body: %q", rendered.FormattedBody)
	}
}

func TestRenderInboundMessageAttachmentOnly(t *testing.T) {
	rendered := RenderInboundMessage("", "", []model.TeamsAttachment{
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
	rendered := RenderInboundMessage("hello", "", []model.TeamsAttachment{
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
