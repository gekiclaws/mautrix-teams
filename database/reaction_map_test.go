package database

import (
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/maulogger/v2/maulogadapt"
	"maunium.net/go/mautrix/id"
)

func TestReactionMapUpsertAndLookup(t *testing.T) {
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

	row := &ReactionMap{
		ThreadID:              "19:abc@thread.v2",
		TeamsMessageID:        "msg/123",
		TeamsUserID:           "8:live:alice",
		ReactionKey:           "like",
		MatrixRoomID:          id.RoomID("!room:example"),
		MatrixTargetEventID:   id.EventID("$target"),
		MatrixReactionEventID: id.EventID("$reaction"),
		UpdatedTSMS:           100,
	}
	if err := db.ReactionMap.Upsert(row); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	row.UpdatedTSMS = 200
	if err := db.ReactionMap.Upsert(row); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	got := db.ReactionMap.GetByKey("19:abc@thread.v2", "msg/123", "8:live:alice", "like")
	if got == nil {
		t.Fatalf("expected key lookup result")
	}
	if got.UpdatedTSMS != 200 {
		t.Fatalf("expected latest upsert to win, got %d", got.UpdatedTSMS)
	}

	gotByEvent := db.ReactionMap.GetByMatrixReaction("!room:example", "$reaction")
	if gotByEvent == nil {
		t.Fatalf("expected matrix event lookup result")
	}
	if gotByEvent.TeamsUserID != "8:live:alice" {
		t.Fatalf("unexpected teams user: %s", gotByEvent.TeamsUserID)
	}
}

func TestReactionMapListByMessage(t *testing.T) {
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

	rows := []*ReactionMap{
		{
			ThreadID:              "thread",
			TeamsMessageID:        "msg/1",
			TeamsUserID:           "8:u1",
			ReactionKey:           "like",
			MatrixRoomID:          "!room",
			MatrixTargetEventID:   "$target",
			MatrixReactionEventID: "$r1",
			UpdatedTSMS:           1,
		},
		{
			ThreadID:              "thread",
			TeamsMessageID:        "msg/1",
			TeamsUserID:           "8:u2",
			ReactionKey:           "like",
			MatrixRoomID:          "!room",
			MatrixTargetEventID:   "$target",
			MatrixReactionEventID: "$r2",
			UpdatedTSMS:           2,
		},
		{
			ThreadID:              "thread",
			TeamsMessageID:        "msg/2",
			TeamsUserID:           "8:u1",
			ReactionKey:           "like",
			MatrixRoomID:          "!room",
			MatrixTargetEventID:   "$target2",
			MatrixReactionEventID: "$r3",
			UpdatedTSMS:           3,
		},
	}
	for _, row := range rows {
		if err := db.ReactionMap.Upsert(row); err != nil {
			t.Fatalf("upsert failed: %v", err)
		}
	}

	list, err := db.ReactionMap.ListByMessage("thread", "msg/1")
	if err != nil {
		t.Fatalf("list by message failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(list))
	}
}
