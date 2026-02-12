package teamsid

// Bridgev2 metadata types for mautrix-teams.
//
// These are stored in the bridgev2 database as JSON blobs and must remain
// backward-compatible. Keep fields optional and additive.

type PortalMetadata struct {
	// Intentionally empty for now.
}

type GhostMetadata struct {
	// Intentionally empty for now.
}

type MessageMetadata struct {
	// Intentionally empty for now.
}

type ReactionMetadata struct {
	// Intentionally empty for now.
}

// UserLoginMetadata is persisted in bridgev2's user_login.metadata JSON column.
//
// It stores enough information to (re-)acquire a Teams skypetoken without redoing the full browser flow.
type UserLoginMetadata struct {
	RefreshToken         string `json:"refresh_token,omitempty"`
	AccessTokenExpiresAt int64  `json:"access_token_expires_at,omitempty"`

	SkypeToken          string `json:"skype_token,omitempty"`
	SkypeTokenExpiresAt int64  `json:"skype_token_expires_at,omitempty"`
	TeamsUserID         string `json:"teams_user_id,omitempty"`
}
