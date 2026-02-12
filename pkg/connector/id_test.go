package connector

import "testing"

func TestIsLikelyThreadID(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{input: "", want: false},
		{input: "8:live:someone", want: false},
		{input: "19:abc@thread.v2", want: true},
		{input: "19:abc@THREAD.V2", want: true},
		{input: "19:abc@thread.skype", want: true},
		{input: "19:abc@unq.gbl.spaces", want: true},
	}

	for _, tc := range cases {
		if got := isLikelyThreadID(tc.input); got != tc.want {
			t.Fatalf("isLikelyThreadID(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
