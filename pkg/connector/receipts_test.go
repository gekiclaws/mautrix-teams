package connector

import "testing"

func TestUnreadCycleGating(t *testing.T) {
	client := &TeamsClient{}
	threadID := "19:thread@thread.v2"
	if client.shouldSendReceipt(threadID) {
		t.Fatalf("should not send receipt without unread")
	}
	client.markUnread(threadID)
	if !client.shouldSendReceipt(threadID) {
		t.Fatalf("expected first receipt to be allowed after unread")
	}
	if client.shouldSendReceipt(threadID) {
		t.Fatalf("should not send receipt twice for same unread cycle")
	}
	client.markUnread(threadID)
	if !client.shouldSendReceipt(threadID) {
		t.Fatalf("expected receipt after new unread cycle")
	}
}
