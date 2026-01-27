package model

import (
	"testing"
	"time"
)

func TestNormalizeTeamsUserID(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"   ":         "",
		"user":        "user",
		" user ":      "user",
		" 8:live:id ": "8:live:id",
	}
	for input, expected := range cases {
		if got := NormalizeTeamsUserID(input); got != expected {
			t.Fatalf("NormalizeTeamsUserID(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestChooseLastSeenTS(t *testing.T) {
	messageTS := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	now := time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC)

	if got := ChooseLastSeenTS(messageTS, now); !got.Equal(messageTS) {
		t.Fatalf("expected message timestamp, got %s", got.Format(time.RFC3339Nano))
	}

	zero := time.Time{}
	if got := ChooseLastSeenTS(zero, now); !got.Equal(now) {
		t.Fatalf("expected fallback timestamp, got %s", got.Format(time.RFC3339Nano))
	}
}
