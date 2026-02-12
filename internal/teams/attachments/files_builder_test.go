package attachments

import (
	"encoding/json"
	"errors"
	"testing"

	"go.mau.fi/mautrix-teams/internal/teams/graph"
)

func TestBuildTeamsAttachmentFilesPropertySuccess(t *testing.T) {
	uploaded := &graph.UploadedDriveItem{
		DriveItemID:      "01ABCDEF2GHIJKL3MNOPQ4RSTUV567WXYZ",
		ListItemUniqueID: "11111111-2222-3333-4444-555555555555",
		SiteURL:          "https://tenant-my.sharepoint.com/personal/user",
	}
	share := &graph.CreatedShareLink{
		ShareID:  "u!abc123",
		ShareURL: "https://1drv.ms/u/s!abc123",
	}

	raw, err := BuildTeamsAttachmentFilesProperty(uploaded, share, "spec sheet.pdf", "pdf")
	if err != nil {
		t.Fatalf("BuildTeamsAttachmentFilesProperty failed: %v", err)
	}

	var decoded []teamsFileDescriptor
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected one descriptor, got %d", len(decoded))
	}

	got := decoded[0]
	expectedURL := "https://tenant-my.sharepoint.com/personal/user/Documents/Microsoft Teams Chat Files/spec%20sheet.pdf"

	if got.ItemID != uploaded.ListItemUniqueID {
		t.Fatalf("unexpected itemid: %q", got.ItemID)
	}
	if got.FileInfo.ItemID != uploaded.DriveItemID {
		t.Fatalf("unexpected fileInfo.itemId: %q", got.FileInfo.ItemID)
	}
	if got.FileInfo.ShareURL != share.ShareURL {
		t.Fatalf("unexpected fileInfo.shareUrl: %q", got.FileInfo.ShareURL)
	}
	if got.FileChicletState.ServiceName != "p2p" {
		t.Fatalf("unexpected fileChicletState.serviceName: %q", got.FileChicletState.ServiceName)
	}
	if got.TypeSchema != "http://schema.skype.com/File" {
		t.Fatalf("unexpected @type: %q", got.TypeSchema)
	}
	if got.FileInfo.FileURL != expectedURL {
		t.Fatalf("unexpected fileInfo.fileUrl: %q", got.FileInfo.FileURL)
	}
	if got.ObjectURL != expectedURL {
		t.Fatalf("unexpected objectUrl: %q", got.ObjectURL)
	}
	if got.BaseURL != uploaded.SiteURL {
		t.Fatalf("unexpected baseUrl: %q", got.BaseURL)
	}
	if got.ID != uploaded.ListItemUniqueID {
		t.Fatalf("unexpected id: %q", got.ID)
	}
	if got.FileType != "pdf" || got.Type != "pdf" {
		t.Fatalf("unexpected file type/type: %q %q", got.FileType, got.Type)
	}
	if got.Title != "spec sheet.pdf" || got.FileName != "spec sheet.pdf" {
		t.Fatalf("unexpected name/title: %q %q", got.FileName, got.Title)
	}
}

func TestBuildTeamsAttachmentFilesPropertyGuards(t *testing.T) {
	uploaded := &graph.UploadedDriveItem{
		DriveItemID:      "01ABCDEF2GHIJKL3MNOPQ4RSTUV567WXYZ",
		ListItemUniqueID: "11111111-2222-3333-4444-555555555555",
		SiteURL:          "https://tenant-my.sharepoint.com/personal/user",
	}
	share := &graph.CreatedShareLink{
		ShareID:  "u!abc123",
		ShareURL: "https://1drv.ms/u/s!abc123",
	}

	tests := []struct {
		name    string
		up      *graph.UploadedDriveItem
		share   *graph.CreatedShareLink
		file    string
		ext     string
		wantErr error
	}{
		{
			name:    "nil uploaded",
			up:      nil,
			share:   share,
			file:    "file.txt",
			ext:     "txt",
			wantErr: ErrNilUploadedDriveItem,
		},
		{
			name:    "nil share",
			up:      uploaded,
			share:   nil,
			file:    "file.txt",
			ext:     "txt",
			wantErr: ErrNilCreatedShareLink,
		},
		{
			name:    "empty filename",
			up:      uploaded,
			share:   share,
			file:    "   ",
			ext:     "txt",
			wantErr: ErrEmptyAttachmentFilename,
		},
		{
			name: "missing list item id",
			up: &graph.UploadedDriveItem{
				DriveItemID:      "01ABCDEF2GHIJKL3MNOPQ4RSTUV567WXYZ",
				ListItemUniqueID: "",
				SiteURL:          "https://tenant-my.sharepoint.com/personal/user",
			},
			share:   share,
			file:    "file.txt",
			ext:     "txt",
			wantErr: ErrEmptyAttachmentListItemID,
		},
		{
			name: "missing drive item id",
			up: &graph.UploadedDriveItem{
				DriveItemID:      "",
				ListItemUniqueID: "11111111-2222-3333-4444-555555555555",
				SiteURL:          "https://tenant-my.sharepoint.com/personal/user",
			},
			share:   share,
			file:    "file.txt",
			ext:     "txt",
			wantErr: ErrEmptyAttachmentDriveItem,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildTeamsAttachmentFilesProperty(tt.up, tt.share, tt.file, tt.ext)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}
