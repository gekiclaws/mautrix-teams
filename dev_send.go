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
	"go.mau.fi/util/configupgrade"
	"go.mau.fi/util/dbutil"
	"go.mau.fi/util/random"
	"gopkg.in/yaml.v3"
	"maunium.net/go/maulogger/v2/maulogadapt"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/config"
	"go.mau.fi/mautrix-teams/database"
	teamsbridge "go.mau.fi/mautrix-teams/internal/bridge"
	"go.mau.fi/mautrix-teams/internal/teams/auth"
	consumerclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

const devSendUsage = "usage: dev-send --room <room_id> --sender <teams_user_id> --text <message> [--event-id <event_id>] [--config <path>]"

type DevSendOptions struct {
	ConfigPath string
	RoomID     id.RoomID
	Sender     string
	Text       string
	EventID    id.EventID
}

func runDevSendIfRequested(args []string) (bool, int) {
	if len(args) < 1 || args[0] != "dev-send" {
		return false, 0
	}
	if err := runDevSend(args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		return true, 1
	}
	return true, 0
}

func runDevSend(args []string) error {
	opts, err := parseDevSendArgs(args)
	if err != nil {
		return err
	}
	log := zerolog.New(os.Stdout).With().Timestamp().Str("component", "dev-send").Logger()
	log.Info().
		Str("room_id", opts.RoomID.String()).
		Str("sender", opts.Sender).
		Int("text_len", len(opts.Text)).
		Msg("dev-send harness invoked")

	if opts.EventID == "" {
		opts.EventID = newDevEventID()
	}
	evt := buildDevMatrixTextEvent(opts)
	if evt == nil {
		return errors.New("failed to build dev matrix event")
	}

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

	sendLog := log.With().Str("component", "teams-consumer-send").Logger()
	authClient := auth.NewClient(nil)
	authClient.Log = &sendLog
	consumer := consumerclient.NewClient(authClient.HTTP)
	consumer.Token = state.SkypeToken
	consumer.Log = &sendLog

	store := teamsbridge.NewTeamsThreadStore(teamsDB)
	store.LoadAll()
	sender := teamsbridge.NewTeamsConsumerSender(consumer, teamsDB.TeamsSendIntent, store, state.TeamsUserID, sendLog)

	threadID, ok := store.GetThreadID(opts.RoomID)
	if !ok || strings.TrimSpace(threadID) == "" {
		return fmt.Errorf("missing thread mapping for room %s", opts.RoomID)
	}
	log.Info().
		Str("room_id", opts.RoomID.String()).
		Str("thread_id", threadID).
		Msg("dev-send room mapped to teams thread")

	writer := func(ctx context.Context, status database.TeamsSendStatus, clientMessageID string, ts int64) error {
		log.Info().
			Str("status", string(status)).
			Str("client_message_id", clientMessageID).
			Int64("ts", ts).
			Str("event_id", evt.ID.String()).
			Msg("dev-send MSS transition")
		return nil
	}

	if err := sender.SendMatrixText(context.Background(), opts.RoomID, opts.Text, evt.ID, writer); err != nil {
		log.Warn().Err(err).Str("event_id", evt.ID.String()).Msg("dev-send teams send failed")
		return err
	}
	return nil
}

func parseDevSendArgs(args []string) (DevSendOptions, error) {
	fs := flag.NewFlagSet("dev-send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	room := fs.String("room", "", "Matrix room ID")
	sender := fs.String("sender", "", "Teams user ID (8:*)")
	text := fs.String("text", "", "Message text")
	eventID := fs.String("event-id", "", "Synthetic event ID")
	configPath := fs.String("config", "config.yaml", "Config path")
	if err := fs.Parse(args); err != nil {
		return DevSendOptions{}, fmt.Errorf("%w\n%s", err, devSendUsage)
	}
	if strings.TrimSpace(*room) == "" || strings.TrimSpace(*sender) == "" || strings.TrimSpace(*text) == "" {
		return DevSendOptions{}, fmt.Errorf("missing required flags\n%s", devSendUsage)
	}
	if !strings.HasPrefix(strings.TrimSpace(*sender), "8:") {
		return DevSendOptions{}, fmt.Errorf("sender must be a Teams user ID (8:*)\n%s", devSendUsage)
	}
	opts := DevSendOptions{
		ConfigPath: strings.TrimSpace(*configPath),
		RoomID:     id.RoomID(strings.TrimSpace(*room)),
		Sender:     strings.TrimSpace(*sender),
		Text:       *text,
	}
	if strings.TrimSpace(*eventID) != "" {
		opts.EventID = id.EventID(strings.TrimSpace(*eventID))
	}
	return opts, nil
}

func buildDevMatrixTextEvent(opts DevSendOptions) *event.Event {
	if strings.TrimSpace(string(opts.RoomID)) == "" || strings.TrimSpace(opts.Sender) == "" || strings.TrimSpace(opts.Text) == "" {
		return nil
	}
	content := &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    opts.Text,
	}
	return &event.Event{
		ID:     opts.EventID,
		RoomID: opts.RoomID,
		Sender: id.UserID(opts.Sender),
		Type:   event.EventMessage,
		Content: event.Content{
			Parsed: content,
		},
	}
}

func newDevEventID() id.EventID {
	return id.EventID(fmt.Sprintf("$dev-%s", random.String(16)))
}

func loadDevSendConfig(path string) (*config.Config, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("missing config path")
	}
	upgrader := &configupgrade.StructUpgrader{
		SimpleUpgrader: configupgrade.SimpleUpgrader(config.DoUpgrade),
		Blocks:         config.SpacedBlocks,
		Base:           ExampleConfig,
	}
	configData, upgraded, err := configupgrade.Do(path, false, upgrader)
	if err != nil {
		if configData == nil {
			return nil, err
		}
	}
	cfg := &config.Config{
		BaseConfig: &bridgeconfig.BaseConfig{},
	}
	cfg.BaseConfig.Bridge = &cfg.Bridge
	if !upgraded {
		if err := yaml.Unmarshal([]byte(ExampleConfig), &cfg); err != nil {
			return nil, err
		}
	}
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
