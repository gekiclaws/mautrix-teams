package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/config"
)

func TestResolveTeamsAdminInviteMXIDsReturnsExplicitAdmins(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	br := &DiscordBridge{
		Config: &config.Config{
			Bridge: config.BridgeConfig{
				Permissions: bridgeconfig.PermissionConfig{
					"*":                   bridgeconfig.PermissionLevelRelay,
					"example.org":         bridgeconfig.PermissionLevelAdmin,
					"@second:example.org": bridgeconfig.PermissionLevelAdmin,
					"@admin:example.org":  bridgeconfig.PermissionLevelAdmin,
				},
			},
		},
	}

	got := br.resolveTeamsAdminInviteMXIDs(log)
	want := []id.UserID{"@admin:example.org", "@second:example.org"}
	if len(got) != len(want) {
		t.Fatalf("unexpected admin count: got %d, want %d", len(got), len(want))
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("unexpected admin at index %d: got %s, want %s", idx, got[idx], want[idx])
		}
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no warning logs for explicit admins, got: %s", buf.String())
	}
}

func TestResolveTeamsAdminInviteMXIDsWarnsOnceWithoutExplicitAdmins(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	br := &DiscordBridge{
		Config: &config.Config{
			Bridge: config.BridgeConfig{
				Permissions: bridgeconfig.PermissionConfig{
					"*":           bridgeconfig.PermissionLevelAdmin,
					"example.org": bridgeconfig.PermissionLevelAdmin,
				},
			},
		},
	}

	first := br.resolveTeamsAdminInviteMXIDs(log)
	second := br.resolveTeamsAdminInviteMXIDs(log)
	if len(first) != 0 || len(second) != 0 {
		t.Fatalf("expected no explicit admin mxids, got %v and %v", first, second)
	}

	const warnMsg = "no explicit admin mxids found in bridge.permissions, skipping teams room admin invites"
	if got := strings.Count(buf.String(), warnMsg); got != 1 {
		t.Fatalf("expected warning once, got %d occurrences in logs: %s", got, buf.String())
	}
}
