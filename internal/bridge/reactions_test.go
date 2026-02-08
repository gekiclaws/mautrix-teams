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
	byReaction map[id.EventID]*database.TeamsReactionMap
	inserted   []*database.TeamsReactionMap
	deleted    []id.EventID
}

func (f *fakeReactionMapStore) GetByReactionMXID(mxid id.EventID) *database.TeamsReactionMap {
	if f == nil || f.byReaction == nil {
		return nil
	}
	return f.byReaction[mxid]
}

func (f *fakeReactionMapStore) Insert(mapping *database.TeamsReactionMap) error {
	f.inserted = append(f.inserted, mapping)
	if f.byReaction == nil {
		f.byReaction = make(map[id.EventID]*database.TeamsReactionMap)
	}
	f.byReaction[mapping.ReactionMXID] = mapping
	return nil
}

func (f *fakeReactionMapStore) Delete(reactionMXID id.EventID) error {
	f.deleted = append(f.deleted, reactionMXID)
	delete(f.byReaction, reactionMXID)
	return nil
}

func TestTeamsConsumerReactorAddSuccess(t *testing.T) {
	client := &fakeReactionClient{addStatus: 200}
	messages := &fakeMessageMapStore{byMXID: map[id.EventID]*database.TeamsMessageMap{
		"$target": {MXID: "$target", ThreadID: "19:abc@thread.v2", TeamsMessageID: "1"},
	}}
	reactions := &fakeReactionMapStore{}
	reactor := NewTeamsConsumerReactor(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, messages, reactions, zerolog.Nop())

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
	if call.threadID != "19:abc@thread.v2" || call.teamsMessageID != "msg/1" || call.emotionKey != "like" {
		t.Fatalf("unexpected add call: %#v", call)
	}
	if call.appliedAtMS == 0 {
		t.Fatalf("expected applied timestamp")
	}
	if len(reactions.inserted) != 1 {
		t.Fatalf("expected reaction map insert, got %d", len(reactions.inserted))
	}
}

func TestTeamsConsumerReactorAddUnmapped(t *testing.T) {
	client := &fakeReactionClient{addStatus: 200}
	messages := &fakeMessageMapStore{byMXID: map[id.EventID]*database.TeamsMessageMap{
		"$target": {MXID: "$target", ThreadID: "19:abc@thread.v2", TeamsMessageID: "m1"},
	}}
	reactions := &fakeReactionMapStore{}
	reactor := NewTeamsConsumerReactor(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, messages, reactions, zerolog.Nop())

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
	if len(reactions.inserted) != 0 {
		t.Fatalf("expected no reaction map insert, got %d", len(reactions.inserted))
	}
}

func TestTeamsConsumerReactorRemoveSuccess(t *testing.T) {
	client := &fakeReactionClient{removeStatus: 200}
	messages := &fakeMessageMapStore{byMXID: map[id.EventID]*database.TeamsMessageMap{
		"$target": {MXID: "$target", ThreadID: "19:abc@thread.v2", TeamsMessageID: "1"},
	}}
	reactions := &fakeReactionMapStore{byReaction: map[id.EventID]*database.TeamsReactionMap{
		"$reaction": {ReactionMXID: "$reaction", TargetMXID: "$target", EmotionKey: "like"},
	}}
	reactor := NewTeamsConsumerReactor(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, messages, reactions, zerolog.Nop())

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
	if call.threadID != "19:abc@thread.v2" || call.teamsMessageID != "msg/1" || call.emotionKey != "like" {
		t.Fatalf("unexpected remove call: %#v", call)
	}
	if len(reactions.deleted) != 1 || reactions.deleted[0] != "$reaction" {
		t.Fatalf("unexpected delete calls: %#v", reactions.deleted)
	}
}

func TestTeamsConsumerReactorRemoveMissingMessage(t *testing.T) {
	client := &fakeReactionClient{removeStatus: 200}
	reactions := &fakeReactionMapStore{byReaction: map[id.EventID]*database.TeamsReactionMap{
		"$reaction": {ReactionMXID: "$reaction", TargetMXID: "$target", EmotionKey: "like"},
	}}
	reactor := NewTeamsConsumerReactor(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, &fakeMessageMapStore{}, reactions, zerolog.Nop())

	evt := &event.Event{ID: id.EventID("$redaction"), Type: event.EventRedaction, Redacts: id.EventID("$reaction")}

	if err := reactor.RemoveMatrixReaction(context.Background(), "!room:example", evt); err != nil {
		t.Fatalf("RemoveMatrixReaction failed: %v", err)
	}
	if len(client.removeCalls) != 0 {
		t.Fatalf("expected no remove call, got %d", len(client.removeCalls))
	}
	if len(reactions.deleted) != 0 {
		t.Fatalf("expected no delete calls, got %d", len(reactions.deleted))
	}
}

func TestTeamsConsumerReactorSkipsTeamsIngestedReactionEcho(t *testing.T) {
	client := &fakeReactionClient{addStatus: 200}
	messages := &fakeMessageMapStore{byMXID: map[id.EventID]*database.TeamsMessageMap{
		"$target": {MXID: "$target", ThreadID: "19:abc@thread.v2", TeamsMessageID: "1"},
	}}
	reactions := &fakeReactionMapStore{}
	reactor := NewTeamsConsumerReactor(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, messages, reactions, zerolog.Nop())

	evt := &event.Event{
		ID:     id.EventID("$reaction"),
		Sender: id.UserID("@bot:example"),
		Type:   event.EventReaction,
		Content: event.Content{Parsed: &event.ReactionEventContent{RelatesTo: event.RelatesTo{
			Type:    event.RelAnnotation,
			EventID: "$target",
			Key:     "👍",
		}}, Raw: map[string]any{
			"com.beeper.teams.ingested_reaction": true,
		}},
	}

	if err := reactor.AddMatrixReaction(context.Background(), "!room:example", evt); err != nil {
		t.Fatalf("AddMatrixReaction failed: %v", err)
	}
	if len(client.addCalls) != 0 {
		t.Fatalf("expected no add call, got %d", len(client.addCalls))
	}
}
