package database

import (
	"database/sql"
	"errors"

	"go.mau.fi/util/dbutil"
	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix/id"
)

type TeamsSendStatus string

const (
	TeamsSendStatusPending  TeamsSendStatus = "pending"
	TeamsSendStatusAccepted TeamsSendStatus = "accepted"
	TeamsSendStatusFailed   TeamsSendStatus = "failed"
)

type TeamsSendIntentQuery struct {
	db  *Database
	log log.Logger
}

// language=postgresql
const (
	teamsSendIntentSelect = "SELECT thread_id, client_message_id, timestamp, status, mxid, intent_mxid FROM teams_send_intent"
	teamsSendIntentInsert = "INSERT INTO teams_send_intent (thread_id, client_message_id, timestamp, status, mxid, intent_mxid) VALUES ($1, $2, $3, $4, $5, $6)"
	teamsSendIntentUpdate = "UPDATE teams_send_intent SET status=$1 WHERE client_message_id=$2"
	teamsSendIntentClear  = "UPDATE teams_send_intent SET intent_mxid='' WHERE client_message_id=$1"
)

func (tq *TeamsSendIntentQuery) New() *TeamsSendIntent {
	return &TeamsSendIntent{
		db:  tq.db,
		log: tq.log,
	}
}

func (tq *TeamsSendIntentQuery) GetByClientMessageID(clientMessageID string) *TeamsSendIntent {
	query := teamsSendIntentSelect + " WHERE client_message_id=$1"
	return tq.New().Scan(tq.db.QueryRow(query, clientMessageID))
}

func (tq *TeamsSendIntentQuery) Insert(intent *TeamsSendIntent) error {
	if intent == nil {
		return errors.New("missing intent")
	}
	if intent.ThreadID == "" {
		return errors.New("missing thread id")
	}
	if intent.ClientMessageID == "" {
		return errors.New("missing client message id")
	}
	if intent.Timestamp == 0 {
		return errors.New("missing timestamp")
	}
	if !intent.Status.Valid() {
		return errors.New("invalid status")
	}
	_, err := tq.db.Exec(teamsSendIntentInsert, intent.ThreadID, intent.ClientMessageID, intent.Timestamp, string(intent.Status), intent.MXID, intent.IntentMXID)
	if err != nil {
		tq.log.Warnfln("Failed to insert teams send intent %s: %v", intent.ClientMessageID, err)
	}
	return err
}

func (tq *TeamsSendIntentQuery) UpdateStatus(clientMessageID string, status TeamsSendStatus) error {
	if tq == nil || tq.db == nil {
		return errors.New("missing database")
	}
	if clientMessageID == "" {
		return errors.New("missing client message id")
	}
	if !status.Valid() {
		return errors.New("invalid status")
	}
	_, err := tq.db.Exec(teamsSendIntentUpdate, string(status), clientMessageID)
	if err != nil {
		tq.log.Warnfln("Failed to update teams send intent %s: %v", clientMessageID, err)
	}
	return err
}

func (tq *TeamsSendIntentQuery) ClearIntentMXID(clientMessageID string) error {
	if tq == nil || tq.db == nil {
		return errors.New("missing database")
	}
	if clientMessageID == "" {
		return errors.New("missing client message id")
	}
	_, err := tq.db.Exec(teamsSendIntentClear, clientMessageID)
	if err != nil {
		tq.log.Warnfln("Failed to clear teams send intent mxid %s: %v", clientMessageID, err)
	}
	return err
}

type TeamsSendIntent struct {
	db  *Database
	log log.Logger

	ThreadID        string
	ClientMessageID string
	Timestamp       int64
	Status          TeamsSendStatus
	MXID            id.EventID
	IntentMXID      id.UserID
}

func (t *TeamsSendIntent) Scan(row dbutil.Scannable) *TeamsSendIntent {
	var status string
	var timestamp sql.NullInt64
	if err := row.Scan(&t.ThreadID, &t.ClientMessageID, &timestamp, &status, &t.MXID, &t.IntentMXID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			t.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}
	if timestamp.Valid {
		t.Timestamp = timestamp.Int64
	}
	t.Status = TeamsSendStatus(status)
	return t
}

func (s TeamsSendStatus) Valid() bool {
	switch s {
	case TeamsSendStatusPending, TeamsSendStatusAccepted, TeamsSendStatusFailed:
		return true
	default:
		return false
	}
}
