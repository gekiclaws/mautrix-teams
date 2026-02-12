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

func isLikelyThreadID(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "@thread.v2") ||
		strings.Contains(normalized, "@thread.skype") ||
		strings.Contains(normalized, "@unq.gbl.spaces")
}
