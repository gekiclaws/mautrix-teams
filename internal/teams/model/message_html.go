package model

import (
	"html"
	"net/url"
	"regexp"
	"strings"

	nethtml "golang.org/x/net/html"
)

var htmlTagPattern = regexp.MustCompile(`(?i)<\s*/?\s*[a-z][^>]*>`)
var manyNewlinesPattern = regexp.MustCompile(`\n{3,}`)

var allowedFormattedTags = map[string]bool{
	"a":          true,
	"b":          true,
	"blockquote": true,
	"br":         true,
	"code":       true,
	"em":         true,
	"i":          true,
	"li":         true,
	"ol":         true,
	"p":          true,
	"pre":        true,
	"s":          true,
	"strong":     true,
	"u":          true,
	"ul":         true,
}

var blockTags = map[string]bool{
	"blockquote": true,
	"div":        true,
	"h1":         true,
	"h2":         true,
	"h3":         true,
	"h4":         true,
	"h5":         true,
	"h6":         true,
	"li":         true,
	"ol":         true,
	"p":          true,
	"pre":        true,
	"section":    true,
	"ul":         true,
}

func NormalizeMessageBody(raw string) MessageContent {
	normalized := normalizePlainText(html.UnescapeString(raw))
	if normalized == "" {
		return MessageContent{}
	}
	if !looksLikeHTML(raw) {
		return MessageContent{Body: normalized}
	}

	body, formatted, ok := normalizeHTMLFragment(raw)
	if !ok {
		return MessageContent{Body: normalized}
	}
	if body == "" {
		return MessageContent{}
	}
	if formatted == "" {
		return MessageContent{Body: body}
	}
	return MessageContent{
		Body:          body,
		FormattedBody: formatted,
	}
}

func looksLikeHTML(value string) bool {
	return htmlTagPattern.MatchString(value)
}

func normalizeHTMLFragment(value string) (body string, formatted string, ok bool) {
	doc, err := nethtml.Parse(strings.NewReader("<div>" + value + "</div>"))
	if err != nil {
		return "", "", false
	}
	wrapper := findWrapperDiv(doc)
	if wrapper == nil {
		return "", "", false
	}

	var nodes []*nethtml.Node
	for child := wrapper.FirstChild; child != nil; child = child.NextSibling {
		nodes = append(nodes, child)
	}
	if len(nodes) == 0 {
		return "", "", true
	}

	var plainBuilder strings.Builder
	var htmlBuilder strings.Builder
	var renderedTag bool
	for _, node := range nodes {
		renderPlainNode(&plainBuilder, node)
		if renderFormattedNode(&htmlBuilder, node) {
			renderedTag = true
		}
	}

	plain := normalizePlainText(plainBuilder.String())
	sanitized := normalizeSanitizedHTML(htmlBuilder.String())
	if !renderedTag {
		sanitized = ""
	}
	return plain, sanitized, true
}

func renderPlainNode(builder *strings.Builder, node *nethtml.Node) {
	switch node.Type {
	case nethtml.TextNode:
		builder.WriteString(node.Data)
	case nethtml.ElementNode:
		tag := strings.ToLower(node.Data)
		if isUnsafeTag(tag) {
			return
		}
		if tag == "br" {
			appendPlainNewline(builder)
			return
		}
		if blockTags[tag] {
			appendPlainNewline(builder)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderPlainNode(builder, child)
		}
		if blockTags[tag] {
			appendPlainNewline(builder)
		}
	default:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderPlainNode(builder, child)
		}
	}
}

func renderFormattedNode(builder *strings.Builder, node *nethtml.Node) bool {
	switch node.Type {
	case nethtml.TextNode:
		builder.WriteString(html.EscapeString(node.Data))
		return false
	case nethtml.ElementNode:
		tag := strings.ToLower(node.Data)
		if isUnsafeTag(tag) {
			return false
		}
		if tag == "br" {
			builder.WriteString("<br>")
			return true
		}
		if !allowedFormattedTags[tag] {
			var rendered bool
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if renderFormattedNode(builder, child) {
					rendered = true
				}
			}
			return rendered
		}

		builder.WriteByte('<')
		builder.WriteString(tag)
		if tag == "a" {
			if href, ok := extractSafeHref(node); ok {
				builder.WriteString(` href="`)
				builder.WriteString(html.EscapeString(href))
				builder.WriteByte('"')
			}
		}
		builder.WriteByte('>')

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderFormattedNode(builder, child)
		}

		builder.WriteString("</")
		builder.WriteString(tag)
		builder.WriteByte('>')
		return true
	default:
		var rendered bool
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if renderFormattedNode(builder, child) {
				rendered = true
			}
		}
		return rendered
	}
}

func findWrapperDiv(node *nethtml.Node) *nethtml.Node {
	if node == nil {
		return nil
	}
	if node.Type == nethtml.ElementNode && strings.EqualFold(node.Data, "div") {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findWrapperDiv(child); found != nil {
			return found
		}
	}
	return nil
}

func extractSafeHref(node *nethtml.Node) (string, bool) {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "href") {
			return sanitizeHref(attr.Val)
		}
	}
	return "", false
}

func sanitizeHref(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "mailto", "matrix":
		return value, true
	default:
		return "", false
	}
}

func isUnsafeTag(tag string) bool {
	switch tag {
	case "script", "style", "iframe", "object", "embed", "meta", "link", "head":
		return true
	default:
		return false
	}
}

func appendPlainNewline(builder *strings.Builder) {
	if builder.Len() == 0 {
		return
	}
	if strings.HasSuffix(builder.String(), "\n") {
		return
	}
	builder.WriteByte('\n')
}

func normalizePlainText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = manyNewlinesPattern.ReplaceAllString(value, "\n\n")
	value = strings.TrimSpace(value)
	return value
}

func normalizeSanitizedHTML(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return value
}
