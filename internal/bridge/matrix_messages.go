package teamsbridge

import (
	"fmt"
	"html"
	"strings"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type RenderedInboundMessage struct {
	Body          string
	FormattedBody string
	Extra         map[string]any
}

func RenderInboundMessage(body string, formattedBody string, attachments []model.TeamsAttachment) RenderedInboundMessage {
	result := RenderedInboundMessage{
		Body:          body,
		FormattedBody: formattedBody,
	}
	if len(attachments) == 0 {
		return result
	}

	attachmentBody := renderAttachmentBody(attachments)
	switch {
	case strings.TrimSpace(result.Body) == "":
		result.Body = attachmentBody
	default:
		result.Body = result.Body + "\n\n" + attachmentBody
	}

	baseHTML := result.FormattedBody
	if baseHTML == "" && strings.TrimSpace(body) != "" {
		baseHTML = plainTextToHTML(body)
	}
	attachmentsHTML := renderAttachmentHTML(attachments)
	if baseHTML == "" {
		result.FormattedBody = attachmentsHTML
	} else {
		result.FormattedBody = baseHTML + "<br><br>" + attachmentsHTML
	}

	// Extension point for client-specific attachment metadata when needed.
	result.Extra = nil
	return result
}

func renderAttachmentBody(attachments []model.TeamsAttachment) string {
	lines := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		lines = append(lines, fmt.Sprintf("Attachment: %s - %s", attachment.Filename, attachment.ShareURL))
	}
	return strings.Join(lines, "\n")
}

func renderAttachmentHTML(attachments []model.TeamsAttachment) string {
	var b strings.Builder
	b.WriteString("<ul>")
	for _, attachment := range attachments {
		b.WriteString("<li>Attachment: <a href=\"")
		b.WriteString(html.EscapeString(attachment.ShareURL))
		b.WriteString("\">")
		b.WriteString(html.EscapeString(attachment.Filename))
		b.WriteString("</a></li>")
	}
	b.WriteString("</ul>")
	return b.String()
}

func plainTextToHTML(text string) string {
	escaped := html.EscapeString(text)
	escaped = strings.ReplaceAll(escaped, "\n", "<br>")
	return escaped
}
