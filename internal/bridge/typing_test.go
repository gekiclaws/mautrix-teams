package teamsbridge

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"
)

type fakeTypingClient struct {
	calls []typingCall
	err   error
	code  int
}

type typingCall struct {
	threadID   string
	fromUserID string
}

func (f *fakeTypingClient) SendTypingIndicator(ctx context.Context, threadID string, fromUserID string) (int, error) {
	f.calls = append(f.calls, typingCall{threadID: threadID, fromUserID: fromUserID})
	if f.code == 0 {
		f.code = 200
	}
	return f.code, f.err
}

func TestTeamsConsumerTyperSendTyping(t *testing.T) {
	client := &fakeTypingClient{}
	typer := NewTeamsConsumerTyper(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, "8:live:me", zerolog.Nop())

	err := typer.SendTyping(context.Background(), id.RoomID("!room:example.org"))
	if err != nil {
		t.Fatalf("SendTyping failed: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one typing call, got %d", len(client.calls))
	}
	if client.calls[0].threadID != "19:abc@thread.v2" {
		t.Fatalf("unexpected thread id: %q", client.calls[0].threadID)
	}
	if client.calls[0].fromUserID != "8:live:me" {
		t.Fatalf("unexpected from user id: %q", client.calls[0].fromUserID)
	}
}

func TestTeamsConsumerTyperMissingThread(t *testing.T) {
	client := &fakeTypingClient{}
	typer := NewTeamsConsumerTyper(client, fakeThreadLookup{ok: false}, "8:live:me", zerolog.Nop())

	err := typer.SendTyping(context.Background(), id.RoomID("!room:example.org"))
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no typing calls, got %d", len(client.calls))
	}
}

func TestTeamsConsumerTyperPropagatesError(t *testing.T) {
	client := &fakeTypingClient{err: errors.New("boom")}
	typer := NewTeamsConsumerTyper(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, "8:live:me", zerolog.Nop())

	err := typer.SendTyping(context.Background(), id.RoomID("!room:example.org"))
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one typing call, got %d", len(client.calls))
	}
}
