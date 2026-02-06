package teamsbridge

import (
	"context"
	"errors"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
)

type TeamsConsumerIngestor struct {
	Syncer       *ThreadSyncer
	ReadReceipts ReadReceiptIngestor
	Log          zerolog.Logger
}

func (i *TeamsConsumerIngestor) PollOnce(ctx context.Context, thread *database.TeamsThread) (SyncResult, error) {
	if i == nil || i.Syncer == nil {
		return SyncResult{}, errors.New("missing thread syncer")
	}
	if thread == nil {
		return SyncResult{}, errors.New("missing thread")
	}
	result, err := i.Syncer.SyncThread(ctx, thread)
	if i.ReadReceipts != nil {
		if receiptErr := i.ReadReceipts.PollOnce(ctx, thread.ThreadID, thread.RoomID); receiptErr != nil {
			i.Log.Warn().
				Err(receiptErr).
				Str("thread_id", thread.ThreadID).
				Str("room_id", thread.RoomID.String()).
				Msg("teams consumption horizons ingest failed")
		}
	}
	return result, err
}

type ReadReceiptIngestor interface {
	PollOnce(ctx context.Context, threadID string, roomID id.RoomID) error
}
