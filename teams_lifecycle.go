package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-teams/internal/teams/auth"
)

var startTeamsConsumerReactorFn = func(br *TeamsBridge, ctx context.Context, state *auth.AuthState) error {
	return br.startTeamsConsumerMessageSync(ctx, state)
}

func (br *TeamsBridge) StartTeamsConsumers(ctx context.Context, state *auth.AuthState) error {
	if ctx == nil {
		ctx = context.Background()
	}
	log := br.teamsLifecycleLogger()
	if br == nil {
		err := errors.New("bridge is nil")
		log.Warn().Err(err).Msg("Teams consumers skipped: runtime prerequisites missing")
		return err
	}

	br.teamsRunLock.Lock()
	defer br.teamsRunLock.Unlock()

	if br.teamsRunning {
		log.Info().Msg("Teams consumers already running")
		return nil
	}

	if err := validateTeamsAuthState(state, time.Now().UTC()); err != nil {
		log.Warn().Err(err).Msg("Teams consumers skipped: invalid auth state")
		return err
	}
	if err := br.validateTeamsRuntimePrereqs(); err != nil {
		log.Warn().Err(err).Msg("Teams consumers skipped: runtime prerequisites missing")
		return err
	}

	log.Info().Msg("Starting Teams consumer reactor")
	log.Info().Str("teams_user_id", strings.TrimSpace(state.TeamsUserID)).Msg("Authenticated as Teams user")

	if err := startTeamsConsumerReactorFn(br, ctx, state); err != nil {
		log.Error().Err(err).Msg("Failed to start Teams consumer reactor")
		return err
	}

	br.teamsRunning = true
	return nil
}

func (br *TeamsBridge) teamsLifecycleLogger() *zerolog.Logger {
	if br == nil || br.ZLog == nil {
		nop := zerolog.Nop()
		return &nop
	}
	return br.ZLog
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
	if br == nil {
		return false
	}
	state := br.getTeamsAuthState()
	return validateTeamsAuthState(state, now.UTC()) == nil
}

func (br *TeamsBridge) areTeamsConsumersRunning() bool {
	if br == nil {
		return false
	}
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
