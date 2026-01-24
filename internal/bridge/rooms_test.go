package teamsbridge

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type fakeStore struct {
	rooms map[string]id.RoomID
}

func (s *fakeStore) Get(threadID string) (id.RoomID, bool) {
	room, ok := s.rooms[threadID]
	return room, ok
}

func (s *fakeStore) Put(threadID string, roomID id.RoomID) error {
	s.rooms[threadID] = roomID
	return nil
}

type fakeCreator struct {
	roomID id.RoomID
	calls  int
}

func (c *fakeCreator) CreateRoom(thread model.Thread) (id.RoomID, error) {
	c.calls++
	return c.roomID, nil
}

type fakeLister struct {
	conversations []model.RemoteConversation
}

func (l *fakeLister) ListConversations(ctx context.Context, token string) ([]model.RemoteConversation, error) {
	return l.conversations, nil
}

func TestEnsureRoomIdempotent(t *testing.T) {
	store := &fakeStore{rooms: map[string]id.RoomID{"thread-1": "!room:example.org"}}
	creator := &fakeCreator{roomID: "!created:example.org"}
	rooms := NewRoomsService(store, creator, zerolog.Nop())

	thread := model.Thread{ID: "thread-1"}
	roomID, created, err := rooms.EnsureRoom(thread)
	if err != nil {
		t.Fatalf("EnsureRoom failed: %v", err)
	}
	if created {
		t.Fatalf("expected created=false")
	}
	if roomID != "!room:example.org" {
		t.Fatalf("unexpected room ID: %s", roomID)
	}
	if creator.calls != 0 {
		t.Fatalf("expected creator not to be called")
	}
}

func TestEnsureRoomCreates(t *testing.T) {
	store := &fakeStore{rooms: map[string]id.RoomID{}}
	creator := &fakeCreator{roomID: "!created:example.org"}
	rooms := NewRoomsService(store, creator, zerolog.Nop())

	thread := model.Thread{ID: "thread-2"}
	roomID, created, err := rooms.EnsureRoom(thread)
	if err != nil {
		t.Fatalf("EnsureRoom failed: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
	if roomID != "!created:example.org" {
		t.Fatalf("unexpected room ID: %s", roomID)
	}
	if creator.calls != 1 {
		t.Fatalf("expected creator to be called once")
	}
}

func TestDiscoverAndEnsureRoomsSkipsMissingID(t *testing.T) {
	lister := &fakeLister{conversations: []model.RemoteConversation{
		{ThreadProperties: model.ThreadProperties{OriginalThreadID: ""}},
		{ThreadProperties: model.ThreadProperties{OriginalThreadID: "thread-3", ProductThreadType: "GroupChat"}},
	}}
	store := &fakeStore{rooms: map[string]id.RoomID{}}
	creator := &fakeCreator{roomID: "!created:example.org"}
	rooms := NewRoomsService(store, creator, zerolog.Nop())

	err := DiscoverAndEnsureRooms(context.Background(), "token123", lister, rooms, zerolog.Nop())
	if err != nil {
		t.Fatalf("DiscoverAndEnsureRooms failed: %v", err)
	}
	if creator.calls != 1 {
		t.Fatalf("expected creator to be called once, got %d", creator.calls)
	}
	if _, ok := store.rooms["thread-3"]; !ok {
		t.Fatalf("expected store to contain thread-3")
	}
}
