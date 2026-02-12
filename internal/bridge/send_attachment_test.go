package bridge

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/mautrix-teams/internal/teams/attachments"
	"go.mau.fi/mautrix-teams/internal/teams/graph"
)

type mockGraph struct {
	calls []string

	uploaded  *graph.UploadedDriveItem
	uploadErr error

	share   *graph.CreatedShareLink
	linkErr error
}

func (m *mockGraph) UploadTeamsChatFile(ctx context.Context, filename string, content []byte) (*graph.UploadedDriveItem, error) {
	_ = ctx
	m.calls = append(m.calls, "upload")
	if m.uploadErr != nil {
		return nil, m.uploadErr
	}
	return m.uploaded, nil
}

func (m *mockGraph) CreateShareLink(ctx context.Context, listItemUniqueID string) (*graph.CreatedShareLink, error) {
	_ = ctx
	_ = listItemUniqueID
	m.calls = append(m.calls, "create_link")
	if m.linkErr != nil {
		return nil, m.linkErr
	}
	return m.share, nil
}

type mockTeams struct {
	calls []string

	gotThreadID        string
	gotHTML            string
	gotFiles           string
	gotFrom            string
	gotClientMessageID string

	err error
}

func (m *mockTeams) SendAttachmentMessageWithID(ctx context.Context, threadID string, htmlContent string, filesProperty string, fromUserID string, clientMessageID string) (int, error) {
	_ = ctx
	m.calls = append(m.calls, "teams_send")
	m.gotThreadID = threadID
	m.gotHTML = htmlContent
	m.gotFiles = filesProperty
	m.gotFrom = fromUserID
	m.gotClientMessageID = clientMessageID
	if m.err != nil {
		return 0, m.err
	}
	return 200, nil
}

func TestSendAttachmentMessageSuccess(t *testing.T) {
	up := &graph.UploadedDriveItem{
		DriveItemID:      "CID!sabc123",
		ListItemUniqueID: "11111111-2222-3333-4444-555555555555",
		SiteURL:          "https://tenant-my.sharepoint.com/personal/user",
		FileName:         "spec.pdf",
		Size:             123,
	}
	share := &graph.CreatedShareLink{
		ShareID:  "u!abc123",
		ShareURL: "https://1drv.ms/u/s!abc123",
	}
	mg := &mockGraph{uploaded: up, share: share}
	mt := &mockTeams{}

	orch := &AttachmentOrchestrator{
		Graph:               mg,
		Teams:               mt,
		FromUserID:          "8:live:me",
		GenerateMessageID:   func() string { return "999" },
		MaxBytes:            MaxAttachmentBytesV0,
		FormatCaptionToHTML: func(c string) string { return "<p>" + c + "</p>" },
	}

	_, err := orch.SendAttachmentMessage(context.Background(), "@19:abc@thread.v2", "spec.pdf", []byte("hello"), "caption")
	if err != nil {
		t.Fatalf("SendAttachmentMessage failed: %v", err)
	}

	wantCalls := []string{"upload", "create_link"}
	if len(mg.calls) != len(wantCalls) {
		t.Fatalf("unexpected graph calls: %#v", mg.calls)
	}
	for i, c := range wantCalls {
		if mg.calls[i] != c {
			t.Fatalf("unexpected graph call order: %#v", mg.calls)
		}
	}
	if len(mt.calls) != 1 || mt.calls[0] != "teams_send" {
		t.Fatalf("unexpected teams calls: %#v", mt.calls)
	}

	if mt.gotThreadID != "@19:abc@thread.v2" {
		t.Fatalf("unexpected thread id: %q", mt.gotThreadID)
	}
	if mt.gotFrom != "8:live:me" {
		t.Fatalf("unexpected from user: %q", mt.gotFrom)
	}
	if mt.gotClientMessageID != "999" {
		t.Fatalf("unexpected clientmessageid: %q", mt.gotClientMessageID)
	}
	if mt.gotHTML != "<p>caption</p>" {
		t.Fatalf("unexpected html caption: %q", mt.gotHTML)
	}

	wantFiles, err := attachments.BuildTeamsAttachmentFilesProperty(up, share, "spec.pdf", ".pdf")
	if err != nil {
		t.Fatalf("BuildTeamsAttachmentFilesProperty failed: %v", err)
	}
	if mt.gotFiles != wantFiles {
		t.Fatalf("unexpected files property:\nwant: %s\ngot:  %s", wantFiles, mt.gotFiles)
	}
}

func TestSendAttachmentMessageUploadFails(t *testing.T) {
	mg := &mockGraph{uploadErr: errors.New("upload failed")}
	mt := &mockTeams{}
	orch := &AttachmentOrchestrator{
		Graph:             mg,
		Teams:             mt,
		FromUserID:        "8:live:me",
		GenerateMessageID: func() string { return "1" },
	}

	_, err := orch.SendAttachmentMessage(context.Background(), "@19:abc@thread.v2", "spec.pdf", []byte("hello"), "")
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(mg.calls) != 1 || mg.calls[0] != "upload" {
		t.Fatalf("unexpected graph calls: %#v", mg.calls)
	}
	if len(mt.calls) != 0 {
		t.Fatalf("expected no teams calls, got %#v", mt.calls)
	}
}

func TestSendAttachmentMessageCreateLinkFails(t *testing.T) {
	up := &graph.UploadedDriveItem{
		DriveItemID:      "CID!sabc123",
		ListItemUniqueID: "11111111-2222-3333-4444-555555555555",
		SiteURL:          "https://tenant-my.sharepoint.com/personal/user",
	}
	mg := &mockGraph{uploaded: up, linkErr: errors.New("link failed")}
	mt := &mockTeams{}
	orch := &AttachmentOrchestrator{
		Graph:             mg,
		Teams:             mt,
		FromUserID:        "8:live:me",
		GenerateMessageID: func() string { return "1" },
	}

	_, err := orch.SendAttachmentMessage(context.Background(), "@19:abc@thread.v2", "spec.pdf", []byte("hello"), "")
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(mg.calls) != 2 || mg.calls[0] != "upload" || mg.calls[1] != "create_link" {
		t.Fatalf("unexpected graph calls: %#v", mg.calls)
	}
	if len(mt.calls) != 0 {
		t.Fatalf("expected no teams calls, got %#v", mt.calls)
	}
}

func TestSendAttachmentMessageTeamsSendFails(t *testing.T) {
	up := &graph.UploadedDriveItem{
		DriveItemID:      "CID!sabc123",
		ListItemUniqueID: "11111111-2222-3333-4444-555555555555",
		SiteURL:          "https://tenant-my.sharepoint.com/personal/user",
		FileName:         "spec.pdf",
	}
	share := &graph.CreatedShareLink{
		ShareID:  "u!abc123",
		ShareURL: "https://1drv.ms/u/s!abc123",
	}
	mg := &mockGraph{uploaded: up, share: share}
	mt := &mockTeams{err: errors.New("teams send failed")}
	orch := &AttachmentOrchestrator{
		Graph:             mg,
		Teams:             mt,
		FromUserID:        "8:live:me",
		GenerateMessageID: func() string { return "1" },
	}

	_, err := orch.SendAttachmentMessage(context.Background(), "@19:abc@thread.v2", "spec.pdf", []byte("hello"), "")
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(mg.calls) != 2 {
		t.Fatalf("unexpected graph calls: %#v", mg.calls)
	}
	if len(mt.calls) != 1 {
		t.Fatalf("expected teams send called once, got %#v", mt.calls)
	}
}

func TestSendAttachmentMessageSizeGuard(t *testing.T) {
	mg := &mockGraph{}
	mt := &mockTeams{}
	orch := &AttachmentOrchestrator{
		Graph:             mg,
		Teams:             mt,
		FromUserID:        "8:live:me",
		GenerateMessageID: func() string { return "1" },
		MaxBytes:          MaxAttachmentBytesV0,
	}

	tooBig := make([]byte, MaxAttachmentBytesV0+1)
	_, err := orch.SendAttachmentMessage(context.Background(), "@19:abc@thread.v2", "spec.pdf", tooBig, "")
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(mg.calls) != 0 {
		t.Fatalf("expected no graph calls, got %#v", mg.calls)
	}
	if len(mt.calls) != 0 {
		t.Fatalf("expected no teams calls, got %#v", mt.calls)
	}
}
