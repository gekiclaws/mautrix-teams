package database

import (
	"database/sql"
	"errors"

	"go.mau.fi/util/dbutil"
	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix/id"
)

type TeamsThreadQuery struct {
	db  *Database
	log log.Logger
}

const (
	teamsThreadSelect = "SELECT thread_id, room_id, last_sequence_id, last_message_ts, last_message_id FROM teams_thread"
	teamsThreadUpsert = `
		INSERT INTO teams_thread (thread_id, room_id, last_sequence_id, last_message_ts, last_message_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (thread_id) DO UPDATE
		    SET room_id=excluded.room_id,
		        last_sequence_id=excluded.last_sequence_id,
		        last_message_ts=excluded.last_message_ts,
		        last_message_id=excluded.last_message_id
	`
)

func (tq *TeamsThreadQuery) New() *TeamsThread {
	return &TeamsThread{
		db:  tq.db,
		log: tq.log,
	}
}

func (tq *TeamsThreadQuery) GetByThreadID(threadID string) *TeamsThread {
	row := tq.db.QueryRow(teamsThreadSelect+" WHERE thread_id=$1", threadID)
	if row == nil {
		return nil
	}
	return tq.New().Scan(row)
}

func (tq *TeamsThreadQuery) GetAll() []*TeamsThread {
	rows, err := tq.db.Query(teamsThreadSelect)
	if err != nil || rows == nil {
		if err != nil {
			tq.log.Errorfln("Failed to query teams threads: %v", err)
		}
		return nil
	}
	defer rows.Close()

	var threads []*TeamsThread
	for rows.Next() {
		thread := tq.New().Scan(rows)
		if thread != nil {
			threads = append(threads, thread)
		}
	}
	return threads
}

type TeamsThread struct {
	db  *Database
	log log.Logger

	ThreadID string
	RoomID   id.RoomID

	LastSequenceID *string
	LastMessageTS  *int64
	LastMessageID  *string
}

func (t *TeamsThread) Scan(row dbutil.Scannable) *TeamsThread {
	var roomID sql.NullString
	var lastSequenceID sql.NullString
	var lastMessageTS sql.NullInt64
	var lastMessageID sql.NullString

	err := row.Scan(&t.ThreadID, &roomID, &lastSequenceID, &lastMessageTS, &lastMessageID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			t.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}

	t.RoomID = id.RoomID(roomID.String)
	if lastSequenceID.Valid {
		val := lastSequenceID.String
		t.LastSequenceID = &val
	}
	if lastMessageTS.Valid {
		val := lastMessageTS.Int64
		t.LastMessageTS = &val
	}
	if lastMessageID.Valid {
		val := lastMessageID.String
		t.LastMessageID = &val
	}
	return t
}

func (t *TeamsThread) Upsert() error {
	_, err := t.db.Exec(teamsThreadUpsert, t.ThreadID, string(t.RoomID), nullableString(t.LastSequenceID), nullableInt64(t.LastMessageTS), nullableString(t.LastMessageID))
	if err != nil {
		t.log.Warnfln("Failed to upsert teams thread %s: %v", t.ThreadID, err)
	}
	return err
}

func nullableString(val *string) *string {
	if val == nil || *val == "" {
		return nil
	}
	return val
}

func nullableInt64(val *int64) *int64 {
	if val == nil {
		return nil
	}
	return val
}
