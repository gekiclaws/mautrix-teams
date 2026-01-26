package database

import (
	"database/sql"
	"errors"

	"go.mau.fi/util/dbutil"
	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix/id"
)

type TeamsReactionStateQuery struct {
	db  *Database
	log log.Logger
}

// language=postgresql
const (
	teamsReactionStateSelect = "SELECT thread_id, teams_message_id, emotion_key, user_mri, matrix_event_id FROM teams_reaction"
	teamsReactionStateInsert = `
		INSERT INTO teams_reaction (thread_id, teams_message_id, emotion_key, user_mri, matrix_event_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (thread_id, teams_message_id, emotion_key, user_mri) DO NOTHING
	`
	teamsReactionStateDelete = `
		DELETE FROM teams_reaction
		WHERE thread_id=$1 AND teams_message_id=$2 AND emotion_key=$3 AND user_mri=$4
	`
)

func (rq *TeamsReactionStateQuery) New() *TeamsReactionState {
	return &TeamsReactionState{
		db:  rq.db,
		log: rq.log,
	}
}

func (rq *TeamsReactionStateQuery) ListByMessage(threadID string, teamsMessageID string) ([]*TeamsReactionState, error) {
	if rq == nil || rq.db == nil {
		return nil, errors.New("missing database")
	}
	if threadID == "" {
		return nil, errors.New("missing thread id")
	}
	if teamsMessageID == "" {
		return nil, errors.New("missing teams message id")
	}
	query := teamsReactionStateSelect + " WHERE thread_id=$1 AND teams_message_id=$2"
	rows, err := rq.db.Query(query, threadID, teamsMessageID)
	if err != nil || rows == nil {
		return nil, err
	}
	defer rows.Close()

	var states []*TeamsReactionState
	for rows.Next() {
		states = append(states, rq.New().Scan(rows))
	}
	return states, rows.Err()
}

func (rq *TeamsReactionStateQuery) Insert(state *TeamsReactionState) error {
	if state == nil {
		return errors.New("missing state")
	}
	if state.ThreadID == "" {
		return errors.New("missing thread id")
	}
	if state.TeamsMessageID == "" {
		return errors.New("missing teams message id")
	}
	if state.EmotionKey == "" {
		return errors.New("missing emotion key")
	}
	if state.UserMRI == "" {
		return errors.New("missing user mri")
	}
	if state.MatrixEventID == "" {
		return errors.New("missing matrix event id")
	}
	_, err := rq.db.Exec(teamsReactionStateInsert, state.ThreadID, state.TeamsMessageID, state.EmotionKey, state.UserMRI, state.MatrixEventID)
	if err != nil {
		rq.log.Warnfln("Failed to insert teams reaction state for %s/%s: %v", state.ThreadID, state.TeamsMessageID, err)
	}
	return err
}

func (rq *TeamsReactionStateQuery) Delete(threadID string, teamsMessageID string, emotionKey string, userMRI string) error {
	if rq == nil || rq.db == nil {
		return errors.New("missing database")
	}
	if threadID == "" {
		return errors.New("missing thread id")
	}
	if teamsMessageID == "" {
		return errors.New("missing teams message id")
	}
	if emotionKey == "" {
		return errors.New("missing emotion key")
	}
	if userMRI == "" {
		return errors.New("missing user mri")
	}
	_, err := rq.db.Exec(teamsReactionStateDelete, threadID, teamsMessageID, emotionKey, userMRI)
	if err != nil {
		rq.log.Warnfln("Failed to delete teams reaction state for %s/%s: %v", threadID, teamsMessageID, err)
	}
	return err
}

type TeamsReactionState struct {
	db  *Database
	log log.Logger

	ThreadID       string
	TeamsMessageID string
	EmotionKey     string
	UserMRI        string
	MatrixEventID  id.EventID
}

func (s *TeamsReactionState) Scan(row dbutil.Scannable) *TeamsReactionState {
	if err := row.Scan(&s.ThreadID, &s.TeamsMessageID, &s.EmotionKey, &s.UserMRI, &s.MatrixEventID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}
	return s
}
