package database

import (
	"database/sql"
	"errors"
	"time"

	"go.mau.fi/util/dbutil"
	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix/id"
)

type ReactionMapQuery struct {
	db  *Database
	log log.Logger
}

// language=postgresql
const (
	reactionMapSelect = `
		SELECT thread_id, teams_message_id, teams_user_id, reaction_key,
		       matrix_room_id, matrix_target_event_id, matrix_reaction_event_id, updated_ts_ms
		FROM reaction_map
	`
	reactionMapUpsert = `
		INSERT INTO reaction_map (
			thread_id, teams_message_id, teams_user_id, reaction_key,
			matrix_room_id, matrix_target_event_id, matrix_reaction_event_id, updated_ts_ms
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (thread_id, teams_message_id, teams_user_id, reaction_key) DO UPDATE
		    SET matrix_room_id=excluded.matrix_room_id,
		        matrix_target_event_id=excluded.matrix_target_event_id,
		        matrix_reaction_event_id=excluded.matrix_reaction_event_id,
		        updated_ts_ms=excluded.updated_ts_ms
	`
	reactionMapDeleteByKey = `
		DELETE FROM reaction_map
		WHERE thread_id=$1 AND teams_message_id=$2 AND teams_user_id=$3 AND reaction_key=$4
	`
)

func (rq *ReactionMapQuery) New() *ReactionMap {
	return &ReactionMap{db: rq.db, log: rq.log}
}

func (rq *ReactionMapQuery) GetByKey(threadID string, teamsMessageID string, teamsUserID string, reactionKey string) *ReactionMap {
	if rq == nil || rq.db == nil {
		return nil
	}
	if threadID == "" || teamsMessageID == "" || teamsUserID == "" || reactionKey == "" {
		return nil
	}
	query := reactionMapSelect + " WHERE thread_id=$1 AND teams_message_id=$2 AND teams_user_id=$3 AND reaction_key=$4"
	return rq.New().Scan(rq.db.QueryRow(query, threadID, teamsMessageID, teamsUserID, reactionKey))
}

func (rq *ReactionMapQuery) ListByMessage(threadID string, teamsMessageID string) ([]*ReactionMap, error) {
	if rq == nil || rq.db == nil {
		return nil, errors.New("missing database")
	}
	if threadID == "" {
		return nil, errors.New("missing thread id")
	}
	if teamsMessageID == "" {
		return nil, errors.New("missing teams message id")
	}
	query := reactionMapSelect + " WHERE thread_id=$1 AND teams_message_id=$2"
	rows, err := rq.db.Query(query, threadID, teamsMessageID)
	if err != nil || rows == nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ReactionMap
	for rows.Next() {
		out = append(out, rq.New().Scan(rows))
	}
	return out, rows.Err()
}

func (rq *ReactionMapQuery) GetByMatrixReaction(roomID id.RoomID, reactionEventID id.EventID) *ReactionMap {
	if rq == nil || rq.db == nil {
		return nil
	}
	if roomID == "" || reactionEventID == "" {
		return nil
	}
	query := reactionMapSelect + " WHERE matrix_room_id=$1 AND matrix_reaction_event_id=$2"
	return rq.New().Scan(rq.db.QueryRow(query, roomID, reactionEventID))
}

func (rq *ReactionMapQuery) Upsert(mapping *ReactionMap) error {
	if mapping == nil {
		return errors.New("missing mapping")
	}
	if mapping.ThreadID == "" {
		return errors.New("missing thread id")
	}
	if mapping.TeamsMessageID == "" {
		return errors.New("missing teams message id")
	}
	if mapping.TeamsUserID == "" {
		return errors.New("missing teams user id")
	}
	if mapping.ReactionKey == "" {
		return errors.New("missing reaction key")
	}
	if mapping.MatrixRoomID == "" {
		return errors.New("missing matrix room id")
	}
	if mapping.MatrixTargetEventID == "" {
		return errors.New("missing matrix target event id")
	}
	if mapping.MatrixReactionEventID == "" {
		return errors.New("missing matrix reaction event id")
	}
	if mapping.UpdatedTSMS == 0 {
		mapping.UpdatedTSMS = time.Now().UTC().UnixMilli()
	}
	_, err := rq.db.Exec(
		reactionMapUpsert,
		mapping.ThreadID,
		mapping.TeamsMessageID,
		mapping.TeamsUserID,
		mapping.ReactionKey,
		mapping.MatrixRoomID,
		mapping.MatrixTargetEventID,
		mapping.MatrixReactionEventID,
		mapping.UpdatedTSMS,
	)
	if err != nil {
		rq.log.Warnfln("Failed to upsert reaction map %s/%s/%s/%s: %v", mapping.ThreadID, mapping.TeamsMessageID, mapping.TeamsUserID, mapping.ReactionKey, err)
	}
	return err
}

func (rq *ReactionMapQuery) DeleteByKey(threadID string, teamsMessageID string, teamsUserID string, reactionKey string) error {
	if rq == nil || rq.db == nil {
		return errors.New("missing database")
	}
	if threadID == "" {
		return errors.New("missing thread id")
	}
	if teamsMessageID == "" {
		return errors.New("missing teams message id")
	}
	if teamsUserID == "" {
		return errors.New("missing teams user id")
	}
	if reactionKey == "" {
		return errors.New("missing reaction key")
	}
	_, err := rq.db.Exec(reactionMapDeleteByKey, threadID, teamsMessageID, teamsUserID, reactionKey)
	if err != nil {
		rq.log.Warnfln("Failed to delete reaction map %s/%s/%s/%s: %v", threadID, teamsMessageID, teamsUserID, reactionKey, err)
	}
	return err
}

type ReactionMap struct {
	db  *Database
	log log.Logger

	ThreadID              string
	TeamsMessageID        string
	TeamsUserID           string
	ReactionKey           string
	MatrixRoomID          id.RoomID
	MatrixTargetEventID   id.EventID
	MatrixReactionEventID id.EventID
	UpdatedTSMS           int64
}

func (m *ReactionMap) Scan(row dbutil.Scannable) *ReactionMap {
	if err := row.Scan(
		&m.ThreadID,
		&m.TeamsMessageID,
		&m.TeamsUserID,
		&m.ReactionKey,
		&m.MatrixRoomID,
		&m.MatrixTargetEventID,
		&m.MatrixReactionEventID,
		&m.UpdatedTSMS,
	); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			m.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}
	return m
}
