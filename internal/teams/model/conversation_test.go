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
		ID: "@oneToOne.skype",
		ThreadProperties: ThreadProperties{
			OriginalThreadID:  "thread-123",
			ProductThreadType: "OneToOneChat",
			CreatedAt:         "2024-01-01T00:00:00Z",
			IsCreator:         true,
		},
		Members: []ConversationMember{
			{ID: "8:self", DisplayName: "Me", IsSelf: true},
			{ID: "8:other", DisplayName: "Alex"},
		},
	}
	thread, ok := conv.NormalizeForSelf("8:self")
	if !ok {
		t.Fatalf("expected Normalize to succeed")
	}
	if thread.ID != "thread-123" {
		t.Fatalf("unexpected thread ID: %q", thread.ID)
	}
	if thread.ConversationID != "@oneToOne.skype" {
		t.Fatalf("unexpected conversation ID: %q", thread.ConversationID)
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
	if thread.RoomName != "Alex" {
		t.Fatalf("unexpected room name: %q", thread.RoomName)
	}
}

func TestNormalizeGroupUsesThreadName(t *testing.T) {
	conv := RemoteConversation{
		ID: "19:group@thread.v2",
		ThreadProperties: ThreadProperties{
			OriginalThreadID:  "thread-group",
			ProductThreadType: "GroupChat",
			Topic:             "Project Alpha",
		},
	}
	thread, ok := conv.NormalizeForSelf("8:self")
	if !ok {
		t.Fatalf("expected Normalize to succeed")
	}
	if thread.RoomName != "Project Alpha" {
		t.Fatalf("unexpected room name: %q", thread.RoomName)
	}
}

func TestNormalizeFallbackRoomName(t *testing.T) {
	conv := RemoteConversation{
		ID: "19:group@thread.v2",
		ThreadProperties: ThreadProperties{
			OriginalThreadID:  "thread-group",
			ProductThreadType: "GroupChat",
		},
	}
	thread, ok := conv.Normalize()
	if !ok {
		t.Fatalf("expected Normalize to succeed")
	}
	if thread.RoomName != "Chat" {
		t.Fatalf("unexpected fallback room name: %q", thread.RoomName)
	}
}

func TestNormalizeBotAugmentedOneToOneStillUsesHumanName(t *testing.T) {
	conv := RemoteConversation{
		ID: "19:thread@thread.v2",
		ThreadProperties: ThreadProperties{
			OriginalThreadID:  "thread-bot-mixed",
			ProductThreadType: "GroupChat",
		},
		Members: []ConversationMember{
			{ID: "8:self", DisplayName: "Me", IsSelf: true},
			{ID: "28:teamsbot", DisplayName: "Teams Bot"},
			{ID: "8:live:.cid.remote", DisplayName: "Max W"},
		},
	}
	thread, ok := conv.NormalizeForSelf("8:self")
	if !ok {
		t.Fatalf("expected Normalize to succeed")
	}
	if !thread.IsOneToOne {
		t.Fatalf("expected bot-augmented chat to be treated as one-to-one")
	}
	if thread.RoomName != "Max W" {
		t.Fatalf("unexpected room name: %q", thread.RoomName)
	}
}

func TestNormalizeOneToOneSkipsSelfGhostDisplayName(t *testing.T) {
	conv := RemoteConversation{
		ID: "@oneToOne.skype",
		ThreadProperties: ThreadProperties{
			OriginalThreadID:  "thread-ghost-dm",
			ProductThreadType: "OneToOneChat",
		},
		Members: []ConversationMember{
			{ID: "8:live:self", DisplayName: "Me", IsSelf: true},
			{ID: "28:teamsbot", DisplayName: "Teams Bot"},
			{ID: "29:self-ghost", DisplayName: "Me"},
			{ID: "29:remote-ghost", DisplayName: "Alex"},
		},
	}

	thread, ok := conv.NormalizeForSelf("8:live:self")
	if !ok {
		t.Fatalf("expected Normalize to succeed")
	}
	if !thread.IsOneToOne {
		t.Fatalf("expected IsOneToOne to be true")
	}
	if thread.RoomName != "Alex" {
		t.Fatalf("unexpected room name: %q", thread.RoomName)
	}
}

func TestNormalizeGroupChatInferenceHandlesSelfGhostPair(t *testing.T) {
	conv := RemoteConversation{
		ID: "19:thread@thread.v2",
		ThreadProperties: ThreadProperties{
			OriginalThreadID:  "thread-ghost-inferred",
			ProductThreadType: "GroupChat",
		},
		Members: []ConversationMember{
			{ID: "8:live:self", DisplayName: "Me", IsSelf: true},
			{ID: "28:teamsbot", DisplayName: "Teams Bot"},
			{ID: "29:self-ghost", DisplayName: "Me"},
			{ID: "29:remote-ghost", DisplayName: "Alex"},
		},
	}

	thread, ok := conv.NormalizeForSelf("8:live:self")
	if !ok {
		t.Fatalf("expected Normalize to succeed")
	}
	if !thread.IsOneToOne {
		t.Fatalf("expected bot+ghost augmented chat to be treated as one-to-one")
	}
	if thread.RoomName != "Alex" {
		t.Fatalf("unexpected room name: %q", thread.RoomName)
	}
}
