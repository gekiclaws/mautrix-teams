package teamsbridge

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
)

type fakeReactionClient struct {
	addCalls     []reactionAddCall
	removeCalls  []reactionRemoveCall
	addStatus    int
	removeStatus int
	addErr       error
	removeErr    error
}

type reactionAddCall struct {
	threadID       string
	teamsMessageID string
	emotionKey     string
	appliedAtMS    int64
}

type reactionRemoveCall struct {
	threadID       string
	teamsMessageID string
	emotionKey     string
}

func (f *fakeReactionClient) AddReaction(ctx context.Context, threadID string, teamsMessageID string, emotionKey string, appliedAtMS int64) (int, error) {
	f.addCalls = append(f.addCalls, reactionAddCall{
		threadID:       threadID,
		teamsMessageID: teamsMessageID,
		emotionKey:     emotionKey,
		appliedAtMS:    appliedAtMS,
	})
	return f.addStatus, f.addErr
}

func (f *fakeReactionClient) RemoveReaction(ctx context.Context, threadID string, teamsMessageID string, emotionKey string) (int, error) {
	f.removeCalls = append(f.removeCalls, reactionRemoveCall{
		threadID:       threadID,
		teamsMessageID: teamsMessageID,
		emotionKey:     emotionKey,
	})
	return f.removeStatus, f.removeErr
}

type fakeMessageMapStore struct {
	byMXID map[id.EventID]*database.TeamsMessageMap
}

func (f *fakeMessageMapStore) GetByMXID(mxid id.EventID) *database.TeamsMessageMap {
	if f == nil || f.byMXID == nil {
		return nil
	}
	return f.byMXID[mxid]
}

type fakeReactionMapStore struct {
	byKey      map[string]*database.ReactionMap
	byReaction map[string]*database.ReactionMap
	upserts    []*database.ReactionMap
	deleted    []string
}

func reactionMapKey(threadID, teamsMessageID, teamsUserID, reactionKey string) string {
	return threadID + "|" + teamsMessageID + "|" + teamsUserID + "|" + reactionKey
}

func reactionMapEventKey(roomID id.RoomID, eventID id.EventID) string {
	return roomID.String() + "|" + eventID.String()
}

func (f *fakeReactionMapStore) GetByKey(threadID string, teamsMessageID string, teamsUserID string, reactionKey string) *database.ReactionMap {
	if f == nil || f.byKey == nil {
		return nil
	}
	return f.byKey[reactionMapKey(threadID, teamsMessageID, teamsUserID, reactionKey)]
}

func (f *fakeReactionMapStore) GetByMatrixReaction(roomID id.RoomID, reactionEventID id.EventID) *database.ReactionMap {
	if f == nil || f.byReaction == nil {
		return nil
	}
	return f.byReaction[reactionMapEventKey(roomID, reactionEventID)]
}

func (f *fakeReactionMapStore) Upsert(mapping *database.ReactionMap) error {
	if f.byKey == nil {
		f.byKey = make(map[string]*database.ReactionMap)
	}
	if f.byReaction == nil {
		f.byReaction = make(map[string]*database.ReactionMap)
	}
	f.upserts = append(f.upserts, mapping)
	f.byKey[reactionMapKey(mapping.ThreadID, mapping.TeamsMessageID, mapping.TeamsUserID, mapping.ReactionKey)] = mapping
	f.byReaction[reactionMapEventKey(mapping.MatrixRoomID, mapping.MatrixReactionEventID)] = mapping
	return nil
}

func (f *fakeReactionMapStore) DeleteByKey(threadID string, teamsMessageID string, teamsUserID string, reactionKey string) error {
	key := reactionMapKey(threadID, teamsMessageID, teamsUserID, reactionKey)
	f.deleted = append(f.deleted, key)
	if mapping := f.byKey[key]; mapping != nil {
		delete(f.byReaction, reactionMapEventKey(mapping.MatrixRoomID, mapping.MatrixReactionEventID))
	}
	delete(f.byKey, key)
	return nil
}

func TestTeamsConsumerReactorAddSuccess(t *testing.T) {
	client := &fakeReactionClient{addStatus: 200}
	messages := &fakeMessageMapStore{byMXID: map[id.EventID]*database.TeamsMessageMap{
		"$target": {MXID: "$target", ThreadID: "19:abc@thread.v2", TeamsMessageID: "1"},
	}}
	reactions := &fakeReactionMapStore{}
	reactor := NewTeamsConsumerReactor(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, messages, reactions, "8:live:me", zerolog.Nop())

	evt := &event.Event{
		ID:   id.EventID("$reaction"),
		Type: event.EventReaction,
		Content: event.Content{Parsed: &event.ReactionEventContent{RelatesTo: event.RelatesTo{
			Type:    event.RelAnnotation,
			EventID: "$target",
			Key:     "👍",
		}}},
	}

	if err := reactor.AddMatrixReaction(context.Background(), "!room:example", evt); err != nil {
		t.Fatalf("AddMatrixReaction failed: %v", err)
	}
	if len(client.addCalls) != 1 {
		t.Fatalf("expected add call, got %d", len(client.addCalls))
	}
	call := client.addCalls[0]
	if call.threadID != "19:abc@thread.v2" || call.teamsMessageID != "1" || call.emotionKey != "like" {
		t.Fatalf("unexpected add call: %#v", call)
	}
	if call.appliedAtMS == 0 {
		t.Fatalf("expected applied timestamp")
	}
	if len(reactions.upserts) != 1 {
		t.Fatalf("expected reaction map upsert, got %d", len(reactions.upserts))
	}
	stored := reactions.upserts[0]
	if stored.TeamsUserID != "8:live:me" || stored.TeamsMessageID != "msg/1" {
		t.Fatalf("unexpected mapping: %#v", stored)
	}
}

func TestTeamsConsumerReactorAddSkipsIfMapped(t *testing.T) {
	client := &fakeReactionClient{addStatus: 200}
	messages := &fakeMessageMapStore{byMXID: map[id.EventID]*database.TeamsMessageMap{
		"$target": {MXID: "$target", ThreadID: "19:abc@thread.v2", TeamsMessageID: "msg/1"},
	}}
	reactions := &fakeReactionMapStore{byKey: map[string]*database.ReactionMap{
		reactionMapKey("19:abc@thread.v2", "msg/1", "8:live:me", "like"): {
			ThreadID: "19:abc@thread.v2", TeamsMessageID: "msg/1", TeamsUserID: "8:live:me", ReactionKey: "like",
		},
	}}
	reactor := NewTeamsConsumerReactor(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, messages, reactions, "8:live:me", zerolog.Nop())

	evt := &event.Event{
		ID:   id.EventID("$reaction"),
		Type: event.EventReaction,
		Content: event.Content{Parsed: &event.ReactionEventContent{RelatesTo: event.RelatesTo{
			Type:    event.RelAnnotation,
			EventID: "$target",
			Key:     "👍",
		}}},
	}

	if err := reactor.AddMatrixReaction(context.Background(), "!room:example", evt); err != nil {
		t.Fatalf("AddMatrixReaction failed: %v", err)
	}
	if len(client.addCalls) != 0 {
		t.Fatalf("expected no add call, got %d", len(client.addCalls))
	}
	if len(reactions.upserts) != 0 {
		t.Fatalf("expected no upserts, got %d", len(reactions.upserts))
	}
}

func TestTeamsConsumerReactorRemoveSuccess(t *testing.T) {
	client := &fakeReactionClient{removeStatus: 200}
	reactions := &fakeReactionMapStore{byReaction: map[string]*database.ReactionMap{
		reactionMapEventKey("!room:example", "$reaction"): {
			ThreadID: "19:abc@thread.v2", TeamsMessageID: "msg/1", TeamsUserID: "8:live:me", ReactionKey: "like",
			MatrixRoomID: "!room:example", MatrixTargetEventID: "$target", MatrixReactionEventID: "$reaction",
		},
	}}
	reactor := NewTeamsConsumerReactor(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, &fakeMessageMapStore{}, reactions, "8:live:me", zerolog.Nop())

	evt := &event.Event{
		ID:      id.EventID("$redaction"),
		Type:    event.EventRedaction,
		Redacts: id.EventID("$reaction"),
	}

	if err := reactor.RemoveMatrixReaction(context.Background(), "!room:example", evt); err != nil {
		t.Fatalf("RemoveMatrixReaction failed: %v", err)
	}
	if len(client.removeCalls) != 1 {
		t.Fatalf("expected remove call, got %d", len(client.removeCalls))
	}
	call := client.removeCalls[0]
	if call.threadID != "19:abc@thread.v2" || call.teamsMessageID != "1" || call.emotionKey != "like" {
		t.Fatalf("unexpected remove call: %#v", call)
	}
	if len(reactions.deleted) != 1 || reactions.deleted[0] != reactionMapKey("19:abc@thread.v2", "msg/1", "8:live:me", "like") {
		t.Fatalf("unexpected delete calls: %#v", reactions.deleted)
	}
}

func TestTeamsConsumerReactorAddUnmapped(t *testing.T) {
	client := &fakeReactionClient{addStatus: 200}
	messages := &fakeMessageMapStore{byMXID: map[id.EventID]*database.TeamsMessageMap{
		"$target": {MXID: "$target", ThreadID: "19:abc@thread.v2", TeamsMessageID: "m1"},
	}}
	reactions := &fakeReactionMapStore{}
	reactor := NewTeamsConsumerReactor(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, messages, reactions, "8:live:me", zerolog.Nop())

	evt := &event.Event{
		ID:   id.EventID("$reaction"),
		Type: event.EventReaction,
		Content: event.Content{Parsed: &event.ReactionEventContent{RelatesTo: event.RelatesTo{
			Type:    event.RelAnnotation,
			EventID: "$target",
			Key:     "❓",
		}}},
	}

	if err := reactor.AddMatrixReaction(context.Background(), "!room:example", evt); err != nil {
		t.Fatalf("AddMatrixReaction failed: %v", err)
	}
	if len(client.addCalls) != 0 {
		t.Fatalf("expected no add call, got %d", len(client.addCalls))
	}
}
