package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendAttachmentMessageWithIDPayload(t *testing.T) {
	var gotPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(server.Client())
	c.SendMessagesURL = server.URL + "/conversations"
	c.Token = "token123"

	files := `[{"id":"x"}]`
	html := ""
	status, err := c.SendAttachmentMessageWithID(context.Background(), "@19:abc@thread.v2", html, files, "8:live:me", "123")
	if err != nil {
		t.Fatalf("SendAttachmentMessageWithID failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("unexpected status: %d", status)
	}

	props, ok := gotPayload["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %#v", gotPayload["properties"])
	}
	if props["files"] != files {
		t.Fatalf("unexpected properties.files: %#v", props["files"])
	}
	if gotPayload["content"] != "" {
		t.Fatalf("expected empty content, got %#v", gotPayload["content"])
	}
}
