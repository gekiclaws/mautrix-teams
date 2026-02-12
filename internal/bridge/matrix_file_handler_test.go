package bridge

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestHandleOutboundMatrixFileSuccess(t *testing.T) {
	var downloadCalls int
	var sendCalls int

	wantBytes := []byte("hello world")
	var gotThreadID, gotFileName, gotCaption string
	var gotLen int

	download := func(ctx context.Context, mxcURL string) ([]byte, error) {
		_ = ctx
		downloadCalls++
		if mxcURL != "mxc://example.org/abc123" {
			t.Fatalf("unexpected mxc url: %q", mxcURL)
		}
		return wantBytes, nil
	}
	send := func(ctx context.Context, threadID, filename string, content []byte, caption string) error {
		_ = ctx
		sendCalls++
		gotThreadID = threadID
		gotFileName = filename
		gotCaption = caption
		gotLen = len(content)
		return nil
	}

	content := &event.MessageEventContent{
		MsgType: event.MsgFile,
		Body:    "spec.pdf",
		URL:     id.ContentURIString("mxc://example.org/abc123"),
	}

	log := zerolog.Nop()
	err := HandleOutboundMatrixFile(context.Background(), id.RoomID("!room:example.org"), "@19:abc@thread.v2", content, download, send, &log)
	if err != nil {
		t.Fatalf("HandleOutboundMatrixFile failed: %v", err)
	}
	if downloadCalls != 1 {
		t.Fatalf("expected download called once, got %d", downloadCalls)
	}
	if sendCalls != 1 {
		t.Fatalf("expected send called once, got %d", sendCalls)
	}
	if gotThreadID != "@19:abc@thread.v2" {
		t.Fatalf("unexpected thread id: %q", gotThreadID)
	}
	if gotFileName != "spec.pdf" {
		t.Fatalf("unexpected filename: %q", gotFileName)
	}
	if gotCaption != "" {
		t.Fatalf("unexpected caption: %q", gotCaption)
	}
	if gotLen != len(wantBytes) {
		t.Fatalf("unexpected content length: %d", gotLen)
	}
}

func TestCaptionExtraction(t *testing.T) {
	info1, err := ExtractMatrixFileInfo(&event.MessageEventContent{
		MsgType: event.MsgFile,
		Body:    "spec.pdf",
		URL:     id.ContentURIString("mxc://example.org/abc123"),
	})
	if err != nil {
		t.Fatalf("ExtractMatrixFileInfo failed: %v", err)
	}
	if info1.Caption != "" {
		t.Fatalf("expected empty caption when body==filename, got %q", info1.Caption)
	}
	if info1.FileName != "spec.pdf" {
		t.Fatalf("unexpected filename: %q", info1.FileName)
	}

	info2, err := ExtractMatrixFileInfo(&event.MessageEventContent{
		MsgType:       event.MsgFile,
		Body:          "caption text",
		FileName:      "spec.pdf",
		URL:           id.ContentURIString("mxc://example.org/abc123"),
		FormattedBody: "<b>caption text</b>",
		Format:        event.FormatHTML,
	})
	if err != nil {
		t.Fatalf("ExtractMatrixFileInfo failed: %v", err)
	}
	if info2.Caption != "caption text" {
		t.Fatalf("expected caption preserved, got %q", info2.Caption)
	}
	if info2.FileName != "spec.pdf" {
		t.Fatalf("unexpected filename: %q", info2.FileName)
	}
}

func TestHandleOutboundMatrixFileMissingMXCURL(t *testing.T) {
	download := func(ctx context.Context, mxcURL string) ([]byte, error) {
		t.Fatalf("download should not be called")
		return nil, nil
	}
	send := func(ctx context.Context, threadID, filename string, content []byte, caption string) error {
		t.Fatalf("send should not be called")
		return nil
	}

	content := &event.MessageEventContent{
		MsgType: event.MsgFile,
		Body:    "spec.pdf",
	}

	log := zerolog.Nop()
	err := HandleOutboundMatrixFile(context.Background(), id.RoomID("!room:example.org"), "@19:abc@thread.v2", content, download, send, &log)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleOutboundMatrixFileDownloadFailure(t *testing.T) {
	var sendCalls int
	download := func(ctx context.Context, mxcURL string) ([]byte, error) {
		_ = ctx
		_ = mxcURL
		return nil, errors.New("download failed")
	}
	send := func(ctx context.Context, threadID, filename string, content []byte, caption string) error {
		sendCalls++
		return nil
	}

	content := &event.MessageEventContent{
		MsgType: event.MsgFile,
		Body:    "spec.pdf",
		URL:     id.ContentURIString("mxc://example.org/abc123"),
	}

	log := zerolog.Nop()
	err := HandleOutboundMatrixFile(context.Background(), id.RoomID("!room:example.org"), "@19:abc@thread.v2", content, download, send, &log)
	if err == nil {
		t.Fatalf("expected error")
	}
	if sendCalls != 0 {
		t.Fatalf("expected send not called, got %d", sendCalls)
	}
}

func TestHandleOutboundMatrixFileTooLargeRejectedBeforeSend(t *testing.T) {
	var sendCalls int
	download := func(ctx context.Context, mxcURL string) ([]byte, error) {
		_ = ctx
		_ = mxcURL
		return make([]byte, MaxAttachmentBytesV0+1), nil
	}
	send := func(ctx context.Context, threadID, filename string, content []byte, caption string) error {
		sendCalls++
		return nil
	}

	content := &event.MessageEventContent{
		MsgType: event.MsgFile,
		Body:    "spec.pdf",
		URL:     id.ContentURIString("mxc://example.org/abc123"),
	}

	log := zerolog.Nop()
	err := HandleOutboundMatrixFile(context.Background(), id.RoomID("!room:example.org"), "@19:abc@thread.v2", content, download, send, &log)
	if err == nil {
		t.Fatalf("expected error")
	}
	if sendCalls != 0 {
		t.Fatalf("expected send not called, got %d", sendCalls)
	}
}
