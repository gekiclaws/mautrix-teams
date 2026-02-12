package connector

import (
	"maunium.net/go/mautrix/bridgev2/database"

	"go.mau.fi/mautrix-teams/pkg/teamsid"
)

func (t *TeamsConnector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{
		Portal: func() any {
			return &teamsid.PortalMetadata{}
		},
		Ghost: func() any {
			return &teamsid.GhostMetadata{}
		},
		Message: func() any {
			return &teamsid.MessageMetadata{}
		},
		Reaction: func() any {
			return &teamsid.ReactionMetadata{}
		},
		UserLogin: func() any {
			return &teamsid.UserLoginMetadata{}
		},
	}
}
