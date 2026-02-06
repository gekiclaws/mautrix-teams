package teamsbridge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/internal/teams/client"
)

type ReceiptClient interface {
	SetConsumptionHorizon(ctx context.Context, threadID string, horizon string) (int, error)
}

type ReceiptUnreadTracker interface {
	ShouldSendReadReceipt(roomID id.RoomID) bool
}

type TeamsConsumerReceiptSender struct {
	Client  ReceiptClient
	Threads ThreadLookup
	Unread  ReceiptUnreadTracker
	Log     zerolog.Logger
}

func NewTeamsConsumerReceiptSender(client ReceiptClient, threads ThreadLookup, unread ReceiptUnreadTracker, log zerolog.Logger) *TeamsConsumerReceiptSender {
	return &TeamsConsumerReceiptSender{
		Client:  client,
		Threads: threads,
		Unread:  unread,
		Log:     log,
	}
}

func (s *TeamsConsumerReceiptSender) SendReadReceipt(ctx context.Context, roomID id.RoomID, now time.Time) error {
	if s == nil || s.Client == nil {
		return errors.New("missing receipt client")
	}
	if s.Threads == nil {
		return errors.New("missing thread store")
	}
	if s.Unread == nil {
		return errors.New("missing unread tracker")
	}
	if !s.Unread.ShouldSendReadReceipt(roomID) {
		return nil
	}
	threadID, ok := s.Threads.GetThreadID(roomID)
	if !ok || strings.TrimSpace(threadID) == "" {
		s.Log.Warn().Str("room_id", roomID.String()).Msg("teams receipt missing thread id")
		return errors.New("missing thread id")
	}

	horizon := client.ConsumptionHorizonNow(now)
	s.Log.Info().
		Str("room_id", roomID.String()).
		Str("thread_id", threadID).
		Str("consumption_horizon", horizon).
		Msg("teams receipt attempt")

	status, err := s.Client.SetConsumptionHorizon(ctx, threadID, horizon)
	if status != 0 {
		s.Log.Info().
			Str("room_id", roomID.String()).
			Str("thread_id", threadID).
			Int("status", status).
			Msg("teams receipt response")
	}
	if err != nil {
		s.Log.Warn().
			Err(err).
			Str("room_id", roomID.String()).
			Str("thread_id", threadID).
			Msg("teams receipt failed")
		return err
	}
	return nil
}
