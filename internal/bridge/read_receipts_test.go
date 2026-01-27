package teamsbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"
)

type fakeReceiptClient struct {
	calls []receiptCall
	err   error
	code  int
}

type receiptCall struct {
	threadID string
	horizon  string
}

func (f *fakeReceiptClient) SetConsumptionHorizon(ctx context.Context, threadID string, horizon string) (int, error) {
	f.calls = append(f.calls, receiptCall{threadID: threadID, horizon: horizon})
	if f.code == 0 {
		f.code = 200
	}
	return f.code, f.err
}

type fakeReceiptUnreadTracker struct {
	allow bool
}

func (f *fakeReceiptUnreadTracker) ShouldSendReadReceipt(roomID id.RoomID) bool {
	if !f.allow {
		return false
	}
	f.allow = false
	return true
}

func TestTeamsConsumerReceiptSenderOneShot(t *testing.T) {
	client := &fakeReceiptClient{}
	unread := &fakeReceiptUnreadTracker{allow: true}
	sender := NewTeamsConsumerReceiptSender(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, unread, zerolog.Nop())
	now := time.UnixMilli(1700000000123)

	if err := sender.SendReadReceipt(context.Background(), id.RoomID("!room:example.org"), now); err != nil {
		t.Fatalf("SendReadReceipt failed: %v", err)
	}
	if err := sender.SendReadReceipt(context.Background(), id.RoomID("!room:example.org"), now); err != nil {
		t.Fatalf("SendReadReceipt second call failed: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one receipt call, got %d", len(client.calls))
	}
	if client.calls[0].threadID != "19:abc@thread.v2" {
		t.Fatalf("unexpected thread id: %q", client.calls[0].threadID)
	}
}

func TestTeamsConsumerReceiptSenderNoUnread(t *testing.T) {
	client := &fakeReceiptClient{}
	unread := &fakeReceiptUnreadTracker{allow: false}
	sender := NewTeamsConsumerReceiptSender(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, unread, zerolog.Nop())

	if err := sender.SendReadReceipt(context.Background(), id.RoomID("!room:example.org"), time.Now().UTC()); err != nil {
		t.Fatalf("SendReadReceipt failed: %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no receipt calls, got %d", len(client.calls))
	}
}

func TestTeamsConsumerReceiptSenderMissingThread(t *testing.T) {
	client := &fakeReceiptClient{}
	unread := &fakeReceiptUnreadTracker{allow: true}
	sender := NewTeamsConsumerReceiptSender(client, fakeThreadLookup{ok: false}, unread, zerolog.Nop())

	err := sender.SendReadReceipt(context.Background(), id.RoomID("!room:example.org"), time.Now().UTC())
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no receipt calls, got %d", len(client.calls))
	}
}

func TestTeamsConsumerReceiptSenderPropagatesError(t *testing.T) {
	client := &fakeReceiptClient{err: errors.New("boom")}
	unread := &fakeReceiptUnreadTracker{allow: true}
	sender := NewTeamsConsumerReceiptSender(client, fakeThreadLookup{threadID: "19:abc@thread.v2", ok: true}, unread, zerolog.Nop())

	err := sender.SendReadReceipt(context.Background(), id.RoomID("!room:example.org"), time.Now().UTC())
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one receipt call, got %d", len(client.calls))
	}
}
