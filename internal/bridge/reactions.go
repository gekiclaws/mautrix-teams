package teamsbridge

import (
	"context"
	"errors"
	"strconv"
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
	variationselector.FullyQualify("👍🏻"): "like",
	variationselector.FullyQualify("👌🏻"): "ok",
	variationselector.FullyQualify("🔥"):  "fire",
	variationselector.FullyQualify("💙"):  "heartblue",

	// Page 1
	variationselector.FullyQualify("🙂"):  "smile",
	variationselector.FullyQualify("😄"):  "laugh",
	variationselector.FullyQualify("❤️"): "heart",
	variationselector.FullyQualify("😘"):  "kiss",
	variationselector.FullyQualify("☹️"): "sad",
	variationselector.FullyQualify("😛"):  "tongueout",
	variationselector.FullyQualify("😉"):  "wink",
	variationselector.FullyQualify("😢"):  "cry",
	variationselector.FullyQualify("😍"):  "inlove",
	variationselector.FullyQualify("🤗"):  "hug",
	variationselector.FullyQualify("😂"):  "cwl",
	variationselector.FullyQualify("💋"):  "lips",

	// Page 2
	variationselector.FullyQualify("😊"):  "blush",
	variationselector.FullyQualify("😮"):  "surprised",
	variationselector.FullyQualify("🐧"):  "penguin",
	variationselector.FullyQualify("👍"):  "like",
	variationselector.FullyQualify("😎"):  "cool",
	variationselector.FullyQualify("🤣"):  "rofl",
	variationselector.FullyQualify("🐱"):  "cat",
	variationselector.FullyQualify("🐵"):  "monkey",
	variationselector.FullyQualify("👋"):  "hi",
	variationselector.FullyQualify("❄️"): "snowangel",
	variationselector.FullyQualify("🌸"):  "flower",
	variationselector.FullyQualify("😁"):  "giggle",
	variationselector.FullyQualify("😈"):  "devil",
	variationselector.FullyQualify("🥳"):  "party",

	// Page 3
	variationselector.FullyQualify("😟"):    "worry",
	variationselector.FullyQualify("🍾"):    "champagne",
	variationselector.FullyQualify("☀️"):   "sun",
	variationselector.FullyQualify("⭐"):    "star",
	variationselector.FullyQualify("🐻‍❄️"): "polarbear",
	variationselector.FullyQualify("🙄"):    "eyeroll",
	variationselector.FullyQualify("😶"):    "speechless",
	variationselector.FullyQualify("🤔"):    "wonder",
	variationselector.FullyQualify("😠"):    "angry",
	variationselector.FullyQualify("🤮"):    "puke",
	variationselector.FullyQualify("🤦"):    "facepalm",
	variationselector.FullyQualify("😓"):    "sweat",
	variationselector.FullyQualify("🤡"):    "holidayspirit",
	variationselector.FullyQualify("😴"):    "sleepy",

	// Page 4
	variationselector.FullyQualify("🙇"): "bow",
	variationselector.FullyQualify("💄"): "makeup",
	variationselector.FullyQualify("💵"): "cash",
	variationselector.FullyQualify("🤐"): "lipssealed",
	variationselector.FullyQualify("🥶"): "shivering",
	variationselector.FullyQualify("🎂"): "cake",
	variationselector.FullyQualify("🤕"): "headbang",
	variationselector.FullyQualify("💃"): "dance",
	variationselector.FullyQualify("😳"): "wasntme",
	variationselector.FullyQualify("🤢"): "hungover",
	variationselector.FullyQualify("🥱"): "yawn",
	variationselector.FullyQualify("🎁"): "gift",
	variationselector.FullyQualify("😇"): "angel",
	variationselector.FullyQualify("🎄"): "xmastree",

	// Page 5
	variationselector.FullyQualify("💔"): "brokenheart",
	variationselector.FullyQualify("🤔"): "think",
	variationselector.FullyQualify("👏"): "clap",
	variationselector.FullyQualify("👊"): "punch",
	variationselector.FullyQualify("😒"): "envy",
	variationselector.FullyQualify("🤝"): "handshake",
	variationselector.FullyQualify("🙂"): "nod",
	variationselector.FullyQualify("🤓"): "nerdy",
	variationselector.FullyQualify("🖤"): "emo",
	variationselector.FullyQualify("💪"): "muscle",
	variationselector.FullyQualify("😋"): "mmm",
	variationselector.FullyQualify("🙌"): "highfive",
	variationselector.FullyQualify("🦃"): "turkey",
	variationselector.FullyQualify("📞"): "call",

	// Page 6
	variationselector.FullyQualify("🧔"):  "movember",
	variationselector.FullyQualify("🐶"):  "dog",
	variationselector.FullyQualify("☕"):  "coffee",
	variationselector.FullyQualify("👉"):  "poke",
	variationselector.FullyQualify("🤬"):  "swear",
	variationselector.FullyQualify("😑"):  "donttalktome",
	variationselector.FullyQualify("🤞"):  "fingerscrossed",
	variationselector.FullyQualify("🌈"):  "rainbow",
	variationselector.FullyQualify("🎧"):  "headphones",
	variationselector.FullyQualify("⏳"):  "waiting",
	variationselector.FullyQualify("🎉"):  "festiveparty",
	variationselector.FullyQualify("🥷"):  "bandit",
	variationselector.FullyQualify("🐿️"): "heidy",
	variationselector.FullyQualify("🍺"):  "beer",

	// Page 7
	variationselector.FullyQualify("🤦‍♂️"): "doh",
	variationselector.FullyQualify("💣"):    "bomb",
	variationselector.FullyQualify("😀"):    "happy",
	variationselector.FullyQualify("🥷"):    "ninja",
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

func NormalizeTeamsReactionMessageID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "msg/") || strings.Contains(value, "/") {
		return value
	}
	if _, err := strconv.ParseUint(value, 10, 64); err == nil {
		return "msg/" + value
	}
	return value
}

func NormalizeTeamsReactionTargetMessageID(value string) string {
	normalized := NormalizeTeamsReactionMessageID(value)
	if strings.HasPrefix(normalized, "msg/") {
		return strings.TrimPrefix(normalized, "msg/")
	}
	return normalized
}

func isTeamsIngestedReaction(evt *event.Event) bool {
	if evt == nil {
		return false
	}
	if evt.Content.Raw == nil {
		return false
	}
	v, ok := evt.Content.Raw["com.beeper.teams.ingested_reaction"]
	if !ok {
		return false
	}
	flag, ok := v.(bool)
	return ok && flag
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
	if isTeamsIngestedReaction(evt) {
		r.Log.Debug().
			Str("room_id", roomID.String()).
			Str("event_id", evt.ID.String()).
			Str("sender", evt.Sender.String()).
			Msg("reaction dropped: teams-ingested echo")
		return nil
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
	r.Log.Info().
		Str("room_id", roomID.String()).
		Str("event_id", evt.ID.String()).
		Str("sender", evt.Sender.String()).
		Str("target_mxid", reaction.RelatesTo.EventID.String()).
		Str("reaction_key", reaction.RelatesTo.Key).
		Msg("matrix reaction ingested")

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
	teamsMessageID := NormalizeTeamsReactionTargetMessageID(mapping.TeamsMessageID)
	r.Log.Info().
		Str("room_id", roomID.String()).
		Str("event_id", evt.ID.String()).
		Str("target_mxid", reaction.RelatesTo.EventID.String()).
		Str("thread_id", threadID).
		Str("teams_message_id", teamsMessageID).
		Msg("teams reaction target resolved")

	log := r.Log.With().
		Str("room_id", roomID.String()).
		Str("thread_id", threadID).
		Str("teams_message_id", teamsMessageID).
		Str("emotion_key", emotionKey).
		Str("event_id", evt.ID.String()).
		Logger()
	log.Info().Msg("teams reaction add attempt")

	status, err := r.Client.AddReaction(ctx, threadID, teamsMessageID, emotionKey, time.Now().UTC().UnixMilli())
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
	r.Log.Info().
		Str("room_id", roomID.String()).
		Str("event_id", evt.ID.String()).
		Str("sender", evt.Sender.String()).
		Str("redacts", evt.Redacts.String()).
		Msg("matrix reaction redaction ingested")
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
	teamsMessageID := NormalizeTeamsReactionTargetMessageID(mapping.TeamsMessageID)
	r.Log.Info().
		Str("room_id", roomID.String()).
		Str("event_id", evt.ID.String()).
		Str("target_mxid", reactionMap.TargetMXID.String()).
		Str("thread_id", threadID).
		Str("teams_message_id", teamsMessageID).
		Msg("teams reaction target resolved")

	log := r.Log.With().
		Str("room_id", roomID.String()).
		Str("thread_id", threadID).
		Str("teams_message_id", teamsMessageID).
		Str("emotion_key", reactionMap.EmotionKey).
		Str("event_id", evt.ID.String()).
		Str("reaction_event_id", reactionMap.ReactionMXID.String()).
		Logger()
	log.Info().Msg("teams reaction remove attempt")

	status, err := r.Client.RemoveReaction(ctx, threadID, teamsMessageID, reactionMap.EmotionKey)
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
