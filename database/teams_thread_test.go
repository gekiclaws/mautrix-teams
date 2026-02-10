package database

import (
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/maulogger/v2/maulogadapt"
)

func TestTeamsThreadUpsertPreservesProgressFieldsWhenUnset(t *testing.T) {
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

	conv := "conv-1"
	seq := "42"
	ts := int64(1700000000000)
	lastMsgID := "msg-42"
	thread := db.TeamsThread.New()
	thread.ThreadID = "thread-1"
	thread.RoomID = "!room:example.com"
	thread.ConversationID = &conv
	thread.LastSequenceID = &seq
	thread.LastMessageTS = &ts
	thread.LastMessageID = &lastMsgID
	if err := thread.Upsert(); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	update := db.TeamsThread.New()
	update.ThreadID = "thread-1"
	update.RoomID = "!room:example.com"
	if err := update.Upsert(); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	got := db.TeamsThread.GetByThreadID("thread-1")
	if got == nil {
		t.Fatalf("expected thread row to exist")
	}
	if got.LastSequenceID == nil || *got.LastSequenceID != seq {
		t.Fatalf("expected last_sequence_id %q to be preserved, got %#v", seq, got.LastSequenceID)
	}
	if got.LastMessageTS == nil || *got.LastMessageTS != ts {
		t.Fatalf("expected last_message_ts %d to be preserved, got %#v", ts, got.LastMessageTS)
	}
	if got.LastMessageID == nil || *got.LastMessageID != lastMsgID {
		t.Fatalf("expected last_message_id %q to be preserved, got %#v", lastMsgID, got.LastMessageID)
	}
	if got.ConversationID == nil || *got.ConversationID != conv {
		t.Fatalf("expected conversation_id %q to be preserved, got %#v", conv, got.ConversationID)
	}
}
