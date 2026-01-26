package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type AuthState struct {
	// Bootstrap-only legacy tokens; do not use for Teams-native calls.
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	ExpiresAtUnix int64  `json:"expires_at"`
	IDToken       string `json:"id_token,omitempty"`

	SkypeToken          string `json:"skype_token,omitempty"`
	SkypeTokenExpiresAt int64  `json:"skype_token_expires_at,omitempty"`
	TeamsUserID         string `json:"teams_user_id,omitempty"`
}

func (a *AuthState) Expiry() time.Time {
	if a == nil || a.ExpiresAtUnix == 0 {
		return time.Time{}
	}
	return time.Unix(a.ExpiresAtUnix, 0).UTC()
}

type StateStore struct {
	path string
	mu   sync.Mutex
}

func NewStateStore(path string) *StateStore {
	return &StateStore{path: path}
}

func (s *StateStore) Load() (*AuthState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var state AuthState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	if !state.hasAnyToken() {
		return nil, nil
	}

	return &state, nil
}

func (s *StateStore) Save(state *AuthState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if state == nil {
		return errors.New("auth state is nil")
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	return writeFileAtomic(s.path, data, 0o600)
}

func (a *AuthState) hasAnyToken() bool {
	if a == nil {
		return false
	}
	return a.AccessToken != "" || a.RefreshToken != "" || a.IDToken != "" || a.SkypeToken != ""
}

func NormalizeTeamsUserID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "8:") {
		return trimmed
	}
	return "8:" + trimmed
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-auth-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
