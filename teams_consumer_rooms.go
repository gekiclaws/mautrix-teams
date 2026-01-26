package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"

	teamsbridge "go.mau.fi/mautrix-teams/internal/bridge"
	"go.mau.fi/mautrix-teams/internal/teams/auth"
	consumerclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

func (br *DiscordBridge) startTeamsConsumerRoomSync() {
	// All Teams → Matrix ingest begins here for the bridge process.
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
	store := br.ensureTeamsThreadStore()
	store.LoadAll()
	creator := teamsbridge.NewIntentRoomCreator(br.Bot, &br.Config.Bridge)
	rooms := teamsbridge.NewRoomsService(store, creator, log)

	return teamsbridge.DiscoverAndEnsureRooms(ctx, state.SkypeToken, consumer, rooms, log)
}

func (br *DiscordBridge) startTeamsConsumerMessageSync() {
	go func() {
		br.WaitWebsocketConnected()
		log := br.ZLog.With().Str("component", "teams-consumer-sync").Logger()
		if err := br.runTeamsConsumerMessageSync(context.Background(), log); err != nil {
			log.Error().Err(err).Msg("Teams message sync failed")
		}
	}()
}

func (br *DiscordBridge) runTeamsConsumerMessageSync(ctx context.Context, log zerolog.Logger) error {
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
	consumer.Token = state.SkypeToken
	consumer.Log = &log

	store := br.ensureTeamsThreadStore()
	store.LoadAll()

	ingestor := teamsbridge.MessageIngestor{
		Lister:      consumer,
		Sender:      &teamsbridge.BotMatrixSender{Client: br.Bot.Client},
		Profiles:    br.DB.TeamsProfile,
		SendIntents: br.DB.TeamsSendIntent,
		MessageMap:  br.DB.TeamsMessageMap,
		ReactionIngestor: &teamsbridge.TeamsReactionIngestor{
			Sender:    &teamsbridge.BotMatrixReactionSender{Client: br.Bot.Client},
			Messages:  br.DB.TeamsMessageMap,
			Reactions: br.DB.TeamsReaction,
			Log:       log,
		},
		Log: log,
	}
	syncer := teamsbridge.ThreadSyncer{
		Ingestor: &ingestor,
		Store:    br.DB.TeamsThread,
		Log:      log,
	}

	for _, thread := range br.DB.TeamsThread.GetAll() {
		if thread == nil {
			continue
		}
		if thread.RoomID == "" {
			log.Debug().
				Str("thread_id", thread.ThreadID).
				Msg("skipping message ingestion without room")
			continue
		}
		if !strings.HasSuffix(thread.ThreadID, "@thread.v2") {
			log.Debug().
				Str("thread_id", thread.ThreadID).
				Msg("skipping non-v2 thread")
			continue
		}
		if err := syncer.SyncThread(ctx, thread); err != nil {
			return err
		}
	}

	return nil
}

func (br *DiscordBridge) startTeamsConsumerSender() {
	log := br.ZLog.With().Str("component", "teams-consumer-send").Logger()
	if err := br.initTeamsConsumerSender(log); err != nil {
		log.Warn().Err(err).Msg("Teams consumer sender unavailable")
	}
}

func (br *DiscordBridge) initTeamsConsumerSender(log zerolog.Logger) error {
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
	if state.TeamsUserID == "" {
		return errors.New("missing teams user id")
	}

	authClient := auth.NewClient(cookieStore)
	authClient.Log = &log

	consumer := consumerclient.NewClient(authClient.HTTP)
	consumer.Token = state.SkypeToken
	consumer.Log = &log

	store := br.ensureTeamsThreadStore()
	store.LoadAll()
	br.TeamsConsumerSender = teamsbridge.NewTeamsConsumerSender(consumer, br.DB.TeamsSendIntent, store, state.TeamsUserID, log)
	br.TeamsConsumerReactor = teamsbridge.NewTeamsConsumerReactor(consumer, store, br.DB.TeamsMessageMap, br.DB.TeamsReactionMap, log)
	return nil
}

func (br *DiscordBridge) ensureTeamsThreadStore() *teamsbridge.TeamsThreadStore {
	if br.TeamsThreadStore == nil {
		br.TeamsThreadStore = teamsbridge.NewTeamsThreadStore(br.DB)
	}
	return br.TeamsThreadStore
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
