package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGetConsumptionHorizonsRequestShape(t *testing.T) {
	threadID := "19:abc/def@thread.v2"
	escapedThreadID := url.PathEscape(threadID)

	var gotMethod string
	var gotPath string
	var gotAuth string
	var gotAccept string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("authentication")
		gotAccept = r.Header.Get("Accept")
		payload := map[string]any{
			"id":      threadID,
			"version": "1",
			"consumptionhorizons": []map[string]any{
				{"id": "8:live:remote", "consumptionhorizon": "0;1;0", "messageVisibilityTime": 1},
			},
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	consumer := NewClient(server.Client())
	consumer.ConsumptionHorizonsURL = server.URL + "/api/chatsvc/consumer/v1/threads"
	consumer.Token = "token123"

	resp, err := consumer.GetConsumptionHorizons(context.Background(), threadID)
	if err != nil {
		t.Fatalf("GetConsumptionHorizons failed: %v", err)
	}
	if resp == nil || resp.ID != threadID {
		t.Fatalf("unexpected response: %#v", resp)
	}

	if gotMethod != http.MethodGet {
		t.Fatalf("unexpected method: got %s want %s", gotMethod, http.MethodGet)
	}
	expectedPath := "/api/chatsvc/consumer/v1/threads/" + escapedThreadID + "/consumptionhorizons"
	if gotPath != expectedPath {
		t.Fatalf("unexpected path: got %s want %s", gotPath, expectedPath)
	}
	if gotAuth != "skypetoken=token123" {
		t.Fatalf("unexpected authentication header: %q", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Fatalf("unexpected Accept header: %q", gotAccept)
	}
}

func TestGetConsumptionHorizonsNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad horizons"))
	}))
	defer server.Close()

	consumer := NewClient(server.Client())
	consumer.ConsumptionHorizonsURL = server.URL + "/api/chatsvc/consumer/v1/threads"
	consumer.Token = "token123"

	_, err := consumer.GetConsumptionHorizons(context.Background(), "19:abc@thread.v2")
	if err == nil {
		t.Fatalf("expected error")
	}
	var horizonsErr ConsumptionHorizonsError
	if !errors.As(err, &horizonsErr) {
		t.Fatalf("expected ConsumptionHorizonsError, got %T", err)
	}
	if horizonsErr.Status != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d", horizonsErr.Status, http.StatusBadRequest)
	}
	if !strings.Contains(horizonsErr.BodySnippet, "bad horizons") {
		t.Fatalf("unexpected body snippet: %q", horizonsErr.BodySnippet)
	}
}
