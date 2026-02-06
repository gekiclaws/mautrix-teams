package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/mautrix-teams/internal/teams/auth"
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
