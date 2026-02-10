package database

import (
	_ "embed"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/util/dbutil"
	_ "go.mau.fi/util/dbutil/litestream"
	"maunium.net/go/maulogger/v2"

	"go.mau.fi/mautrix-teams/database/upgrades"
)

type Database struct {
	*dbutil.Database

	User                    *UserQuery
	TeamsThread             *TeamsThreadQuery
	TeamsProfile            *TeamsProfileQuery
	TeamsSendIntent         *TeamsSendIntentQuery
	TeamsMessageMap         *TeamsMessageMapQuery
	ReactionMap             *ReactionMapQuery
	TeamsConsumptionHorizon *TeamsConsumptionHorizonQuery
}

func New(baseDB *dbutil.Database, log maulogger.Logger) *Database {
	db := &Database{Database: baseDB}
	db.UpgradeTable = upgrades.Table
	db.User = &UserQuery{
		db:  db,
		log: log.Sub("User"),
	}
	db.TeamsThread = &TeamsThreadQuery{
		db:  db,
		log: log.Sub("TeamsThread"),
	}
	db.TeamsProfile = &TeamsProfileQuery{
		db:  db,
		log: log.Sub("TeamsProfile"),
	}
	db.TeamsSendIntent = &TeamsSendIntentQuery{
		db:  db,
		log: log.Sub("TeamsSendIntent"),
	}
	db.TeamsMessageMap = &TeamsMessageMapQuery{
		db:  db,
		log: log.Sub("TeamsMessageMap"),
	}
	db.ReactionMap = &ReactionMapQuery{
		db:  db,
		log: log.Sub("ReactionMap"),
	}
	db.TeamsConsumptionHorizon = &TeamsConsumptionHorizonQuery{
		db:  db,
		log: log.Sub("TeamsConsumptionHorizon"),
	}
	return db
}

func strPtr[T ~string](val T) *string {
	if val == "" {
		return nil
	}
	valStr := string(val)
	return &valStr
}
