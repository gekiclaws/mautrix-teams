package main

import (
	"testing"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestParseDirectGIFURL(t *testing.T) {
	cases := []struct {
		name  string
		input string
		ok    bool
	}{
		{name: "https gif", input: "https://media4.giphy.com/media/test/giphy.gif", ok: true},
		{name: "https gif with query", input: "https://media4.giphy.com/media/test/giphy.gif?cid=abc", ok: true},
		{name: "https not gif", input: "https://media4.giphy.com/media/test/giphy.webp", ok: false},
		{name: "mxc url", input: "mxc://example.org/abc123", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := parseDirectGIFURL(tc.input)
			if ok != tc.ok {
				t.Fatalf("unexpected result for %q: got %v want %v", tc.input, ok, tc.ok)
			}
		})
	}
}

func TestExtractOutboundGIFRequiresDirectGIFURL(t *testing.T) {
	portal := &TeamsConsumerPortal{}
	content := &event.MessageEventContent{
		MsgType: event.MsgImage,
		Body:    "image.gif",
		URL:     id.ContentURIString("mxc://example.org/abc123"),
		Info: &event.FileInfo{
			MimeType: "image/gif",
		},
	}
	_, _, ok := portal.extractOutboundGIF(content)
	if ok {
		t.Fatalf("expected mxc url to be rejected for outbound gif")
	}

	content.URL = id.ContentURIString("https://media4.giphy.com/media/test/giphy.gif")
	_, gifURL, ok := portal.extractOutboundGIF(content)
	if !ok {
		t.Fatalf("expected direct gif url to be accepted")
	}
	if gifURL != "https://media4.giphy.com/media/test/giphy.gif" {
		t.Fatalf("unexpected gif url: %q", gifURL)
	}
}
