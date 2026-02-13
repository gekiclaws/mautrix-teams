package connector

import (
	"context"
	"fmt"
	"html"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	internalbridge "go.mau.fi/mautrix-teams/internal/bridge"
	"go.mau.fi/mautrix-teams/internal/teams/graph"
	"go.mau.fi/mautrix-teams/internal/teams/model"
)

func (c *TeamsClient) convertTeamsMessage(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, msg model.RemoteMessage) (*bridgev2.ConvertedMessage, error) {
	attachments, _ := model.ParseAttachments(msg.PropertiesFiles)
	// If no attachments have DriveItemIDs (i.e. no Graph download ID), preserve legacy behavior.
	// This keeps the conversion robust for older payload variants.
	hasDriveItemID := false
	for _, att := range attachments {
		if strings.TrimSpace(att.DriveItemID) != "" {
			hasDriveItemID = true
			break
		}
	}
	if !hasDriveItemID || intent == nil || c == nil {
		return convertTeamsMessageLegacy(msg), nil
	}

	extra := perMessageExtra(msg)

	roomID := id.RoomID("")
	if portal != nil {
		roomID = portal.MXID
	}
	mediaParts, fallback := c.reuploadInboundAttachments(ctx, roomID, intent, attachments, extra)
	parts := make([]*bridgev2.ConvertedMessagePart, 0, len(mediaParts)+1)
	parts = append(parts, mediaParts...)

	// Caption: always preserve Teams message body (and include any GIFs and fallback attachment lines)
	// as a separate m.text message after all attachment parts.
	captionRendered := renderInboundMessageWithGIFs(msg.Body, msg.FormattedBody, fallback, msg.GIFs)
	if captionPart := buildCaptionPart(networkid.PartID("caption"), captionRendered, extra); captionPart != nil {
		parts = append(parts, captionPart)
	}

	if len(parts) == 0 {
		// Hard guarantee: never drop the message entirely.
		return &bridgev2.ConvertedMessage{
			Parts: []*bridgev2.ConvertedMessagePart{{
				Type:    event.EventMessage,
				Content: &event.MessageEventContent{MsgType: event.MsgText, Body: " "},
				Extra:   extra,
			}},
		}, nil
	}

	return &bridgev2.ConvertedMessage{Parts: parts}, nil
}

func convertTeamsMessageLegacy(msg model.RemoteMessage) *bridgev2.ConvertedMessage {
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

	extra := perMessageExtraWithRendered(msg, rendered)

	return &bridgev2.ConvertedMessage{
		Parts: []*bridgev2.ConvertedMessagePart{{
			Type:    event.EventMessage,
			Content: content,
			Extra:   extra,
		}},
	}
}

func perMessageExtra(msg model.RemoteMessage) map[string]any {
	extra := make(map[string]any)
	if senderID := strings.TrimSpace(msg.SenderID); senderID != "" {
		if displayName := strings.TrimSpace(msg.SenderName); displayName != "" {
			extra["com.beeper.per_message_profile"] = map[string]any{
				"id":          senderID,
				"displayname": displayName,
			}
		}
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func perMessageExtraWithRendered(msg model.RemoteMessage, rendered renderedInboundMessage) map[string]any {
	extra := perMessageExtra(msg)
	if len(rendered.Extra) == 0 {
		return extra
	}
	if extra == nil {
		extra = make(map[string]any, len(rendered.Extra))
	}
	for k, v := range rendered.Extra {
		extra[k] = v
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func cloneExtra(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	out := make(map[string]any, len(extra))
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func buildCaptionPart(partID networkid.PartID, rendered renderedInboundMessage, extra map[string]any) *bridgev2.ConvertedMessagePart {
	body := strings.TrimSpace(rendered.Body)
	formatted := strings.TrimSpace(rendered.FormattedBody)
	if body == "" && formatted == "" {
		return nil
	}
	if body == "" {
		body = " "
	}
	content := &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    body,
	}
	if formatted != "" {
		content.Format = event.FormatHTML
		content.FormattedBody = formatted
		if content.Body == " " {
			content.Body = stripHTMLFallback(formatted)
			if strings.TrimSpace(content.Body) == "" {
				content.Body = " "
			}
		}
	}
	return &bridgev2.ConvertedMessagePart{
		ID:      partID,
		Type:    event.EventMessage,
		Content: content,
		Extra:   extra,
	}
}

func (c *TeamsClient) reuploadInboundAttachments(
	ctx context.Context,
	roomID id.RoomID,
	intent bridgev2.MatrixAPI,
	attachments []model.TeamsAttachment,
	extra map[string]any,
) (mediaParts []*bridgev2.ConvertedMessagePart, fallback []model.TeamsAttachment) {
	// Missing Graph token: degrade per attachment (never drop message).
	if err := c.ensureValidGraphToken(ctx); err != nil {
		for _, att := range attachments {
			if strings.TrimSpace(att.DriveItemID) == "" {
				fallback = append(fallback, att)
			} else {
				fallback = append(fallback, att)
			}
		}
		return nil, fallback
	}
	graphToken, err := c.Meta.GetGraphAccessToken()
	if err != nil {
		return nil, attachments
	}
	httpClient := c.getConsumerHTTP()
	if httpClient == nil {
		return nil, attachments
	}
	gc := graph.NewClient(httpClient)
	gc.AccessToken = graphToken
	gc.MaxUploadSize = internalbridge.MaxAttachmentBytesV0
	if c.Login != nil {
		gc.Log = &c.Login.Log
	}

	for i, att := range attachments {
		driveItemID := strings.TrimSpace(att.DriveItemID)
		if driveItemID == "" {
			// Missing DriveItemID: fall back to share-link rendering.
			fallback = append(fallback, att)
			continue
		}

		content, err := gc.DownloadDriveItemContent(ctx, driveItemID)
		if err != nil || content == nil || len(content.Bytes) == 0 {
			fallback = append(fallback, att)
			continue
		}

		mimeType := detectMIMEType(att.Filename, content.ContentType, content.Bytes)
		msgType := matrixMsgTypeForMIME(mimeType)

		mxc, file, err := intent.UploadMedia(ctx, roomID, content.Bytes, strings.TrimSpace(att.Filename), mimeType)
		if err != nil {
			fallback = append(fallback, att)
			continue
		}

		part := &bridgev2.ConvertedMessagePart{
			ID:      networkid.PartID(fmt.Sprintf("att_%d", i)),
			Type:    event.EventMessage,
			Extra:   cloneExtra(extra),
			Content: buildMediaContent(msgType, strings.TrimSpace(att.Filename), mimeType, len(content.Bytes), mxc, file),
		}
		mediaParts = append(mediaParts, part)
	}

	return mediaParts, fallback
}

func detectMIMEType(filename string, headerContentType string, data []byte) string {
	if ct := normalizeContentType(headerContentType); ct != "" && ct != "application/octet-stream" {
		return ct
	}
	if len(data) > 0 {
		if sniffed := normalizeContentType(http.DetectContentType(data)); sniffed != "" {
			return sniffed
		}
	}
	if ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename))); ext != "" {
		if byExt := normalizeContentType(mime.TypeByExtension(ext)); byExt != "" {
			return byExt
		}
	}
	return "application/octet-stream"
}

func normalizeContentType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if semi := strings.IndexByte(value, ';'); semi >= 0 {
		value = strings.TrimSpace(value[:semi])
	}
	return value
}

func matrixMsgTypeForMIME(mimeType string) event.MessageType {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return event.MsgImage
	case strings.HasPrefix(mimeType, "video/"):
		return event.MsgVideo
	case strings.HasPrefix(mimeType, "audio/"):
		return event.MsgAudio
	default:
		return event.MsgFile
	}
}

func buildMediaContent(
	msgType event.MessageType,
	filename string,
	mimeType string,
	size int,
	mxc id.ContentURIString,
	file *event.EncryptedFileInfo,
) *event.MessageEventContent {
	content := &event.MessageEventContent{
		MsgType:  msgType,
		Body:     filename,
		FileName: filename,
		Info: &event.FileInfo{
			MimeType: mimeType,
			Size:     size,
		},
	}
	if file != nil {
		// In encrypted rooms, bridgev2 UploadMedia returns url="" and populates file.URL.
		// Don't clobber it by overwriting with the empty return value.
		if file.URL == "" && mxc != "" {
			file.URL = mxc
		}
		content.File = file
	} else {
		content.URL = mxc
	}
	if strings.TrimSpace(content.Body) == "" {
		content.Body = "file"
	}
	return content
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
		if strings.TrimSpace(attachment.ShareURL) == "" {
			lines = append(lines, fmt.Sprintf("Attachment: %s", attachment.Filename))
			continue
		}
		lines = append(lines, fmt.Sprintf("Attachment: %s - %s", attachment.Filename, attachment.ShareURL))
	}
	return strings.Join(lines, "\n")
}

func renderAttachmentHTML(attachments []model.TeamsAttachment) string {
	var b strings.Builder
	b.WriteString("<ul>")
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.ShareURL) == "" {
			b.WriteString("<li>Attachment: ")
			b.WriteString(html.EscapeString(attachment.Filename))
			b.WriteString("</li>")
			continue
		}
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
