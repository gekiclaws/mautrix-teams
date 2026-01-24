package model

import "testing"

func TestNormalizeMissingID(t *testing.T) {
	conv := RemoteConversation{
		ThreadProperties: ThreadProperties{
			OriginalThreadID:  "",
			ProductThreadType: "OneToOneChat",
		},
	}
	_, ok := conv.Normalize()
	if ok {
		t.Fatalf("expected Normalize to skip when originalThreadId is missing")
	}
}

func TestNormalizeOneToOne(t *testing.T) {
	conv := RemoteConversation{
		ThreadProperties: ThreadProperties{
			OriginalThreadID:  "thread-123",
			ProductThreadType: "OneToOneChat",
			CreatedAt:         "2024-01-01T00:00:00Z",
			IsCreator:         true,
		},
	}
	thread, ok := conv.Normalize()
	if !ok {
		t.Fatalf("expected Normalize to succeed")
	}
	if thread.ID != "thread-123" {
		t.Fatalf("unexpected thread ID: %q", thread.ID)
	}
	if !thread.IsOneToOne {
		t.Fatalf("expected IsOneToOne to be true")
	}
	if thread.CreatedAtRaw != "2024-01-01T00:00:00Z" {
		t.Fatalf("unexpected created_at: %q", thread.CreatedAtRaw)
	}
	if !thread.IsCreator {
		t.Fatalf("expected IsCreator to be true")
	}
}
