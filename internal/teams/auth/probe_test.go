package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeTeamsEndpoint(t *testing.T) {
	body := strings.Repeat("a", maxProbeBytes+10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authentication") != "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		w.Header().Set("Set-Cookie", "session=abc; Path=/; Secure; HttpOnly")
		w.Header().Set("X-Ms-Request-Id", "123")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := NewClient(nil)
	result, err := client.ProbeTeamsEndpoint(context.Background(), server.URL, "")
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if result.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected status: %d", result.StatusCode)
	}
	if len(result.BodySnippet) != maxProbeBytes {
		t.Fatalf("unexpected body length: %d", len(result.BodySnippet))
	}
	if result.AuthHeaders["Set-Cookie"] == "" {
		t.Fatalf("missing auth header: Set-Cookie")
	}
	if result.AuthHeaders["X-Ms-Request-Id"] == "" {
		t.Fatalf("missing auth header: X-Ms-Request-Id")
	}

	result, err = client.ProbeTeamsEndpoint(context.Background(), server.URL, "token123")
	if err != nil {
		t.Fatalf("probe with token failed: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status with token: %d", result.StatusCode)
	}
}
