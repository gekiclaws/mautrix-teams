package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/mautrix-teams/internal/teams/auth"
)

var (
	ErrTeamsAuthMissingToken   = errors.New("missing skypetoken")
	ErrTeamsAuthExpiredToken   = errors.New("expired skypetoken")
	ErrTeamsAuthMissingState   = errors.New("missing auth state")
	ErrTeamsAuthMissingCfgPath = errors.New("missing config path")
)

func resolveTeamsAuthPath(configPath, configuredAuthPath string) (string, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return "", ErrTeamsAuthMissingCfgPath
	}

	baseDir := filepath.Dir(configPath)
	envPath := strings.TrimSpace(os.Getenv("MAUTRIX_TEAMS_AUTH_PATH"))
	if envPath != "" {
		if filepath.IsAbs(envPath) {
			return envPath, nil
		}
		return filepath.Join(baseDir, envPath), nil
	}

	configuredAuthPath = strings.TrimSpace(configuredAuthPath)
	if configuredAuthPath != "" {
		if filepath.IsAbs(configuredAuthPath) {
			return configuredAuthPath, nil
		}
		return filepath.Join(baseDir, configuredAuthPath), nil
	}

	return filepath.Join(baseDir, "auth.json"), nil
}

func loadTeamsConsumerAuth(configPath, configuredAuthPath string) (*auth.AuthState, string, error) {
	authPath, err := resolveTeamsAuthPath(configPath, configuredAuthPath)
	if err != nil {
		return nil, "", err
	}

	stateStore := auth.NewStateStore(authPath)
	state, err := stateStore.Load()
	if err != nil {
		return nil, authPath, err
	}
	return state, authPath, nil
}

func validateTeamsAuthState(state *auth.AuthState, now time.Time) error {
	if state == nil {
		return ErrTeamsAuthMissingState
	}
	if strings.TrimSpace(state.SkypeToken) == "" {
		return ErrTeamsAuthMissingToken
	}
	if !state.HasValidSkypeToken(now.UTC()) {
		expiresAt := time.Unix(state.SkypeTokenExpiresAt, 0).UTC().Format(time.RFC3339)
		return fmt.Errorf("%w (%s)", ErrTeamsAuthExpiredToken, expiresAt)
	}
	return nil
}

func (br *TeamsBridge) loadTeamsAuthState() (*auth.AuthState, string, error) {
	if br == nil || br.Config == nil {
		return nil, "", errors.New("bridge config is nil")
	}
	return loadTeamsConsumerAuth(br.ConfigPath, br.Config.Bridge.TeamsAuthPath)
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
