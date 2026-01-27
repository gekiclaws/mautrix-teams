package teamsbridge

import (
	"sync"

	"maunium.net/go/mautrix/id"
)

type unreadCycleState struct {
	unread      bool
	receiptSent bool
}

type UnreadCycleTracker struct {
	mu     sync.Mutex
	states map[id.RoomID]unreadCycleState
}

func NewUnreadCycleTracker() *UnreadCycleTracker {
	return &UnreadCycleTracker{states: make(map[id.RoomID]unreadCycleState)}
}

func (t *UnreadCycleTracker) MarkUnread(roomID id.RoomID) {
	if t == nil || roomID == "" {
		return
	}
	t.mu.Lock()
	t.states[roomID] = unreadCycleState{unread: true, receiptSent: false}
	t.mu.Unlock()
}

// ShouldSendReadReceipt returns true exactly once per unread cycle.
func (t *UnreadCycleTracker) ShouldSendReadReceipt(roomID id.RoomID) bool {
	if t == nil || roomID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.states[roomID]
	if !state.unread {
		return false
	}
	if state.receiptSent {
		return false
	}
	state.receiptSent = true
	state.unread = false
	t.states[roomID] = state
	return true
}
