package connector

import (
	"context"
	"fmt"
	"html"
	"strings"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

func convertTeamsMessage(_ context.Context, _ *bridgev2.Portal, _ bridgev2.MatrixAPI, msg model.RemoteMessage) (*bridgev2.ConvertedMessage, error) {
	attachments, _ := model.ParseAttachments(msg.PropertiesFiles)
	rendered := renderInboundMessageWithGIFs(msg.Body, msg.FormattedBody, attachments, msg.GIFs)

	body := strings.TrimSpace(rendered.Body)
	if body == "" && strings.TrimSpace(rendered.FormattedBody) == "" {
		body = " "
	}
	content := &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    body,
	}
	if formatted := strings.TrimSpace(rendered.FormattedBody); formatted != "" {
		content.Format = event.FormatHTML
		content.FormattedBody = formatted
		if content.Body == " " {
			// Provide a slightly better fallback for clients that don't support HTML.
			content.Body = stripHTMLFallback(formatted)
			if strings.TrimSpace(content.Body) == "" {
				content.Body = " "
			}
		}
	}

	extra := make(map[string]any)
	if senderID := strings.TrimSpace(msg.SenderID); senderID != "" {
		if displayName := strings.TrimSpace(msg.SenderName); displayName != "" {
			extra["com.beeper.per_message_profile"] = map[string]any{
				"id":          senderID,
				"displayname": displayName,
			}
		}
	}
	for key, value := range rendered.Extra {
		extra[key] = value
	}
	if len(extra) == 0 {
		extra = nil
	}

	return &bridgev2.ConvertedMessage{
		Parts: []*bridgev2.ConvertedMessagePart{{
			Type:    event.EventMessage,
			Content: content,
			Extra:   extra,
		}},
	}, nil
}

type renderedInboundMessage struct {
	Body          string
	FormattedBody string
	Extra         map[string]any
}

func renderInboundMessage(body string, formattedBody string, attachments []model.TeamsAttachment) renderedInboundMessage {
	return renderInboundMessageWithGIFs(body, formattedBody, attachments, nil)
}

func renderInboundMessageWithGIFs(body string, formattedBody string, attachments []model.TeamsAttachment, gifs []model.TeamsGIF) renderedInboundMessage {
	result := renderedInboundMessage{
		Body:          strings.TrimSpace(body),
		FormattedBody: strings.TrimSpace(formattedBody),
	}
	if len(attachments) == 0 && len(gifs) == 0 {
		return result
	}

	sections := make([]string, 0, 3)
	if strings.TrimSpace(result.Body) != "" {
		sections = append(sections, result.Body)
	}
	if len(attachments) > 0 {
		sections = append(sections, renderAttachmentBody(attachments))
	}
	if len(gifs) > 0 {
		sections = append(sections, renderGIFBody(gifs))
	}
	result.Body = strings.Join(sections, "\n\n")

	baseHTML := result.FormattedBody
	if baseHTML == "" && strings.TrimSpace(body) != "" {
		baseHTML = plainTextToHTML(body)
	}
	htmlSections := make([]string, 0, 3)
	if baseHTML != "" {
		htmlSections = append(htmlSections, baseHTML)
	}
	if len(attachments) > 0 {
		htmlSections = append(htmlSections, renderAttachmentHTML(attachments))
	}
	if len(gifs) > 0 {
		htmlSections = append(htmlSections, renderGIFHTML(gifs))
	}
	result.FormattedBody = strings.Join(htmlSections, "<br><br>")
	result.Extra = nil
	return result
}

func stripHTMLFallback(html string) string {
	// Very small fallback: teams formatted bodies are usually <p>...<br>...</p>.
	out := strings.ReplaceAll(html, "<br>", "\n")
	out = strings.ReplaceAll(out, "<br/>", "\n")
	out = strings.ReplaceAll(out, "<br />", "\n")
	out = strings.ReplaceAll(out, "<p>", "")
	out = strings.ReplaceAll(out, "</p>", "")
	return out
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

func renderGIFBody(gifs []model.TeamsGIF) string {
	lines := make([]string, 0, len(gifs))
	for _, gif := range gifs {
		lines = append(lines, fmt.Sprintf("GIF: %s - %s", gif.Title, gif.URL))
	}
	return strings.Join(lines, "\n")
}

func renderGIFHTML(gifs []model.TeamsGIF) string {
	var b strings.Builder
	b.WriteString("<ul>")
	for _, gif := range gifs {
		b.WriteString("<li>GIF: <a href=\"")
		b.WriteString(html.EscapeString(gif.URL))
		b.WriteString("\">")
		b.WriteString(html.EscapeString(gif.Title))
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
