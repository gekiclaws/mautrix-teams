package graph

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	teamsclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

func TestCreateShareLinkSuccess(t *testing.T) {
	token := "graph-token"
	uniqueID := "11111111-2222-3333-4444-555555555555"
	var gotBody struct {
		Type string `json:"type"`
	}

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if r.URL.String() != "https://graph.microsoft.com/v1.0/drive/items/"+uniqueID+"/createLink" {
				t.Fatalf("unexpected url: %s", r.URL.String())
			}
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected content type: %s", r.Header.Get("Content-Type"))
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &gotBody); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}
			if gotBody.Type != "edit" {
				t.Fatalf("unexpected request body type: %q", gotBody.Type)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"shareId":"u!abc123",
					"link":{"webUrl":"https://1drv.ms/u/s!abc123"}
				}`)),
				Header:  make(http.Header),
				Request: r,
			}, nil
		}),
	}

	client := NewClient(httpClient)
	client.AccessToken = token

	created, err := client.CreateShareLink(context.Background(), uniqueID)
	if err != nil {
		t.Fatalf("CreateShareLink failed: %v", err)
	}
	if created.ShareID != "u!abc123" {
		t.Fatalf("unexpected share id: %s", created.ShareID)
	}
	if created.ShareURL != "https://1drv.ms/u/s!abc123" {
		t.Fatalf("unexpected share url: %s", created.ShareURL)
	}
}

func TestCreateShareLinkRetryable429(t *testing.T) {
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

	_, err := client.CreateShareLink(context.Background(), "11111111-2222-3333-4444-555555555555")
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

func TestCreateShareLinkRetryable500(t *testing.T) {
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

	_, err := client.CreateShareLink(context.Background(), "11111111-2222-3333-4444-555555555555")
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

func TestCreateShareLinkNonRetryable403(t *testing.T) {
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

	_, err := client.CreateShareLink(context.Background(), "11111111-2222-3333-4444-555555555555")
	if err == nil {
		t.Fatalf("expected error")
	}
	var createLinkErr GraphCreateLinkError
	if !errors.As(err, &createLinkErr) {
		t.Fatalf("expected GraphCreateLinkError, got %T (%v)", err, err)
	}
	if createLinkErr.Status != http.StatusForbidden {
		t.Fatalf("unexpected status: %d", createLinkErr.Status)
	}
	if createLinkErr.BodySnippet == "" {
		t.Fatalf("expected body snippet")
	}
}

