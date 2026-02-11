package connector

import (
	"context"
	"strings"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

func convertTeamsMessage(_ context.Context, _ *bridgev2.Portal, _ bridgev2.MatrixAPI, msg model.RemoteMessage) (*bridgev2.ConvertedMessage, error) {
	body := strings.TrimSpace(msg.Body)
	if body == "" && strings.TrimSpace(msg.FormattedBody) == "" {
		body = " "
	}
	content := &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    body,
	}
	if formatted := strings.TrimSpace(msg.FormattedBody); formatted != "" {
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
	return &bridgev2.ConvertedMessage{
		Parts: []*bridgev2.ConvertedMessagePart{{
			Type:    event.EventMessage,
			Content: content,
		}},
	}, nil
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
