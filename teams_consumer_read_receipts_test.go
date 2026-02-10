package main

import (
	"testing"

	"go.mau.fi/mautrix-teams/config"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
)

func TestMXIDForTeamsVirtualUser_DeterministicScheme(t *testing.T) {
	br := &TeamsBridge{
		Config: &config.Config{
			BaseConfig: &bridgeconfig.BaseConfig{},
			Bridge:     config.BridgeConfig{},
		},
	}
	br.Config.Homeserver.Domain = "beeper.local"

	got := br.mxidForTeamsVirtualUser("8:live:.cid.af0c29ad04be1b79")
	want := "@sh-msteams_8=3alive=3a.cid.af0c29ad04be1b79:beeper.local"
	if got.String() != want {
		t.Fatalf("mxid mismatch: got %s want %s", got.String(), want)
	}
}
