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

	User             *UserQuery
	Portal           *PortalQuery
	Puppet           *PuppetQuery
	Message          *MessageQuery
	Thread           *ThreadQuery
	Reaction         *ReactionQuery
	Guild            *GuildQuery
	Role             *RoleQuery
	File             *FileQuery
	TeamsThread      *TeamsThreadQuery
	TeamsProfile     *TeamsProfileQuery
	TeamsSendIntent  *TeamsSendIntentQuery
	TeamsMessageMap  *TeamsMessageMapQuery
	TeamsReactionMap *TeamsReactionMapQuery
}

func New(baseDB *dbutil.Database, log maulogger.Logger) *Database {
	db := &Database{Database: baseDB}
	db.UpgradeTable = upgrades.Table
	db.User = &UserQuery{
		db:  db,
		log: log.Sub("User"),
	}
	db.Portal = &PortalQuery{
		db:  db,
		log: log.Sub("Portal"),
	}
	db.Puppet = &PuppetQuery{
		db:  db,
		log: log.Sub("Puppet"),
	}
	db.Message = &MessageQuery{
		db:  db,
		log: log.Sub("Message"),
	}
	db.Thread = &ThreadQuery{
		db:  db,
		log: log.Sub("Thread"),
	}
	db.Reaction = &ReactionQuery{
		db:  db,
		log: log.Sub("Reaction"),
	}
	db.Guild = &GuildQuery{
		db:  db,
		log: log.Sub("Guild"),
	}
	db.Role = &RoleQuery{
		db:  db,
		log: log.Sub("Role"),
	}
	db.File = &FileQuery{
		db:  db,
		log: log.Sub("File"),
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
	db.TeamsReactionMap = &TeamsReactionMapQuery{
		db:  db,
		log: log.Sub("TeamsReactionMap"),
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
