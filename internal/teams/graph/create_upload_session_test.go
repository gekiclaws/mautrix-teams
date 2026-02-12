package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCreateUploadSessionSuccess(t *testing.T) {
	filename := "spec sheet.pdf"
	token := "graph-token"
	wantUploadURL := "https://upload.example.com/session?token=abc"

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if r.URL.EscapedPath() != "/v1.0/me/drive/root:/Microsoft%20Teams%20Chat%20Files/spec%20sheet.pdf:/createUploadSession" {
				t.Fatalf("unexpected path: %s", r.URL.EscapedPath())
			}
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected content type: %s", r.Header.Get("Content-Type"))
			}

			raw, _ := io.ReadAll(r.Body)
			var body map[string]string
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("invalid json body: %v", err)
			}
			if body["@microsoft.graph.conflictBehavior"] != "rename" {
				t.Fatalf("unexpected conflictBehavior: %q", body["@microsoft.graph.conflictBehavior"])
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"uploadUrl":"` + wantUploadURL + `"}`)),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		}),
	}

	client := NewClient(httpClient)
	client.AccessToken = token
	client.UploadBaseURL = "https://graph.microsoft.com/v1.0/me/drive/root:/Microsoft Teams Chat Files"

	uploadURL, err := client.CreateUploadSession(context.Background(), filename)
	if err != nil {
		t.Fatalf("CreateUploadSession failed: %v", err)
	}
	if uploadURL != wantUploadURL {
		t.Fatalf("unexpected upload url: %s", uploadURL)
	}
}
