package connector

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	internalbridge "go.mau.fi/mautrix-teams/internal/bridge"

	"maunium.net/go/mautrix/bridgev2/matrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func (c *TeamsClient) DownloadMatrixMedia(ctx context.Context, mxcURL string) ([]byte, error) {
	return c.downloadMatrixMedia(ctx, mxcURL, nil)
}

func (c *TeamsClient) downloadMatrixMedia(ctx context.Context, mxcURL string, file *event.EncryptedFileInfo) ([]byte, error) {
	mxcURL = strings.TrimSpace(mxcURL)
	if mxcURL == "" {
		return nil, errors.New("missing mxc url")
	}
	if c == nil || c.Main == nil || c.Main.Bridge == nil || c.Main.Bridge.Bot == nil {
		return nil, errors.New("missing matrix client")
	}

	// We need access to the underlying appservice intent to stream the download and enforce a size limit.
	bot, ok := c.Main.Bridge.Bot.(*matrix.ASIntent)
	if !ok || bot.Matrix == nil {
		return nil, errors.New("unsupported matrix client type")
	}

	parsed, err := id.ContentURIString(mxcURL).Parse()
	if err != nil {
		return nil, err
	}

	resp, err := bot.Matrix.Download(ctx, parsed)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Body == nil {
		return nil, errors.New("download returned empty response")
	}
	defer resp.Body.Close()

	max := int64(internalbridge.MaxAttachmentBytesV0)
	if resp.ContentLength > max {
		return nil, errors.New("attachment exceeds max size")
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New("attachment exceeds max size")
	}
	if len(data) == 0 && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil, errors.New("downloaded media is empty")
	}

	if file != nil {
		// Decrypt after size limiting; ciphertext is slightly larger than plaintext,
		// but this enforces the same guardrail as the attachment pipeline.
		if err := file.PrepareForDecryption(); err != nil {
			return nil, err
		}
		if err := file.DecryptInPlace(data); err != nil {
			return nil, err
		}
	}

	return data, nil
}
