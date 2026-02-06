package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTeamsLoginAuthFailureReply(t *testing.T) {
	configPath := "config.yaml"
	expiredErr := &TeamsAuthExpiredError{ExpiresAt: time.Unix(100, 0).UTC()}

	tests := []struct {
		name    string
		err     error
		mustHas []string
	}{
		{
			name:    "missing file",
			err:     ErrTeamsAuthMissingFile,
			mustHas: []string{"Teams auth missing", "teams-login -c config.yaml"},
		},
		{
			name:    "missing token",
			err:     ErrTeamsAuthMissingToken,
			mustHas: []string{"Teams auth missing", "teams-login -c config.yaml"},
		},
		{
			name:    "expired",
			err:     expiredErr,
			mustHas: []string{"Teams auth expired at 1970-01-01T00:01:40Z", "teams-login -c config.yaml"},
		},
		{
			name:    "invalid json",
			err:     ErrTeamsAuthInvalidJSON,
			mustHas: []string{"invalid JSON", "teams-login -c config.yaml"},
		},
		{
			name:    "unknown",
			err:     errors.New("boom"),
			mustHas: []string{"Failed to load Teams auth", "teams-login -c config.yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := teamsLoginAuthFailureReply(tt.err, configPath)
			for _, snippet := range tt.mustHas {
				if !strings.Contains(msg, snippet) {
					t.Fatalf("reply %q missing %q", msg, snippet)
				}
			}
		})
	}
}
