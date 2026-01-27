package teamsbridge

import (
	"testing"

	"maunium.net/go/mautrix/id"
)

func TestUnreadCycleOneShot(t *testing.T) {
	tracker := NewUnreadCycleTracker()
	roomID := id.RoomID("!room:example.org")

	tracker.MarkUnread(roomID)
	if !tracker.ShouldSendReadReceipt(roomID) {
		t.Fatalf("expected first read receipt to send")
	}
	if tracker.ShouldSendReadReceipt(roomID) {
		t.Fatalf("expected second read receipt to be gated")
	}

	tracker.MarkUnread(roomID)
	if !tracker.ShouldSendReadReceipt(roomID) {
		t.Fatalf("expected new unread cycle to allow receipt")
	}
}

func TestUnreadCycleMultipleUnreadStillOneShot(t *testing.T) {
	tracker := NewUnreadCycleTracker()
	roomID := id.RoomID("!room:example.org")

	tracker.MarkUnread(roomID)
	tracker.MarkUnread(roomID)
	if !tracker.ShouldSendReadReceipt(roomID) {
		t.Fatalf("expected receipt after unread")
	}
	if tracker.ShouldSendReadReceipt(roomID) {
		t.Fatalf("expected one-shot gating even after repeated unread")
	}
}

func TestUnreadCycleEmptyRoomID(t *testing.T) {
	tracker := NewUnreadCycleTracker()
	tracker.MarkUnread("")
	if tracker.ShouldSendReadReceipt("") {
		t.Fatalf("expected empty room id to be ignored")
	}
}
