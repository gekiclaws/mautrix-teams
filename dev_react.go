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
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	teamsbridge "go.mau.fi/mautrix-teams/internal/bridge"
	"go.mau.fi/mautrix-teams/internal/teams/auth"
	consumerclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

const devReactUsage = "usage: dev-react --room <room_id> --target <event_id> --emoji <emoji> [--event-id <event_id>] [--config <path>] | --room <room_id> --redact <event_id> [--event-id <event_id>] [--config <path>]"

type DevReactOptions struct {
	ConfigPath string
	RoomID     id.RoomID
	TargetID   id.EventID
	Emoji      string
	EventID    id.EventID
	RedactID   id.EventID
}

func runDevReactIfRequested(args []string) (bool, int) {
	if len(args) < 1 || args[0] != "dev-react" {
		return false, 0
	}
	if err := runDevReact(args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		return true, 1
	}
	return true, 0
}

func runDevReact(args []string) error {
	opts, err := parseDevReactArgs(args)
	if err != nil {
		return err
	}
	log := zerolog.New(os.Stdout).With().Timestamp().Str("component", "dev-react").Logger()
	log.Info().
		Str("room_id", opts.RoomID.String()).
		Str("target_mxid", opts.TargetID.String()).
		Str("emoji", opts.Emoji).
		Str("redact_mxid", opts.RedactID.String()).
		Msg("dev-react harness invoked")

	if opts.EventID == "" {
		opts.EventID = newDevEventID()
	}
	var evt *event.Event
	if opts.RedactID != "" {
		evt = buildDevMatrixRedactionEvent(opts)
	} else {
		evt = buildDevMatrixReactionEvent(opts)
	}
	if evt == nil {
		return errors.New("failed to build dev matrix reaction event")
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

	reactLog := log.With().Str("component", "teams-consumer-reaction").Logger()
	authClient := auth.NewClient(nil)
	authClient.Log = &reactLog
	consumer := consumerclient.NewClient(authClient.HTTP)
	consumer.Token = state.SkypeToken
	consumer.Log = &reactLog

	store := teamsbridge.NewTeamsThreadStore(teamsDB)
	store.LoadAll()
	reactor := teamsbridge.NewTeamsConsumerReactor(consumer, store, teamsDB.TeamsMessageMap, teamsDB.TeamsReactionMap, reactLog)

	threadID, ok := store.GetThreadID(opts.RoomID)
	if !ok || strings.TrimSpace(threadID) == "" {
		return fmt.Errorf("missing thread mapping for room %s", opts.RoomID)
	}
	log.Info().
		Str("room_id", opts.RoomID.String()).
		Str("thread_id", threadID).
		Msg("dev-react room mapped to teams thread")

	if opts.RedactID != "" {
		if err := reactor.RemoveMatrixReaction(context.Background(), opts.RoomID, evt); err != nil {
			log.Warn().Err(err).Str("event_id", evt.ID.String()).Msg("dev-react teams reaction removal failed")
			return err
		}
	} else {
		if err := reactor.AddMatrixReaction(context.Background(), opts.RoomID, evt); err != nil {
			log.Warn().Err(err).Str("event_id", evt.ID.String()).Msg("dev-react teams reaction failed")
			return err
		}
	}
	return nil
}

func parseDevReactArgs(args []string) (DevReactOptions, error) {
	fs := flag.NewFlagSet("dev-react", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	room := fs.String("room", "", "Matrix room ID")
	target := fs.String("target", "", "Target Matrix event ID")
	redact := fs.String("redact", "", "Redact Matrix event ID")
	emoji := fs.String("emoji", "", "Reaction emoji")
	eventID := fs.String("event-id", "", "Synthetic reaction event ID")
	configPath := fs.String("config", "config.yaml", "Config path")
	if err := fs.Parse(args); err != nil {
		return DevReactOptions{}, fmt.Errorf("%w\n%s", err, devReactUsage)
	}
	roomVal := strings.TrimSpace(*room)
	targetVal := strings.TrimSpace(*target)
	redactVal := strings.TrimSpace(*redact)
	emojiVal := strings.TrimSpace(*emoji)
	if roomVal == "" {
		return DevReactOptions{}, fmt.Errorf("missing required flags\n%s", devReactUsage)
	}
	if targetVal == "" && redactVal == "" {
		return DevReactOptions{}, fmt.Errorf("missing target or redact\n%s", devReactUsage)
	}
	if targetVal != "" && redactVal != "" {
		return DevReactOptions{}, fmt.Errorf("target and redact are mutually exclusive\n%s", devReactUsage)
	}
	if targetVal != "" && emojiVal == "" {
		return DevReactOptions{}, fmt.Errorf("missing required flags\n%s", devReactUsage)
	}
	opts := DevReactOptions{
		ConfigPath: strings.TrimSpace(*configPath),
		RoomID:     id.RoomID(roomVal),
		TargetID:   id.EventID(targetVal),
		RedactID:   id.EventID(redactVal),
		Emoji:      emojiVal,
	}
	if strings.TrimSpace(*eventID) != "" {
		opts.EventID = id.EventID(strings.TrimSpace(*eventID))
	}
	return opts, nil
}

func buildDevMatrixReactionEvent(opts DevReactOptions) *event.Event {
	if strings.TrimSpace(string(opts.RoomID)) == "" || strings.TrimSpace(opts.Emoji) == "" || opts.TargetID == "" {
		return nil
	}
	content := &event.ReactionEventContent{RelatesTo: event.RelatesTo{
		Type:    event.RelAnnotation,
		EventID: opts.TargetID,
		Key:     opts.Emoji,
	}}
	return &event.Event{
		ID:     opts.EventID,
		RoomID: opts.RoomID,
		Type:   event.EventReaction,
		Content: event.Content{
			Parsed: content,
		},
	}
}

func buildDevMatrixRedactionEvent(opts DevReactOptions) *event.Event {
	if strings.TrimSpace(string(opts.RoomID)) == "" || opts.RedactID == "" {
		return nil
	}
	return &event.Event{
		ID:      opts.EventID,
		RoomID:  opts.RoomID,
		Type:    event.EventRedaction,
		Redacts: opts.RedactID,
	}
}
