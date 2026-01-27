package teamsbridge

import (
	"context"
	"errors"

	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-teams/database"
)

type TeamsConsumerIngestor struct {
	Syncer *ThreadSyncer
	Log    zerolog.Logger
}

func (i *TeamsConsumerIngestor) PollOnce(ctx context.Context, thread *database.TeamsThread) (SyncResult, error) {
	if i == nil || i.Syncer == nil {
		return SyncResult{}, errors.New("missing thread syncer")
	}
	if thread == nil {
		return SyncResult{}, errors.New("missing thread")
	}
	return i.Syncer.SyncThread(ctx, thread)
}
