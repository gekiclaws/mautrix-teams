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

	state, err := loadTeamsConsumerAuth(br.ConfigPath)
	if err != nil {
		return err
	}
	if state == nil || !state.HasValidSkypeToken(time.Now().UTC()) {
		return errors.New("missing or expired skypetoken")
	}

	authClient := auth.NewClient(nil)
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

	state, err := loadTeamsConsumerAuth(br.ConfigPath)
	if err != nil {
		return err
	}
	if state == nil || !state.HasValidSkypeToken(time.Now().UTC()) {
		return errors.New("missing or expired skypetoken")
	}

	authClient := auth.NewClient(nil)
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
	consumerIngestor := teamsbridge.TeamsConsumerIngestor{
		Syncer: &syncer,
		Log:    log,
	}

	threads := br.DB.TeamsThread.GetAll()
	log.Info().Int("threads", len(threads)).Msg("teams polling loop started")

	type threadPollState struct {
		Backoff    teamsbridge.PollBackoff
		NextPollAt time.Time
	}

	const baseDelay = 2 * time.Second

	states := make(map[string]*threadPollState, len(threads))
	now := time.Now().UTC()
	for _, thread := range threads {
		if thread == nil || thread.ThreadID == "" {
			continue
		}
		states[thread.ThreadID] = &threadPollState{
			Backoff: teamsbridge.PollBackoff{
				Delay: baseDelay,
			},
			NextPollAt: now,
		}
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		now = time.Now().UTC()
		earliestNext := now.Add(24 * time.Hour)
		nextThreadID := ""
		dueCount := 0

		for _, thread := range threads {
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

			state := states[thread.ThreadID]
			if state == nil {
				state = &threadPollState{
					Backoff: teamsbridge.PollBackoff{Delay: baseDelay},
				}
				states[thread.ThreadID] = state
			}

			if now.Before(state.NextPollAt) {
				if state.NextPollAt.Before(earliestNext) {
					earliestNext = state.NextPollAt
					nextThreadID = thread.ThreadID
				}
				continue
			}

			dueCount++
			res, err := consumerIngestor.PollOnce(ctx, thread)
			delay, reason := teamsbridge.ApplyPollBackoff(&state.Backoff, res, err)
			state.NextPollAt = now.Add(delay)

			backoffLog := log.Info()
			if err != nil {
				backoffLog = log.Warn().Err(err)
			}

			var retryable consumerclient.RetryableError
			if err != nil && errors.As(err, &retryable) {
				backoffLog.
					Int("status", retryable.Status).
					Dur("retry_after", retryable.RetryAfter)
			}

			var msgErr consumerclient.MessagesError
			if err != nil && errors.As(err, &msgErr) {
				backoffLog.Int("status", msgErr.Status)
			}

			backoffLog.
				Str("thread_id", thread.ThreadID).
				Str("reason", string(reason)).
				Dur("delay", delay).
				Int("messages_ingested", res.MessagesIngested).
				Bool("advanced", res.Advanced).
				Msg("teams poll backoff updated")

			if state.NextPollAt.Before(earliestNext) {
				earliestNext = state.NextPollAt
				nextThreadID = thread.ThreadID
			}
		}

		if earliestNext.Equal(now.Add(24 * time.Hour)) {
			earliestNext = now.Add(baseDelay)
		}
		sleepFor := earliestNext.Sub(now)
		if sleepFor < 200*time.Millisecond {
			sleepFor = 200 * time.Millisecond
		}

		// TODO: periodically re-run thread discovery to pick up new conversations.
		log.Info().
			Int("due_threads", dueCount).
			Str("next_thread_id", nextThreadID).
			Time("next_poll_at", earliestNext).
			Dur("duration", sleepFor).
			Msg("teams poll sleeping")

		timer := time.NewTimer(sleepFor)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
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
	state, err := loadTeamsConsumerAuth(br.ConfigPath)
	if err != nil {
		return err
	}
	if state == nil || !state.HasValidSkypeToken(time.Now().UTC()) {
		return errors.New("missing or expired skypetoken")
	}
	if state.TeamsUserID == "" {
		return errors.New("missing teams user id")
	}

	authClient := auth.NewClient(nil)
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

func loadTeamsConsumerAuth(configPath string) (*auth.AuthState, error) {
	stateDir := filepath.Dir(configPath)
	authPath := filepath.Join(stateDir, "auth.json")

	stateStore := auth.NewStateStore(authPath)
	state, err := stateStore.Load()
	if err != nil {
		return nil, err
	}

	return state, nil
}
