package teamsbridge

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/event"
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
	calls  atomic.Int32
	waitCh <-chan struct{}
}

func (c *fakeCreator) CreateRoom(thread model.Thread) (id.RoomID, error) {
	c.calls.Add(1)
	if c.waitCh != nil {
		<-c.waitCh
	}
	return c.roomID, nil
}

type inviteCall struct {
	roomID id.RoomID
	userID id.UserID
}

type fakeAdminInviter struct {
	calls     []inviteCall
	errByUser map[id.UserID]error
	resByUser map[id.UserID]string
}

func (f *fakeAdminInviter) EnsureInvited(roomID id.RoomID, userID id.UserID) (string, error) {
	f.calls = append(f.calls, inviteCall{roomID: roomID, userID: userID})
	if err, ok := f.errByUser[userID]; ok {
		return "", err
	}
	if res, ok := f.resByUser[userID]; ok {
		return res, nil
	}
	return "invited", nil
}

type fakeRoomStateReconciler struct {
	calls []id.RoomID
	err   error
}

func (f *fakeRoomStateReconciler) EnsureRoomState(roomID id.RoomID, adminMXIDs []id.UserID) (string, string, error) {
	f.calls = append(f.calls, roomID)
	if f.err != nil {
		return "set_shared", "set_failed", f.err
	}
	return "set_shared", "already_sufficient", nil
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
	inviter := &fakeAdminInviter{}
	reconciler := &fakeRoomStateReconciler{}
	rooms := NewRoomsService(store, creator, inviter, reconciler, []id.UserID{"@admin:example.org"}, zerolog.Nop())

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
	if creator.calls.Load() != 0 {
		t.Fatalf("expected creator not to be called")
	}
	if len(inviter.calls) != 1 {
		t.Fatalf("expected one admin invite ensure call, got %d", len(inviter.calls))
	}
	if inviter.calls[0].roomID != "!room:example.org" {
		t.Fatalf("unexpected room invite target: %s", inviter.calls[0].roomID)
	}
	if inviter.calls[0].userID != "@admin:example.org" {
		t.Fatalf("unexpected admin mxid: %s", inviter.calls[0].userID)
	}
	if len(reconciler.calls) != 1 || reconciler.calls[0] != "!room:example.org" {
		t.Fatalf("expected one state reconcile for existing room")
	}
}

func TestEnsureRoomCreates(t *testing.T) {
	store := &fakeStore{rooms: map[string]id.RoomID{}}
	creator := &fakeCreator{roomID: "!created:example.org"}
	inviter := &fakeAdminInviter{}
	reconciler := &fakeRoomStateReconciler{}
	rooms := NewRoomsService(store, creator, inviter, reconciler, []id.UserID{"@admin:example.org"}, zerolog.Nop())

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
	if creator.calls.Load() != 1 {
		t.Fatalf("expected creator to be called once")
	}
	if len(inviter.calls) != 1 {
		t.Fatalf("expected one admin invite ensure call, got %d", len(inviter.calls))
	}
	if inviter.calls[0].roomID != "!created:example.org" {
		t.Fatalf("unexpected room invite target: %s", inviter.calls[0].roomID)
	}
	if len(reconciler.calls) != 1 || reconciler.calls[0] != "!created:example.org" {
		t.Fatalf("expected one state reconcile for created room")
	}
}

func TestEnsureRoomInviteFailureNonFatal(t *testing.T) {
	store := &fakeStore{rooms: map[string]id.RoomID{"thread-1": "!room:example.org"}}
	creator := &fakeCreator{roomID: "!created:example.org"}
	inviter := &fakeAdminInviter{
		errByUser: map[id.UserID]error{
			"@admin:example.org": errors.New("invite failed"),
		},
	}
	rooms := NewRoomsService(store, creator, inviter, &fakeRoomStateReconciler{}, []id.UserID{"@admin:example.org"}, zerolog.Nop())

	roomID, created, err := rooms.EnsureRoom(model.Thread{ID: "thread-1"})
	if err != nil {
		t.Fatalf("EnsureRoom failed unexpectedly: %v", err)
	}
	if created {
		t.Fatalf("expected created=false")
	}
	if roomID != "!room:example.org" {
		t.Fatalf("unexpected room ID: %s", roomID)
	}
}

func TestEnsureRoomAlreadyInvitedNonFatal(t *testing.T) {
	store := &fakeStore{rooms: map[string]id.RoomID{"thread-1": "!room:example.org"}}
	creator := &fakeCreator{roomID: "!created:example.org"}
	inviter := &fakeAdminInviter{
		resByUser: map[id.UserID]string{
			"@admin:example.org": "already_invited",
		},
	}
	rooms := NewRoomsService(store, creator, inviter, &fakeRoomStateReconciler{}, []id.UserID{"@admin:example.org"}, zerolog.Nop())

	_, _, err := rooms.EnsureRoom(model.Thread{ID: "thread-1"})
	if err != nil {
		t.Fatalf("EnsureRoom failed unexpectedly: %v", err)
	}
}

func TestEnsureRoomStateReconcileFailureNonFatal(t *testing.T) {
	store := &fakeStore{rooms: map[string]id.RoomID{"thread-1": "!room:example.org"}}
	creator := &fakeCreator{roomID: "!created:example.org"}
	reconciler := &fakeRoomStateReconciler{err: errors.New("state update failed")}
	rooms := NewRoomsService(store, creator, &fakeAdminInviter{}, reconciler, []id.UserID{"@admin:example.org"}, zerolog.Nop())

	_, _, err := rooms.EnsureRoom(model.Thread{ID: "thread-1"})
	if err != nil {
		t.Fatalf("EnsureRoom failed unexpectedly: %v", err)
	}
}

func TestDiscoverAndEnsureRoomsSkipsMissingID(t *testing.T) {
	lister := &fakeLister{conversations: []model.RemoteConversation{
		{ThreadProperties: model.ThreadProperties{OriginalThreadID: ""}},
		{ThreadProperties: model.ThreadProperties{OriginalThreadID: "thread-3", ProductThreadType: "GroupChat"}},
	}}
	store := &fakeStore{rooms: map[string]id.RoomID{}}
	creator := &fakeCreator{roomID: "!created:example.org"}
	inviter := &fakeAdminInviter{}
	rooms := NewRoomsService(store, creator, inviter, &fakeRoomStateReconciler{}, []id.UserID{"@admin:example.org"}, zerolog.Nop())

	err := DiscoverAndEnsureRooms(context.Background(), "token123", lister, rooms, zerolog.Nop())
	if err != nil {
		t.Fatalf("DiscoverAndEnsureRooms failed: %v", err)
	}
	if creator.calls.Load() != 1 {
		t.Fatalf("expected creator to be called once, got %d", creator.calls.Load())
	}
	if _, ok := store.rooms["thread-3"]; !ok {
		t.Fatalf("expected store to contain thread-3")
	}
	if len(inviter.calls) != 1 {
		t.Fatalf("expected one admin invite ensure call, got %d", len(inviter.calls))
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
	inviter := &fakeAdminInviter{}
	rooms := NewRoomsService(store, creator, inviter, &fakeRoomStateReconciler{}, []id.UserID{"@admin:example.org"}, zerolog.Nop())

	discovered, regs, err := RefreshAndRegisterThreads(context.Background(), discoverer, store, rooms, zerolog.Nop())
	if err != nil {
		t.Fatalf("RefreshAndRegisterThreads failed: %v", err)
	}
	if discovered != 2 {
		t.Fatalf("unexpected discovered count: %d", discovered)
	}
	if creator.calls.Load() != 1 {
		t.Fatalf("expected creator to be called once, got %d", creator.calls.Load())
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
	if len(inviter.calls) != 2 {
		t.Fatalf("expected two admin invite ensure calls, got %d", len(inviter.calls))
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
	inviter := &fakeAdminInviter{}
	rooms := NewRoomsService(store, creator, inviter, &fakeRoomStateReconciler{}, []id.UserID{"@admin:example.org"}, zerolog.Nop())

	discovered, regs, err := RefreshAndRegisterThreads(context.Background(), discoverer, store, rooms, zerolog.Nop())
	if err != nil {
		t.Fatalf("RefreshAndRegisterThreads failed: %v", err)
	}
	if discovered != 2 {
		t.Fatalf("unexpected discovered count: %d", discovered)
	}
	if creator.calls.Load() != 0 {
		t.Fatalf("expected creator to not be called, got %d", creator.calls.Load())
	}
	if len(regs) != 0 {
		t.Fatalf("expected 0 registrations, got %d", len(regs))
	}
	if len(inviter.calls) != 2 {
		t.Fatalf("expected two admin invite ensure calls, got %d", len(inviter.calls))
	}
}

func TestResolveExplicitAdminMXIDs(t *testing.T) {
	perms := bridgeconfig.PermissionConfig{
		"*":                  bridgeconfig.PermissionLevelRelay,
		"example.org":        bridgeconfig.PermissionLevelAdmin,
		" @ops:example.org":  bridgeconfig.PermissionLevelAdmin,
		"@admin:example.org": bridgeconfig.PermissionLevelAdmin,
		"@user:example.org":  bridgeconfig.PermissionLevelUser,
	}

	admins := ResolveExplicitAdminMXIDs(perms)
	expected := []id.UserID{"@admin:example.org", "@ops:example.org"}
	if !reflect.DeepEqual(admins, expected) {
		t.Fatalf("unexpected admins: got %v, want %v", admins, expected)
	}
}

func TestEnsureRoomConcurrentCreateDedupes(t *testing.T) {
	store := &fakeStore{rooms: map[string]id.RoomID{}}
	createGate := make(chan struct{})
	creator := &fakeCreator{roomID: "!created:example.org", waitCh: createGate}
	rooms := NewRoomsService(store, creator, &fakeAdminInviter{}, &fakeRoomStateReconciler{}, nil, zerolog.Nop())

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)

	type result struct {
		roomID  id.RoomID
		created bool
		err     error
	}
	results := make(chan result, workers)
	for range workers {
		go func() {
			defer wg.Done()
			<-start
			roomID, created, err := rooms.EnsureRoom(model.Thread{ID: "thread-race"})
			results <- result{roomID: roomID, created: created, err: err}
		}()
	}

	close(start)
	deadline := time.Now().Add(2 * time.Second)
	for creator.calls.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for create call")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(createGate)
	wg.Wait()
	close(results)

	if creator.calls.Load() != 1 {
		t.Fatalf("expected one room create call, got %d", creator.calls.Load())
	}

	createdCount := 0
	for res := range results {
		if res.err != nil {
			t.Fatalf("EnsureRoom returned error: %v", res.err)
		}
		if res.roomID != "!created:example.org" {
			t.Fatalf("unexpected room ID: %s", res.roomID)
		}
		if res.created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("expected exactly one created=true result, got %d", createdCount)
	}
}

func TestEnsureAdminSendPermissionLevels(t *testing.T) {
	pl := &event.PowerLevelsEventContent{
		Users: map[id.UserID]int{
			"@admin:example.org": 0,
		},
		Events: map[string]int{
			event.EventMessage.String(): 10,
		},
	}

	changed := ensureAdminSendPermissionLevels(pl, []id.UserID{"@admin:example.org", "@ops:example.org"})
	if !changed {
		t.Fatalf("expected power levels to change")
	}
	if got := pl.GetUserLevel("@admin:example.org"); got != 10 {
		t.Fatalf("expected @admin level 10, got %d", got)
	}
	if got := pl.GetUserLevel("@ops:example.org"); got != 10 {
		t.Fatalf("expected @ops level 10, got %d", got)
	}
}

func TestRoomInitialStateIncludesHistoryAndPowerLevels(t *testing.T) {
	state := roomInitialState(bridgeconfig.EncryptionConfig{}, "@bot:example.org", []id.UserID{"@admin:example.org"})
	if len(state) < 2 {
		t.Fatalf("expected at least history visibility and power levels state events")
	}
	if state[0].Type != event.StateHistoryVisibility {
		t.Fatalf("expected first state event to be history visibility, got %s", state[0].Type.String())
	}
	if state[1].Type != event.StatePowerLevels {
		t.Fatalf("expected second state event to be power levels, got %s", state[1].Type.String())
	}
	pl, ok := state[1].Content.Parsed.(*event.PowerLevelsEventContent)
	if !ok {
		t.Fatalf("expected power levels content type")
	}
	if got := pl.Events[event.EventMessage.String()]; got != 0 {
		t.Fatalf("expected m.room.message power level 0, got %d", got)
	}
	if got := pl.Users["@admin:example.org"]; got != 0 {
		t.Fatalf("expected admin user level 0, got %d", got)
	}
	if got := pl.Users["@bot:example.org"]; got != 100 {
		t.Fatalf("expected creator user level 100, got %d", got)
	}
}
