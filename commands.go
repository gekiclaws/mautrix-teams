package main

import "maunium.net/go/mautrix/bridge/commands"

type WrappedCommandEvent struct {
	*commands.Event
	Bridge *TeamsBridge
	User   *User
	Portal *Portal
}

func (br *TeamsBridge) RegisterCommands() {
	// Phase 1: remove Discord-era command surface.
}
