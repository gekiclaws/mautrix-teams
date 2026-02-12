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

func TestUploadFileInChunksSuccess202Then200(t *testing.T) {
	chunkSize := chunkSizeAlign
	content := bytes.Repeat([]byte{0xAB}, chunkSize*2+10)
	uploadURL := "https://upload.example.com/upload"

	var seenRanges []string
	var seenBodies [][]byte

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPut {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if r.URL.String() != uploadURL {
				t.Fatalf("unexpected url: %s", r.URL.String())
			}
			if r.Header.Get("Authorization") != "" {
				t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Fatalf("unexpected content type: %s", r.Header.Get("Content-Type"))
			}
			cr := r.Header.Get("Content-Range")
			if cr == "" {
				t.Fatalf("missing content-range header")
			}
			seenRanges = append(seenRanges, cr)

			body, _ := io.ReadAll(r.Body)
			seenBodies = append(seenBodies, body)

			// Determine response by range.
			switch cr {
			case "bytes 0-327679/655370":
				return &http.Response{
					StatusCode: http.StatusAccepted,
					Body:       io.NopCloser(strings.NewReader(`{"nextExpectedRanges":["327680-"]}`)),
					Header:     make(http.Header),
					Request:    r,
				}, nil
			case "bytes 327680-655359/655370":
				return &http.Response{
					StatusCode: http.StatusAccepted,
					Body:       io.NopCloser(strings.NewReader(`{"nextExpectedRanges":["655360-"]}`)),
					Header:     make(http.Header),
					Request:    r,
				}, nil
			case "bytes 655360-655369/655370":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"id":"CID!final123",
						"name":"uploaded.bin",
						"size":655370,
						"sharepointIds":{"listItemUniqueId":"11111111-2222-3333-4444-555555555555"},
						"parentReference":{"sharepointIds":{"siteUrl":"https://tenant-my.sharepoint.com/personal/user"}}
					}`)),
					Header:  make(http.Header),
					Request: r,
				}, nil
			default:
				t.Fatalf("unexpected content-range: %s", cr)
				return nil, nil
			}
		}),
	}

	client := NewClient(httpClient)
	client.ChunkSize = chunkSize
	client.Executor = &teamsclient.TeamsRequestExecutor{HTTP: httpClient, MaxRetries: 0}

	item, err := client.UploadFileInChunks(context.Background(), uploadURL, content)
	if err != nil {
		t.Fatalf("UploadFileInChunks failed: %v", err)
	}
	if item.DriveItemID != "CID!final123" {
		t.Fatalf("unexpected drive item id: %s", item.DriveItemID)
	}
	if len(seenRanges) != 3 {
		t.Fatalf("expected 3 chunk requests, got %d", len(seenRanges))
	}
	if !bytes.Equal(seenBodies[0], content[:chunkSize]) {
		t.Fatalf("unexpected first chunk body")
	}
	if !bytes.Equal(seenBodies[1], content[chunkSize:2*chunkSize]) {
		t.Fatalf("unexpected second chunk body")
	}
	if !bytes.Equal(seenBodies[2], content[2*chunkSize:]) {
		t.Fatalf("unexpected third chunk body")
	}
}

func TestUploadFileInChunksRetryOn429MidChunk(t *testing.T) {
	chunkSize := chunkSizeAlign
	content := bytes.Repeat([]byte{0xCD}, chunkSize*2)
	uploadURL := "https://upload.example.com/upload"

	var secondChunkAttempts int

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			cr := r.Header.Get("Content-Range")
			switch cr {
			case "bytes 0-327679/655360":
				return &http.Response{
					StatusCode: http.StatusAccepted,
					Body:       io.NopCloser(strings.NewReader(`{"nextExpectedRanges":["327680-"]}`)),
					Header:     make(http.Header),
					Request:    r,
				}, nil
			case "bytes 327680-655359/655360":
				secondChunkAttempts++
				if secondChunkAttempts == 1 {
					h := make(http.Header)
					h.Set("Retry-After", "0")
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Body:       io.NopCloser(strings.NewReader(`{"error":"rate limited"}`)),
						Header:     h,
						Request:    r,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"id":"CID!done",
						"name":"uploaded.bin",
						"size":655360,
						"sharepointIds":{"listItemUniqueId":"11111111-2222-3333-4444-555555555555"},
						"parentReference":{"sharepointIds":{"siteUrl":"https://tenant-my.sharepoint.com/personal/user"}}
					}`)),
					Header:  make(http.Header),
					Request: r,
				}, nil
			default:
				t.Fatalf("unexpected content-range: %s", cr)
				return nil, nil
			}
		}),
	}

	client := NewClient(httpClient)
	client.ChunkSize = chunkSize
	client.Executor = &teamsclient.TeamsRequestExecutor{
		HTTP:        httpClient,
		MaxRetries:  1,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  1 * time.Millisecond,
	}

	_, err := client.UploadFileInChunks(context.Background(), uploadURL, content)
	if err != nil {
		t.Fatalf("UploadFileInChunks failed: %v", err)
	}
	if secondChunkAttempts != 2 {
		t.Fatalf("expected second chunk to be attempted twice, got %d", secondChunkAttempts)
	}
}

func TestUploadFileInChunksFailsOn403(t *testing.T) {
	chunkSize := chunkSizeAlign
	content := bytes.Repeat([]byte{0xEF}, chunkSize)
	uploadURL := "https://upload.example.com/upload"

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
	client.ChunkSize = chunkSize
	client.Executor = &teamsclient.TeamsRequestExecutor{HTTP: httpClient, MaxRetries: 0}

	_, err := client.UploadFileInChunks(context.Background(), uploadURL, content)
	if err == nil {
		t.Fatalf("expected error")
	}
	var chunkErr GraphChunkUploadError
	if !errors.As(err, &chunkErr) {
		t.Fatalf("expected GraphChunkUploadError, got %T (%v)", err, err)
	}
	if chunkErr.Status != http.StatusForbidden {
		t.Fatalf("unexpected status: %d", chunkErr.Status)
	}
}

func TestUploadFileInChunksFetchesDriveItemWhenFinalResponseMissingSharepointIDs(t *testing.T) {
	chunkSize := chunkSizeAlign
	content := bytes.Repeat([]byte{0xAA}, chunkSize)
	uploadURL := "https://upload.example.com/upload"
	token := "graph-token"

	var sawChunkPut bool
	var sawGetDriveItem bool

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodPut && r.URL.String() == uploadURL {
				sawChunkPut = true
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"id":"CID!missing","name":"x.bin","size":327680}`)),
					Header:     make(http.Header),
					Request:    r,
				}, nil
			}
			if r.Method == http.MethodGet && strings.Contains(r.URL.String(), "/v1.0/me/drive/items/") {
				sawGetDriveItem = true
				if r.Header.Get("Authorization") != "Bearer "+token {
					t.Fatalf("missing bearer token on get drive item")
				}
				if !strings.Contains(r.URL.RawQuery, "%24select=") && !strings.Contains(r.URL.RawQuery, "$select=") {
					t.Fatalf("missing $select query: %s", r.URL.String())
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"id":"CID!missing",
						"name":"x.bin",
						"size":327680,
						"sharepointIds":{"listItemUniqueId":"11111111-2222-3333-4444-555555555555"},
						"parentReference":{"sharepointIds":{"siteUrl":"https://tenant-my.sharepoint.com/personal/user"}}
					}`)),
					Header:  make(http.Header),
					Request: r,
				}, nil
			}
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}),
	}

	client := NewClient(httpClient)
	client.AccessToken = token
	client.ChunkSize = chunkSize
	client.Executor = &teamsclient.TeamsRequestExecutor{HTTP: httpClient, MaxRetries: 0}

	item, err := client.UploadFileInChunks(context.Background(), uploadURL, content)
	if err != nil {
		t.Fatalf("UploadFileInChunks failed: %v", err)
	}
	if item.ListItemUniqueID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("unexpected list item id: %s", item.ListItemUniqueID)
	}
	if !sawChunkPut {
		t.Fatalf("expected chunk PUT")
	}
	if !sawGetDriveItem {
		t.Fatalf("expected get drive item fallback")
	}
}

func TestUploadTeamsChatFileUsesDirectAtBoundary(t *testing.T) {
	content := []byte("abc")
	token := "graph-token"

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPut {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if !strings.Contains(r.URL.EscapedPath(), ":/content") {
				t.Fatalf("unexpected path: %s", r.URL.EscapedPath())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"id":"CID!direct",
					"name":"x.txt",
					"size":3,
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
	client.SmallFileLimit = len(content) // boundary
	client.Executor = &teamsclient.TeamsRequestExecutor{HTTP: httpClient, MaxRetries: 0}

	_, err := client.UploadTeamsChatFile(context.Background(), "x.txt", content)
	if err != nil {
		t.Fatalf("UploadTeamsChatFile failed: %v", err)
	}
}

func TestUploadTeamsChatFileUsesChunkedWhenOverLimit(t *testing.T) {
	content := []byte("abcd")
	token := "graph-token"
	uploadURL := "https://upload.example.com/upload"

	var sawCreateSession bool
	var sawChunkPut bool

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.EscapedPath(), ":/createUploadSession") {
				sawCreateSession = true
				if r.Header.Get("Authorization") != "Bearer "+token {
					t.Fatalf("missing bearer token on createUploadSession")
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"uploadUrl":"` + uploadURL + `"}`)),
					Header:     make(http.Header),
					Request:    r,
				}, nil
			}
			if r.Method == http.MethodPut && r.URL.String() == uploadURL {
				sawChunkPut = true
				if r.Header.Get("Authorization") != "" {
					t.Fatalf("unexpected authorization header on chunk put: %s", r.Header.Get("Authorization"))
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"id":"CID!chunked",
						"name":"x.txt",
						"size":4,
						"sharepointIds":{"listItemUniqueId":"11111111-2222-3333-4444-555555555555"},
						"parentReference":{"sharepointIds":{"siteUrl":"https://tenant-my.sharepoint.com/personal/user"}}
					}`)),
					Header:  make(http.Header),
					Request: r,
				}, nil
			}
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}),
	}

	client := NewClient(httpClient)
	client.AccessToken = token
	client.SmallFileLimit = 3
	client.ChunkSize = chunkSizeAlign
	client.Executor = &teamsclient.TeamsRequestExecutor{HTTP: httpClient, MaxRetries: 0}
	client.UploadBaseURL = "https://graph.microsoft.com/v1.0/me/drive/root:/Microsoft Teams Chat Files"

	_, err := client.UploadTeamsChatFile(context.Background(), "x.txt", content)
	if err != nil {
		t.Fatalf("UploadTeamsChatFile failed: %v", err)
	}
	if !sawCreateSession {
		t.Fatalf("expected createUploadSession call")
	}
	if !sawChunkPut {
		t.Fatalf("expected chunk PUT call")
	}
}
