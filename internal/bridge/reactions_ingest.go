package teamsbridge

import (
	"context"
	"errors"
	"strings"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type MatrixReactionSender interface {
	SendReaction(roomID id.RoomID, target id.EventID, key string) (id.EventID, error)
	Redact(roomID id.RoomID, eventID id.EventID) (id.EventID, error)
}

type BotMatrixReactionSender struct {
	Client *mautrix.Client
}

func (s *BotMatrixReactionSender) SendReaction(roomID id.RoomID, target id.EventID, key string) (id.EventID, error) {
	if s == nil || s.Client == nil {
		return "", errors.New("missing matrix client")
	}
	content := event.ReactionEventContent{
		RelatesTo: event.RelatesTo{
			Type:    event.RelAnnotation,
			EventID: target,
			Key:     key,
		},
	}
	wrapped := event.Content{
		Parsed: &content,
		Raw: map[string]any{
			"com.beeper.teams.ingested_reaction": true,
		},
	}
	resp, err := s.Client.SendMessageEvent(roomID, event.EventReaction, &wrapped)
	if err != nil {
		return "", err
	}
	return resp.EventID, nil
}

func (s *BotMatrixReactionSender) Redact(roomID id.RoomID, eventID id.EventID) (id.EventID, error) {
	if s == nil || s.Client == nil {
		return "", errors.New("missing matrix client")
	}
	resp, err := s.Client.RedactEvent(roomID, eventID)
	if err != nil {
		return "", err
	}
	return resp.EventID, nil
}

type TeamsReactionStateStore interface {
	ListByMessage(threadID string, teamsMessageID string) ([]*database.TeamsReactionState, error)
	Insert(state *database.TeamsReactionState) error
	Delete(threadID string, teamsMessageID string, emotionKey string, userMRI string) error
}

type TeamsMessageMapLookup interface {
	GetByTeamsMessageID(threadID string, teamsMessageID string) *database.TeamsMessageMap
}

type TeamsReactionIngestor struct {
	Sender    MatrixReactionSender
	Messages  TeamsMessageMapLookup
	Reactions TeamsReactionStateStore
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
		return errors.New("missing reaction state store")
	}
	if threadID == "" {
		return errors.New("missing thread id")
	}
	teamsMessageID := NormalizeTeamsReactionMessageID(msg.SequenceID)
	if teamsMessageID == "" {
		teamsMessageID = strings.TrimSpace(msg.MessageID)
	}
	if teamsMessageID == "" {
		return errors.New("missing teams message id")
	}

	current, timestamps := buildReactionSet(msg.Reactions)
	existing, err := r.Reactions.ListByMessage(threadID, teamsMessageID)
	if err != nil {
		return err
	}

	existingMap := make(map[string]*database.TeamsReactionState, len(existing))
	for _, state := range existing {
		if state == nil {
			continue
		}
		key := reactionKey(state.EmotionKey, state.UserMRI)
		if key == "" {
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

	for key, reaction := range current {
		if _, ok := existingMap[key]; ok {
			continue
		}
		if target == "" {
			r.Log.Info().
				Str("thread_id", threadID).
				Str("teams_message_id", teamsMessageID).
				Str("emotion_key", reaction.EmotionKey).
				Str("user_mri", reaction.UserMRI).
				Msg("reaction dropped: no target mxid")
			continue
		}
		emoji, ok := MapEmotionKeyToEmoji(reaction.EmotionKey)
		if !ok {
			r.Log.Info().
				Str("thread_id", threadID).
				Str("teams_message_id", teamsMessageID).
				Str("emotion_key", reaction.EmotionKey).
				Str("user_mri", reaction.UserMRI).
				Msg("reaction dropped: unmapped emotion key")
			continue
		}
		log := r.Log.With().
			Str("thread_id", threadID).
			Str("teams_message_id", teamsMessageID).
			Str("emotion_key", reaction.EmotionKey).
			Str("user_mri", reaction.UserMRI).
			Str("target_mxid", target.String()).
			Logger()
		if ts := timestamps[key]; ts != 0 {
			log = log.With().Int64("time_ms", ts).Logger()
		}
		log.Info().Msg("matrix reaction add attempt")
		eventID, err := r.Sender.SendReaction(roomID, target, emoji)
		if err != nil {
			log.Error().Err(err).Msg("matrix reaction add failed")
			continue
		}
		if err := r.Reactions.Insert(&database.TeamsReactionState{
			ThreadID:       threadID,
			TeamsMessageID: teamsMessageID,
			EmotionKey:     reaction.EmotionKey,
			UserMRI:        reaction.UserMRI,
			MatrixEventID:  eventID,
		}); err != nil {
			log.Error().Err(err).Msg("failed to persist teams reaction state")
		} else {
			log.Info().Str("reaction_mxid", eventID.String()).Msg("matrix reaction added")
		}
	}

	for key, state := range existingMap {
		if _, ok := current[key]; ok {
			continue
		}
		if state.MatrixEventID == "" {
			continue
		}
		log := r.Log.With().
			Str("thread_id", threadID).
			Str("teams_message_id", teamsMessageID).
			Str("emotion_key", state.EmotionKey).
			Str("user_mri", state.UserMRI).
			Str("reaction_mxid", state.MatrixEventID.String()).
			Logger()
		log.Info().Msg("matrix reaction remove attempt")
		if _, err := r.Sender.Redact(roomID, state.MatrixEventID); err != nil {
			log.Error().Err(err).Msg("matrix reaction remove failed")
			continue
		}
		if err := r.Reactions.Delete(threadID, teamsMessageID, state.EmotionKey, state.UserMRI); err != nil {
			log.Error().Err(err).Msg("failed to delete teams reaction state")
		} else {
			log.Info().Msg("matrix reaction removed")
		}
	}

	return nil
}

type reactionIdentity struct {
	EmotionKey string
	UserMRI    string
}

func buildReactionSet(reactions []model.MessageReaction) (map[string]reactionIdentity, map[string]int64) {
	current := map[string]reactionIdentity{}
	timestamps := map[string]int64{}
	for _, reaction := range reactions {
		emotionKey := strings.TrimSpace(reaction.EmotionKey)
		if emotionKey == "" {
			continue
		}
		for _, user := range reaction.Users {
			userMRI := strings.TrimSpace(user.MRI)
			if userMRI == "" {
				continue
			}
			key := reactionKey(emotionKey, userMRI)
			if key == "" {
				continue
			}
			current[key] = reactionIdentity{EmotionKey: emotionKey, UserMRI: userMRI}
			if user.TimeMS != 0 {
				timestamps[key] = user.TimeMS
			}
		}
	}
	return current, timestamps
}

func reactionKey(emotionKey string, userMRI string) string {
	emotionKey = strings.TrimSpace(emotionKey)
	userMRI = strings.TrimSpace(userMRI)
	if emotionKey == "" || userMRI == "" {
		return ""
	}
	return emotionKey + "\x00" + userMRI
}
