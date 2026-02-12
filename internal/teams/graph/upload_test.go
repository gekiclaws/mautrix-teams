package graph

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	teamsclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestUploadTeamsChatFileSuccess(t *testing.T) {
	content := []byte("hello graph upload")
	filename := "report q1.txt"
	token := "graph-token"

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPut {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if r.URL.EscapedPath() != "/v1.0/me/drive/root:/Microsoft%20Teams%20Chat%20Files/report%20q1.txt:/content" {
				t.Fatalf("unexpected path: %s", r.URL.EscapedPath())
			}
			if r.URL.Query().Get("@microsoft.graph.conflictBehavior") != "rename" {
				t.Fatalf("missing conflictBehavior query")
			}
			if r.URL.Query().Get("$select") != "id,name,size,sharepointIds,parentReference" {
				t.Fatalf("missing $select query")
			}
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Fatalf("unexpected content type: %s", r.Header.Get("Content-Type"))
			}
			body, _ := io.ReadAll(r.Body)
			if !bytes.Equal(body, content) {
				t.Fatalf("unexpected request body: %q", string(body))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"id":"CID!sabc123",
					"name":"report q1.txt",
					"size":18,
					"sharepointIds":{"listItemUniqueId":"11111111-2222-3333-4444-555555555555"},
					"parentReference":{"sharepointIds":{"siteUrl":"https://tenant-my.sharepoint.com/personal/user"}}
				}`)),
				Header:  make(http.Header),
				Request: r,
			}, nil
		}),
	}

	client := NewClient(httpClient)
	client.AccessToken = token
	client.UploadBaseURL = "https://graph.microsoft.com/v1.0/me/drive/root:/Microsoft Teams Chat Files"

	item, err := client.UploadTeamsChatFile(context.Background(), filename, content)
	if err != nil {
		t.Fatalf("UploadTeamsChatFile failed: %v", err)
	}
	if item.DriveItemID != "CID!sabc123" {
		t.Fatalf("unexpected drive item id: %s", item.DriveItemID)
	}
	if item.ListItemUniqueID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("unexpected list item unique id: %s", item.ListItemUniqueID)
	}
	if item.SiteURL != "https://tenant-my.sharepoint.com/personal/user" {
		t.Fatalf("unexpected site url: %s", item.SiteURL)
	}
	if item.FileName != "report q1.txt" {
		t.Fatalf("unexpected file name: %s", item.FileName)
	}
	if item.Size != 18 {
		t.Fatalf("unexpected size: %d", item.Size)
	}
}

func TestUploadTeamsChatFileEscapesFilename(t *testing.T) {
	client := NewClient(&http.Client{})
	client.UploadBaseURL = "https://graph.microsoft.com/v1.0/me/drive/root:/Microsoft Teams Chat Files"
	got, err := client.uploadURL("report/q1.txt")
	if err != nil {
		t.Fatalf("uploadURL failed: %v", err)
	}
	if !strings.Contains(got, "report%2Fq1.txt:/content") {
		t.Fatalf("expected escaped filename in URL, got: %s", got)
	}
}

func TestUploadTeamsChatFileRetryable429(t *testing.T) {
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Retry-After", "2")
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":"rate limited"}`)),
				Header:     header,
				Request:    r,
			}, nil
		}),
	}

	client := NewClient(httpClient)
	client.AccessToken = "graph-token"
	client.Executor = &teamsclient.TeamsRequestExecutor{HTTP: httpClient, MaxRetries: 0}

	_, err := client.UploadTeamsChatFile(context.Background(), "test.txt", []byte("abc"))
	if err == nil {
		t.Fatalf("expected error")
	}
	var retryable teamsclient.RetryableError
	if !errors.As(err, &retryable) {
		t.Fatalf("expected retryable error, got %T (%v)", err, err)
	}
	if retryable.Status != http.StatusTooManyRequests {
		t.Fatalf("unexpected status: %d", retryable.Status)
	}
	if retryable.RetryAfter != 2*time.Second {
		t.Fatalf("unexpected retry-after: %s", retryable.RetryAfter)
	}
}

func TestUploadTeamsChatFileRetryable500(t *testing.T) {
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(`{"error":"server error"}`)),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		}),
	}

	client := NewClient(httpClient)
	client.AccessToken = "graph-token"
	client.Executor = &teamsclient.TeamsRequestExecutor{HTTP: httpClient, MaxRetries: 0}

	_, err := client.UploadTeamsChatFile(context.Background(), "test.txt", []byte("abc"))
	if err == nil {
		t.Fatalf("expected error")
	}
	var retryable teamsclient.RetryableError
	if !errors.As(err, &retryable) {
		t.Fatalf("expected retryable error, got %T (%v)", err, err)
	}
	if retryable.Status != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %d", retryable.Status)
	}
}

func TestUploadTeamsChatFileNonRetryable403(t *testing.T) {
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Body:       io.NopCloser(strings.NewReader(`{"error":"forbidden"}`)),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		}),
	}

	client := NewClient(httpClient)
	client.AccessToken = "graph-token"
	client.Executor = &teamsclient.TeamsRequestExecutor{HTTP: httpClient, MaxRetries: 0}

	_, err := client.UploadTeamsChatFile(context.Background(), "test.txt", []byte("abc"))
	if err == nil {
		t.Fatalf("expected error")
	}
	var uploadErr GraphUploadError
	if !errors.As(err, &uploadErr) {
		t.Fatalf("expected GraphUploadError, got %T (%v)", err, err)
	}
	if uploadErr.Status != http.StatusForbidden {
		t.Fatalf("unexpected status: %d", uploadErr.Status)
	}
	if uploadErr.BodySnippet == "" {
		t.Fatalf("expected body snippet")
	}
}
