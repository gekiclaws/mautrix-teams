package model

import (
	"net/url"
	"strings"

	nethtml "golang.org/x/net/html"
)

type TeamsGIF struct {
	Title string
	URL   string
}

func ParseGIFsFromHTML(raw string) ([]TeamsGIF, bool) {
	if !looksLikeHTML(raw) {
		return nil, false
	}
	doc, err := nethtml.Parse(strings.NewReader("<div>" + raw + "</div>"))
	if err != nil {
		return nil, false
	}

	gifs := make([]TeamsGIF, 0, 1)
	seenURLs := make(map[string]struct{})
	collectGIFs(doc, &gifs, seenURLs)
	if len(gifs) == 0 {
		return nil, false
	}
	return gifs, true
}

func collectGIFs(node *nethtml.Node, gifs *[]TeamsGIF, seenURLs map[string]struct{}) {
	if node == nil {
		return
	}
	if node.Type == nethtml.ElementNode {
		tag := strings.ToLower(node.Data)
		switch tag {
		case "readonly":
			if hasGiphyItemType(node) {
				title := firstNonEmpty(
					getAttr(node, "title"),
					getAttr(node, "aria-label"),
				)
				appendGIFsFromImages(node, title, gifs, seenURLs)
			}
		case "img":
			if hasGiphyItemType(node) {
				appendSingleGIF(node, "", gifs, seenURLs)
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectGIFs(child, gifs, seenURLs)
	}
}

func appendGIFsFromImages(node *nethtml.Node, fallbackTitle string, gifs *[]TeamsGIF, seenURLs map[string]struct{}) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == nethtml.ElementNode && strings.EqualFold(child.Data, "img") {
			appendSingleGIF(child, fallbackTitle, gifs, seenURLs)
		}
		appendGIFsFromImages(child, fallbackTitle, gifs, seenURLs)
	}
}

func appendSingleGIF(node *nethtml.Node, fallbackTitle string, gifs *[]TeamsGIF, seenURLs map[string]struct{}) {
	src := strings.TrimSpace(getAttr(node, "src"))
	if !isSafeHTTPURL(src) {
		return
	}
	if _, exists := seenURLs[src]; exists {
		return
	}
	title := firstNonEmpty(
		strings.TrimSpace(getAttr(node, "alt")),
		strings.TrimSpace(fallbackTitle),
		"GIF",
	)
	*gifs = append(*gifs, TeamsGIF{
		Title: title,
		URL:   src,
	})
	seenURLs[src] = struct{}{}
}

func hasGiphyItemType(node *nethtml.Node) bool {
	itemType := strings.ToLower(strings.TrimSpace(getAttr(node, "itemtype")))
	return strings.Contains(itemType, "schema.skype.com/giphy")
}

func getAttr(node *nethtml.Node, key string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func isSafeHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
