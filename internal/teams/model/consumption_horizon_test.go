package model

import "testing"

func TestParseConsumptionHorizonLatestReadTS(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantTS int64
		wantOK bool
	}{
		{
			name:   "valid three parts",
			input:  "1;1769620117227;2621949452385992439",
			wantTS: 1769620117227,
			wantOK: true,
		},
		{
			name:   "missing second segment",
			input:  "1;",
			wantOK: false,
		},
		{
			name:   "single part",
			input:  "1",
			wantOK: false,
		},
		{
			name:   "non-numeric second segment",
			input:  "1;not-a-number;2",
			wantOK: false,
		},
		{
			name:   "whitespace second segment",
			input:  "1;   ;2",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTS, gotOK := ParseConsumptionHorizonLatestReadTS(tt.input)
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotOK && gotTS != tt.wantTS {
				t.Fatalf("ts = %d, want %d", gotTS, tt.wantTS)
			}
		})
	}
}
