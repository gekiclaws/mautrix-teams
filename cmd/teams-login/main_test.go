package main

import "testing"

func TestShouldRunProbe(t *testing.T) {
	tests := []struct {
		name     string
		disabled bool
		want     bool
	}{
		{name: "default enabled", disabled: false, want: true},
		{name: "disabled", disabled: true, want: false},
	}
	for _, tt := range tests {
		if got := shouldRunProbe(tt.disabled); got != tt.want {
			t.Fatalf("%s: got %v want %v", tt.name, got, tt.want)
		}
	}
}
