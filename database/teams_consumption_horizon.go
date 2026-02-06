package database

import (
	"database/sql"
	"errors"

	"go.mau.fi/util/dbutil"
	log "maunium.net/go/maulogger/v2"
)

type TeamsConsumptionHorizonQuery struct {
	db  *Database
	log log.Logger
}

const (
	teamsConsumptionHorizonSelect = "SELECT thread_id, teams_user_id, last_read_ts FROM teams_consumption_horizon"
	teamsConsumptionHorizonUpsert = `
		INSERT INTO teams_consumption_horizon (thread_id, teams_user_id, last_read_ts)
		VALUES ($1, $2, $3)
		ON CONFLICT (thread_id, teams_user_id) DO UPDATE
		    SET last_read_ts=excluded.last_read_ts
	`
)

func (hq *TeamsConsumptionHorizonQuery) New() *TeamsConsumptionHorizon {
	return &TeamsConsumptionHorizon{
		db:  hq.db,
		log: hq.log,
	}
}

func (hq *TeamsConsumptionHorizonQuery) Get(threadID string, teamsUserID string) *TeamsConsumptionHorizon {
	if hq == nil || hq.db == nil {
		return nil
	}
	if threadID == "" || teamsUserID == "" {
		return nil
	}
	query := teamsConsumptionHorizonSelect + " WHERE thread_id=$1 AND teams_user_id=$2"
	return hq.New().Scan(hq.db.QueryRow(query, threadID, teamsUserID))
}

func (hq *TeamsConsumptionHorizonQuery) UpsertLastRead(threadID string, teamsUserID string, lastReadTS int64) error {
	if hq == nil || hq.db == nil {
		return errors.New("missing database")
	}
	if threadID == "" {
		return errors.New("missing thread id")
	}
	if teamsUserID == "" {
		return errors.New("missing teams user id")
	}
	if lastReadTS <= 0 {
		return errors.New("missing last read timestamp")
	}
	_, err := hq.db.Exec(teamsConsumptionHorizonUpsert, threadID, teamsUserID, lastReadTS)
	if err != nil {
		hq.log.Warnfln("Failed to upsert teams consumption horizon %s/%s: %v", threadID, teamsUserID, err)
	}
	return err
}

type TeamsConsumptionHorizon struct {
	db  *Database
	log log.Logger

	ThreadID    string
	TeamsUserID string
	LastReadTS  int64
}

func (h *TeamsConsumptionHorizon) Scan(row dbutil.Scannable) *TeamsConsumptionHorizon {
	if err := row.Scan(&h.ThreadID, &h.TeamsUserID, &h.LastReadTS); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			h.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}
	return h
}
