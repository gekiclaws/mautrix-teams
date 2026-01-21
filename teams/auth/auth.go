package auth

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-teams/teams"
)

// RunGraphAuthTest performs a client-credentials flow and calls one Graph endpoint.
func RunGraphAuthTest(ctx context.Context, log zerolog.Logger) error {
	creds, err := teams.LoadGraphCredentialsFromEnv(".env")
	if err != nil {
		return err
	}

	userID := os.Getenv(teams.EnvGraphUserID)
	if userID == "" {
		return fmt.Errorf("missing required env var: %s", teams.EnvGraphUserID)
	}

	client, err := teams.NewGraphClient(ctx, creds)
	if err != nil {
		return err
	}

	user, resp, err := client.GetUser(ctx, userID)
	if err != nil {
		return err
	}

	log.Info().
		Str("endpoint", resp.Endpoint).
		Int("status", resp.Status).
		Str("graph_user_id", user.ID).
		Str("display_name", user.DisplayName).
		Msg("Teams Graph auth test succeeded")
	return nil
}
