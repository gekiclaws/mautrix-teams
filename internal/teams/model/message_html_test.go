package model

import "testing"

func TestNormalizeMessageBodyHTML(t *testing.T) {
	content := NormalizeMessageBody("<p>hi</p><p>there<br>friend</p>")
	if content.Body != "hi\nthere\nfriend" {
		t.Fatalf("unexpected plaintext body: %q", content.Body)
	}
	if content.FormattedBody != "<p>hi</p><p>there<br>friend</p>" {
		t.Fatalf("unexpected formatted body: %q", content.FormattedBody)
	}
}

func TestNormalizeMessageBodySanitizesUnsafeHTML(t *testing.T) {
	content := NormalizeMessageBody(`<p>Hello <a href="javascript:alert('x')" onclick="evil()">world</a></p><script>alert(1)</script>`)
	if content.Body != "Hello world" {
		t.Fatalf("unexpected plaintext body: %q", content.Body)
	}
	if content.FormattedBody != "<p>Hello <a>world</a></p>" {
		t.Fatalf("unexpected formatted body: %q", content.FormattedBody)
	}
}

func TestNormalizeMessageBodyPlainText(t *testing.T) {
	content := NormalizeMessageBody("hey how&apos;ve u been?")
	if content.Body != "hey how've u been?" {
		t.Fatalf("unexpected plaintext body: %q", content.Body)
	}
	if content.FormattedBody != "" {
		t.Fatalf("expected empty formatted body, got %q", content.FormattedBody)
	}
}

func TestNormalizeMessageBodyUnsupportedTagFallsBackToPlain(t *testing.T) {
	content := NormalizeMessageBody("<custom>hello</custom>")
	if content.Body != "hello" {
		t.Fatalf("unexpected plaintext body: %q", content.Body)
	}
	if content.FormattedBody != "" {
		t.Fatalf("expected empty formatted body, got %q", content.FormattedBody)
	}
}
