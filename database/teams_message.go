package database

import (
	"database/sql"
	"errors"

	"go.mau.fi/util/dbutil"
	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix/id"
)

type TeamsMessageMapQuery struct {
	db  *Database
	log log.Logger
}

// language=postgresql
const (
	teamsMessageMapSelect = "SELECT mxid, thread_id, teams_message_id FROM teams_message_map"
	teamsMessageMapUpsert = `
		INSERT INTO teams_message_map (mxid, thread_id, teams_message_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (mxid) DO UPDATE
		    SET thread_id=excluded.thread_id,
		        teams_message_id=excluded.teams_message_id
	`
)

func (mq *TeamsMessageMapQuery) New() *TeamsMessageMap {
	return &TeamsMessageMap{
		db:  mq.db,
		log: mq.log,
	}
}

func (mq *TeamsMessageMapQuery) GetByMXID(mxid id.EventID) *TeamsMessageMap {
	query := teamsMessageMapSelect + " WHERE mxid=$1"
	return mq.New().Scan(mq.db.QueryRow(query, mxid))
}

func (mq *TeamsMessageMapQuery) GetByTeamsMessageID(threadID string, teamsMessageID string) *TeamsMessageMap {
	if mq == nil || mq.db == nil {
		return nil
	}
	if threadID == "" || teamsMessageID == "" {
		return nil
	}
	query := teamsMessageMapSelect + " WHERE thread_id=$1 AND teams_message_id=$2"
	return mq.New().Scan(mq.db.QueryRow(query, threadID, teamsMessageID))
}

func (mq *TeamsMessageMapQuery) Upsert(mapping *TeamsMessageMap) error {
	if mapping == nil {
		return errors.New("missing mapping")
	}
	if mapping.MXID == "" {
		return errors.New("missing mxid")
	}
	if mapping.ThreadID == "" {
		return errors.New("missing thread id")
	}
	if mapping.TeamsMessageID == "" {
		return errors.New("missing teams message id")
	}
	_, err := mq.db.Exec(teamsMessageMapUpsert, mapping.MXID, mapping.ThreadID, mapping.TeamsMessageID)
	if err != nil {
		mq.log.Warnfln("Failed to upsert teams message map %s: %v", mapping.MXID, err)
	}
	return err
}

type TeamsMessageMap struct {
	db  *Database
	log log.Logger

	MXID           id.EventID
	ThreadID       string
	TeamsMessageID string
}

func (m *TeamsMessageMap) Scan(row dbutil.Scannable) *TeamsMessageMap {
	if err := row.Scan(&m.MXID, &m.ThreadID, &m.TeamsMessageID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			m.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}
	return m
}
