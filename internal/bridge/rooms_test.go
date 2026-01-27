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

func (s *fakeStore) Put(thread model.Thread, roomID id.RoomID) error {
	s.rooms[thread.ID] = roomID
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

func TestTeamsThreadDiscovererSkipsMissingThreadID(t *testing.T) {
	lister := &fakeLister{conversations: []model.RemoteConversation{
		{ThreadProperties: model.ThreadProperties{OriginalThreadID: ""}},
		{
			ID: " @oneToOne.skype ",
			ThreadProperties: model.ThreadProperties{
				OriginalThreadID:  " thread-1 ",
				ProductThreadType: "OneToOneChat",
			},
		},
	}}

	discoverer := &TeamsThreadDiscoverer{
		Lister: lister,
		Token:  "token123",
		Log:    zerolog.Nop(),
	}

	threads, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	if threads[0].ID != "thread-1" {
		t.Fatalf("unexpected thread ID: %q", threads[0].ID)
	}
}

func TestTeamsThreadDiscovererNormalizesThreadFields(t *testing.T) {
	lister := &fakeLister{conversations: []model.RemoteConversation{
		{
			ID: " @oneToOne.skype ",
			ThreadProperties: model.ThreadProperties{
				OriginalThreadID:  " thread-2 ",
				ProductThreadType: "OneToOneChat",
			},
		},
	}}

	discoverer := &TeamsThreadDiscoverer{
		Lister: lister,
		Token:  "token123",
		Log:    zerolog.Nop(),
	}

	threads, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}

	thread := threads[0]
	if thread.ID != "thread-2" {
		t.Fatalf("unexpected thread ID: %q", thread.ID)
	}
	if thread.ConversationID != "@oneToOne.skype" {
		t.Fatalf("unexpected conversation ID: %q", thread.ConversationID)
	}
	if !thread.IsOneToOne {
		t.Fatalf("expected IsOneToOne to be true")
	}
}

func TestRefreshAndRegisterThreadsRegistersOnlyNew(t *testing.T) {
	lister := &fakeLister{conversations: []model.RemoteConversation{
		{ThreadProperties: model.ThreadProperties{OriginalThreadID: "thread-1", ProductThreadType: "GroupChat"}},
		{ThreadProperties: model.ThreadProperties{OriginalThreadID: "thread-2", ProductThreadType: "GroupChat"}},
	}}
	discoverer := &TeamsThreadDiscoverer{
		Lister: lister,
		Token:  "token123",
		Log:    zerolog.Nop(),
	}

	store := &fakeStore{rooms: map[string]id.RoomID{
		"thread-1": "!existing:example.org",
	}}
	creator := &fakeCreator{roomID: "!created:example.org"}
	rooms := NewRoomsService(store, creator, zerolog.Nop())

	discovered, regs, err := RefreshAndRegisterThreads(context.Background(), discoverer, store, rooms, zerolog.Nop())
	if err != nil {
		t.Fatalf("RefreshAndRegisterThreads failed: %v", err)
	}
	if discovered != 2 {
		t.Fatalf("unexpected discovered count: %d", discovered)
	}
	if creator.calls != 1 {
		t.Fatalf("expected creator to be called once, got %d", creator.calls)
	}
	if len(regs) != 1 {
		t.Fatalf("expected 1 registration, got %d", len(regs))
	}
	if regs[0].Thread.ID != "thread-2" {
		t.Fatalf("unexpected registered thread: %q", regs[0].Thread.ID)
	}
	if regs[0].RoomID != "!created:example.org" {
		t.Fatalf("unexpected room id: %q", regs[0].RoomID)
	}
}

func TestRefreshAndRegisterThreadsNoNewThreadsNoCreates(t *testing.T) {
	lister := &fakeLister{conversations: []model.RemoteConversation{
		{ThreadProperties: model.ThreadProperties{OriginalThreadID: "thread-1", ProductThreadType: "GroupChat"}},
		{ThreadProperties: model.ThreadProperties{OriginalThreadID: "thread-2", ProductThreadType: "GroupChat"}},
	}}
	discoverer := &TeamsThreadDiscoverer{
		Lister: lister,
		Token:  "token123",
		Log:    zerolog.Nop(),
	}

	store := &fakeStore{rooms: map[string]id.RoomID{
		"thread-1": "!one:example.org",
		"thread-2": "!two:example.org",
	}}
	creator := &fakeCreator{roomID: "!created:example.org"}
	rooms := NewRoomsService(store, creator, zerolog.Nop())

	discovered, regs, err := RefreshAndRegisterThreads(context.Background(), discoverer, store, rooms, zerolog.Nop())
	if err != nil {
		t.Fatalf("RefreshAndRegisterThreads failed: %v", err)
	}
	if discovered != 2 {
		t.Fatalf("unexpected discovered count: %d", discovered)
	}
	if creator.calls != 0 {
		t.Fatalf("expected creator to not be called, got %d", creator.calls)
	}
	if len(regs) != 0 {
		t.Fatalf("expected 0 registrations, got %d", len(regs))
	}
}
