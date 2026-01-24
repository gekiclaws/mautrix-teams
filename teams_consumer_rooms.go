package main

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"

	teamsbridge "go.mau.fi/mautrix-teams/internal/bridge"
	"go.mau.fi/mautrix-teams/internal/teams/auth"
	consumerclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

func (br *DiscordBridge) startTeamsConsumerRoomSync() {
	go func() {
		br.WaitWebsocketConnected()
		log := br.ZLog.With().Str("component", "teams-consumer").Logger()
		if err := br.runTeamsConsumerRoomSync(context.Background(), log); err != nil {
			var convErr consumerclient.ConversationsError
			if errors.As(err, &convErr) {
				return
			}
			log.Error().Err(err).Msg("Teams room discovery failed")
		}
	}()
}

func (br *DiscordBridge) runTeamsConsumerRoomSync(ctx context.Context, log zerolog.Logger) error {
	if br.ConfigPath == "" {
		return errors.New("missing config path")
	}

	state, cookieStore, err := loadTeamsConsumerAuth(br.ConfigPath)
	if err != nil {
		return err
	}
	if state == nil || !state.HasValidSkypeToken(time.Now().UTC()) {
		return errors.New("missing or expired skypetoken")
	}

	authClient := auth.NewClient(cookieStore)
	authClient.Log = &log

	consumer := consumerclient.NewClient(authClient.HTTP)
	store := teamsbridge.NewTeamsThreadStore(br.DB)
	store.LoadAll()
	creator := teamsbridge.NewIntentRoomCreator(br.Bot, &br.Config.Bridge)
	rooms := teamsbridge.NewRoomsService(store, creator, log)

	return teamsbridge.DiscoverAndEnsureRooms(ctx, state.SkypeToken, consumer, rooms, log)
}

func loadTeamsConsumerAuth(configPath string) (*auth.AuthState, *auth.CookieStore, error) {
	stateDir := filepath.Dir(configPath)
	authPath := filepath.Join(stateDir, "auth.json")
	cookiesPath := filepath.Join(stateDir, "cookies.json")

	stateStore := auth.NewStateStore(authPath)
	state, err := stateStore.Load()
	if err != nil {
		return nil, nil, err
	}

	cookieStore, err := auth.LoadCookieStore(cookiesPath)
	if err != nil {
		return nil, nil, err
	}

	return state, cookieStore, nil
}
