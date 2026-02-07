package main

import (
	"context"
	"errors"
	"fmt"
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
	ce.ZLog.Info().Msg("fnTeamsLogin invoked")
	state, err := ce.Bridge.LoadTeamsAuth(time.Now().UTC())
	if err != nil {
		ce.Reply("%s", teamsLoginAuthFailureReply(err, ce.Bridge.ConfigPath))
		return
	}
	ce.Bridge.setTeamsAuthState(state)
	if err := ce.Bridge.StartTeamsConsumers(context.Background(), state); err != nil {
		ce.Reply("Teams auth is valid, but failed to start consumers: %v", err)
		return
	}
	ce.Reply("Teams auth OK")
}

func teamsLoginAuthFailureReply(err error, configPath string) string {
	retryMsg := fmt.Sprintf("Run `teams-login -c %s` and try `$cmdprefix login` again.", configPath)
	switch {
	case errors.Is(err, ErrTeamsAuthMissingFile), errors.Is(err, ErrTeamsAuthMissingState), errors.Is(err, ErrTeamsAuthMissingToken):
		return fmt.Sprintf("Teams auth missing. %s", retryMsg)
	case errors.Is(err, ErrTeamsAuthExpiredToken):
		if expiresAt, ok := TeamsAuthExpiredAt(err); ok {
			return fmt.Sprintf("Teams auth expired at %s. %s", expiresAt.Format(time.RFC3339), retryMsg)
		}
		return fmt.Sprintf("Teams auth expired. %s", retryMsg)
	case errors.Is(err, ErrTeamsAuthInvalidJSON):
		return fmt.Sprintf("Teams auth file is invalid JSON. %s", retryMsg)
	default:
		return fmt.Sprintf("Failed to load Teams auth: %v. %s", err, retryMsg)
	}
}
