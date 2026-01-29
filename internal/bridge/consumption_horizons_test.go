package teamsbridge

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type fakeConsumptionClient struct {
	response *model.ConsumptionHorizonsResponse
	err      error
}

func (f *fakeConsumptionClient) GetConsumptionHorizons(ctx context.Context, threadID string) (*model.ConsumptionHorizonsResponse, error) {
	return f.response, f.err
}

type fakeConsumptionMessageLookup struct {
	entry *database.TeamsMessageMap
}

func (f *fakeConsumptionMessageLookup) GetLatestInboundBefore(threadID string, maxTS int64, selfUserID string) *database.TeamsMessageMap {
	return f.entry
}

type fakeConsumptionState struct {
	stored map[string]int64
	ups    []string
}

func (f *fakeConsumptionState) key(threadID string, userID string) string {
	return threadID + "|" + userID
}

func (f *fakeConsumptionState) Get(threadID string, teamsUserID string) *database.TeamsConsumptionHorizon {
	if f == nil || f.stored == nil {
		return nil
	}
	key := f.key(threadID, teamsUserID)
	ts, ok := f.stored[key]
	if !ok {
		return nil
	}
	return &database.TeamsConsumptionHorizon{
		ThreadID:    threadID,
		TeamsUserID: teamsUserID,
		LastReadTS:  ts,
	}
}

func (f *fakeConsumptionState) UpsertLastRead(threadID string, teamsUserID string, lastReadTS int64) error {
	if f.stored == nil {
		f.stored = make(map[string]int64)
	}
	key := f.key(threadID, teamsUserID)
	f.stored[key] = lastReadTS
	f.ups = append(f.ups, key)
	return nil
}

type fakeReadMarkerSender struct {
	calls []id.EventID
	err   error
}

func (f *fakeReadMarkerSender) SetReadMarkers(roomID id.RoomID, eventID id.EventID) error {
	f.calls = append(f.calls, eventID)
	return f.err
}

func TestConsumptionHorizonAdvancesAndSendsMarker(t *testing.T) {
	client := &fakeConsumptionClient{
		response: &model.ConsumptionHorizonsResponse{
			Horizons: []model.ConsumptionHorizon{
				{ID: "8:self", ConsumptionHorizon: "0;1000;0"},
				{ID: "8:remote", ConsumptionHorizon: "0;2000;0"},
			},
		},
	}
	messages := &fakeConsumptionMessageLookup{
		entry: &database.TeamsMessageMap{MXID: "$event"},
	}
	state := &fakeConsumptionState{}
	sender := &fakeReadMarkerSender{}
	ingestor := NewTeamsConsumptionHorizonIngestor(client, messages, state, sender, "8:self", zerolog.Nop())

	if err := ingestor.PollOnce(context.Background(), "19:abc@thread.v2", "!room:example"); err != nil {
		t.Fatalf("PollOnce failed: %v", err)
	}
	if len(sender.calls) != 1 || sender.calls[0] != "$event" {
		t.Fatalf("unexpected read marker calls: %#v", sender.calls)
	}
	if state.stored["19:abc@thread.v2|8:remote"] != 2000 {
		t.Fatalf("expected last_read_ts to be persisted, got %v", state.stored)
	}
}

func TestConsumptionHorizonNoAdvanceNoop(t *testing.T) {
	client := &fakeConsumptionClient{
		response: &model.ConsumptionHorizonsResponse{
			Horizons: []model.ConsumptionHorizon{
				{ID: "8:remote", ConsumptionHorizon: "0;2000;0"},
			},
		},
	}
	state := &fakeConsumptionState{
		stored: map[string]int64{"19:abc@thread.v2|8:remote": 2000},
	}
	sender := &fakeReadMarkerSender{}
	ingestor := NewTeamsConsumptionHorizonIngestor(client, &fakeConsumptionMessageLookup{}, state, sender, "8:self", zerolog.Nop())

	if err := ingestor.PollOnce(context.Background(), "19:abc@thread.v2", "!room:example"); err != nil {
		t.Fatalf("PollOnce failed: %v", err)
	}
	if len(sender.calls) != 0 {
		t.Fatalf("expected no marker calls, got %#v", sender.calls)
	}
	if len(state.ups) != 0 {
		t.Fatalf("expected no persistence, got %#v", state.ups)
	}
}

func TestConsumptionHorizonPersistsWithoutMapping(t *testing.T) {
	client := &fakeConsumptionClient{
		response: &model.ConsumptionHorizonsResponse{
			Horizons: []model.ConsumptionHorizon{
				{ID: "8:remote", ConsumptionHorizon: "0;3000;0"},
			},
		},
	}
	state := &fakeConsumptionState{}
	sender := &fakeReadMarkerSender{}
	ingestor := NewTeamsConsumptionHorizonIngestor(client, &fakeConsumptionMessageLookup{}, state, sender, "8:self", zerolog.Nop())

	if err := ingestor.PollOnce(context.Background(), "19:abc@thread.v2", "!room:example"); err != nil {
		t.Fatalf("PollOnce failed: %v", err)
	}
	if len(sender.calls) != 0 {
		t.Fatalf("expected no marker calls, got %#v", sender.calls)
	}
	if state.stored["19:abc@thread.v2|8:remote"] != 3000 {
		t.Fatalf("expected last_read_ts persisted, got %v", state.stored)
	}
}

func TestConsumptionHorizonSkipsMultipleRemotes(t *testing.T) {
	client := &fakeConsumptionClient{
		response: &model.ConsumptionHorizonsResponse{
			Horizons: []model.ConsumptionHorizon{
				{ID: "8:remote-1", ConsumptionHorizon: "0;2000;0"},
				{ID: "8:remote-2", ConsumptionHorizon: "0;3000;0"},
			},
		},
	}
	state := &fakeConsumptionState{}
	sender := &fakeReadMarkerSender{}
	ingestor := NewTeamsConsumptionHorizonIngestor(client, &fakeConsumptionMessageLookup{}, state, sender, "8:self", zerolog.Nop())

	if err := ingestor.PollOnce(context.Background(), "19:abc@thread.v2", "!room:example"); err != nil {
		t.Fatalf("PollOnce failed: %v", err)
	}
	if len(sender.calls) != 0 {
		t.Fatalf("expected no marker calls, got %#v", sender.calls)
	}
	if len(state.ups) != 0 {
		t.Fatalf("expected no persistence, got %#v", state.ups)
	}
}

func TestConsumptionHorizonSenderErrorStillPersists(t *testing.T) {
	client := &fakeConsumptionClient{
		response: &model.ConsumptionHorizonsResponse{
			Horizons: []model.ConsumptionHorizon{
				{ID: "8:remote", ConsumptionHorizon: "0;4000;0"},
			},
		},
	}
	messages := &fakeConsumptionMessageLookup{
		entry: &database.TeamsMessageMap{MXID: "$event"},
	}
	state := &fakeConsumptionState{}
	sender := &fakeReadMarkerSender{err: errors.New("boom")}
	ingestor := NewTeamsConsumptionHorizonIngestor(client, messages, state, sender, "8:self", zerolog.Nop())

	if err := ingestor.PollOnce(context.Background(), "19:abc@thread.v2", "!room:example"); err != nil {
		t.Fatalf("PollOnce failed: %v", err)
	}
	if state.stored["19:abc@thread.v2|8:remote"] != 4000 {
		t.Fatalf("expected last_read_ts persisted, got %v", state.stored)
	}
}
