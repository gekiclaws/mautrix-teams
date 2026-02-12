package attachments

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"go.mau.fi/mautrix-teams/internal/teams/graph"
)

var (
	ErrNilUploadedDriveItem      = errors.New("uploaded drive item is nil")
	ErrNilCreatedShareLink       = errors.New("created share link is nil")
	ErrEmptyAttachmentFilename   = errors.New("filename is empty")
	ErrEmptyAttachmentListItemID = errors.New("uploaded list item unique id is empty")
	ErrEmptyAttachmentDriveItem  = errors.New("uploaded drive item id is empty")
)

type teamsFileDescriptor struct {
	ItemID   string `json:"itemid"`
	FileName string `json:"fileName"`
	FileType string `json:"fileType"`
	FileInfo struct {
		ItemID            string `json:"itemId"`
		FileURL           string `json:"fileUrl"`
		SiteURL           string `json:"siteUrl"`
		ServerRelativeURL string `json:"serverRelativeUrl"`
		ShareURL          string `json:"shareUrl"`
		ShareID           string `json:"shareId"`
	} `json:"fileInfo"`
	FileChicletState struct {
		ServiceName string `json:"serviceName"`
		State       string `json:"state"`
	} `json:"fileChicletState"`
	TypeSchema string `json:"@type"`
	Version    int    `json:"version"`
	ID         string `json:"id"`
	BaseURL    string `json:"baseUrl"`
	ObjectURL  string `json:"objectUrl"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	State      string `json:"state"`
}

func BuildTeamsAttachmentFilesProperty(uploaded *graph.UploadedDriveItem, share *graph.CreatedShareLink, filename string, fileExt string) (string, error) {
	if uploaded == nil {
		return "", ErrNilUploadedDriveItem
	}
	if share == nil {
		return "", ErrNilCreatedShareLink
	}

	trimmedFilename := strings.TrimSpace(filename)
	if trimmedFilename == "" {
		return "", ErrEmptyAttachmentFilename
	}

	listItemID := strings.TrimSpace(uploaded.ListItemUniqueID)
	if listItemID == "" {
		return "", ErrEmptyAttachmentListItemID
	}

	driveItemID := strings.TrimSpace(uploaded.DriveItemID)
	if driveItemID == "" {
		return "", ErrEmptyAttachmentDriveItem
	}

	siteURL := strings.TrimSpace(uploaded.SiteURL)
	escapedFilename := url.PathEscape(trimmedFilename)
	fileURL := strings.TrimRight(siteURL, "/") + "/Documents/Microsoft Teams Chat Files/" + escapedFilename
	trimmedExt := strings.TrimPrefix(strings.TrimSpace(fileExt), ".")

	descriptor := teamsFileDescriptor{
		ItemID:     listItemID,
		FileName:   trimmedFilename,
		FileType:   trimmedExt,
		TypeSchema: "http://schema.skype.com/File",
		Version:    2,
		ID:         listItemID,
		BaseURL:    siteURL,
		ObjectURL:  fileURL,
		Type:       trimmedExt,
		Title:      trimmedFilename,
		State:      "active",
	}
	descriptor.FileInfo.ItemID = driveItemID
	descriptor.FileInfo.FileURL = fileURL
	descriptor.FileInfo.SiteURL = siteURL
	descriptor.FileInfo.ServerRelativeURL = ""
	descriptor.FileInfo.ShareURL = strings.TrimSpace(share.ShareURL)
	descriptor.FileInfo.ShareID = strings.TrimSpace(share.ShareID)
	descriptor.FileChicletState.ServiceName = "p2p"
	descriptor.FileChicletState.State = "active"

	payload, err := json.Marshal([]teamsFileDescriptor{descriptor})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
