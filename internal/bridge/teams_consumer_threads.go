package teamsbridge

import (
	"context"
	"errors"

	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-teams/internal/teams/client"
	"go.mau.fi/mautrix-teams/internal/teams/model"
)

// TeamsThreadDiscoverer performs remote discovery and normalization only.
// It does not write to the database or create rooms.
type TeamsThreadDiscoverer struct {
	Lister ConversationLister
	Token  string
	Log    zerolog.Logger
}

// Discover lists conversations, normalizes them into threads, and filters out
// entries that are missing a thread ID.
func (d *TeamsThreadDiscoverer) Discover(ctx context.Context) ([]model.Thread, error) {
	if d == nil || d.Lister == nil {
		return nil, errors.New("missing conversation lister")
	}

	convos, err := d.Lister.ListConversations(ctx, d.Token)
	if err != nil {
		var convErr client.ConversationsError
		if errors.As(err, &convErr) {
			d.Log.Error().
				Int("status", convErr.Status).
				Str("body_snippet", convErr.BodySnippet).
				Msg("failed to list conversations")
		} else {
			d.Log.Error().Err(err).Msg("failed to list conversations")
		}
		return nil, err
	}

	threads := make([]model.Thread, 0, len(convos))
	for _, conv := range convos {
		thread, ok := conv.Normalize()
		if !ok {
			d.Log.Warn().Msg("skipping conversation with missing thread_id")
			continue
		}
		threads = append(threads, thread)
	}

	return threads, nil
}
