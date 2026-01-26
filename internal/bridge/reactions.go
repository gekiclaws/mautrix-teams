package teamsbridge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/variationselector"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
)

type ReactionClient interface {
	AddReaction(ctx context.Context, threadID string, teamsMessageID string, emotionKey string, appliedAtMS int64) (int, error)
	RemoveReaction(ctx context.Context, threadID string, teamsMessageID string, emotionKey string) (int, error)
}

type TeamsMessageMapStore interface {
	GetByMXID(mxid id.EventID) *database.TeamsMessageMap
}

type TeamsReactionMapStore interface {
	GetByReactionMXID(mxid id.EventID) *database.TeamsReactionMap
	Insert(mapping *database.TeamsReactionMap) error
	Delete(reactionMXID id.EventID) error
}

type TeamsConsumerReactor struct {
	Client    ReactionClient
	Threads   ThreadLookup
	Messages  TeamsMessageMapStore
	Reactions TeamsReactionMapStore
	Log       zerolog.Logger
}

func NewTeamsConsumerReactor(client ReactionClient, threads ThreadLookup, messages TeamsMessageMapStore, reactions TeamsReactionMapStore, log zerolog.Logger) *TeamsConsumerReactor {
	return &TeamsConsumerReactor{
		Client:    client,
		Threads:   threads,
		Messages:  messages,
		Reactions: reactions,
		Log:       log,
	}
}

var emojiToEmotionKey = map[string]string{
	variationselector.FullyQualify("❤️"): "heart",
	variationselector.FullyQualify("👍"):  "like",
	variationselector.FullyQualify("😂"):  "laugh",
	variationselector.FullyQualify("😮"):  "surprised",
	variationselector.FullyQualify("😢"):  "sad",
	variationselector.FullyQualify("😡"):  "angry",
}

var emotionKeyToEmoji = func() map[string]string {
	inverse := make(map[string]string, len(emojiToEmotionKey))
	for emoji, key := range emojiToEmotionKey {
		if _, exists := inverse[key]; !exists {
			inverse[key] = emoji
		}
	}
	return inverse
}()

func MapEmojiToEmotionKey(emoji string) (string, bool) {
	if strings.TrimSpace(emoji) == "" {
		return "", false
	}
	normalized := variationselector.FullyQualify(emoji)
	key, ok := emojiToEmotionKey[normalized]
	return key, ok
}

func MapEmotionKeyToEmoji(emotionKey string) (string, bool) {
	emotionKey = strings.TrimSpace(emotionKey)
	if emotionKey == "" {
		return "", false
	}
	emoji, ok := emotionKeyToEmoji[emotionKey]
	return emoji, ok
}

func (r *TeamsConsumerReactor) AddMatrixReaction(ctx context.Context, roomID id.RoomID, evt *event.Event) error {
	if r == nil || r.Client == nil {
		return errors.New("missing teams reaction client")
	}
	if r.Threads == nil {
		return errors.New("missing thread lookup")
	}
	if r.Messages == nil {
		return errors.New("missing teams message map store")
	}
	if r.Reactions == nil {
		return errors.New("missing teams reaction map store")
	}
	if evt == nil {
		return errors.New("missing event")
	}
	threadID, ok := r.Threads.GetThreadID(roomID)
	if !ok || strings.TrimSpace(threadID) == "" {
		return errors.New("missing thread id")
	}
	if evt.Content.Parsed == nil {
		_ = evt.Content.ParseRaw(evt.Type)
	}
	reaction := evt.Content.AsReaction()
	if reaction == nil {
		return errors.New("missing reaction content")
	}
	if reaction.RelatesTo.Type != event.RelAnnotation {
		return errors.New("unsupported relation type")
	}

	emotionKey, ok := MapEmojiToEmotionKey(reaction.RelatesTo.Key)
	if !ok {
		r.Log.Info().
			Str("room_id", roomID.String()).
			Str("event_id", evt.ID.String()).
			Str("emoji", reaction.RelatesTo.Key).
			Msg("reaction dropped: unmapped emoji")
		return nil
	}

	mapping := r.Messages.GetByMXID(reaction.RelatesTo.EventID)
	if mapping == nil || mapping.TeamsMessageID == "" {
		r.Log.Info().
			Str("room_id", roomID.String()).
			Str("event_id", evt.ID.String()).
			Str("target_mxid", reaction.RelatesTo.EventID.String()).
			Msg("reaction dropped: no teams_message_id for target mxid")
		return nil
	}

	log := r.Log.With().
		Str("room_id", roomID.String()).
		Str("thread_id", threadID).
		Str("teams_message_id", mapping.TeamsMessageID).
		Str("emotion_key", emotionKey).
		Str("event_id", evt.ID.String()).
		Logger()
	log.Info().Msg("teams reaction add attempt")

	status, err := r.Client.AddReaction(ctx, threadID, mapping.TeamsMessageID, emotionKey, time.Now().UTC().UnixMilli())
	if status != 0 {
		log.Info().Int("status", status).Msg("teams reaction response")
	}
	if err != nil {
		log.Error().Err(err).Msg("teams reaction error")
		return err
	}

	if err := r.Reactions.Insert(&database.TeamsReactionMap{
		ReactionMXID: evt.ID,
		TargetMXID:   reaction.RelatesTo.EventID,
		EmotionKey:   emotionKey,
	}); err != nil {
		log.Error().Err(err).Msg("failed to persist teams reaction map")
	}

	return nil
}

func (r *TeamsConsumerReactor) RemoveMatrixReaction(ctx context.Context, roomID id.RoomID, evt *event.Event) error {
	if r == nil || r.Client == nil {
		return errors.New("missing teams reaction client")
	}
	if r.Threads == nil {
		return errors.New("missing thread lookup")
	}
	if r.Messages == nil {
		return errors.New("missing teams message map store")
	}
	if r.Reactions == nil {
		return errors.New("missing teams reaction map store")
	}
	if evt == nil {
		return errors.New("missing event")
	}
	threadID, ok := r.Threads.GetThreadID(roomID)
	if !ok || strings.TrimSpace(threadID) == "" {
		return errors.New("missing thread id")
	}

	if evt.Redacts == "" {
		return errors.New("missing redacts id")
	}
	reactionMap := r.Reactions.GetByReactionMXID(evt.Redacts)
	if reactionMap == nil {
		return nil
	}

	mapping := r.Messages.GetByMXID(reactionMap.TargetMXID)
	if mapping == nil || mapping.TeamsMessageID == "" {
		r.Log.Info().
			Str("room_id", roomID.String()).
			Str("event_id", evt.ID.String()).
			Str("target_mxid", reactionMap.TargetMXID.String()).
			Msg("reaction dropped: no teams_message_id for target mxid")
		return nil
	}

	log := r.Log.With().
		Str("room_id", roomID.String()).
		Str("thread_id", threadID).
		Str("teams_message_id", mapping.TeamsMessageID).
		Str("emotion_key", reactionMap.EmotionKey).
		Str("event_id", evt.ID.String()).
		Str("reaction_event_id", reactionMap.ReactionMXID.String()).
		Logger()
	log.Info().Msg("teams reaction remove attempt")

	status, err := r.Client.RemoveReaction(ctx, threadID, mapping.TeamsMessageID, reactionMap.EmotionKey)
	if status != 0 {
		log.Info().Int("status", status).Msg("teams reaction response")
	}
	if err != nil {
		log.Error().Err(err).Msg("teams reaction error")
		return err
	}

	if err := r.Reactions.Delete(reactionMap.ReactionMXID); err != nil {
		log.Error().Err(err).Msg("failed to delete teams reaction map")
	}

	return nil
}
