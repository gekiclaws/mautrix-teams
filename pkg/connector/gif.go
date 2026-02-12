package connector

import (
	"net/url"
	"strings"

	"maunium.net/go/mautrix/event"
)

func extractOutboundGIF(content *event.MessageEventContent) (title string, gifURL string, ok bool) {
	if content == nil {
		return "", "", false
	}
	if !looksLikeGIFMessage(content) {
		return "", "", false
	}

	title = strings.TrimSpace(content.FileName)
	if title == "" {
		title = strings.TrimSpace(content.Body)
	}
	if title == "" {
		title = "GIF"
	}

	rawURL := strings.TrimSpace(string(content.URL))
	if parsedGIFURL, ok := parseDirectGIFURL(rawURL); ok {
		return title, parsedGIFURL, true
	}
	if parsedGIFURL, ok := parseDirectGIFURL(strings.TrimSpace(content.Body)); ok {
		return title, parsedGIFURL, true
	}
	return "", "", false
}

func looksLikeGIFMessage(content *event.MessageEventContent) bool {
	if content == nil {
		return false
	}
	if content.Info != nil && strings.EqualFold(strings.TrimSpace(content.Info.MimeType), "image/gif") {
		return true
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(content.FileName)), ".gif") {
		return true
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(content.Body)), ".gif") {
		return true
	}
	if _, ok := parseDirectGIFURL(strings.TrimSpace(string(content.URL))); ok {
		return true
	}
	return false
}

func parseDirectGIFURL(value string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if strings.HasSuffix(strings.ToLower(parsed.Path), ".gif") {
			return parsed.String(), true
		}
		return "", false
	default:
		return "", false
	}
}
