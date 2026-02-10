package teamsbridge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type MatrixReactionSender interface {
	SendReactionAsTeamsUser(roomID id.RoomID, target id.EventID, key string, teamsUserID string) (id.EventID, error)
	RedactReactionAsTeamsUser(roomID id.RoomID, eventID id.EventID, teamsUserID string) error
}

type ReactionMapStore interface {
	GetByKey(threadID string, teamsMessageID string, teamsUserID string, reactionKey string) *database.ReactionMap
	ListByMessage(threadID string, teamsMessageID string) ([]*database.ReactionMap, error)
	Upsert(mapping *database.ReactionMap) error
	DeleteByKey(threadID string, teamsMessageID string, teamsUserID string, reactionKey string) error
}

type TeamsMessageMapLookup interface {
	GetByTeamsMessageID(threadID string, teamsMessageID string) *database.TeamsMessageMap
}

type TeamsReactionIngestor struct {
	Sender    MatrixReactionSender
	Messages  TeamsMessageMapLookup
	Reactions ReactionMapStore
	Log       zerolog.Logger
}

func (r *TeamsReactionIngestor) IngestMessageReactions(ctx context.Context, threadID string, roomID id.RoomID, msg model.RemoteMessage, targetMXID id.EventID) error {
	if r == nil || r.Sender == nil {
		return errors.New("missing reaction sender")
	}
	if r.Messages == nil {
		return errors.New("missing message map lookup")
	}
	if r.Reactions == nil {
		return errors.New("missing reaction map store")
	}
	if threadID == "" {
		return errors.New("missing thread id")
	}
	teamsMessageID := NormalizeTeamsReactionMessageID(msg.MessageID)
	if teamsMessageID == "" {
		teamsMessageID = NormalizeTeamsReactionMessageID(msg.SequenceID)
	}
	if teamsMessageID == "" {
		return errors.New("missing teams message id")
	}

	current, timestamps := buildReactionSet(threadID, teamsMessageID, msg.Reactions)
	existing, err := r.Reactions.ListByMessage(threadID, teamsMessageID)
	if err != nil {
		return err
	}

	existingMap := make(map[ReactionKey]*database.ReactionMap, len(existing))
	for _, state := range existing {
		if state == nil {
			continue
		}
		key, ok := NewReactionKey(state.ThreadID, state.TeamsMessageID, state.TeamsUserID, state.ReactionKey)
		if !ok {
			continue
		}
		existingMap[key] = state
	}

	target := targetMXID
	if target == "" {
		if mapping := r.Messages.GetByTeamsMessageID(threadID, teamsMessageID); mapping != nil {
			target = mapping.MXID
		}
	}

	for key := range current {
		if _, ok := existingMap[key]; ok {
			continue
		}
		if target == "" {
			r.Log.Warn().
				Str("thread_id", key.ThreadID).
				Str("teams_message_id", key.TeamsMessageID).
				Str("teams_user_id", key.TeamsUserID).
				Str("reaction_key", key.ReactionKey).
				Msg("reaction dropped: no target mxid")
			continue
		}
		emoji, ok := MapEmotionKeyToEmoji(key.ReactionKey)
		if !ok {
			r.Log.Info().
				Str("thread_id", key.ThreadID).
				Str("teams_message_id", key.TeamsMessageID).
				Str("teams_user_id", key.TeamsUserID).
				Str("reaction_key", key.ReactionKey).
				Msg("reaction dropped: unmapped emotion key")
			continue
		}
		log := r.Log.With().
			Str("thread_id", key.ThreadID).
			Str("teams_message_id", key.TeamsMessageID).
			Str("teams_user_id", key.TeamsUserID).
			Str("reaction_key", key.ReactionKey).
			Str("target_mxid", target.String()).
			Logger()
		if ts := timestamps[key]; ts != 0 {
			log = log.With().Int64("time_ms", ts).Logger()
		}
		log.Info().Msg("matrix reaction add attempt")
		eventID, err := r.Sender.SendReactionAsTeamsUser(roomID, target, emoji, key.TeamsUserID)
		if err != nil {
			log.Error().Err(err).Msg("matrix reaction add failed")
			continue
		}
		if err := r.Reactions.Upsert(&database.ReactionMap{
			ThreadID:              key.ThreadID,
			TeamsMessageID:        key.TeamsMessageID,
			TeamsUserID:           key.TeamsUserID,
			ReactionKey:           key.ReactionKey,
			MatrixRoomID:          roomID,
			MatrixTargetEventID:   target,
			MatrixReactionEventID: eventID,
			UpdatedTSMS:           time.Now().UTC().UnixMilli(),
		}); err != nil {
			log.Error().Err(err).Msg("failed to persist reaction map")
		} else {
			log.Info().Str("reaction_mxid", eventID.String()).Msg("matrix reaction added")
		}
	}

	for key, state := range existingMap {
		if _, ok := current[key]; ok {
			continue
		}
		if state.MatrixReactionEventID == "" {
			continue
		}
		log := r.Log.With().
			Str("thread_id", key.ThreadID).
			Str("teams_message_id", key.TeamsMessageID).
			Str("teams_user_id", key.TeamsUserID).
			Str("reaction_key", key.ReactionKey).
			Str("reaction_mxid", state.MatrixReactionEventID.String()).
			Logger()
		log.Info().Msg("matrix reaction remove attempt")
		if err := r.Sender.RedactReactionAsTeamsUser(roomID, state.MatrixReactionEventID, key.TeamsUserID); err != nil {
			if errors.Is(err, mautrix.MNotFound) {
				if deleteErr := r.Reactions.DeleteByKey(key.ThreadID, key.TeamsMessageID, key.TeamsUserID, key.ReactionKey); deleteErr != nil {
					log.Error().Err(deleteErr).Msg("failed to delete reaction map after m_not_found")
				}
				continue
			}
			log.Error().Err(err).Msg("matrix reaction remove failed")
			continue
		}
		if err := r.Reactions.DeleteByKey(key.ThreadID, key.TeamsMessageID, key.TeamsUserID, key.ReactionKey); err != nil {
			log.Error().Err(err).Msg("failed to delete reaction map")
		} else {
			log.Info().Msg("matrix reaction removed")
		}
	}

	return nil
}

func buildReactionSet(threadID string, teamsMessageID string, reactions []model.MessageReaction) (map[ReactionKey]struct{}, map[ReactionKey]int64) {
	current := map[ReactionKey]struct{}{}
	timestamps := map[ReactionKey]int64{}
	for _, reaction := range reactions {
		emotionKey := strings.TrimSpace(reaction.EmotionKey)
		if emotionKey == "" {
			continue
		}
		for _, user := range reaction.Users {
			key, ok := NewReactionKey(threadID, teamsMessageID, user.MRI, emotionKey)
			if !ok {
				continue
			}
			current[key] = struct{}{}
			if user.TimeMS != 0 {
				timestamps[key] = user.TimeMS
			}
		}
	}
	return current, timestamps
}
