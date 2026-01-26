package teamsbridge

import (
	"testing"

	"maunium.net/go/mautrix/id"
)

func TestTeamsThreadStoreReverseLookup(t *testing.T) {
	store := &TeamsThreadStore{
		byThreadID: map[string]id.RoomID{"thread-1": "!room:example.org"},
		byRoomID:   map[id.RoomID]string{"!room:example.org": "thread-1"},
	}
	threadID, ok := store.GetThreadID("!room:example.org")
	if !ok {
		t.Fatalf("expected thread id to be found")
	}
	if threadID != "thread-1" {
		t.Fatalf("unexpected thread id: %q", threadID)
	}
}
