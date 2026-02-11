package connector

// TeamsUserLoginMetadata is persisted in bridgev2's user_login.metadata JSON column.
//
// It stores enough information to (re-)acquire a Teams skypetoken without redoing the full browser flow.
type TeamsUserLoginMetadata struct {
	RefreshToken         string `json:"refresh_token,omitempty"`
	AccessTokenExpiresAt int64  `json:"access_token_expires_at,omitempty"`

	SkypeToken          string `json:"skype_token,omitempty"`
	SkypeTokenExpiresAt int64  `json:"skype_token_expires_at,omitempty"`
	TeamsUserID         string `json:"teams_user_id,omitempty"`
}
