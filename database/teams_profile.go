package database

import (
	"database/sql"
	"errors"
	"time"

	"go.mau.fi/util/dbutil"
	log "maunium.net/go/maulogger/v2"
)

type TeamsProfileQuery struct {
	db  *Database
	log log.Logger
}

// language=postgresql
const (
	teamsProfileSelect = "SELECT teams_user_id, display_name, last_seen_ts FROM teams_profile"
	teamsProfileInsert = `
		INSERT INTO teams_profile (teams_user_id, display_name, last_seen_ts)
		VALUES ($1, $2, $3)
		ON CONFLICT (teams_user_id) DO NOTHING
	`
)

func (tq *TeamsProfileQuery) New() *TeamsProfile {
	return &TeamsProfile{
		db:  tq.db,
		log: tq.log,
	}
}

func (tq *TeamsProfileQuery) GetByTeamsUserID(teamsUserID string) *TeamsProfile {
	query := teamsProfileSelect + " WHERE teams_user_id=$1"
	return tq.New().Scan(tq.db.QueryRow(query, teamsUserID))
}

func (tq *TeamsProfileQuery) InsertIfMissing(profile *TeamsProfile) (bool, error) {
	if profile == nil {
		return false, errors.New("missing profile")
	}
	res, err := tq.db.Exec(teamsProfileInsert, profile.TeamsUserID, profile.DisplayName, profile.LastSeenTS.UnixMilli())
	if err != nil {
		tq.log.Warnfln("Failed to insert teams profile %s: %v", profile.TeamsUserID, err)
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

type TeamsProfile struct {
	db  *Database
	log log.Logger

	TeamsUserID string
	DisplayName string
	LastSeenTS  time.Time
}

func (t *TeamsProfile) Scan(row dbutil.Scannable) *TeamsProfile {
	var lastSeenTS int64
	err := row.Scan(&t.TeamsUserID, &t.DisplayName, &lastSeenTS)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			t.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}
	t.LastSeenTS = time.UnixMilli(lastSeenTS).UTC()
	return t
}
