package database

import (
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/maulogger/v2/maulogadapt"
	"maunium.net/go/mautrix/id"
)

func TestTeamsMessageGetLatestInboundBeforeUsesSelfAuthoredMessagesOnly(t *testing.T) {
	baseDB, err := dbutil.NewWithDialect(":memory:", "sqlite3")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer baseDB.Close()

	log := zerolog.Nop()
	baseDB.Log = dbutil.ZeroLogger(log)
	db := New(baseDB, maulogadapt.ZeroAsMau(&log))
	if err := db.Upgrade(); err != nil {
		t.Fatalf("failed to upgrade database: %v", err)
	}

	selfSender := "8:self"
	cases := []TeamsMessageMap{
		{
			MXID:           id.EventID("$self-early"),
			ThreadID:       "19:abc@thread.v2",
			TeamsMessageID: "self-early",
			MessageTS:      int64Ptr(100),
			SenderID:       &selfSender,
		},
		{
			MXID:           id.EventID("$unknown-late"),
			ThreadID:       "19:abc@thread.v2",
			TeamsMessageID: "unknown-late",
			MessageTS:      int64Ptr(200),
		},
		{
			MXID:           id.EventID("$remote-mid"),
			ThreadID:       "19:abc@thread.v2",
			TeamsMessageID: "remote-mid",
			MessageTS:      int64Ptr(150),
			SenderID:       strPtr("8:remote"),
		},
		{
			MXID:           id.EventID("$self-late"),
			ThreadID:       "19:abc@thread.v2",
			TeamsMessageID: "self-late",
			MessageTS:      int64Ptr(180),
			SenderID:       &selfSender,
		},
	}
	for i := range cases {
		msg := cases[i]
		if err := db.TeamsMessageMap.Upsert(&msg); err != nil {
			t.Fatalf("upsert %d failed: %v", i, err)
		}
	}

	got := db.TeamsMessageMap.GetLatestInboundBefore("19:abc@thread.v2", 220, "8:self")
	if got == nil {
		t.Fatalf("expected message map match")
	}
	if got.MXID != "$self-late" {
		t.Fatalf("expected latest self-authored event, got %s", got.MXID)
	}

	// If self-authored rows are absent, no target should be selected.
	if _, err := db.Exec("DELETE FROM teams_message_map WHERE sender_id=$1", selfSender); err != nil {
		t.Fatalf("delete self-authored rows failed: %v", err)
	}
	got = db.TeamsMessageMap.GetLatestInboundBefore("19:abc@thread.v2", 220, "8:self")
	if got != nil {
		t.Fatalf("expected nil when no self-authored rows remain, got %s", got.MXID)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
