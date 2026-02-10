package teamsbridge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/client"
)

type ThreadLookup interface {
	GetThreadID(roomID id.RoomID) (string, bool)
}

type SendIntentStore interface {
	Insert(intent *database.TeamsSendIntent) error
	UpdateStatus(clientMessageID string, status database.TeamsSendStatus) error
	ClearIntentMXID(clientMessageID string) error
}

type MSSWriter func(ctx context.Context, status database.TeamsSendStatus, clientMessageID string, ts int64) error

type TeamsConsumerSender struct {
	Client      *client.Client
	SendIntents SendIntentStore
	Threads     ThreadLookup
	UserID      string
	Log         zerolog.Logger
}

func NewTeamsConsumerSender(client *client.Client, intents SendIntentStore, threads ThreadLookup, userID string, log zerolog.Logger) *TeamsConsumerSender {
	return &TeamsConsumerSender{
		Client:      client,
		SendIntents: intents,
		Threads:     threads,
		UserID:      userID,
		Log:         log,
	}
}

func (s *TeamsConsumerSender) SendMatrixText(ctx context.Context, roomID id.RoomID, body string, eventID id.EventID, intentMXID id.UserID, writer MSSWriter) error {
	return s.sendMatrixPayload(ctx, roomID, eventID, intentMXID, writer, func(threadID string, clientMessageID string) (int, error) {
		return s.Client.SendMessageWithID(ctx, threadID, body, s.UserID, clientMessageID)
	})
}

func (s *TeamsConsumerSender) SendMatrixGIF(ctx context.Context, roomID id.RoomID, gifURL string, title string, eventID id.EventID, intentMXID id.UserID, writer MSSWriter) error {
	return s.sendMatrixPayload(ctx, roomID, eventID, intentMXID, writer, func(threadID string, clientMessageID string) (int, error) {
		return s.Client.SendGIFWithID(ctx, threadID, gifURL, title, s.UserID, clientMessageID)
	})
}

func (s *TeamsConsumerSender) sendMatrixPayload(
	ctx context.Context,
	roomID id.RoomID,
	eventID id.EventID,
	intentMXID id.UserID,
	writer MSSWriter,
	send func(threadID string, clientMessageID string) (int, error),
) error {
	if s == nil || s.Client == nil {
		return errors.New("missing teams consumer client")
	}
	if s.SendIntents == nil {
		return errors.New("missing send intent store")
	}
	if s.Threads == nil {
		return errors.New("missing teams thread store")
	}
	if strings.TrimSpace(s.UserID) == "" {
		return errors.New("missing teams user id")
	}
	threadID, ok := s.Threads.GetThreadID(roomID)
	if !ok || strings.TrimSpace(threadID) == "" {
		return errors.New("missing thread id")
	}
	s.Log.Info().
		Str("room_id", roomID.String()).
		Str("thread_id", threadID).
		Msg("teams thread resolved")
	if !strings.Contains(threadID, "@thread.v2") {
		s.Log.Warn().Str("thread_id", threadID).Msg("teams thread id missing @thread.v2")
	}

	clientMessageID := client.GenerateClientMessageID()
	pendingTS := time.Now().UTC().UnixMilli()
	intent := &database.TeamsSendIntent{
		ThreadID:        threadID,
		ClientMessageID: clientMessageID,
		Timestamp:       pendingTS,
		Status:          database.TeamsSendStatusPending,
		MXID:            eventID,
		IntentMXID:      intentMXID,
	}
	if err := s.SendIntents.Insert(intent); err != nil {
		return err
	}
	s.Log.Info().
		Str("client_message_id", clientMessageID).
		Str("thread_id", threadID).
		Str("status", string(database.TeamsSendStatusPending)).
		Msg("teams send intent created")
	if writer != nil {
		if err := writer(ctx, database.TeamsSendStatusPending, clientMessageID, pendingTS); err != nil {
			s.Log.Warn().Err(err).Str("event_id", eventID.String()).Msg("failed to write pending MSS metadata")
		}
	}

	s.Log.Info().
		Str("thread_id", threadID).
		Str("room_id", roomID.String()).
		Str("event_id", eventID.String()).
		Str("client_message_id", clientMessageID).
		Msg("teams send attempt")

	statusCode, err := send(threadID, clientMessageID)
	newStatus := database.TeamsSendStatusAccepted
	if err != nil {
		newStatus = database.TeamsSendStatusFailed
	}
	if statusCode != 0 {
		s.Log.Info().
			Str("client_message_id", clientMessageID).
			Int("status", statusCode).
			Msg("teams send response")
	}

	updateTS := time.Now().UTC().UnixMilli()
	if updateErr := s.SendIntents.UpdateStatus(clientMessageID, newStatus); updateErr != nil {
		s.Log.Error().Err(updateErr).
			Str("client_message_id", clientMessageID).
			Str("status", string(newStatus)).
			Msg("failed to update teams send intent")
	}
	if writer != nil {
		if err := writer(ctx, newStatus, clientMessageID, updateTS); err != nil {
			s.Log.Warn().Err(err).Str("event_id", eventID.String()).Msg("failed to write MSS metadata")
		}
	}
	if clearErr := s.SendIntents.ClearIntentMXID(clientMessageID); clearErr != nil {
		s.Log.Warn().Err(clearErr).
			Str("client_message_id", clientMessageID).
			Msg("failed to clear matrix intent mapping for MSS")
	}

	s.Log.Info().
		Str("client_message_id", clientMessageID).
		Str("status", string(newStatus)).
		Msg("teams send status transition")

	return err
}
