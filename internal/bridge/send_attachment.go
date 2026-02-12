package bridge

import (
	"context"
	"errors"
	"html"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-teams/internal/teams/attachments"
	"go.mau.fi/mautrix-teams/internal/teams/client"
	"go.mau.fi/mautrix-teams/internal/teams/graph"
)

const MaxAttachmentBytesV0 = 10 * 1024 * 1024

type GraphAPI interface {
	UploadTeamsChatFile(ctx context.Context, filename string, content []byte) (*graph.UploadedDriveItem, error)
	CreateShareLink(ctx context.Context, listItemUniqueID string) (*graph.CreatedShareLink, error)
}

type TeamsSendAPI interface {
	SendAttachmentMessageWithID(ctx context.Context, threadID string, htmlContent string, filesProperty string, fromUserID string, clientMessageID string) (int, error)
}

type AttachmentOrchestrator struct {
	Graph GraphAPI
	Teams TeamsSendAPI

	Log *zerolog.Logger

	FromUserID string

	MaxBytes            int
	GenerateMessageID   func() string
	FormatCaptionToHTML func(caption string) string
}

func (o *AttachmentOrchestrator) SendAttachmentMessage(ctx context.Context, threadID string, filename string, content []byte, caption string) (string, error) {
	if strings.TrimSpace(threadID) == "" {
		return "", errors.New("missing thread id")
	}
	if strings.TrimSpace(filename) == "" {
		return "", errors.New("missing filename")
	}
	if len(content) == 0 {
		return "", errors.New("missing content")
	}
	maxBytes := o.MaxBytes
	if maxBytes <= 0 {
		maxBytes = MaxAttachmentBytesV0
	}
	if len(content) > maxBytes {
		return "", errors.New("attachment exceeds max size")
	}
	if o.Graph == nil {
		return "", errors.New("missing graph client")
	}
	if o.Teams == nil {
		return "", errors.New("missing teams client")
	}
	fromUserID := strings.TrimSpace(o.FromUserID)
	if fromUserID == "" {
		return "", errors.New("missing from user id")
	}

	genID := o.GenerateMessageID
	if genID == nil {
		genID = client.GenerateClientMessageID
	}
	formatCaption := o.FormatCaptionToHTML
	if formatCaption == nil {
		formatCaption = formatCaptionHTML
	}

	clientMessageID := genID()
	log := zerolog.Nop()
	if o.Log != nil {
		log = *o.Log
	}
	log = log.With().
		Str("thread_id", strings.TrimSpace(threadID)).
		Str("clientmessageid", clientMessageID).
		Str("filename", strings.TrimSpace(filename)).
		Int("upload_size", len(content)).
		Logger()

	uploaded, err := o.Graph.UploadTeamsChatFile(ctx, filename, content)
	if err != nil {
		log.Err(err).Str("phase", "upload").Msg("send_attachment failed")
		return "", err
	}
	log.Info().
		Str("listItemUniqueID", strings.TrimSpace(uploaded.ListItemUniqueID)).
		Msg("upload_success")

	share, err := o.Graph.CreateShareLink(ctx, uploaded.ListItemUniqueID)
	if err != nil {
		log.Err(err).Str("phase", "create_link").Msg("send_attachment failed")
		return "", err
	}
	log.Info().
		Str("listItemUniqueID", strings.TrimSpace(uploaded.ListItemUniqueID)).
		Msg("link_created")

	ext := filepath.Ext(filename)
	filesStr, err := attachments.BuildTeamsAttachmentFilesProperty(uploaded, share, filename, ext)
	if err != nil {
		log.Err(err).Str("phase", "build_files").Msg("send_attachment failed")
		return "", err
	}

	htmlContent := ""
	if strings.TrimSpace(caption) != "" {
		htmlContent = formatCaption(caption)
	}

	_, err = o.Teams.SendAttachmentMessageWithID(ctx, threadID, htmlContent, filesStr, fromUserID, clientMessageID)
	if err != nil {
		log.Err(err).Str("phase", "teams_send").Msg("send_attachment failed")
		return "", err
	}
	log.Info().
		Str("listItemUniqueID", strings.TrimSpace(uploaded.ListItemUniqueID)).
		Msg("teams_send_success")

	return clientMessageID, nil
}

func formatCaptionHTML(text string) string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	escaped := html.EscapeString(normalized)
	escaped = strings.ReplaceAll(escaped, "\n", "<br>")
	return "<p>" + escaped + "</p>"
}
