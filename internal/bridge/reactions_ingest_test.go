package teamsbridge

import (
	"context"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type fakeMatrixReactionSender struct {
	nextID  int
	sent    []reactionSendCall
	redacts []reactionRedactCall
}

type reactionSendCall struct {
	roomID  id.RoomID
	target  id.EventID
	emoji   string
	eventID id.EventID
}

type reactionRedactCall struct {
	roomID  id.RoomID
	eventID id.EventID
}

func (f *fakeMatrixReactionSender) SendReaction(roomID id.RoomID, target id.EventID, key string) (id.EventID, error) {
	f.nextID++
	eventID := id.EventID(fmt.Sprintf("$reaction-%d", f.nextID))
	f.sent = append(f.sent, reactionSendCall{
		roomID:  roomID,
		target:  target,
		emoji:   key,
		eventID: eventID,
	})
	return eventID, nil
}

func (f *fakeMatrixReactionSender) Redact(roomID id.RoomID, eventID id.EventID) (id.EventID, error) {
	f.redacts = append(f.redacts, reactionRedactCall{
		roomID:  roomID,
		eventID: eventID,
	})
	return id.EventID("$redact"), nil
}

type fakeTeamsMessageMapLookup struct {
	byKey map[string]*database.TeamsMessageMap
}

func (f *fakeTeamsMessageMapLookup) GetByTeamsMessageID(threadID string, teamsMessageID string) *database.TeamsMessageMap {
	if f == nil {
		return nil
	}
	return f.byKey[threadID+"|"+teamsMessageID]
}

type fakeTeamsReactionStateStore struct {
	byKey    map[string]*database.TeamsReactionState
	inserted []*database.TeamsReactionState
	deleted  []string
}

func (f *fakeTeamsReactionStateStore) ListByMessage(threadID string, teamsMessageID string) ([]*database.TeamsReactionState, error) {
	if f == nil || f.byKey == nil {
		return nil, nil
	}
	var states []*database.TeamsReactionState
	for _, state := range f.byKey {
		if state.ThreadID == threadID && state.TeamsMessageID == teamsMessageID {
			states = append(states, state)
		}
	}
	return states, nil
}

func (f *fakeTeamsReactionStateStore) Insert(state *database.TeamsReactionState) error {
	if f.byKey == nil {
		f.byKey = make(map[string]*database.TeamsReactionState)
	}
	key := reactionKey(state.EmotionKey, state.UserMRI)
	f.byKey[state.ThreadID+"|"+state.TeamsMessageID+"|"+key] = state
	f.inserted = append(f.inserted, state)
	return nil
}

func (f *fakeTeamsReactionStateStore) Delete(threadID string, teamsMessageID string, emotionKey string, userMRI string) error {
	key := threadID + "|" + teamsMessageID + "|" + reactionKey(emotionKey, userMRI)
	delete(f.byKey, key)
	f.deleted = append(f.deleted, key)
	return nil
}

func TestTeamsReactionIngestAddsNewReaction(t *testing.T) {
	sender := &fakeMatrixReactionSender{}
	store := &fakeTeamsReactionStateStore{}
	ingestor := &TeamsReactionIngestor{
		Sender:    sender,
		Messages:  &fakeTeamsMessageMapLookup{},
		Reactions: store,
		Log:       zerolog.Nop(),
	}
	msg := model.RemoteMessage{
		MessageID: "m1",
		Reactions: []model.MessageReaction{
			{
				EmotionKey: "like",
				Users:      []model.MessageReactionUser{{MRI: "8:one", TimeMS: 1700000000000}},
			},
		},
	}
	target := id.EventID("$target")
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", msg, target); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected one send call, got %d", len(sender.sent))
	}
	if sender.sent[0].target != target {
		t.Fatalf("unexpected target: %q", sender.sent[0].target)
	}
	if sender.sent[0].emoji == "" {
		t.Fatalf("expected emoji")
	}
	if len(store.inserted) != 1 {
		t.Fatalf("expected one insert, got %d", len(store.inserted))
	}
}

func TestTeamsReactionIngestUsesMappingWhenTargetMissing(t *testing.T) {
	sender := &fakeMatrixReactionSender{}
	store := &fakeTeamsReactionStateStore{}
	lookup := &fakeTeamsMessageMapLookup{
		byKey: map[string]*database.TeamsMessageMap{
			"t1|m1": {MXID: "$mapped", ThreadID: "t1", TeamsMessageID: "m1"},
		},
	}
	ingestor := &TeamsReactionIngestor{
		Sender:    sender,
		Messages:  lookup,
		Reactions: store,
		Log:       zerolog.Nop(),
	}
	msg := model.RemoteMessage{
		MessageID: "m1",
		Reactions: []model.MessageReaction{
			{
				EmotionKey: "like",
				Users:      []model.MessageReactionUser{{MRI: "8:one"}},
			},
		},
	}
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", msg, ""); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0].target != "$mapped" {
		t.Fatalf("expected send call with mapped target, got %#v", sender.sent)
	}
}

func TestTeamsReactionIngestRemovesMissingReaction(t *testing.T) {
	sender := &fakeMatrixReactionSender{}
	store := &fakeTeamsReactionStateStore{
		byKey: map[string]*database.TeamsReactionState{
			"t1|m1|like\x008:one": {
				ThreadID:       "t1",
				TeamsMessageID: "m1",
				EmotionKey:     "like",
				UserMRI:        "8:one",
				MatrixEventID:  "$reaction",
			},
		},
	}
	ingestor := &TeamsReactionIngestor{
		Sender:    sender,
		Messages:  &fakeTeamsMessageMapLookup{},
		Reactions: store,
		Log:       zerolog.Nop(),
	}
	msg := model.RemoteMessage{MessageID: "m1"}
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", msg, ""); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(sender.redacts) != 1 || sender.redacts[0].eventID != "$reaction" {
		t.Fatalf("expected redaction call, got %#v", sender.redacts)
	}
	if len(store.deleted) != 1 {
		t.Fatalf("expected one delete, got %d", len(store.deleted))
	}
}

func TestTeamsReactionIngestIdempotent(t *testing.T) {
	sender := &fakeMatrixReactionSender{}
	store := &fakeTeamsReactionStateStore{
		byKey: map[string]*database.TeamsReactionState{
			"t1|m1|like\x008:one": {
				ThreadID:       "t1",
				TeamsMessageID: "m1",
				EmotionKey:     "like",
				UserMRI:        "8:one",
				MatrixEventID:  "$reaction",
			},
		},
	}
	ingestor := &TeamsReactionIngestor{
		Sender:    sender,
		Messages:  &fakeTeamsMessageMapLookup{},
		Reactions: store,
		Log:       zerolog.Nop(),
	}
	msg := model.RemoteMessage{
		MessageID: "m1",
		Reactions: []model.MessageReaction{
			{EmotionKey: "like", Users: []model.MessageReactionUser{{MRI: "8:one"}}},
		},
	}
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", msg, ""); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(sender.sent) != 0 || len(sender.redacts) != 0 {
		t.Fatalf("expected no sender calls, got sends=%d redacts=%d", len(sender.sent), len(sender.redacts))
	}
}

func TestTeamsReactionIngestSkipsUnmapped(t *testing.T) {
	sender := &fakeMatrixReactionSender{}
	store := &fakeTeamsReactionStateStore{}
	ingestor := &TeamsReactionIngestor{
		Sender:    sender,
		Messages:  &fakeTeamsMessageMapLookup{},
		Reactions: store,
		Log:       zerolog.Nop(),
	}
	msg := model.RemoteMessage{
		MessageID: "m1",
		Reactions: []model.MessageReaction{
			{EmotionKey: "unknown", Users: []model.MessageReactionUser{{MRI: "8:one"}}},
		},
	}
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", msg, "$target"); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(sender.sent) != 0 || len(store.inserted) != 0 {
		t.Fatalf("expected no sends for unmapped reaction")
	}
}

func TestTeamsReactionIngestSkipsWhenNoTarget(t *testing.T) {
	sender := &fakeMatrixReactionSender{}
	store := &fakeTeamsReactionStateStore{}
	ingestor := &TeamsReactionIngestor{
		Sender:    sender,
		Messages:  &fakeTeamsMessageMapLookup{},
		Reactions: store,
		Log:       zerolog.Nop(),
	}
	msg := model.RemoteMessage{
		MessageID: "m1",
		Reactions: []model.MessageReaction{
			{EmotionKey: "like", Users: []model.MessageReactionUser{{MRI: "8:one"}}},
		},
	}
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", msg, ""); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(sender.sent) != 0 || len(store.inserted) != 0 {
		t.Fatalf("expected no sends when target missing")
	}
}
