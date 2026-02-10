package model

import "testing"

func TestParseGIFsFromHTMLGiphyReadonly(t *testing.T) {
	raw := `<p>&nbsp;</p><readonly title="Sam Darnold Football GIF (GIF Image)" itemtype="http://schema.skype.com/Giphy" contenteditable="false"><img alt="Sam Darnold Football GIF (GIF Image)" src="https://media4.giphy.com/media/test/giphy.gif" itemtype="http://schema.skype.com/Giphy"></readonly><p>&nbsp;</p>`
	gifs, ok := ParseGIFsFromHTML(raw)
	if !ok {
		t.Fatalf("expected gif parse success")
	}
	if len(gifs) != 1 {
		t.Fatalf("expected one gif, got %d", len(gifs))
	}
	if gifs[0].Title != "Sam Darnold Football GIF (GIF Image)" {
		t.Fatalf("unexpected gif title: %q", gifs[0].Title)
	}
	if gifs[0].URL != "https://media4.giphy.com/media/test/giphy.gif" {
		t.Fatalf("unexpected gif url: %q", gifs[0].URL)
	}
}

func TestExtractContentIncludesGIFs(t *testing.T) {
	content := ExtractContent([]byte(`"<readonly itemtype=\"http://schema.skype.com/Giphy\"><img alt=\"A GIF\" src=\"https://media4.giphy.com/media/test/giphy.gif\" itemtype=\"http://schema.skype.com/Giphy\"></readonly>"`))
	if len(content.GIFs) != 1 {
		t.Fatalf("expected one gif, got %#v", content.GIFs)
	}
	if content.GIFs[0].Title != "A GIF" {
		t.Fatalf("unexpected gif title: %q", content.GIFs[0].Title)
	}
}
