package bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// MatrixAttachment is a normalized representation of a Matrix file message suitable for sending out as an attachment.
// It is intentionally ephemeral and should not be persisted.
type MatrixAttachment struct {
	FileName string
	MimeType string
	Bytes    []byte
	Caption  string
}

type MatrixFileInfo struct {
	MXCURL        string
	FileName      string
	MimeType      string
	Caption       string
	EncryptedFile *event.EncryptedFileInfo
}

func ExtractMatrixFileInfo(content *event.MessageEventContent) (MatrixFileInfo, error) {
	if content == nil {
		return MatrixFileInfo{}, errors.New("missing message content")
	}
	if content.MsgType != event.MsgFile {
		return MatrixFileInfo{}, errors.New("not a file message")
	}

	mxcURL := ""
	var encryptedFile *event.EncryptedFileInfo
	if content.File != nil {
		encryptedFile = content.File
		mxcURL = strings.TrimSpace(string(content.File.URL))
	} else {
		mxcURL = strings.TrimSpace(string(content.URL))
	}
	if mxcURL == "" {
		return MatrixFileInfo{}, errors.New("missing mxc url")
	}

	fileName := strings.TrimSpace(content.GetFileName())
	if fileName == "" {
		// Fallback: derive from MXC ID.
		if parsed, err := id.ContentURIString(mxcURL).Parse(); err == nil && strings.TrimSpace(parsed.FileID) != "" {
			fileName = strings.TrimSpace(parsed.FileID)
		}
	}
	if fileName == "" {
		fileName = "file"
	}

	caption := strings.TrimSpace(content.GetCaption())

	mimeType := ""
	if content.Info != nil {
		mimeType = strings.TrimSpace(content.Info.MimeType)
	}

	return MatrixFileInfo{
		MXCURL:        mxcURL,
		FileName:      fileName,
		MimeType:      mimeType,
		Caption:       caption,
		EncryptedFile: encryptedFile,
	}, nil
}

func HandleOutboundMatrixFile(
	ctx context.Context,
	roomID id.RoomID,
	threadID string,
	content *event.MessageEventContent,
	download func(context.Context, string, *event.EncryptedFileInfo) ([]byte, error),
	send func(context.Context, string, string, []byte, string) error,
	log *zerolog.Logger,
) error {
	if download == nil {
		return errors.New("missing download function")
	}
	if send == nil {
		return errors.New("missing send function")
	}

	info, err := ExtractMatrixFileInfo(content)
	if err != nil {
		return err
	}

	l := zerolog.Nop()
	if log != nil {
		l = *log
	}
	l = l.With().
		Stringer("room_id", roomID).
		Str("thread_id", strings.TrimSpace(threadID)).
		Str("mxc_url", info.MXCURL).
		Str("filename", info.FileName).
		Logger()

	b, err := download(ctx, info.MXCURL, info.EncryptedFile)
	if err != nil {
		l.Err(err).Msg("matrix file download failed")
		return err
	}
	if len(b) > MaxAttachmentBytesV0 {
		err = fmt.Errorf("attachment exceeds max size: %d > %d", len(b), MaxAttachmentBytesV0)
		l.Err(err).Int("size", len(b)).Msg("matrix file rejected (too large)")
		return err
	}

	l.Info().Int("size", len(b)).Msg("matrix file downloaded")

	att := MatrixAttachment{
		FileName: info.FileName,
		MimeType: info.MimeType,
		Bytes:    b,
		Caption:  info.Caption,
	}
	if err := send(ctx, strings.TrimSpace(threadID), att.FileName, att.Bytes, att.Caption); err != nil {
		l.Err(err).Int("size", len(att.Bytes)).Msg("matrix file send failed")
		return err
	}
	return nil
}
