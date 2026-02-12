package model

import (
	"encoding/json"
	"strings"
)

type TeamsAttachment struct {
	Filename    string
	DriveItemID string
	ShareURL    string
	DownloadURL string
	FileType    string
}

func ExtractFilesProperty(properties json.RawMessage) string {
	if len(properties) == 0 {
		return ""
	}
	var payload struct {
		Files json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(properties, &payload); err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(payload.Files))
	if raw == "" || raw == "null" {
		return ""
	}
	var encoded string
	if err := json.Unmarshal(payload.Files, &encoded); err == nil {
		return strings.TrimSpace(encoded)
	}
	if strings.HasPrefix(raw, "[") {
		return raw
	}
	return ""
}

func ParseAttachments(raw string) ([]TeamsAttachment, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "[]" {
		return nil, false
	}
	if strings.HasPrefix(trimmed, "\"") {
		var decoded string
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			trimmed = strings.TrimSpace(decoded)
		}
	}
	if trimmed == "" || trimmed == "[]" {
		return nil, false
	}

	var payload []struct {
		FileName string `json:"fileName"`
		FileInfo struct {
			ItemID   string `json:"itemId"`
			ShareURL string `json:"shareUrl"`
			FileURL  string `json:"fileUrl"`
		} `json:"fileInfo"`
		FileType string `json:"fileType"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, false
	}
	if len(payload) == 0 {
		return nil, false
	}

	attachments := make([]TeamsAttachment, 0, len(payload))
	for _, entry := range payload {
		filename := strings.TrimSpace(entry.FileName)
		driveItemID := strings.TrimSpace(entry.FileInfo.ItemID)
		shareURL := strings.TrimSpace(entry.FileInfo.ShareURL)
		// Keep attachments for which we have either a share URL (legacy rendering) or a drive item ID
		// (inbound media re-upload).
		if filename == "" || (shareURL == "" && driveItemID == "") {
			continue
		}
		attachments = append(attachments, TeamsAttachment{
			Filename:    filename,
			DriveItemID: driveItemID,
			ShareURL:    shareURL,
			DownloadURL: strings.TrimSpace(entry.FileInfo.FileURL),
			FileType:    strings.TrimSpace(entry.FileType),
		})
	}
	if len(attachments) == 0 {
		return nil, false
	}
	return attachments, true
}
