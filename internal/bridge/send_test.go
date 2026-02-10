package teamsbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/client"
)

type fakeThreadLookup struct {
	threadID string
	ok       bool
}

func (f fakeThreadLookup) GetThreadID(roomID id.RoomID) (string, bool) {
	return f.threadID, f.ok
}

type fakeSendIntentStore struct {
	mu       sync.Mutex
	inserted []*database.TeamsSendIntent
	updates  map[string]database.TeamsSendStatus
	cleared  []string
}

func (f *fakeSendIntentStore) Insert(intent *database.TeamsSendIntent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inserted = append(f.inserted, intent)
	return nil
}

func (f *fakeSendIntentStore) UpdateStatus(clientMessageID string, status database.TeamsSendStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updates == nil {
		f.updates = make(map[string]database.TeamsSendStatus)
	}
	f.updates[clientMessageID] = status
	return nil
}

func (f *fakeSendIntentStore) ClearIntentMXID(clientMessageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared = append(f.cleared, clientMessageID)
	return nil
}

func TestTeamsConsumerSenderSuccess(t *testing.T) {
	store := &fakeSendIntentStore{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	consumer := client.NewClient(server.Client())
	consumer.SendMessagesURL = server.URL + "/conversations"
	consumer.Token = "token123"

	statuses := []database.TeamsSendStatus{}
	writer := func(ctx context.Context, status database.TeamsSendStatus, clientMessageID string, ts int64) error {
		statuses = append(statuses, status)
		return nil
	}

	sender := NewTeamsConsumerSender(consumer, store, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, "8:live:me", zerolog.Nop())
	err := sender.SendMatrixText(context.Background(), "!room:example.org", "hello", "$event", "@alice:example.org", writer)
	if err != nil {
		t.Fatalf("SendMatrixText failed: %v", err)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("expected one send intent, got %d", len(store.inserted))
	}
	intent := store.inserted[0]
	if intent.Status != database.TeamsSendStatusPending {
		t.Fatalf("expected pending status, got %s", intent.Status)
	}
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(intent.ClientMessageID) {
		t.Fatalf("client message id is not numeric: %q", intent.ClientMessageID)
	}
	if intent.IntentMXID != "@alice:example.org" {
		t.Fatalf("expected intent mxid @alice:example.org, got %s", intent.IntentMXID)
	}
	if got := store.updates[intent.ClientMessageID]; got != database.TeamsSendStatusAccepted {
		t.Fatalf("expected accepted status, got %s", got)
	}
	if len(store.cleared) != 1 || store.cleared[0] != intent.ClientMessageID {
		t.Fatalf("expected cleared intent mapping for %s, got %v", intent.ClientMessageID, store.cleared)
	}
	if len(statuses) != 2 || statuses[0] != database.TeamsSendStatusPending || statuses[1] != database.TeamsSendStatusAccepted {
		t.Fatalf("unexpected status sequence: %v", statuses)
	}
}

func TestTeamsConsumerSenderFailure(t *testing.T) {
	store := &fakeSendIntentStore{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	consumer := client.NewClient(server.Client())
	consumer.SendMessagesURL = server.URL + "/conversations"
	consumer.Token = "token123"

	statuses := []database.TeamsSendStatus{}
	writer := func(ctx context.Context, status database.TeamsSendStatus, clientMessageID string, ts int64) error {
		statuses = append(statuses, status)
		return nil
	}

	sender := NewTeamsConsumerSender(consumer, store, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, "8:live:me", zerolog.Nop())
	err := sender.SendMatrixText(context.Background(), "!room:example.org", "hello", "$event", "@alice:example.org", writer)
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(store.inserted) != 1 {
		t.Fatalf("expected one send intent, got %d", len(store.inserted))
	}
	intent := store.inserted[0]
	if got := store.updates[intent.ClientMessageID]; got != database.TeamsSendStatusFailed {
		t.Fatalf("expected failed status, got %s", got)
	}
	if len(store.cleared) != 1 || store.cleared[0] != intent.ClientMessageID {
		t.Fatalf("expected cleared intent mapping for %s, got %v", intent.ClientMessageID, store.cleared)
	}
	if len(statuses) != 2 || statuses[0] != database.TeamsSendStatusPending || statuses[1] != database.TeamsSendStatusFailed {
		t.Fatalf("unexpected status sequence: %v", statuses)
	}
}

func TestTeamsConsumerSenderGIFSuccess(t *testing.T) {
	store := &fakeSendIntentStore{}
	var payload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	consumer := client.NewClient(server.Client())
	consumer.SendMessagesURL = server.URL + "/conversations"
	consumer.Token = "token123"

	statuses := []database.TeamsSendStatus{}
	writer := func(ctx context.Context, status database.TeamsSendStatus, clientMessageID string, ts int64) error {
		statuses = append(statuses, status)
		return nil
	}

	sender := NewTeamsConsumerSender(consumer, store, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, "8:live:me", zerolog.Nop())
	err := sender.SendMatrixGIF(context.Background(), "!room:example.org", "https://media4.giphy.com/media/test/giphy.gif", "Football GIF", "$event", "@alice:example.org", writer)
	if err != nil {
		t.Fatalf("SendMatrixGIF failed: %v", err)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("expected one send intent, got %d", len(store.inserted))
	}
	intent := store.inserted[0]
	if got := store.updates[intent.ClientMessageID]; got != database.TeamsSendStatusAccepted {
		t.Fatalf("expected accepted status, got %s", got)
	}
	if len(statuses) != 2 || statuses[0] != database.TeamsSendStatusPending || statuses[1] != database.TeamsSendStatusAccepted {
		t.Fatalf("unexpected status sequence: %v", statuses)
	}
	if !strings.Contains(payload["content"], `itemtype="http://schema.skype.com/Giphy"`) {
		t.Fatalf("unexpected content: %q", payload["content"])
	}
}
