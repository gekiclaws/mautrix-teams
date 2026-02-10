package main

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-teams/config"
	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/maulogger/v2/maulogadapt"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
)

func TestMXIDForTeamsVirtualUser_DeterministicScheme(t *testing.T) {
	br := &TeamsBridge{
		Config: &config.Config{
			BaseConfig: &bridgeconfig.BaseConfig{},
			Bridge:     config.BridgeConfig{},
		},
	}
	br.Config.Homeserver.Domain = "beeper.local"

	got := br.mxidForTeamsVirtualUser("8:live:.cid.af0c29ad04be1b79")
	want := "@sh-msteams_8=3alive=3a.cid.af0c29ad04be1b79:beeper.local"
	if got.String() != want {
		t.Fatalf("mxid mismatch: got %s want %s", got.String(), want)
	}
}

func TestTeamsVirtualUserDisplayName_FromProfile(t *testing.T) {
	baseDB, err := dbutil.NewWithDialect(":memory:", "sqlite3")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer baseDB.Close()

	log := zerolog.Nop()
	baseDB.Log = dbutil.ZeroLogger(log)
	db := database.New(baseDB, maulogadapt.ZeroAsMau(&log))
	if err := db.Upgrade(); err != nil {
		t.Fatalf("failed to upgrade database: %v", err)
	}
	inserted, err := db.TeamsProfile.InsertIfMissing(&database.TeamsProfile{
		TeamsUserID: "8:alice",
		DisplayName: "Alex",
		LastSeenTS:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("failed to insert teams profile: %v", err)
	}
	if !inserted {
		t.Fatalf("expected profile insert to affect rows")
	}

	br := &TeamsBridge{
		DB: db,
	}
	got := br.teamsVirtualUserDisplayName("8:alice")
	if got != "Alex" {
		t.Fatalf("unexpected displayname: %s", got)
	}
}

func TestTeamsVirtualUserDisplayName_FallbackAndSuffix(t *testing.T) {
	br := &TeamsBridge{}
	got := br.teamsVirtualUserDisplayName("8:live:.cid.af0c29ad04be1b79")
	if got != "8:live:.cid.af0c29ad04be1b79" {
		t.Fatalf("unexpected fallback displayname: %s", got)
	}
}
