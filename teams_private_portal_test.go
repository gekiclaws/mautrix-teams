package main

import (
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
	"maunium.net/go/mautrix/id"
)

type privatePortalMatrixMock struct {
	mu        sync.Mutex
	joinCalls int
	sendCalls int

	failJoin bool
	failSend bool
}

func (m *privatePortalMatrixMock) handler(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.Contains(r.URL.Path, "/join"):
		m.mu.Lock()
		m.joinCalls++
		fail := m.failJoin
		m.mu.Unlock()
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

func (m *privatePortalMatrixMock) counts() (join, send int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.joinCalls, m.sendCalls
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
	}
	roomID := id.RoomID("!dm:example.com")
	return br, user, roomID
}

func TestClaimManagementRoomOnInviteSuccess(t *testing.T) {
	matrix := &privatePortalMatrixMock{}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)

	br.claimManagementRoomOnInvite(br.Bot, roomID, user)

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

	joinCalls, sendCalls := matrix.counts()
	if joinCalls < 1 {
		t.Fatalf("expected at least one join attempt, got %d", joinCalls)
	}
	if sendCalls != 1 {
		t.Fatalf("expected one readiness message, got %d", sendCalls)
	}
}

func TestClaimManagementRoomOnInviteJoinFailureShortCircuits(t *testing.T) {
	matrix := &privatePortalMatrixMock{failJoin: true}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)

	br.claimManagementRoomOnInvite(br.Bot, roomID, user)

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

	joinCalls, sendCalls := matrix.counts()
	if joinCalls != 1 {
		t.Fatalf("expected exactly one join attempt, got %d", joinCalls)
	}
	if sendCalls != 0 {
		t.Fatalf("expected no readiness message send on join failure, got %d", sendCalls)
	}
}

func TestClaimManagementRoomOnInviteSendFailureStillPersists(t *testing.T) {
	matrix := &privatePortalMatrixMock{failSend: true}
	br, user, roomID := newTeamsBridgeForPrivatePortalTest(t, matrix)

	br.claimManagementRoomOnInvite(br.Bot, roomID, user)

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

	joinCalls, sendCalls := matrix.counts()
	if joinCalls < 1 {
		t.Fatalf("expected at least one join attempt, got %d", joinCalls)
	}
	if sendCalls != 1 {
		t.Fatalf("expected one readiness message attempt, got %d", sendCalls)
	}
}
