package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/mautrix-teams/config"
	"go.mau.fi/mautrix-teams/internal/teams/auth"
	"maunium.net/go/mautrix/bridge"
)

func TestResolveTeamsAuthPathPrecedence(t *testing.T) {
	configPath := filepath.Join("/tmp", "mautrix", "config.yaml")

	t.Setenv("MAUTRIX_TEAMS_AUTH_PATH", "")
	path, err := resolveTeamsAuthPath(configPath, "")
	if err != nil {
		t.Fatalf("resolve default failed: %v", err)
	}
	want := filepath.Join(filepath.Dir(configPath), "auth.json")
	if path != want {
		t.Fatalf("default path mismatch: got %q want %q", path, want)
	}

	path, err = resolveTeamsAuthPath(configPath, "custom/auth.json")
	if err != nil {
		t.Fatalf("resolve config override failed: %v", err)
	}
	want = filepath.Join(filepath.Dir(configPath), "custom", "auth.json")
	if path != want {
		t.Fatalf("config override mismatch: got %q want %q", path, want)
	}

	path, err = resolveTeamsAuthPath(configPath, "/opt/auth/auth.json")
	if err != nil {
		t.Fatalf("resolve absolute config override failed: %v", err)
	}
	if path != "/opt/auth/auth.json" {
		t.Fatalf("absolute override mismatch: got %q", path)
	}

	t.Setenv("MAUTRIX_TEAMS_AUTH_PATH", "env/auth.json")
	path, err = resolveTeamsAuthPath(configPath, "ignored.json")
	if err != nil {
		t.Fatalf("resolve env override failed: %v", err)
	}
	want = filepath.Join(filepath.Dir(configPath), "env", "auth.json")
	if path != want {
		t.Fatalf("env override mismatch: got %q want %q", path, want)
	}

	t.Setenv("MAUTRIX_TEAMS_AUTH_PATH", "/var/lib/mautrix/auth.json")
	path, err = resolveTeamsAuthPath(configPath, "ignored.json")
	if err != nil {
		t.Fatalf("resolve absolute env override failed: %v", err)
	}
	if path != "/var/lib/mautrix/auth.json" {
		t.Fatalf("absolute env override mismatch: got %q", path)
	}
}

func TestResolveTeamsAuthPathMissingConfigPath(t *testing.T) {
	t.Setenv("MAUTRIX_TEAMS_AUTH_PATH", "")
	_, err := resolveTeamsAuthPath("", "")
	if err != ErrTeamsAuthMissingCfgPath {
		t.Fatalf("expected ErrTeamsAuthMissingCfgPath, got %v", err)
	}
}

func TestValidateTeamsAuthState(t *testing.T) {
	now := time.Now().UTC()
	if err := validateTeamsAuthState(nil, now); err != ErrTeamsAuthMissingState {
		t.Fatalf("expected missing state, got %v", err)
	}

	missing := &auth.AuthState{}
	if err := validateTeamsAuthState(missing, now); err != ErrTeamsAuthMissingToken {
		t.Fatalf("expected missing token, got %v", err)
	}

	expired := &auth.AuthState{SkypeToken: "token", SkypeTokenExpiresAt: now.Add(-time.Minute).Unix()}
	if err := validateTeamsAuthState(expired, now); err == nil {
		t.Fatalf("expected expired token error")
	} else if !errors.Is(err, ErrTeamsAuthExpiredToken) {
		t.Fatalf("expected expired sentinel error, got %v", err)
	} else if _, ok := TeamsAuthExpiredAt(err); !ok {
		t.Fatalf("expected structured expiry metadata on expired error")
	}

	valid := &auth.AuthState{SkypeToken: "token", SkypeTokenExpiresAt: now.Add(5 * time.Minute).Unix()}
	if err := validateTeamsAuthState(valid, now); err != nil {
		t.Fatalf("expected valid token, got %v", err)
	}
}

func TestLoadTeamsConsumerAuthFromFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("bridge: {}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	authPath := filepath.Join(dir, "auth.json")
	stateJSON := []byte(`{"skype_token":"abc","skype_token_expires_at":32503680000}`)
	if err := os.WriteFile(authPath, stateJSON, 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	state, resolvedPath, err := loadTeamsConsumerAuth(configPath, "")
	if err != nil {
		t.Fatalf("load auth failed: %v", err)
	}
	if resolvedPath != authPath {
		t.Fatalf("resolved path mismatch: got %q want %q", resolvedPath, authPath)
	}
	if state == nil || state.SkypeToken != "abc" {
		t.Fatalf("loaded state mismatch: %+v", state)
	}
}

func TestLoadTeamsAuth(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name       string
		authJSON   string
		configPath string
		wantErr    error
		checkErr   func(t *testing.T, err error)
		wantToken  string
	}{
		{
			name:       "missing config path",
			configPath: "",
			wantErr:    ErrTeamsAuthMissingCfgPath,
		},
		{
			name:       "missing file",
			configPath: "config.yaml",
			wantErr:    ErrTeamsAuthMissingFile,
		},
		{
			name:       "invalid json",
			configPath: "config.yaml",
			authJSON:   `{"skype_token":`,
			wantErr:    ErrTeamsAuthInvalidJSON,
		},
		{
			name:       "expired token",
			configPath: "config.yaml",
			authJSON:   `{"skype_token":"abc","skype_token_expires_at":1}`,
			wantErr:    ErrTeamsAuthExpiredToken,
			checkErr: func(t *testing.T, err error) {
				expiresAt, ok := TeamsAuthExpiredAt(err)
				if !ok {
					t.Fatalf("expected expiry metadata")
				}
				if expiresAt.Unix() != 1 {
					t.Fatalf("unexpected expiry: %v", expiresAt)
				}
				if !strings.Contains(err.Error(), "1970-01-01T00:00:01Z") {
					t.Fatalf("expected expiry timestamp in error, got %q", err.Error())
				}
			},
		},
		{
			name:       "valid token",
			configPath: "config.yaml",
			authJSON:   `{"skype_token":"abc","skype_token_expires_at":32503680000}`,
			wantToken:  "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MAUTRIX_TEAMS_AUTH_PATH", "")
			dir := t.TempDir()
			var cfgPath string
			if tt.configPath != "" {
				cfgPath = filepath.Join(dir, tt.configPath)
				if err := os.WriteFile(cfgPath, []byte("bridge: {}\n"), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}
			if tt.authJSON != "" {
				authPath := filepath.Join(dir, "auth.json")
				if err := os.WriteFile(authPath, []byte(tt.authJSON), 0o600); err != nil {
					t.Fatalf("write auth: %v", err)
				}
			}

			br := &TeamsBridge{
				Bridge: bridge.Bridge{
					ConfigPath: cfgPath,
				},
				Config: &config.Config{},
			}
			state, err := br.LoadTeamsAuth(now)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				if tt.checkErr != nil {
					tt.checkErr(t, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if state == nil || state.SkypeToken != tt.wantToken {
				t.Fatalf("unexpected state: %+v", state)
			}
		})
	}
}
