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

const devReadReceiptUsage = "usage: dev-read-receipt --room <room_id> [--config <path>]"

type DevReadReceiptOptions struct {
	ConfigPath string
	RoomID     id.RoomID
}

func runDevReadReceiptIfRequested(args []string) (bool, int) {
	if len(args) < 1 || args[0] != "dev-read-receipt" {
		return false, 0
	}
	if err := runDevReadReceipt(args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		return true, 1
	}
	return true, 0
}

func runDevReadReceipt(args []string) error {
	opts, err := parseDevReadReceiptArgs(args)
	if err != nil {
		return err
	}
	log := zerolog.New(os.Stdout).With().Timestamp().Str("component", "dev-read-receipt").Logger()
	log.Info().Str("room_id", opts.RoomID.String()).Msg("dev-read-receipt harness invoked")

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

	state, _, err := loadTeamsConsumerAuth(opts.ConfigPath, "")
	if err != nil {
		return err
	}
	if state == nil || !state.HasValidSkypeToken(time.Now().UTC()) {
		return errors.New("missing or expired skypetoken")
	}

	receiptLog := log.With().Str("component", "teams-consumer-receipt").Logger()
	authClient := auth.NewClient(nil)
	authClient.Log = &receiptLog
	consumer := consumerclient.NewClient(authClient.HTTP)
	consumer.Token = state.SkypeToken
	consumer.Log = &receiptLog

	store := teamsbridge.NewTeamsThreadStore(teamsDB)
	store.LoadAll()
	unread := teamsbridge.NewUnreadCycleTracker()
	unread.MarkUnread(opts.RoomID)
	sender := teamsbridge.NewTeamsConsumerReceiptSender(consumer, store, unread, receiptLog)

	threadID, ok := store.GetThreadID(opts.RoomID)
	if !ok || strings.TrimSpace(threadID) == "" {
		return fmt.Errorf("missing thread mapping for room %s", opts.RoomID)
	}
	log.Info().Str("room_id", opts.RoomID.String()).Str("thread_id", threadID).Msg("dev-read-receipt room mapped to teams thread")

	if err := sender.SendReadReceipt(context.Background(), opts.RoomID, time.Now().UTC()); err != nil {
		receiptLog.Warn().Err(err).Str("room_id", opts.RoomID.String()).Msg("dev-read-receipt teams receipt failed")
		return err
	}
	return nil
}

func parseDevReadReceiptArgs(args []string) (DevReadReceiptOptions, error) {
	fs := flag.NewFlagSet("dev-read-receipt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	room := fs.String("room", "", "Matrix room ID")
	configPath := fs.String("config", "config.yaml", "Config path")
	if err := fs.Parse(args); err != nil {
		return DevReadReceiptOptions{}, fmt.Errorf("%w\n%s", err, devReadReceiptUsage)
	}
	if strings.TrimSpace(*room) == "" {
		return DevReadReceiptOptions{}, fmt.Errorf("missing required flags\n%s", devReadReceiptUsage)
	}
	return DevReadReceiptOptions{
		ConfigPath: strings.TrimSpace(*configPath),
		RoomID:     id.RoomID(strings.TrimSpace(*room)),
	}, nil
}
