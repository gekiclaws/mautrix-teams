package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/maulogger/v2/maulogadapt"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	teamsbridge "go.mau.fi/mautrix-teams/internal/bridge"
	"go.mau.fi/mautrix-teams/internal/teams/auth"
	consumerclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

const devTypingUsage = "usage: dev-typing --room <room_id> [--config <path>]"

type DevTypingOptions struct {
	ConfigPath string
	RoomID     id.RoomID
}

func runDevTypingIfRequested(args []string) (bool, int) {
	if len(args) < 1 || args[0] != "dev-typing" {
		return false, 0
	}
	if err := runDevTyping(args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		return true, 1
	}
	return true, 0
}

func runDevTyping(args []string) error {
	opts, err := parseDevTypingArgs(args)
	if err != nil {
		return err
	}
	log := zerolog.New(os.Stdout).With().Timestamp().Str("component", "dev-typing").Logger()
	log.Info().Str("room_id", opts.RoomID.String()).Msg("dev-typing harness invoked")

	cfg, err := loadDevSendConfig(opts.ConfigPath)
	if err != nil {
		return err
	}

	db, err := dbutil.NewFromConfig("mautrix-teams", cfg.AppService.Database, dbutil.ZeroLogger(log.With().Str("db_section", "main").Logger()))
	if err != nil {
		return err
	}
	defer db.Close()

	teamsDB := database.New(db, maulogadapt.ZeroAsMau(&log).Sub("Database"))
	if err := teamsDB.Upgrade(); err != nil {
		return err
	}

	state, err := loadTeamsConsumerAuth(opts.ConfigPath)
	if err != nil {
		return err
	}
	if state == nil || !state.HasValidSkypeToken(time.Now().UTC()) {
		return errors.New("missing or expired skypetoken")
	}
	if strings.TrimSpace(state.TeamsUserID) == "" {
		return errors.New("missing teams user id")
	}

	typingLog := log.With().Str("component", "teams-consumer-typing").Logger()
	authClient := auth.NewClient(nil)
	authClient.Log = &typingLog
	consumer := consumerclient.NewClient(authClient.HTTP)
	consumer.Token = state.SkypeToken
	consumer.Log = &typingLog

	store := teamsbridge.NewTeamsThreadStore(teamsDB)
	store.LoadAll()
	typer := teamsbridge.NewTeamsConsumerTyper(consumer, store, state.TeamsUserID, typingLog)

	threadID, ok := store.GetThreadID(opts.RoomID)
	if !ok || strings.TrimSpace(threadID) == "" {
		return fmt.Errorf("missing thread mapping for room %s", opts.RoomID)
	}
	log.Info().Str("room_id", opts.RoomID.String()).Str("thread_id", threadID).Msg("dev-typing room mapped to teams thread")

	if err := typer.SendTyping(context.Background(), opts.RoomID); err != nil {
		log.Warn().Err(err).Str("room_id", opts.RoomID.String()).Msg("dev-typing teams typing failed")
		return err
	}
	return nil
}

func parseDevTypingArgs(args []string) (DevTypingOptions, error) {
	fs := flag.NewFlagSet("dev-typing", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	room := fs.String("room", "", "Matrix room ID")
	configPath := fs.String("config", "config.yaml", "Config path")
	if err := fs.Parse(args); err != nil {
		return DevTypingOptions{}, fmt.Errorf("%w\n%s", err, devTypingUsage)
	}
	if strings.TrimSpace(*room) == "" {
		return DevTypingOptions{}, fmt.Errorf("missing required flags\n%s", devTypingUsage)
	}
	return DevTypingOptions{
		ConfigPath: strings.TrimSpace(*configPath),
		RoomID:     id.RoomID(strings.TrimSpace(*room)),
	}, nil
}
