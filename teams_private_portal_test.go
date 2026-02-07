package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-teams/config"
	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/database/upgrades"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type privatePortalMatrixMock struct {
	mu                sync.Mutex
	joinCalls         int
	sendCalls         int
	joinedMembersCall int

	failJoin              bool
	failSend              bool
	failJoinAlreadyJoined bool
	failJoinedMembers     bool
	joinedMembers         []id.UserID
}

func (m *privatePortalMatrixMock) handler(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.Contains(r.URL.Path, "/joined_members"):
		m.mu.Lock()
		m.joinedMembersCall++
		fail := m.failJoinedMembers
		members := append([]id.UserID(nil), m.joinedMembers...)
		m.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errcode":"M_UNKNOWN","error":"joined_members failed"}`))
			return
		}
		if len(members) == 0 {
			members = []id.UserID{"@alice:example.com", "@sh-msteamsbot:example.com"}
		}
		joined := make(map[id.UserID]event.MemberEventContent, len(members))
		for _, mxid := range members {
			joined[mxid] = event.MemberEventContent{
				Membership: event.MembershipJoin,
			}
		}
		resp := map[string]any{"joined": joined}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	case strings.Contains(r.URL.Path, "/join"):
		m.mu.Lock()
		m.joinCalls++
		fail := m.failJoin
		alreadyJoined := m.failJoinAlreadyJoined
		m.mu.Unlock()
		if alreadyJoined {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errcode":"M_FORBIDDEN","error":"You are already joined to this room."}`))
			return
		}
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errcode":"M_UNKNOWN","error":"join failed"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"room_id":"!dm:example.com"}`))
	case strings.Contains(r.URL.Path, "/send/m.room.message/"):
		m.mu.Lock()
		m.sendCalls++
		fail := m.failSend
		m.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errcode":"M_UNKNOWN","error":"send failed"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"event_id":"$evt"}`))
	default:
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}
}

func (m *privatePortalMatrixMock) counts() (join, send, joinedMembers int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.joinCalls, m.sendCalls, m.joinedMembersCall
}

func newTeamsBridgeForPrivatePortalTest(t *testing.T, matrix *privatePortalMatrixMock) (*TeamsBridge, *User, id.RoomID) {
	t.Helper()

	rawDB, err := dbutil.NewWithDialect(":memory:", "sqlite3")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	rawDB.UpgradeTable = upgrades.Table
	rawDB.Log = dbutil.ZeroLogger(zerolog.Nop())
	if err = rawDB.Upgrade(); err != nil {
		t.Fatalf("failed to upgrade db: %v", err)
	}
	t.Cleanup(func() {
		_ = rawDB.Close()
	})

	appserviceLog := zerolog.Nop()
	server := httptest.NewServer(http.HandlerFunc(matrix.handler))
	t.Cleanup(server.Close)

	as := appservice.Create()
	as.Log = appserviceLog
	as.HomeserverDomain = "example.com"
	as.Registration = &appservice.Registration{
		AppToken:        "as-token",
		SenderLocalpart: "sh-msteamsbot",
	}
	if err = as.SetHomeserverURL(server.URL); err != nil {
		t.Fatalf("failed to set homeserver URL: %v", err)
	}

	bot := as.NewIntentAPI("sh-msteamsbot")
	as.StateStore.MarkRegistered(bot.UserID)

	bridgeLog := zerolog.Nop()
	br := &TeamsBridge{
		Config:          &config.Config{},
		DB:              database.New(rawDB, maulogger.Create().Sub("TestDB")),
		usersByMXID:     make(map[id.UserID]*User),
		usersByID:       make(map[string]*User),
		managementRooms: make(map[id.RoomID]*User),
		Bridge: bridge.Bridge{
			Bot: bot,
			ZLog: &bridgeLog,
		},
	}

	dbUser := br.DB.User.New()
	dbUser.MXID = id.UserID("@alice:example.com")
	dbUser.Insert()

	user := &User{
		User:   dbUser,
		bridge: br,
		PermissionLevel: bridgeconfig.PermissionLevelUser,
	}
	br.usersByMXID[user.MXID] = user
	roomID := id.RoomID("!dm:example.com")
	return br, user, roomID
}

func TestClaimManagementRoomOnInviteSuccess(t *testing.T) {
	matrix := &privatePortalMatrixMock{}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)

	br.claimManagementRoom(br.Bot, roomID, user, "invite")

	if user.ManagementRoom != roomID {
		t.Fatalf("expected in-memory management room to be %s, got %s", roomID, user.ManagementRoom)
	}
	stored := br.DB.User.GetByMXID(user.MXID)
	if stored == nil {
		t.Fatalf("expected user to exist in db")
	}
	if stored.ManagementRoom != roomID {
		t.Fatalf("expected persisted management room to be %s, got %s", roomID, stored.ManagementRoom)
	}
	if br.managementRooms[roomID] != user {
		t.Fatalf("expected management room map to point to user")
	}

	joinCalls, sendCalls, joinedMembersCalls := matrix.counts()
	if joinCalls < 1 {
		t.Fatalf("expected at least one join attempt, got %d", joinCalls)
	}
	if sendCalls != 1 {
		t.Fatalf("expected one readiness message, got %d", sendCalls)
	}
	if joinedMembersCalls != 0 {
		t.Fatalf("expected no joined_members call, got %d", joinedMembersCalls)
	}
}

func TestClaimManagementRoomOnInviteJoinFailureShortCircuits(t *testing.T) {
	matrix := &privatePortalMatrixMock{failJoin: true}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)

	br.claimManagementRoom(br.Bot, roomID, user, "invite")

	if user.ManagementRoom != "" {
		t.Fatalf("expected no management room in memory, got %s", user.ManagementRoom)
	}
	stored := br.DB.User.GetByMXID(user.MXID)
	if stored == nil {
		t.Fatalf("expected user to exist in db")
	}
	if stored.ManagementRoom != "" {
		t.Fatalf("expected no persisted management room, got %s", stored.ManagementRoom)
	}
	if _, ok := br.managementRooms[roomID]; ok {
		t.Fatalf("management room map should remain unchanged on join failure")
	}

	joinCalls, sendCalls, joinedMembersCalls := matrix.counts()
	if joinCalls < 1 {
		t.Fatalf("expected at least one join attempt, got %d", joinCalls)
	}
	if sendCalls != 0 {
		t.Fatalf("expected no readiness message send on join failure, got %d", sendCalls)
	}
	if joinedMembersCalls != 0 {
		t.Fatalf("expected no joined_members call, got %d", joinedMembersCalls)
	}
}

func TestClaimManagementRoomOnInviteSendFailureStillPersists(t *testing.T) {
	matrix := &privatePortalMatrixMock{failSend: true}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)

	br.claimManagementRoom(br.Bot, roomID, user, "invite")

	if user.ManagementRoom != roomID {
		t.Fatalf("expected in-memory management room to be %s, got %s", roomID, user.ManagementRoom)
	}
	stored := br.DB.User.GetByMXID(user.MXID)
	if stored == nil {
		t.Fatalf("expected user to exist in db")
	}
	if stored.ManagementRoom != roomID {
		t.Fatalf("expected persisted management room to be %s, got %s", roomID, stored.ManagementRoom)
	}
	if br.managementRooms[roomID] != user {
		t.Fatalf("expected management room map to point to user")
	}

	joinCalls, sendCalls, joinedMembersCalls := matrix.counts()
	if joinCalls < 1 {
		t.Fatalf("expected at least one join attempt, got %d", joinCalls)
	}
	if sendCalls != 1 {
		t.Fatalf("expected one readiness message attempt, got %d", sendCalls)
	}
	if joinedMembersCalls != 0 {
		t.Fatalf("expected no joined_members call, got %d", joinedMembersCalls)
	}
}

func TestClaimManagementRoomAlreadyJoinedErrorStillPersists(t *testing.T) {
	matrix := &privatePortalMatrixMock{failJoinAlreadyJoined: true}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)

	br.claimManagementRoom(br.Bot, roomID, user, "invite")

	if user.ManagementRoom != roomID {
		t.Fatalf("expected in-memory management room to be %s, got %s", roomID, user.ManagementRoom)
	}
	stored := br.DB.User.GetByMXID(user.MXID)
	if stored == nil {
		t.Fatalf("expected user to exist in db")
	}
	if stored.ManagementRoom != roomID {
		t.Fatalf("expected persisted management room to be %s, got %s", roomID, stored.ManagementRoom)
	}
	joinCalls, sendCalls, joinedMembersCalls := matrix.counts()
	if joinCalls < 1 {
		t.Fatalf("expected at least one join attempt, got %d", joinCalls)
	}
	if sendCalls != 1 {
		t.Fatalf("expected one readiness message attempt, got %d", sendCalls)
	}
	if joinedMembersCalls < 1 {
		t.Fatalf("expected joined_members verification call, got %d", joinedMembersCalls)
	}
}

func TestHandleBotInviteManagementRoomClaimDirectInviteClaimsRoom(t *testing.T) {
	matrix := &privatePortalMatrixMock{}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)

	stateKey := br.Bot.UserID.String()
	evt := &event.Event{
		Type:    event.StateMember,
		RoomID:  roomID,
		Sender:  user.MXID,
		StateKey: &stateKey,
		Content: event.Content{
			Parsed: &event.MemberEventContent{
				Membership: event.MembershipInvite,
				IsDirect:   true,
			},
		},
	}

	br.handleBotInviteManagementRoomClaim(evt)

	stored := br.DB.User.GetByMXID(user.MXID)
	if stored == nil {
		t.Fatalf("expected user to exist in db")
	}
	if stored.ManagementRoom != roomID {
		t.Fatalf("expected persisted management room to be %s, got %s", roomID, stored.ManagementRoom)
	}
	joinCalls, sendCalls, joinedMembersCalls := matrix.counts()
	if joinCalls < 1 {
		t.Fatalf("expected at least one join attempt, got %d", joinCalls)
	}
	if sendCalls != 1 {
		t.Fatalf("expected one readiness message, got %d", sendCalls)
	}
	if joinedMembersCalls != 0 {
		t.Fatalf("expected no joined_members call, got %d", joinedMembersCalls)
	}
}

func TestHandleBotInviteManagementRoomClaimNonDirectInviteNoOp(t *testing.T) {
	matrix := &privatePortalMatrixMock{}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)

	stateKey := br.Bot.UserID.String()
	evt := &event.Event{
		Type:    event.StateMember,
		RoomID:  roomID,
		Sender:  user.MXID,
		StateKey: &stateKey,
		Content: event.Content{
			Parsed: &event.MemberEventContent{
				Membership: event.MembershipInvite,
				IsDirect:   false,
			},
		},
	}

	br.handleBotInviteManagementRoomClaim(evt)

	stored := br.DB.User.GetByMXID(user.MXID)
	if stored == nil {
		t.Fatalf("expected user to exist in db")
	}
	if stored.ManagementRoom != "" {
		t.Fatalf("expected no persisted management room, got %s", stored.ManagementRoom)
	}
	joinCalls, sendCalls, joinedMembersCalls := matrix.counts()
	if joinCalls != 0 {
		t.Fatalf("expected no join attempt, got %d", joinCalls)
	}
	if sendCalls != 0 {
		t.Fatalf("expected no readiness message, got %d", sendCalls)
	}
	if joinedMembersCalls != 0 {
		t.Fatalf("expected no joined_members call, got %d", joinedMembersCalls)
	}
}

func TestHandleImplicitDMManagementRoomClaimSuccess(t *testing.T) {
	matrix := &privatePortalMatrixMock{}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)
	matrix.joinedMembers = []id.UserID{user.MXID, br.Bot.UserID}

	evt := &event.Event{
		Type:   event.EventMessage,
		RoomID: roomID,
		Sender: user.MXID,
	}

	br.handleImplicitDMManagementRoomClaim(evt)

	stored := br.DB.User.GetByMXID(user.MXID)
	if stored == nil {
		t.Fatalf("expected user to exist in db")
	}
	if stored.ManagementRoom != roomID {
		t.Fatalf("expected persisted management room to be %s, got %s", roomID, stored.ManagementRoom)
	}
	joinCalls, sendCalls, joinedMembersCalls := matrix.counts()
	if joinCalls != 1 {
		t.Fatalf("expected one join attempt, got %d", joinCalls)
	}
	if joinedMembersCalls != 1 {
		t.Fatalf("expected one joined_members call, got %d", joinedMembersCalls)
	}
	if sendCalls != 1 {
		t.Fatalf("expected one readiness message, got %d", sendCalls)
	}
}

func TestHandleImplicitDMManagementRoomClaimNonDMNoClaim(t *testing.T) {
	matrix := &privatePortalMatrixMock{}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)
	matrix.joinedMembers = []id.UserID{user.MXID, br.Bot.UserID, "@charlie:example.com"}

	evt := &event.Event{
		Type:   event.EventMessage,
		RoomID: roomID,
		Sender: user.MXID,
	}

	br.handleImplicitDMManagementRoomClaim(evt)

	stored := br.DB.User.GetByMXID(user.MXID)
	if stored == nil {
		t.Fatalf("expected user to exist in db")
	}
	if stored.ManagementRoom != "" {
		t.Fatalf("expected no persisted management room, got %s", stored.ManagementRoom)
	}
	joinCalls, sendCalls, joinedMembersCalls := matrix.counts()
	if joinCalls != 1 {
		t.Fatalf("expected one join attempt, got %d", joinCalls)
	}
	if joinedMembersCalls != 1 {
		t.Fatalf("expected one joined_members call, got %d", joinedMembersCalls)
	}
	if sendCalls != 0 {
		t.Fatalf("expected no readiness message, got %d", sendCalls)
	}
}

func TestHandleImplicitDMManagementRoomClaimJoinFailureNoClaim(t *testing.T) {
	matrix := &privatePortalMatrixMock{failJoin: true}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)

	evt := &event.Event{
		Type:   event.EventMessage,
		RoomID: roomID,
		Sender: user.MXID,
	}

	br.handleImplicitDMManagementRoomClaim(evt)

	stored := br.DB.User.GetByMXID(user.MXID)
	if stored == nil {
		t.Fatalf("expected user to exist in db")
	}
	if stored.ManagementRoom != "" {
		t.Fatalf("expected no persisted management room, got %s", stored.ManagementRoom)
	}
	joinCalls, sendCalls, joinedMembersCalls := matrix.counts()
	if joinCalls != 1 {
		t.Fatalf("expected one join attempt, got %d", joinCalls)
	}
	if joinedMembersCalls != 0 {
		t.Fatalf("expected no joined_members call, got %d", joinedMembersCalls)
	}
	if sendCalls != 0 {
		t.Fatalf("expected no readiness message, got %d", sendCalls)
	}
}

func TestHandleImplicitDMManagementRoomClaimJoinedMembersFailureNoClaim(t *testing.T) {
	matrix := &privatePortalMatrixMock{failJoinedMembers: true}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)

	evt := &event.Event{
		Type:   event.EventMessage,
		RoomID: roomID,
		Sender: user.MXID,
	}

	br.handleImplicitDMManagementRoomClaim(evt)

	stored := br.DB.User.GetByMXID(user.MXID)
	if stored == nil {
		t.Fatalf("expected user to exist in db")
	}
	if stored.ManagementRoom != "" {
		t.Fatalf("expected no persisted management room, got %s", stored.ManagementRoom)
	}
	joinCalls, sendCalls, joinedMembersCalls := matrix.counts()
	if joinCalls != 1 {
		t.Fatalf("expected one join attempt, got %d", joinCalls)
	}
	if joinedMembersCalls != 1 {
		t.Fatalf("expected one joined_members call, got %d", joinedMembersCalls)
	}
	if sendCalls != 0 {
		t.Fatalf("expected no readiness message, got %d", sendCalls)
	}
}

func TestHandleImplicitDMManagementRoomClaimAlreadyHasManagementRoomNoOp(t *testing.T) {
	matrix := &privatePortalMatrixMock{}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)
	existingRoom := id.RoomID("!existing:example.com")
	user.SetManagementRoom(existingRoom)
	matrix.joinedMembers = []id.UserID{user.MXID, br.Bot.UserID}

	evt := &event.Event{
		Type:   event.EventMessage,
		RoomID: roomID,
		Sender: user.MXID,
	}

	br.handleImplicitDMManagementRoomClaim(evt)

	stored := br.DB.User.GetByMXID(user.MXID)
	if stored == nil {
		t.Fatalf("expected user to exist in db")
	}
	if stored.ManagementRoom != existingRoom {
		t.Fatalf("expected persisted management room to remain %s, got %s", existingRoom, stored.ManagementRoom)
	}
	joinCalls, sendCalls, joinedMembersCalls := matrix.counts()
	if joinCalls != 0 {
		t.Fatalf("expected no join attempts, got %d", joinCalls)
	}
	if joinedMembersCalls != 0 {
		t.Fatalf("expected no joined_members calls, got %d", joinedMembersCalls)
	}
	if sendCalls != 0 {
		t.Fatalf("expected no readiness messages, got %d", sendCalls)
	}
}
