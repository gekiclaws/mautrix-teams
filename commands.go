package main

import (
	"time"

	"maunium.net/go/mautrix/bridge/commands"
)

type WrappedCommandEvent struct {
	*commands.Event
	Bridge *TeamsBridge
	User   *User
	Portal *Portal
}

func (br *TeamsBridge) RegisterCommands() {
	proc := br.CommandProcessor.(*commands.Processor)
	proc.AddHandlers(
		cmdTeamsLogin,
	)
}

func wrapCommand(handler func(*WrappedCommandEvent)) func(*commands.Event) {
	return func(ce *commands.Event) {
		user := ce.User.(*User)
		var portal *Portal
		if ce.Portal != nil {
			portal = ce.Portal.(*Portal)
		}
		br := ce.Bridge.Child.(*TeamsBridge)
		handler(&WrappedCommandEvent{ce, br, user, portal})
	}
}

var cmdTeamsLogin = &commands.FullHandler{
	Func:    wrapCommand(fnTeamsLogin),
	Name:    "login",
	Aliases: []string{"teams-login"},
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Validate Teams auth.json and activate Teams auth for this bridge process.",
	},
}

func fnTeamsLogin(ce *WrappedCommandEvent) {
	now := time.Now().UTC()
	if ce.Bridge.hasValidTeamsAuth(now) {
		ce.Reply("Teams auth already active.")
		return
	}

	state, authPath, err := ce.Bridge.loadTeamsAuthState()
	if err != nil {
		ce.Reply("Failed to load Teams auth from `%s`: %v", authPath, err)
		ce.Reply("Run `teams-login -c %s` and try `$cmdprefix login` again.", ce.Bridge.ConfigPath)
		return
	}
	if err := validateTeamsAuthState(state, now); err != nil {
		ce.Reply("Teams auth is not usable: %v", err)
		ce.Reply("Run `teams-login -c %s` and try `$cmdprefix login` again.", ce.Bridge.ConfigPath)
		return
	}

	ce.Bridge.setTeamsAuthState(state)
	ce.Reply("Teams auth OK")
}
