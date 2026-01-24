package teamsbridge

import (
	"context"
	"errors"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type MessageLister interface {
	ListMessages(ctx context.Context, conversationID string, sinceSequence string) ([]model.RemoteMessage, error)
}

type MatrixSender interface {
	SendText(roomID id.RoomID, body string) (id.EventID, error)
}

type BotMatrixSender struct {
	Client *mautrix.Client
}

func (s *BotMatrixSender) SendText(roomID id.RoomID, body string) (id.EventID, error) {
	if s == nil || s.Client == nil {
		return "", errors.New("missing matrix client")
	}
	content := event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    body,
	}
	resp, err := s.Client.SendMessageEvent(roomID, event.EventMessage, &content)
	if err != nil {
		return "", err
	}
	return resp.EventID, nil
}

type MessageIngestor struct {
	Lister MessageLister
	Sender MatrixSender
	Log    zerolog.Logger
}

func (m *MessageIngestor) IngestThread(ctx context.Context, threadID string, conversationID string, roomID id.RoomID, lastSequenceID *string) (string, bool, error) {
	if m == nil || m.Lister == nil {
		return "", false, errors.New("missing message lister")
	}
	if m.Sender == nil {
		return "", false, errors.New("missing message sender")
	}
	if conversationID == "" {
		return "", false, errors.New("missing conversation id")
	}

	since := ""
	if lastSequenceID != nil {
		since = *lastSequenceID
	}

	messages, err := m.Lister.ListMessages(ctx, conversationID, since)
	if err != nil {
		m.Log.Error().Err(err).Str("thread_id", threadID).Msg("failed to list messages")
		return "", false, err
	}

	lastSuccess := ""
	for _, msg := range messages {
		if lastSequenceID != nil && model.CompareSequenceID(msg.SequenceID, *lastSequenceID) <= 0 {
			continue
		}
		if msg.Body == "" {
			m.Log.Debug().
				Str("thread_id", threadID).
				Str("seq", msg.SequenceID).
				Msg("skipping empty-body message")
			continue
		}

		m.Log.Info().
			Str("thread_id", threadID).
			Str("seq", msg.SequenceID).
			Msg("teams message discovered")

		if _, err := m.Sender.SendText(roomID, msg.Body); err != nil {
			m.Log.Error().
				Err(err).
				Str("thread_id", threadID).
				Str("room_id", roomID.String()).
				Str("seq", msg.SequenceID).
				Msg("failed to send message")
			return "", false, nil
		}

		m.Log.Info().
			Str("room_id", roomID.String()).
			Str("seq", msg.SequenceID).
			Msg("matrix message sent")

		lastSuccess = msg.SequenceID
	}

	if lastSuccess == "" {
		return "", false, nil
	}

	return lastSuccess, true, nil
}
