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
	"time"
)

func TestSetConsumptionHorizonRequestShape(t *testing.T) {
	threadID := "19:abc/def@thread.v2"
	escapedThreadID := url.PathEscape(threadID)
	horizon := "123;456;0"

	var gotMethod string
	var gotPath string
	var gotQuery string
	var gotAuth string
	var gotAccept string
	var gotContentType string
	var gotBody map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("authentication")
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	consumer := NewClient(server.Client())
	consumer.SendMessagesURL = server.URL + "/consumer/v1/users/ME/conversations"
	consumer.Token = "token123"

	status, err := consumer.SetConsumptionHorizon(context.Background(), threadID, horizon)
	if err != nil {
		t.Fatalf("SetConsumptionHorizon failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", status, http.StatusOK)
	}

	if gotMethod != http.MethodPut {
		t.Fatalf("unexpected method: got %s want %s", gotMethod, http.MethodPut)
	}
	expectedPath := "/consumer/v1/users/ME/conversations/" + escapedThreadID + "/properties"
	if gotPath != expectedPath {
		t.Fatalf("unexpected path: got %s want %s", gotPath, expectedPath)
	}
	if gotQuery != "name=consumptionhorizon" {
		t.Fatalf("unexpected query: %q", gotQuery)
	}
	if gotAuth != "skypetoken=token123" {
		t.Fatalf("unexpected authentication header: %q", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Fatalf("unexpected Accept header: %q", gotAccept)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected Content-Type header: %q", gotContentType)
	}
	if gotBody["consumptionhorizon"] != horizon {
		t.Fatalf("unexpected consumptionhorizon: %q", gotBody["consumptionhorizon"])
	}
}

func TestConsumptionHorizonNow(t *testing.T) {
	now := time.UnixMilli(1700000000123)
	horizon := ConsumptionHorizonNow(now)
	parts := strings.Split(horizon, ";")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %q", len(parts), horizon)
	}
	if parts[0] != "1700000000123" || parts[1] != "1700000000123" {
		t.Fatalf("unexpected timestamp parts: %v", parts[:2])
	}
	if parts[2] != "0" {
		t.Fatalf("unexpected third part: %q", parts[2])
	}
}

func TestSetConsumptionHorizonNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad receipt"))
	}))
	defer server.Close()

	consumer := NewClient(server.Client())
	consumer.SendMessagesURL = server.URL + "/conversations"
	consumer.Token = "token123"

	status, err := consumer.SetConsumptionHorizon(context.Background(), "19:abc@thread.v2", "123;123;0")
	if err == nil {
		t.Fatalf("expected error")
	}
	if status != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d", status, http.StatusBadRequest)
	}
	var receiptErr ReceiptError
	if !errors.As(err, &receiptErr) {
		t.Fatalf("expected ReceiptError, got %T", err)
	}
	if receiptErr.Status != http.StatusBadRequest {
		t.Fatalf("unexpected receipt error status: got %d want %d", receiptErr.Status, http.StatusBadRequest)
	}
	if !strings.Contains(receiptErr.BodySnippet, "bad receipt") {
		t.Fatalf("unexpected body snippet: %q", receiptErr.BodySnippet)
	}
}
