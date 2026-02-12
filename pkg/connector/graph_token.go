package connector

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mau.fi/mautrix-teams/internal/teams/auth"
)

func (c *TeamsClient) ensureValidGraphToken(ctx context.Context) error {
	if c == nil || c.Meta == nil || c.Login == nil {
		return errors.New("missing client/login metadata")
	}
	if c.Meta.GraphTokenValid(time.Now().UTC()) {
		return nil
	}
	refreshToken := strings.TrimSpace(c.Meta.RefreshToken)
	if refreshToken == "" {
		return errors.New("missing refresh token for graph token refresh")
	}

	authClient := auth.NewClient(nil)
	if c.Main != nil && strings.TrimSpace(c.Main.Config.ClientID) != "" {
		authClient.ClientID = strings.TrimSpace(c.Main.Config.ClientID)
	}

	refreshed, err := refreshAccessTokenForGraphScope(ctx, authClient, refreshToken)
	if err != nil {
		return err
	}
	if refreshed == nil || strings.TrimSpace(refreshed.GraphAccessToken) == "" || refreshed.GraphExpiresAt == 0 {
		return errors.New("graph token refresh succeeded but did not return graph access token")
	}

	if rt := strings.TrimSpace(refreshed.RefreshToken); rt != "" {
		c.Meta.RefreshToken = rt
	}
	c.Meta.GraphAccessToken = strings.TrimSpace(refreshed.GraphAccessToken)
	c.Meta.GraphExpiresAt = refreshed.GraphExpiresAt

	return c.Login.Save(ctx)
}

