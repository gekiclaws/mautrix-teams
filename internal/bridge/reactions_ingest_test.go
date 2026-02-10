package teamsbridge

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type fakeMatrixReactionSender struct {
	nextID    int
	sent      []reactionSendCall
	redacts   []reactionRedactCall
	redactErr error
}

type reactionSendCall struct {
	roomID      id.RoomID
	target      id.EventID
	emoji       string
	eventID     id.EventID
	teamsUserID string
}

type reactionRedactCall struct {
	roomID      id.RoomID
	eventID     id.EventID
	teamsUserID string
}

func (f *fakeMatrixReactionSender) SendReactionAsTeamsUser(roomID id.RoomID, target id.EventID, key string, teamsUserID string) (id.EventID, error) {
	f.nextID++
	eventID := id.EventID(fmt.Sprintf("$reaction-%d", f.nextID))
	f.sent = append(f.sent, reactionSendCall{
		roomID:      roomID,
		target:      target,
		emoji:       key,
		eventID:     eventID,
		teamsUserID: teamsUserID,
	})
	return eventID, nil
}

func (f *fakeMatrixReactionSender) RedactReactionAsTeamsUser(roomID id.RoomID, eventID id.EventID, teamsUserID string) error {
	f.redacts = append(f.redacts, reactionRedactCall{
		roomID:      roomID,
		eventID:     eventID,
		teamsUserID: teamsUserID,
	})
	return f.redactErr
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

type fakeReactionStateStore struct {
	byKey   map[string]*database.ReactionMap
	upserts []*database.ReactionMap
	deleted []string
}

func (f *fakeReactionStateStore) key(threadID string, teamsMessageID string, teamsUserID string, reactionKey string) string {
	return threadID + "|" + teamsMessageID + "|" + teamsUserID + "|" + reactionKey
}

func (f *fakeReactionStateStore) GetByKey(threadID string, teamsMessageID string, teamsUserID string, reactionKey string) *database.ReactionMap {
	if f == nil || f.byKey == nil {
		return nil
	}
	return f.byKey[f.key(threadID, teamsMessageID, teamsUserID, reactionKey)]
}

func (f *fakeReactionStateStore) ListByMessage(threadID string, teamsMessageID string) ([]*database.ReactionMap, error) {
	if f == nil || f.byKey == nil {
		return nil, nil
	}
	var states []*database.ReactionMap
	for _, state := range f.byKey {
		if state.ThreadID == threadID && state.TeamsMessageID == teamsMessageID {
			states = append(states, state)
		}
	}
	return states, nil
}

func (f *fakeReactionStateStore) Upsert(state *database.ReactionMap) error {
	if f.byKey == nil {
		f.byKey = make(map[string]*database.ReactionMap)
	}
	f.byKey[f.key(state.ThreadID, state.TeamsMessageID, state.TeamsUserID, state.ReactionKey)] = state
	f.upserts = append(f.upserts, state)
	return nil
}

func (f *fakeReactionStateStore) DeleteByKey(threadID string, teamsMessageID string, teamsUserID string, reactionKey string) error {
	key := f.key(threadID, teamsMessageID, teamsUserID, reactionKey)
	delete(f.byKey, key)
	f.deleted = append(f.deleted, key)
	return nil
}

func TestTeamsReactionIngestAddsNewReaction(t *testing.T) {
	sender := &fakeMatrixReactionSender{}
	store := &fakeReactionStateStore{}
	ingestor := &TeamsReactionIngestor{
		Sender:    sender,
		Messages:  &fakeTeamsMessageMapLookup{},
		Reactions: store,
		Log:       zerolog.Nop(),
	}
	msg := model.RemoteMessage{
		MessageID: "m1",
		Reactions: []model.MessageReaction{{
			EmotionKey: "like",
			Users:      []model.MessageReactionUser{{MRI: "8:one", TimeMS: 1700000000000}},
		}},
	}
	target := id.EventID("$target")
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", msg, target); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected one send call, got %d", len(sender.sent))
	}
	if sender.sent[0].teamsUserID != "8:one" {
		t.Fatalf("expected teams user 8:one, got %s", sender.sent[0].teamsUserID)
	}
	if len(store.upserts) != 1 {
		t.Fatalf("expected one upsert, got %d", len(store.upserts))
	}
}

func TestTeamsReactionIngestUsesMappingWhenTargetMissing(t *testing.T) {
	sender := &fakeMatrixReactionSender{}
	store := &fakeReactionStateStore{}
	lookup := &fakeTeamsMessageMapLookup{byKey: map[string]*database.TeamsMessageMap{
		"t1|msg/m1": {MXID: "$mapped", ThreadID: "t1", TeamsMessageID: "msg/m1"},
	}}
	ingestor := &TeamsReactionIngestor{Sender: sender, Messages: lookup, Reactions: store, Log: zerolog.Nop()}
	msg := model.RemoteMessage{
		MessageID: "m1",
		Reactions: []model.MessageReaction{{EmotionKey: "like", Users: []model.MessageReactionUser{{MRI: "8:one"}}}},
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
	store := &fakeReactionStateStore{byKey: map[string]*database.ReactionMap{
		"t1|msg/m1|8:one|like": {
			ThreadID: "t1", TeamsMessageID: "msg/m1", TeamsUserID: "8:one", ReactionKey: "like", MatrixReactionEventID: "$reaction",
		},
	}}
	ingestor := &TeamsReactionIngestor{Sender: sender, Messages: &fakeTeamsMessageMapLookup{}, Reactions: store, Log: zerolog.Nop()}
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
	store := &fakeReactionStateStore{byKey: map[string]*database.ReactionMap{
		"t1|msg/m1|8:one|like": {
			ThreadID: "t1", TeamsMessageID: "msg/m1", TeamsUserID: "8:one", ReactionKey: "like", MatrixReactionEventID: "$reaction",
		},
	}}
	ingestor := &TeamsReactionIngestor{Sender: sender, Messages: &fakeTeamsMessageMapLookup{}, Reactions: store, Log: zerolog.Nop()}
	msg := model.RemoteMessage{MessageID: "m1", Reactions: []model.MessageReaction{{EmotionKey: "like", Users: []model.MessageReactionUser{{MRI: "8:one"}}}}}
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", msg, ""); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(sender.sent) != 0 || len(sender.redacts) != 0 {
		t.Fatalf("expected no sender calls, got sends=%d redacts=%d", len(sender.sent), len(sender.redacts))
	}
}

func TestTeamsReactionIngestSkipsUnmapped(t *testing.T) {
	sender := &fakeMatrixReactionSender{}
	store := &fakeReactionStateStore{}
	ingestor := &TeamsReactionIngestor{Sender: sender, Messages: &fakeTeamsMessageMapLookup{}, Reactions: store, Log: zerolog.Nop()}
	msg := model.RemoteMessage{MessageID: "m1", Reactions: []model.MessageReaction{{EmotionKey: "unknown", Users: []model.MessageReactionUser{{MRI: "8:one"}}}}}
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", msg, "$target"); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(sender.sent) != 0 || len(store.upserts) != 0 {
		t.Fatalf("expected no sends for unmapped reaction")
	}
}

func TestTeamsReactionIngestSkipsWhenNoTarget(t *testing.T) {
	sender := &fakeMatrixReactionSender{}
	store := &fakeReactionStateStore{}
	ingestor := &TeamsReactionIngestor{Sender: sender, Messages: &fakeTeamsMessageMapLookup{}, Reactions: store, Log: zerolog.Nop()}
	msg := model.RemoteMessage{MessageID: "m1", Reactions: []model.MessageReaction{{EmotionKey: "like", Users: []model.MessageReactionUser{{MRI: "8:one"}}}}}
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", msg, ""); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(sender.sent) != 0 || len(store.upserts) != 0 {
		t.Fatalf("expected no sends when target missing")
	}
}

func TestTeamsReactionIngestKeepsMapOnTransientRedactError(t *testing.T) {
	sender := &fakeMatrixReactionSender{redactErr: errors.New("temporary")}
	store := &fakeReactionStateStore{byKey: map[string]*database.ReactionMap{
		"t1|msg/m1|8:one|like": {
			ThreadID: "t1", TeamsMessageID: "msg/m1", TeamsUserID: "8:one", ReactionKey: "like", MatrixReactionEventID: "$reaction",
		},
	}}
	ingestor := &TeamsReactionIngestor{Sender: sender, Messages: &fakeTeamsMessageMapLookup{}, Reactions: store, Log: zerolog.Nop()}
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", model.RemoteMessage{MessageID: "m1"}, ""); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(store.deleted) != 0 {
		t.Fatalf("expected no delete on transient error")
	}
}

func TestTeamsReactionIngestDeletesOnNotFoundRedact(t *testing.T) {
	sender := &fakeMatrixReactionSender{redactErr: mautrix.MNotFound}
	store := &fakeReactionStateStore{byKey: map[string]*database.ReactionMap{
		"t1|msg/m1|8:one|like": {
			ThreadID: "t1", TeamsMessageID: "msg/m1", TeamsUserID: "8:one", ReactionKey: "like", MatrixReactionEventID: "$reaction",
		},
	}}
	ingestor := &TeamsReactionIngestor{Sender: sender, Messages: &fakeTeamsMessageMapLookup{}, Reactions: store, Log: zerolog.Nop()}
	if err := ingestor.IngestMessageReactions(context.Background(), "t1", "!room", model.RemoteMessage{MessageID: "m1"}, ""); err != nil {
		t.Fatalf("IngestMessageReactions failed: %v", err)
	}
	if len(store.deleted) != 1 {
		t.Fatalf("expected delete on M_NOT_FOUND")
	}
}
