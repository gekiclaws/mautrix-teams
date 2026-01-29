package teamsbridge

import (
	"context"
	"errors"
	"strings"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type ConsumptionHorizonClient interface {
	GetConsumptionHorizons(ctx context.Context, threadID string) (*model.ConsumptionHorizonsResponse, error)
}

type ConsumptionHorizonMessageLookup interface {
	GetLatestInboundBefore(threadID string, maxTS int64, selfUserID string) *database.TeamsMessageMap
}

type ConsumptionHorizonStateStore interface {
	Get(threadID string, teamsUserID string) *database.TeamsConsumptionHorizon
	UpsertLastRead(threadID string, teamsUserID string, lastReadTS int64) error
}

type ReadMarkerSender interface {
	SetReadMarkers(roomID id.RoomID, eventID id.EventID) error
}

type BotMatrixReadMarkerSender struct {
	Client *mautrix.Client
}

func (s *BotMatrixReadMarkerSender) SetReadMarkers(roomID id.RoomID, eventID id.EventID) error {
	if s == nil || s.Client == nil {
		return errors.New("missing matrix client")
	}
	content := &mautrix.ReqSetReadMarkers{
		Read:      eventID,
		FullyRead: eventID,
	}
	return s.Client.SetReadMarkers(roomID, content)
}

type TeamsConsumptionHorizonIngestor struct {
	Client     ConsumptionHorizonClient
	Messages   ConsumptionHorizonMessageLookup
	State      ConsumptionHorizonStateStore
	Sender     ReadMarkerSender
	SelfUserID string
	Log        zerolog.Logger
}

func NewTeamsConsumptionHorizonIngestor(client ConsumptionHorizonClient, messages ConsumptionHorizonMessageLookup, state ConsumptionHorizonStateStore, sender ReadMarkerSender, selfUserID string, log zerolog.Logger) *TeamsConsumptionHorizonIngestor {
	return &TeamsConsumptionHorizonIngestor{
		Client:     client,
		Messages:   messages,
		State:      state,
		Sender:     sender,
		SelfUserID: selfUserID,
		Log:        log,
	}
}

func (i *TeamsConsumptionHorizonIngestor) PollOnce(ctx context.Context, threadID string, roomID id.RoomID) error {
	if i == nil || i.Client == nil {
		return errors.New("missing consumption horizon client")
	}
	if i.Messages == nil {
		return errors.New("missing message map lookup")
	}
	if i.State == nil {
		return errors.New("missing consumption horizon state store")
	}
	if i.Sender == nil {
		return errors.New("missing read marker sender")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return errors.New("missing thread id")
	}
	if roomID == "" {
		return errors.New("missing room id")
	}
	selfUserID := model.NormalizeTeamsUserID(i.SelfUserID)
	if selfUserID == "" {
		return errors.New("missing self user id")
	}

	resp, err := i.Client.GetConsumptionHorizons(ctx, threadID)
	if err != nil {
		return err
	}
	if resp == nil || len(resp.Horizons) == 0 {
		return nil
	}

	var remoteID string
	var remoteHorizon *model.ConsumptionHorizon
	nonSelfCount := 0
	for idx := range resp.Horizons {
		entry := &resp.Horizons[idx]
		entryID := model.NormalizeTeamsUserID(entry.ID)
		if entryID == "" || entryID == selfUserID {
			continue
		}
		nonSelfCount++
		if nonSelfCount > 1 {
			i.Log.Info().
				Str("thread_id", threadID).
				Str("room_id", roomID.String()).
				Int("non_self_count", nonSelfCount).
				Msg("skipping consumption horizons with multiple remote participants")
			return nil
		}
		remoteID = entryID
		remoteHorizon = entry
	}

	if remoteHorizon == nil || remoteID == "" {
		return nil
	}

	latestReadTS, ok := model.ParseConsumptionHorizonLatestReadTS(remoteHorizon.ConsumptionHorizon)
	if !ok || latestReadTS <= 0 {
		return nil
	}

	lastReadTS := int64(0)
	if existing := i.State.Get(threadID, remoteID); existing != nil {
		lastReadTS = existing.LastReadTS
	}
	if latestReadTS <= lastReadTS {
		return nil
	}

	mapping := i.Messages.GetLatestInboundBefore(threadID, latestReadTS, selfUserID)
	markerSent := false
	var markerErr error
	if mapping != nil && mapping.MXID != "" {
		markerErr = i.Sender.SetReadMarkers(roomID, mapping.MXID)
		markerSent = (markerErr == nil)
	}

	persistErr := i.State.UpsertLastRead(threadID, remoteID, latestReadTS)

	logEntry := i.Log.With().
		Str("thread_id", threadID).
		Str("room_id", roomID.String()).
		Str("teams_user_id", remoteID).
		Int64("latest_read_ts", latestReadTS).
		Bool("marker_requested", mapping != nil && mapping.MXID != "").
		Bool("marker_sent", markerSent)
	if mapping != nil && mapping.MXID != "" {
		logEntry = logEntry.Str("event_id", mapping.MXID.String())
	}

	logger := logEntry.Logger()
	level := logger.Info()
	if markerErr != nil || persistErr != nil {
		level = logger.Warn()
	}
	if markerErr != nil {
		level = level.Err(markerErr)
	} else if persistErr != nil {
		level = level.Err(persistErr)
	}
	level.Msg("consumption horizon advanced")

	return persistErr
}
