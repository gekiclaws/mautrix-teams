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

func (s *ThreadSyncer) SyncThread(ctx context.Context, thread *database.TeamsThread) error {
	if s == nil || s.Ingestor == nil {
		return errors.New("missing message ingestor")
	}
	if s.Store == nil {
		return errors.New("missing thread progress store")
	}
	if thread == nil {
		return errors.New("missing thread")
	}
	if thread.RoomID == "" {
		return errors.New("missing room id")
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

	newSeq, advanced, err := s.Ingestor.IngestThread(ctx, thread.ThreadID, conversationID, thread.RoomID, thread.LastSequenceID)
	if err != nil {
		s.Log.Error().
			Err(err).
			Str("thread_id", thread.ThreadID).
			Msg("teams sync failed")
		return nil
	}

	if advanced {
		if err := s.Store.UpdateLastSequenceID(thread.ThreadID, newSeq); err != nil {
			s.Log.Error().
				Err(err).
				Str("thread_id", thread.ThreadID).
				Msg("failed to persist last_sequence_id")
			return err
		}
		lastSeq = newSeq
	}

	s.Log.Info().
		Str("thread_id", thread.ThreadID).
		Str("new_last_seq", lastSeq).
		Msg("teams sync complete")

	return nil
}
