package main

import (
	"testing"
	"time"

	"go.mau.fi/mautrix-teams/internal/teams/auth"
)

func TestEnsureTeamsConsumersRunningRequiresValidAuth(t *testing.T) {
	br := &TeamsBridge{}
	if err := br.ensureTeamsConsumersRunning(); err == nil {
		t.Fatalf("expected error when auth state is missing")
	}
	if br.areTeamsConsumersRunning() {
		t.Fatalf("consumers should not be marked running on auth failure")
	}
}

func TestEnsureTeamsConsumersRunningIsIdempotent(t *testing.T) {
	br := &TeamsBridge{}
	br.teamsRunning = true
	br.setTeamsAuthState(&auth.AuthState{
		SkypeToken:          "token",
		SkypeTokenExpiresAt: time.Now().UTC().Add(10 * time.Minute).Unix(),
	})

	if err := br.ensureTeamsConsumersRunning(); err != nil {
		t.Fatalf("expected idempotent no-op, got %v", err)
	}
	if !br.areTeamsConsumersRunning() {
		t.Fatalf("expected consumers to stay marked running")
	}
}

func TestValidateTeamsRuntimePrereqs(t *testing.T) {
	br := &TeamsBridge{}
	if err := br.validateTeamsRuntimePrereqs(); err == nil {
		t.Fatalf("expected config precondition error")
	}
}
