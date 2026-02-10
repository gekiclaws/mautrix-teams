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
	return RenderInboundMessageWithGIFs(result.Body, result.FormattedBody, attachments, nil)
}

func RenderInboundMessageWithGIFs(body string, formattedBody string, attachments []model.TeamsAttachment, gifs []model.TeamsGIF) RenderedInboundMessage {
	result := RenderedInboundMessage{
		Body:          body,
		FormattedBody: formattedBody,
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
