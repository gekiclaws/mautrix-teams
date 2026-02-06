package main

import (
	"errors"
	"time"

	"go.mau.fi/mautrix-teams/internal/teams/auth"
)

func (br *TeamsBridge) ensureTeamsConsumersRunning() error {
	br.teamsRunLock.Lock()
	defer br.teamsRunLock.Unlock()

	if br.teamsRunning {
		return nil
	}

	state := br.getTeamsAuthState()
	if err := validateTeamsAuthState(state, time.Now().UTC()); err != nil {
		return err
	}
	if err := br.validateTeamsRuntimePrereqs(); err != nil {
		return err
	}

	br.startTeamsConsumerRoomSync(state)
	br.startTeamsConsumerMessageSync(state)
	br.startTeamsConsumerSender(state)
	br.teamsRunning = true
	return nil
}

func (br *TeamsBridge) setTeamsAuthState(state *auth.AuthState) {
	br.teamsAuthLock.Lock()
	defer br.teamsAuthLock.Unlock()
	br.teamsAuthState = state
}

func (br *TeamsBridge) getTeamsAuthState() *auth.AuthState {
	br.teamsAuthLock.RLock()
	defer br.teamsAuthLock.RUnlock()
	return br.teamsAuthState
}

func (br *TeamsBridge) hasValidTeamsAuth(now time.Time) bool {
	state := br.getTeamsAuthState()
	return validateTeamsAuthState(state, now.UTC()) == nil
}

func (br *TeamsBridge) areTeamsConsumersRunning() bool {
	br.teamsRunLock.Lock()
	defer br.teamsRunLock.Unlock()
	return br.teamsRunning
}

func (br *TeamsBridge) validateTeamsRuntimePrereqs() error {
	if br == nil {
		return errors.New("bridge is nil")
	}
	if br.Config == nil {
		return errors.New("bridge config is not initialized")
	}
	if br.DB == nil {
		return errors.New("bridge database is not initialized")
	}
	if br.Bot == nil || br.Bot.Client == nil {
		return errors.New("matrix bot client is not initialized")
	}
	return nil
}
