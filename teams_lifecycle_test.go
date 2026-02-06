package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mau.fi/mautrix-teams/config"
	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/auth"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
)

func TestStartTeamsConsumersRequiresValidAuth(t *testing.T) {
	br := newRuntimeReadyBridgeForLifecycleTests()
	calls := 0
	originalStarter := startTeamsConsumerReactorFn
	startTeamsConsumerReactorFn = func(_ *TeamsBridge, _ context.Context, _ *auth.AuthState) error {
		calls++
		return nil
	}
	defer func() {
		startTeamsConsumerReactorFn = originalStarter
	}()

	if err := br.StartTeamsConsumers(context.Background(), nil); err == nil {
		t.Fatalf("expected error when auth state is missing")
	}
	if br.areTeamsConsumersRunning() {
		t.Fatalf("consumers should not be marked running on auth failure")
	}
	if calls != 0 {
		t.Fatalf("reactor starter should not run on auth failure")
	}
}

func TestStartTeamsConsumersIsIdempotent(t *testing.T) {
	br := newRuntimeReadyBridgeForLifecycleTests()
	state := &auth.AuthState{
		SkypeToken:          "token",
		SkypeTokenExpiresAt: time.Now().UTC().Add(10 * time.Minute).Unix(),
	}
	calls := 0
	originalStarter := startTeamsConsumerReactorFn
	startTeamsConsumerReactorFn = func(_ *TeamsBridge, _ context.Context, _ *auth.AuthState) error {
		calls++
		return nil
	}
	defer func() {
		startTeamsConsumerReactorFn = originalStarter
	}()

	if err := br.StartTeamsConsumers(context.Background(), state); err != nil {
		t.Fatalf("expected initial start to succeed, got %v", err)
	}
	if err := br.StartTeamsConsumers(context.Background(), state); err != nil {
		t.Fatalf("expected idempotent second start, got %v", err)
	}
	if !br.areTeamsConsumersRunning() {
		t.Fatalf("expected consumers to stay marked running")
	}
	if calls != 1 {
		t.Fatalf("expected starter to run once, got %d", calls)
	}
}

func TestStartTeamsConsumersPropagatesStarterError(t *testing.T) {
	br := newRuntimeReadyBridgeForLifecycleTests()
	state := &auth.AuthState{
		SkypeToken:          "token",
		SkypeTokenExpiresAt: time.Now().UTC().Add(10 * time.Minute).Unix(),
	}
	originalStarter := startTeamsConsumerReactorFn
	startTeamsConsumerReactorFn = func(_ *TeamsBridge, _ context.Context, _ *auth.AuthState) error {
		return errors.New("starter failed")
	}
	defer func() {
		startTeamsConsumerReactorFn = originalStarter
	}()

	if err := br.StartTeamsConsumers(context.Background(), state); err == nil {
		t.Fatalf("expected starter error")
	}
	if br.areTeamsConsumersRunning() {
		t.Fatalf("consumers should not be marked running on starter error")
	}
}

func TestValidateTeamsRuntimePrereqs(t *testing.T) {
	br := &TeamsBridge{}
	if err := br.validateTeamsRuntimePrereqs(); err == nil {
		t.Fatalf("expected config precondition error")
	}
}

func newRuntimeReadyBridgeForLifecycleTests() *TeamsBridge {
	return &TeamsBridge{
		Config: &config.Config{},
		DB:     &database.Database{},
		Bridge: bridge.Bridge{
			Bot: &appservice.IntentAPI{
				Client: &mautrix.Client{},
			},
		},
	}
}
