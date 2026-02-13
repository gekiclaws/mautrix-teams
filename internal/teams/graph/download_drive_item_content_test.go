package graph

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	teamsclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

func TestDownloadDriveItemContentSuccess(t *testing.T) {
	token := "graph-token"
	itemID := "CID!sabc123"
	content := []byte{0x01, 0x02, 0x03}
	contentType := "image/png"

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			// Drive item IDs may include reserved characters (e.g. "CID!s..."), so the path should be escaped.
			if r.URL.EscapedPath() != "/v1.0/me/drive/items/CID%21sabc123/content" {
				t.Fatalf("unexpected path: %s", r.URL.EscapedPath())
			}
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
			}
			h := make(http.Header)
			h.Set("Content-Type", contentType)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(content)),
				Header:     h,
				Request:    r,
			}, nil
		}),
	}

	client := NewClient(httpClient)
	client.AccessToken = token
	client.Executor = &teamsclient.TeamsRequestExecutor{HTTP: httpClient, MaxRetries: 0}

	got, err := client.DownloadDriveItemContent(context.Background(), itemID)
	if err != nil {
		t.Fatalf("DownloadDriveItemContent failed: %v", err)
	}
	if got == nil || !bytes.Equal(got.Bytes, content) {
		t.Fatalf("unexpected bytes: %#v", got)
	}
	if got.ContentType != contentType {
		t.Fatalf("unexpected content type: %q", got.ContentType)
	}
}

func TestClassifyDriveItemContentResponseRetryable429(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Body:       io.NopCloser(strings.NewReader(`{"error":"rate limited"}`)),
		Header:     make(http.Header),
	}
	resp.Header.Set("Retry-After", "2")

	err := classifyDriveItemContentResponse(resp)
	if err == nil {
		t.Fatalf("expected error")
	}
	var retryable teamsclient.RetryableError
	if !errors.As(err, &retryable) {
		t.Fatalf("expected RetryableError, got %T (%v)", err, err)
	}
	if retryable.Status != http.StatusTooManyRequests {
		t.Fatalf("unexpected status: %d", retryable.Status)
	}
	if retryable.RetryAfter <= 0 {
		t.Fatalf("expected positive retry-after, got %v", retryable.RetryAfter)
	}
}

func TestClassifyDriveItemContentResponseRetryable500(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(`{"error":"server"}`)),
		Header:     make(http.Header),
	}

	err := classifyDriveItemContentResponse(resp)
	if err == nil {
		t.Fatalf("expected error")
	}
	var retryable teamsclient.RetryableError
	if !errors.As(err, &retryable) {
		t.Fatalf("expected RetryableError, got %T (%v)", err, err)
	}
	if retryable.Status != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %d", retryable.Status)
	}
}
