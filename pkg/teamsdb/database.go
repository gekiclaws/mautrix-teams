package teamsdb

import (
	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-teams/pkg/teamsdb/upgrades"
)

type Database struct {
	*dbutil.Database

	ThreadState        *ThreadStateQuery
	Profile            *ProfileQuery
	ConsumptionHorizon *ConsumptionHorizonQuery
}

func New(bridgeID networkid.BridgeID, db *dbutil.Database, log zerolog.Logger) *Database {
	db = db.Child("teams_version", upgrades.Table, dbutil.ZeroLogger(log))
	return &Database{
		Database: db,
		ThreadState: &ThreadStateQuery{
			BridgeID: bridgeID,
			Database: db,
		},
		Profile: &ProfileQuery{
			BridgeID: bridgeID,
			Database: db,
		},
		ConsumptionHorizon: &ConsumptionHorizonQuery{
			BridgeID: bridgeID,
			Database: db,
		},
	}
}
