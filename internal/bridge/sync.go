package teamsbridge

import (
	"context"
	"errors"

	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-teams/database"
)

type ThreadProgressStore interface {
	UpdateLastSequenceID(threadID string, seq string) error
}

type ThreadSyncer struct {
	Ingestor *MessageIngestor
	Store    ThreadProgressStore
	Log      zerolog.Logger
}

type SyncResult struct {
	MessagesIngested int
	LastSequenceID   string
	Advanced         bool
}

func (s *ThreadSyncer) SyncThread(ctx context.Context, thread *database.TeamsThread) (SyncResult, error) {
	if s == nil || s.Ingestor == nil {
		return SyncResult{}, errors.New("missing message ingestor")
	}
	if s.Store == nil {
		return SyncResult{}, errors.New("missing thread progress store")
	}
	if thread == nil {
		return SyncResult{}, errors.New("missing thread")
	}
	if thread.RoomID == "" {
		return SyncResult{}, errors.New("missing room id")
	}

	lastSeq := ""
	if thread.LastSequenceID != nil {
		lastSeq = *thread.LastSequenceID
	}
	conversationID := ""
	if thread.ConversationID != nil {
		conversationID = *thread.ConversationID
	}

	s.Log.Info().
		Str("thread_id", thread.ThreadID).
		Str("last_seq", lastSeq).
		Msg("teams sync start")

	ingestResult, err := s.Ingestor.IngestThread(ctx, thread.ThreadID, conversationID, thread.RoomID, thread.LastSequenceID)
	if err != nil {
		return SyncResult{}, err
	}

	if ingestResult.Advanced {
		if err := s.Store.UpdateLastSequenceID(thread.ThreadID, ingestResult.LastSequenceID); err != nil {
			s.Log.Error().
				Err(err).
				Str("thread_id", thread.ThreadID).
				Msg("failed to persist last_sequence_id")
			return SyncResult{}, err
		}
		persisted := ingestResult.LastSequenceID
		thread.LastSequenceID = &persisted
		lastSeq = ingestResult.LastSequenceID
	}

	s.Log.Info().
		Str("thread_id", thread.ThreadID).
		Str("new_last_seq", lastSeq).
		Msg("teams sync complete")

	return SyncResult{
		MessagesIngested: ingestResult.MessagesIngested,
		LastSequenceID:   lastSeq,
		Advanced:         ingestResult.Advanced,
	}, nil
}
