package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-teams/internal/teams/auth"
)

var (
	ErrTeamsAuthMissingToken   = errors.New("missing skypetoken")
	ErrTeamsAuthExpiredToken   = errors.New("expired skypetoken")
	ErrTeamsAuthMissingState   = errors.New("missing auth state")
	ErrTeamsAuthMissingCfgPath = errors.New("missing config path")
	ErrTeamsAuthMissingFile    = errors.New("missing auth file")
	ErrTeamsAuthInvalidJSON    = errors.New("invalid auth json")
)

type TeamsAuthExpiredError struct {
	ExpiresAt time.Time
}

func (e *TeamsAuthExpiredError) Error() string {
	if e == nil {
		return ErrTeamsAuthExpiredToken.Error()
	}
	return fmt.Sprintf("%s (%s)", ErrTeamsAuthExpiredToken.Error(), e.ExpiresAt.UTC().Format(time.RFC3339))
}

func (e *TeamsAuthExpiredError) Unwrap() error {
	return ErrTeamsAuthExpiredToken
}

func TeamsAuthExpiredAt(err error) (time.Time, bool) {
	var expired *TeamsAuthExpiredError
	if !errors.As(err, &expired) || expired == nil {
		return time.Time{}, false
	}
	return expired.ExpiresAt.UTC(), true
}

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
		return &TeamsAuthExpiredError{
			ExpiresAt: time.Unix(state.SkypeTokenExpiresAt, 0).UTC(),
		}
	}
	return nil
}

func wrapTeamsAuthLoadError(err error) error {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
		return fmt.Errorf("%w: %v", ErrTeamsAuthInvalidJSON, err)
	}
	return err
}

func (br *TeamsBridge) LoadTeamsAuth(now time.Time) (*auth.AuthState, error) {
	if br == nil {
		return nil, errors.New("bridge is nil")
	}
	if br.Config == nil {
		return nil, errors.New("bridge config is nil")
	}
	log := br.ZLog
	if log == nil {
		nop := zerolog.Nop()
		log = &nop
	}

	authPath, err := resolveTeamsAuthPath(br.ConfigPath, br.Config.Bridge.TeamsAuthPath)
	if err != nil {
		log.Warn().Err(err).Msg("Teams auth path resolution failed")
		return nil, err
	}
	log.Info().Str("auth_path", authPath).Msg("Loading Teams auth state")

	if _, err := os.Stat(authPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			missingFileErr := fmt.Errorf("%w: %s", ErrTeamsAuthMissingFile, authPath)
			log.Warn().Err(missingFileErr).Str("auth_path", authPath).Msg("Teams auth file missing")
			return nil, missingFileErr
		}
		log.Warn().Err(err).Str("auth_path", authPath).Msg("Teams auth file stat failed")
		return nil, err
	}

	state, _, err := loadTeamsConsumerAuth(br.ConfigPath, br.Config.Bridge.TeamsAuthPath)
	if err != nil {
		err = wrapTeamsAuthLoadError(err)
		log.Warn().Err(err).Str("auth_path", authPath).Msg("Teams auth load failed")
		return nil, err
	}
	if err := validateTeamsAuthState(state, now.UTC()); err != nil {
		logEvt := log.Warn().Err(err).Str("auth_path", authPath)
		if expiresAt, ok := TeamsAuthExpiredAt(err); ok {
			logEvt = logEvt.Time("skypetoken_expires_at", expiresAt)
		}
		logEvt.Msg("Teams auth is not usable")
		return nil, err
	}

	log.Info().
		Str("auth_path", authPath).
		Time("skypetoken_expires_at", time.Unix(state.SkypeTokenExpiresAt, 0).UTC()).
		Msg("Loaded Teams auth state")
	return state, nil
}
