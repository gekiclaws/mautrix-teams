package teamsbridge

import (
	"context"
	"errors"
	"strings"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"
)

type TypingClient interface {
	SendTypingIndicator(ctx context.Context, threadID string, fromUserID string) (int, error)
}

type TeamsConsumerTyper struct {
	Client  TypingClient
	Threads ThreadLookup
	UserID  string
	Log     zerolog.Logger
}

func NewTeamsConsumerTyper(client TypingClient, threads ThreadLookup, userID string, log zerolog.Logger) *TeamsConsumerTyper {
	return &TeamsConsumerTyper{
		Client:  client,
		Threads: threads,
		UserID:  userID,
		Log:     log,
	}
}

func (t *TeamsConsumerTyper) SendTyping(ctx context.Context, roomID id.RoomID) error {
	if t == nil || t.Client == nil {
		return errors.New("missing typing client")
	}
	if t.Threads == nil {
		return errors.New("missing thread store")
	}
	if strings.TrimSpace(t.UserID) == "" {
		return errors.New("missing teams user id")
	}
	threadID, ok := t.Threads.GetThreadID(roomID)
	if !ok || strings.TrimSpace(threadID) == "" {
		return errors.New("missing thread id")
	}

	t.Log.Info().
		Str("room_id", roomID.String()).
		Str("thread_id", threadID).
		Msg("teams typing attempt")

	status, err := t.Client.SendTypingIndicator(ctx, threadID, t.UserID)
	if status != 0 {
		t.Log.Info().
			Str("room_id", roomID.String()).
			Str("thread_id", threadID).
			Int("status", status).
			Msg("teams typing response")
	}
	if err != nil {
		t.Log.Warn().
			Err(err).
			Str("room_id", roomID.String()).
			Str("thread_id", threadID).
			Msg("teams typing failed")
		return err
	}
	return nil
}
