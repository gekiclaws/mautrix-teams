package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	teamsclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

var (
	ErrEmptyUploadURL        = errors.New("upload url is empty")
	ErrInvalidChunkSize      = errors.New("invalid chunk size")
	ErrMissingExpectedRanges = errors.New("chunk response missing nextExpectedRanges")
	ErrInvalidExpectedRange  = errors.New("invalid nextExpectedRanges format")
)

type GraphChunkUploadError struct {
	Status      int
	BodySnippet string
}

func (e GraphChunkUploadError) Error() string {
	return "graph chunk upload request failed"
}

func (c *GraphClient) UploadFileInChunks(ctx context.Context, uploadURL string, content []byte) (*UploadedDriveItem, error) {
	if c == nil || c.HTTP == nil {
		return nil, ErrMissingGraphHTTPClient
	}
	if strings.TrimSpace(uploadURL) == "" {
		return nil, ErrEmptyUploadURL
	}
	if len(content) == 0 {
		return nil, ErrEmptyUploadContent
	}

	chunkSize := c.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if chunkSize%chunkSizeAlign != 0 || chunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}

	executor := c.executor()

	total := int64(len(content))
	var start int64
	for start < total {
		end := start + int64(chunkSize) - 1
		if end >= total {
			end = total - 1
		}
		chunk := content[start : end+1]

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimSpace(uploadURL), bytes.NewReader(chunk))
		if err != nil {
			return nil, err
		}

		// The uploadUrl is pre-authorized; don't attach Graph Bearer tokens.
		req.Header.Del("Authorization")
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		req.Header.Set("Content-Length", strconv.Itoa(len(chunk)))
		req.ContentLength = int64(len(chunk))

		resp, err := executor.Do(ctx, req, classifyChunkUploadResponse)
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return nil, err
		}

		if resp == nil || resp.Body == nil {
			return nil, errors.New("missing response")
		}

		if resp.StatusCode == http.StatusAccepted {
			// Intermediate response: parse nextExpectedRanges to decide where to continue.
			var payload struct {
				NextExpectedRanges []string `json:"nextExpectedRanges"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
			_ = resp.Body.Close()
			if decodeErr != nil {
				return nil, decodeErr
			}
			if len(payload.NextExpectedRanges) == 0 || strings.TrimSpace(payload.NextExpectedRanges[0]) == "" {
				return nil, ErrMissingExpectedRanges
			}
			next, err := parseNextExpectedStart(payload.NextExpectedRanges[0])
			if err != nil {
				return nil, err
			}
			start = next
			continue
		}

		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			raw, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				return nil, readErr
			}

			item, parseErr := parseUploadedDriveItem(bytes.NewReader(raw))
			if parseErr == nil {
				return item, nil
			}

			// Upload session final responses may omit sharepointIds fields. If we at least
			// have the drive item ID, fetch full details with a follow-up Graph GET.
			var minimal struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &minimal); err == nil && strings.TrimSpace(minimal.ID) != "" {
				fetched, err := c.GetDriveItem(ctx, minimal.ID)
				if err == nil {
					return fetched, nil
				}
			}
			return nil, parseErr
		}

		// classifyChunkUploadResponse should have converted this into an error,
		// but keep a safety net here to avoid infinite loops.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxUploadErrorBytes))
		_ = resp.Body.Close()
		return nil, GraphChunkUploadError{Status: resp.StatusCode, BodySnippet: string(snippet)}
	}

	return nil, errors.New("chunked upload did not complete")
}

func classifyChunkUploadResponse(resp *http.Response) error {
	if resp == nil {
		return errors.New("missing response")
	}
	// 200/201: final drive item payload.
	// 202: intermediate response with nextExpectedRanges.
	if resp.StatusCode == http.StatusAccepted ||
		(resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices) {
		return nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return teamsclient.RetryableError{
			Status:     resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return teamsclient.RetryableError{Status: resp.StatusCode}
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxUploadErrorBytes))
	return GraphChunkUploadError{
		Status:      resp.StatusCode,
		BodySnippet: string(snippet),
	}
}

func parseNextExpectedStart(rangeValue string) (int64, error) {
	// Formats: "12345-" or "12345-67890"
	v := strings.TrimSpace(rangeValue)
	dash := strings.IndexByte(v, '-')
	if dash <= 0 {
		return 0, ErrInvalidExpectedRange
	}
	startStr := v[:dash]
	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 {
		return 0, ErrInvalidExpectedRange
	}
	return start, nil
}
