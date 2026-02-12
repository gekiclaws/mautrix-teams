package connector

import "testing"

func TestNormalizeTeamsReactionMessageID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "  ", want: ""},
		{name: "raw", input: "1770719942457", want: "1770719942457"},
		{name: "prefixed", input: "msg/1770719942457", want: "1770719942457"},
		{name: "other slash form", input: "abc/def", want: "abc/def"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeTeamsReactionMessageID(tc.input)
			if got != tc.want {
				t.Fatalf("unexpected normalized message id: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeTeamsReactionTargetMessageID(t *testing.T) {
	if got := NormalizeTeamsReactionTargetMessageID("msg/123"); got != "123" {
		t.Fatalf("unexpected target id: %q", got)
	}
	if got := NormalizeTeamsReactionTargetMessageID("123"); got != "123" {
		t.Fatalf("unexpected target id: %q", got)
	}
}
