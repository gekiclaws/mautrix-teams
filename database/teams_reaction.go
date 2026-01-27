package database

import (
	"database/sql"
	"errors"

	"go.mau.fi/util/dbutil"
	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix/id"
)

type TeamsReactionMapQuery struct {
	db  *Database
	log log.Logger
}

// language=postgresql
const (
	teamsReactionMapSelect = "SELECT reaction_mxid, target_mxid, emotion_key FROM teams_reaction_map"
	teamsReactionMapUpsert = `
		INSERT INTO teams_reaction_map (reaction_mxid, target_mxid, emotion_key)
		VALUES ($1, $2, $3)
		ON CONFLICT (reaction_mxid) DO UPDATE
		    SET target_mxid=excluded.target_mxid,
		        emotion_key=excluded.emotion_key
	`
	teamsReactionMapDelete = "DELETE FROM teams_reaction_map WHERE reaction_mxid=$1"
)

func (rq *TeamsReactionMapQuery) New() *TeamsReactionMap {
	return &TeamsReactionMap{
		db:  rq.db,
		log: rq.log,
	}
}

func (rq *TeamsReactionMapQuery) GetByReactionMXID(mxid id.EventID) *TeamsReactionMap {
	query := teamsReactionMapSelect + " WHERE reaction_mxid=$1"
	return rq.New().Scan(rq.db.QueryRow(query, mxid))
}

func (rq *TeamsReactionMapQuery) Insert(mapping *TeamsReactionMap) error {
	if mapping == nil {
		return errors.New("missing mapping")
	}
	if mapping.ReactionMXID == "" {
		return errors.New("missing reaction mxid")
	}
	if mapping.TargetMXID == "" {
		return errors.New("missing target mxid")
	}
	if mapping.EmotionKey == "" {
		return errors.New("missing emotion key")
	}
	_, err := rq.db.Exec(teamsReactionMapUpsert, mapping.ReactionMXID, mapping.TargetMXID, mapping.EmotionKey)
	if err != nil {
		rq.log.Warnfln("Failed to upsert teams reaction map %s: %v", mapping.ReactionMXID, err)
	}
	return err
}

func (rq *TeamsReactionMapQuery) Delete(reactionMXID id.EventID) error {
	if rq == nil || rq.db == nil {
		return errors.New("missing database")
	}
	if reactionMXID == "" {
		return errors.New("missing reaction mxid")
	}
	_, err := rq.db.Exec(teamsReactionMapDelete, reactionMXID)
	if err != nil {
		rq.log.Warnfln("Failed to delete teams reaction map %s: %v", reactionMXID, err)
	}
	return err
}

type TeamsReactionMap struct {
	db  *Database
	log log.Logger

	ReactionMXID id.EventID
	TargetMXID   id.EventID
	EmotionKey   string
}

func (m *TeamsReactionMap) Scan(row dbutil.Scannable) *TeamsReactionMap {
	if err := row.Scan(&m.ReactionMXID, &m.TargetMXID, &m.EmotionKey); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			m.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}
	return m
}
