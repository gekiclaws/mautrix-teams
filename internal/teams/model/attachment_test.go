package model

import (
	"encoding/json"
	"testing"
)

func TestParseAttachmentsEmptyString(t *testing.T) {
	attachments, ok := ParseAttachments("")
	if ok {
		t.Fatalf("expected no attachments")
	}
	if attachments != nil {
		t.Fatalf("expected nil attachments, got %#v", attachments)
	}
}

func TestParseAttachmentsEmptyArray(t *testing.T) {
	attachments, ok := ParseAttachments("[]")
	if ok {
		t.Fatalf("expected no attachments")
	}
	if attachments != nil {
		t.Fatalf("expected nil attachments, got %#v", attachments)
	}
}

func TestParseAttachmentsSingle(t *testing.T) {
	raw := `[{"fileName":"spec.pdf","fileInfo":{"itemId":"CID!sabc123","shareUrl":"https://example.test/share","fileUrl":"https://example.test/download"},"fileType":"pdf"}]`
	attachments, ok := ParseAttachments(raw)
	if !ok {
		t.Fatalf("expected attachments")
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].Filename != "spec.pdf" {
		t.Fatalf("unexpected filename: %q", attachments[0].Filename)
	}
	if attachments[0].DriveItemID != "CID!sabc123" {
		t.Fatalf("unexpected drive item id: %q", attachments[0].DriveItemID)
	}
	if attachments[0].ShareURL != "https://example.test/share" {
		t.Fatalf("unexpected share url: %q", attachments[0].ShareURL)
	}
	if attachments[0].DownloadURL != "https://example.test/download" {
		t.Fatalf("unexpected download url: %q", attachments[0].DownloadURL)
	}
	if attachments[0].FileType != "pdf" {
		t.Fatalf("unexpected file type: %q", attachments[0].FileType)
	}
}

func TestParseAttachmentsMultipleAndSkipInvalid(t *testing.T) {
	raw := `[
		{"fileName":"first.txt","fileInfo":{"itemId":"CID!sfirst","shareUrl":"https://example.test/first"}},
		{"fileName":"","fileInfo":{"shareUrl":"https://example.test/missing-name"}},
		{"fileName":"missing-share.txt","fileInfo":{"shareUrl":""}},
		{"fileName":"second.txt","fileInfo":{"itemId":"CID!ssecond","shareUrl":"https://example.test/second"}}
	]`
	attachments, ok := ParseAttachments(raw)
	if !ok {
		t.Fatalf("expected attachments")
	}
	if len(attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %d", len(attachments))
	}
	if attachments[0].Filename != "first.txt" || attachments[1].Filename != "second.txt" {
		t.Fatalf("unexpected attachments: %#v", attachments)
	}
}

func TestParseAttachmentsMissingShareURLButHasDriveItemID(t *testing.T) {
	raw := `[{"fileName":"spec.pdf","fileInfo":{"itemId":"CID!sabc123","shareUrl":""}}]`
	attachments, ok := ParseAttachments(raw)
	if !ok || len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got ok=%v attachments=%#v", ok, attachments)
	}
	if attachments[0].DriveItemID != "CID!sabc123" {
		t.Fatalf("unexpected drive item id: %q", attachments[0].DriveItemID)
	}
	if attachments[0].ShareURL != "" {
		t.Fatalf("expected empty share url, got %q", attachments[0].ShareURL)
	}
}

func TestParseAttachmentsMalformed(t *testing.T) {
	attachments, ok := ParseAttachments("{")
	if ok {
		t.Fatalf("expected no attachments")
	}
	if attachments != nil {
		t.Fatalf("expected nil attachments, got %#v", attachments)
	}
}

func TestExtractFilesProperty(t *testing.T) {
	properties := json.RawMessage(`{"files":"[{\"fileName\":\"spec.pdf\",\"fileInfo\":{\"shareUrl\":\"https://example.test/share\"}}]"}`)
	raw := ExtractFilesProperty(properties)
	if raw == "" {
		t.Fatalf("expected files payload")
	}
	attachments, ok := ParseAttachments(raw)
	if !ok || len(attachments) != 1 {
		t.Fatalf("unexpected parse result: ok=%v attachments=%#v", ok, attachments)
	}
}
