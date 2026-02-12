// mautrix-teams - A Matrix-Microsoft Teams bridge.
//
// This is a bridgev2-based implementation focused on provisioning/login flows and multi-login support.
package main

import (
	"maunium.net/go/mautrix/bridgev2/matrix/mxmain"

	"go.mau.fi/mautrix-teams/pkg/connector"
)

// Build metadata, filled with -X linker flags.
var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

var m = mxmain.BridgeMain{
	Name:        "mautrix-teams",
	URL:         "https://github.com/mautrix/teams",
	Description: "A Matrix-Microsoft Teams bridge.",
	Version:     "26.02",
	SemCalVer:   true,

	Connector: &connector.TeamsConnector{},
}

func main() {
	m.InitVersion(Tag, Commit, BuildTime)
	m.Run()
}
