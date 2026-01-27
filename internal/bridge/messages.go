package teamsbridge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type MessageLister interface {
	ListMessages(ctx context.Context, conversationID string, sinceSequence string) ([]model.RemoteMessage, error)
}

type MatrixSender interface {
	SendText(roomID id.RoomID, body string, extra map[string]any) (id.EventID, error)
}

type SendIntentLookup interface {
	GetByClientMessageID(clientMessageID string) *database.TeamsSendIntent
}

type TeamsMessageMapWriter interface {
	Upsert(mapping *database.TeamsMessageMap) error
}

type BotMatrixSender struct {
	Client *mautrix.Client
}

func (s *BotMatrixSender) SendText(roomID id.RoomID, body string, extra map[string]any) (id.EventID, error) {
	if s == nil || s.Client == nil {
		return "", errors.New("missing matrix client")
	}
	content := event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    body,
	}
	wrapped := event.Content{Parsed: &content, Raw: extra}
	resp, err := s.Client.SendMessageEvent(roomID, event.EventMessage, &wrapped)
	if err != nil {
		return "", err
	}
	return resp.EventID, nil
}

type ProfileStore interface {
	GetByTeamsUserID(teamsUserID string) *database.TeamsProfile
	InsertIfMissing(profile *database.TeamsProfile) (bool, error)
	UpdateDisplayName(teamsUserID string, displayName string, lastSeenTS time.Time) error
}

type MessageIngestor struct {
	Lister           MessageLister
	Sender           MatrixSender
	Profiles         ProfileStore
	SendIntents      SendIntentLookup
	MessageMap       TeamsMessageMapWriter
	ReactionIngestor MessageReactionIngestor
	Log              zerolog.Logger
}

type IngestResult struct {
	MessagesIngested int
	LastSequenceID   string
	Advanced         bool
}

func (m *MessageIngestor) IngestThread(ctx context.Context, threadID string, conversationID string, roomID id.RoomID, lastSequenceID *string) (IngestResult, error) {
	if m == nil || m.Lister == nil {
		return IngestResult{}, errors.New("missing message lister")
	}
	if m.Sender == nil {
		return IngestResult{}, errors.New("missing message sender")
	}
	if conversationID == "" {
		return IngestResult{}, errors.New("missing conversation id")
	}

	since := ""
	if lastSequenceID != nil {
		since = *lastSequenceID
	}

	messages, err := m.Lister.ListMessages(ctx, conversationID, since)
	if err != nil {
		return IngestResult{}, err
	}

	m.Log.Info().
		Str("thread_id", threadID).
		Int("count", len(messages)).
		Msg("teams messages fetched")

	lastSuccess := ""
	messagesIngested := 0
	reactionIngested := 0
	ingestReactions := func(msg model.RemoteMessage, targetMXID id.EventID) {
		if m.ReactionIngestor != nil {
			reactionIngested++
		}
		m.ingestReactions(ctx, threadID, roomID, msg, targetMXID)
	}
	for _, msg := range messages {
		if lastSequenceID != nil && model.CompareSequenceID(msg.SequenceID, *lastSequenceID) <= 0 {
			ingestReactions(msg, "")
			continue
		}
		if msg.Body == "" {
			m.Log.Debug().
				Str("thread_id", threadID).
				Str("seq", msg.SequenceID).
				Msg("teams message skipped empty body")
			ingestReactions(msg, "")
			continue
		}

		m.Log.Debug().
			Str("thread_id", threadID).
			Str("seq", msg.SequenceID).
			Msg("teams message discovered")

		senderID := model.NormalizeTeamsUserID(msg.SenderID)
		isUserID := strings.HasPrefix(senderID, "8:")
		displayName := ""
		var existingProfile *database.TeamsProfile
		if m.Profiles != nil && senderID != "" && isUserID {
			existingProfile = m.Profiles.GetByTeamsUserID(senderID)
			if existingProfile == nil {
				createdAt := model.ChooseLastSeenTS(msg.Timestamp, time.Now().UTC())
				profile := &database.TeamsProfile{
					TeamsUserID: senderID,
					DisplayName: msg.IMDisplayName,
					LastSeenTS:  createdAt,
				}
				inserted, err := m.Profiles.InsertIfMissing(profile)
				if err != nil {
					m.Log.Error().
						Err(err).
						Str("teams_user_id", senderID).
						Msg("failed to insert teams profile")
				} else if inserted {
					m.Log.Info().
						Str("teams_user_id", senderID).
						Str("display_name", profile.DisplayName).
						Bool("display_name_empty", profile.DisplayName == "").
						Msg("teams profile inserted")
				}
				existingProfile = profile
			}
		}

		if existingProfile != nil && msg.IMDisplayName != "" && existingProfile.DisplayName != msg.IMDisplayName {
			updatedAt := model.ChooseLastSeenTS(msg.Timestamp, time.Now().UTC())
			if err := m.Profiles.UpdateDisplayName(senderID, msg.IMDisplayName, updatedAt); err != nil {
				m.Log.Error().
					Err(err).
					Str("teams_user_id", senderID).
					Str("old_display_name", existingProfile.DisplayName).
					Str("new_display_name", msg.IMDisplayName).
					Msg("failed to update teams profile display name")
			} else {
				m.Log.Info().
					Str("teams_user_id", senderID).
					Str("old_display_name", existingProfile.DisplayName).
					Str("new_display_name", msg.IMDisplayName).
					Msg("teams profile display name updated")
			}
			existingProfile.DisplayName = msg.IMDisplayName
			existingProfile.LastSeenTS = updatedAt
		}

		if existingProfile != nil && existingProfile.DisplayName != "" {
			displayName = existingProfile.DisplayName
		} else if msg.IMDisplayName != "" {
			displayName = msg.IMDisplayName
		} else if msg.TokenDisplayName != "" {
			displayName = msg.TokenDisplayName
		} else if senderID != "" {
			displayName = senderID
		}

		var extra map[string]any
		if senderID != "" && displayName != "" {
			extra = map[string]any{
				"com.beeper.per_message_profile": map[string]any{
					"id":          senderID,
					"displayname": displayName,
				},
			}
		}

		eventID, err := m.Sender.SendText(roomID, msg.Body, extra)
		if err != nil {
			m.Log.Error().
				Err(err).
				Str("thread_id", threadID).
				Str("room_id", roomID.String()).
				Str("seq", msg.SequenceID).
				Msg("failed to send matrix message")
			return IngestResult{}, nil
		}
		messagesIngested++

		maybeMapMXID := eventID
		if msg.ClientMessageID != "" && m.SendIntents != nil {
			if intent := m.SendIntents.GetByClientMessageID(msg.ClientMessageID); intent != nil && intent.MXID != "" {
				maybeMapMXID = intent.MXID
			}
		}
		if m.MessageMap != nil && msg.MessageID != "" && maybeMapMXID != "" {
			if err := m.MessageMap.Upsert(&database.TeamsMessageMap{
				MXID:           maybeMapMXID,
				ThreadID:       threadID,
				TeamsMessageID: msg.MessageID,
			}); err != nil {
				m.Log.Error().
					Err(err).
					Str("thread_id", threadID).
					Str("teams_message_id", msg.MessageID).
					Str("event_id", maybeMapMXID.String()).
					Msg("failed to persist teams message map")
			}
		}

		ingestReactions(msg, maybeMapMXID)

		m.Log.Debug().
			Str("room_id", roomID.String()).
			Str("seq", msg.SequenceID).
			Msg("matrix message sent")

		lastSuccess = msg.SequenceID
	}

	if m.ReactionIngestor != nil {
		m.Log.Info().
			Str("thread_id", threadID).
			Int("count", reactionIngested).
			Msg("teams reactions ingested")
	}

	if lastSuccess == "" {
		return IngestResult{
			MessagesIngested: messagesIngested,
		}, nil
	}

	return IngestResult{
		MessagesIngested: messagesIngested,
		LastSequenceID:   lastSuccess,
		Advanced:         true,
	}, nil
}

type MessageReactionIngestor interface {
	IngestMessageReactions(ctx context.Context, threadID string, roomID id.RoomID, msg model.RemoteMessage, targetMXID id.EventID) error
}

func (m *MessageIngestor) ingestReactions(ctx context.Context, threadID string, roomID id.RoomID, msg model.RemoteMessage, targetMXID id.EventID) {
	if m == nil || m.ReactionIngestor == nil {
		return
	}
	if err := m.ReactionIngestor.IngestMessageReactions(ctx, threadID, roomID, msg, targetMXID); err != nil {
		m.Log.Error().
			Err(err).
			Str("thread_id", threadID).
			Str("teams_message_id", msg.MessageID).
			Str("seq", msg.SequenceID).
			Msg("failed to ingest teams reactions")
	}
}
