package connector

import (
	"strings"

	"maunium.net/go/mautrix/bridgev2/networkid"
)

func (c *TeamsClient) portalKey(threadID string) networkid.PortalKey {
	return networkid.PortalKey{
		ID:       networkid.PortalID(strings.TrimSpace(threadID)),
		Receiver: c.Login.ID,
	}
}

func teamsUserIDToNetworkUserID(teamsUserID string) networkid.UserID {
	return networkid.UserID(strings.TrimSpace(teamsUserID))
}
