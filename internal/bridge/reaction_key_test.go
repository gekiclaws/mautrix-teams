package teamsbridge

import "testing"

func TestNewReactionKeyNormalizesInputs(t *testing.T) {
	key, ok := NewReactionKey(" 19:abc@thread.v2 ", "123", " 8:live:alice ", " like ")
	if !ok {
		t.Fatalf("expected key")
	}
	if key.ThreadID != "19:abc@thread.v2" {
		t.Fatalf("unexpected thread id: %q", key.ThreadID)
	}
	if key.TeamsMessageID != "msg/123" {
		t.Fatalf("unexpected teams message id: %q", key.TeamsMessageID)
	}
	if key.TeamsUserID != "8:live:alice" {
		t.Fatalf("unexpected teams user id: %q", key.TeamsUserID)
	}
	if key.ReactionKey != "like" {
		t.Fatalf("unexpected reaction key: %q", key.ReactionKey)
	}
}

func TestNewReactionKeyRejectsMissingFields(t *testing.T) {
	if _, ok := NewReactionKey("", "123", "8:live:alice", "like"); ok {
		t.Fatalf("expected missing thread id to fail")
	}
	if _, ok := NewReactionKey("thread", "", "8:live:alice", "like"); ok {
		t.Fatalf("expected missing message id to fail")
	}
	if _, ok := NewReactionKey("thread", "123", "", "like"); ok {
		t.Fatalf("expected missing teams user id to fail")
	}
	if _, ok := NewReactionKey("thread", "123", "8:live:alice", ""); ok {
		t.Fatalf("expected missing reaction key to fail")
	}
}
