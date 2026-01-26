package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog"
	deflog "github.com/rs/zerolog/log"
	"go.mau.fi/util/dbutil"
	"go.mau.fi/util/exzerolog"
	"gopkg.in/yaml.v3"
	flag "maunium.net/go/mauflag"
	"maunium.net/go/maulogger/v2/maulogadapt"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/config"
	"go.mau.fi/mautrix-teams/database"
	teamsbridge "go.mau.fi/mautrix-teams/internal/bridge"
	"go.mau.fi/mautrix-teams/internal/teams/auth"
	consumerclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

var configPath = flag.MakeFull("c", "config", "The path to your config file.", "config.yaml").String()
var manualMode = flag.MakeFull("m", "manual", "Manual paste mode.", "false").Bool()
var noBrowser = flag.MakeFull("n", "no-browser", "Do not open a browser automatically.", "false").Bool()

const probeEndpoint = "https://teams.live.com/api/chatsvc/consumer/v1/users/ME/properties"

func main() {
	flag.SetHelpTitles("teams-login", "teams-login [-c <path>] [--manual] [--no-browser]")
	if err := flag.Parse(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		flag.PrintHelp()
		os.Exit(1)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to load config:", err)
		os.Exit(1)
	}
	log, err := setupLogger(cfg)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to initialize logger:", err)
		os.Exit(1)
	}

	stateDir := filepath.Dir(*configPath)
	authPath := filepath.Join(stateDir, "auth.json")
	cookiesPath := filepath.Join(stateDir, "cookies.json")

	stateStore := auth.NewStateStore(authPath)
	cookieStore, err := auth.LoadCookieStore(cookiesPath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load cookie jar")
		os.Exit(1)
	}

	client := auth.NewClient(cookieStore)
	client.Log = log
	ctx := context.Background()

	savedState, err := stateStore.Load()
	if err != nil {
		log.Error().Err(err).Msg("Failed to load auth state")
		os.Exit(1)
	}

	now := time.Now().UTC()
	if savedState != nil && savedState.HasValidSkypeToken(now) {
		log.Info().Msg("Stored skypetoken is valid")
		runProbe(ctx, log, client, savedState)
		if err := runRoomBootstrap(ctx, log, cfg, client, savedState); err != nil {
			if !isConversationsError(err) {
				log.Error().Err(err).Msg("Teams room bootstrap failed")
			}
			os.Exit(1)
		}
		return
	}

	verifier, err := auth.GenerateCodeVerifier()
	if err != nil {
		log.Error().Err(err).Msg("Failed to generate PKCE verifier")
		os.Exit(1)
	}
	challenge := auth.CodeChallengeS256(verifier)
	stateValue, err := randomState()
	if err != nil {
		log.Error().Err(err).Msg("Failed to generate state")
		os.Exit(1)
	}

	log.Info().Str("redirect_uri", client.RedirectURI).Msg("Authorize request redirect URI")
	authorizeURL, err := client.AuthorizeURL(challenge, stateValue)
	if err != nil {
		log.Error().Err(err).Msg("Failed to build authorize URL")
		os.Exit(1)
	}

	manual := *manualMode
	var helper *auth.HelperListener
	if !manual {
		helper, err = auth.StartHelperListener(ctx, log, client.ClientID)
		if err != nil {
			manual = true
			log.Warn().Err(err).Msg("Failed to start helper listener; falling back to manual mode")
		} else {
			log.Info().Msgf("Helper page available at %s", helper.URL)
		}
	}

	if !*noBrowser {
		if err := openBrowser(authorizeURL); err != nil {
			log.Warn().Err(err).Msg("Failed to open browser")
		} else {
			log.Info().Msg("Browser opened for login")
		}
		if helper != nil {
			if err := openBrowser(helper.URL); err != nil {
				log.Warn().Err(err).Msg("Failed to open helper page")
			}
		}
	}

	var state *auth.AuthState
	if manual {
		state, err = auth.WaitForManualState(ctx, os.Stdin, os.Stdout, client.ClientID)
	} else {
		state, err = helper.WaitForState(ctx)
	}
	if err != nil {
		log.Error().Err(err).Msg("Failed to capture auth state")
		os.Exit(1)
	}

	if err := cookieStore.Save(cookiesPath); err != nil {
		log.Error().Err(err).Msg("Failed to save cookies")
		os.Exit(1)
	}

	token, expiresAt, skypeID, err := client.AcquireSkypeToken(ctx, state.AccessToken)
	if err != nil {
		log.Error().Err(err).Msg("Failed to acquire skypetoken")
		os.Exit(1)
	}
	normalizedUserID := auth.NormalizeTeamsUserID(skypeID)
	if normalizedUserID == "" && savedState != nil {
		normalizedUserID = savedState.TeamsUserID
	}
	if normalizedUserID != "" {
		state.TeamsUserID = normalizedUserID
	}
	state.SkypeToken = token
	state.SkypeTokenExpiresAt = expiresAt
	state.AccessToken = ""
	state.RefreshToken = ""
	state.IDToken = ""
	state.ExpiresAtUnix = 0
	if err := stateStore.Save(state); err != nil {
		log.Error().Err(err).Msg("Failed to save skypetoken")
		os.Exit(1)
	}
	log.Info().Int64("expires_at", expiresAt).Msg("Skype token acquired")

	log.Info().Msg("Login completed")
	runProbe(ctx, log, client, state)
	if err := runRoomBootstrap(ctx, log, cfg, client, state); err != nil {
		if !isConversationsError(err) {
			log.Error().Err(err).Msg("Teams room bootstrap failed")
		}
		os.Exit(1)
	}
}

func loadConfig(path string) (*config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &config.Config{BaseConfig: &bridgeconfig.BaseConfig{}}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func setupLogger(cfg *config.Config) (*zerolog.Logger, error) {
	log, err := cfg.Logging.Compile()
	if err != nil {
		return nil, err
	}
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.CallerMarshalFunc = exzerolog.CallerWithFunctionName
	deflog.Logger = log.With().Bool("global_log", true).Caller().Logger()
	return log, nil
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func openBrowser(target string) error {
	if target == "" {
		return errors.New("empty url")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

func runProbe(ctx context.Context, log *zerolog.Logger, client *auth.Client, state *auth.AuthState) {
	token := ""
	if state != nil && state.HasValidSkypeToken(time.Now().UTC()) {
		token = state.SkypeToken
	}
	result, err := client.ProbeTeamsEndpoint(ctx, probeEndpoint, token)
	if err != nil {
		log.Error().Err(err).Msg("Teams probe failed")
		return
	}
	switch result.StatusCode {
	case http.StatusOK:
		log.Info().
			Int("status", result.StatusCode).
			Str("body_snippet", result.BodySnippet).
			Msg("Teams consumer auth OK")
	case http.StatusUnauthorized, http.StatusForbidden:
		log.Error().
			Int("status", result.StatusCode).
			Str("body_snippet", result.BodySnippet).
			Msg("Teams consumer auth failed")
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		log.Debug().
			Int("status", result.StatusCode).
			Str("body_snippet", result.BodySnippet).
			Msg("Teams probe endpoint mismatch")
	default:
		log.Info().
			Int("status", result.StatusCode).
			Str("body_snippet", result.BodySnippet).
			Msg("Teams probe response")
	}
}

func runRoomBootstrap(ctx context.Context, log *zerolog.Logger, cfg *config.Config, authClient *auth.Client, state *auth.AuthState) error {
	if state == nil || !state.HasValidSkypeToken(time.Now().UTC()) {
		return errors.New("missing or expired skypetoken")
	}
	if cfg == nil || cfg.BaseConfig == nil {
		return errors.New("missing config")
	}

	dbConfig := cfg.AppService.Database
	db, err := dbutil.NewFromConfig("mautrix-teams", dbConfig, dbutil.ZeroLogger(log.With().Str("db_section", "main").Logger()))
	if err != nil {
		return err
	}
	defer db.Close()

	teamsDB := database.New(db, maulogadapt.ZeroAsMau(log).Sub("Database"))
	if err := teamsDB.Upgrade(); err != nil {
		return err
	}

	store := teamsbridge.NewTeamsThreadStore(teamsDB)
	store.LoadAll()

	botMXID := id.UserID(fmt.Sprintf("@%s:%s", cfg.AppService.Bot.Username, cfg.Homeserver.Domain))
	client, err := mautrix.NewClient(cfg.Homeserver.Address, botMXID, cfg.AppService.ASToken)
	if err != nil {
		return err
	}
	client.SetAppServiceUserID = true
	client.Log = *log
	client.Logger = maulogadapt.ZeroAsMau(&client.Log)
	creator := teamsbridge.NewClientRoomCreator(client, &cfg.Bridge)
	rooms := teamsbridge.NewRoomsService(store, creator, *log)

	consumer := consumerclient.NewClient(authClient.HTTP)
	consumer.Token = state.SkypeToken
	consumer.Log = log
	if err := teamsbridge.DiscoverAndEnsureRooms(ctx, state.SkypeToken, consumer, rooms, *log); err != nil {
		return err
	}

	ingestor := teamsbridge.MessageIngestor{
		Lister:      consumer,
		Sender:      &teamsbridge.BotMatrixSender{Client: client},
		Profiles:    teamsDB.TeamsProfile,
		SendIntents: teamsDB.TeamsSendIntent,
		MessageMap:  teamsDB.TeamsMessageMap,
		Log:         *log,
	}
	syncer := teamsbridge.ThreadSyncer{
		Ingestor: &ingestor,
		Store:    teamsDB.TeamsThread,
		Log:      *log,
	}

	for _, thread := range teamsDB.TeamsThread.GetAll() {
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

func isConversationsError(err error) bool {
	var convErr consumerclient.ConversationsError
	return errors.As(err, &convErr)
}
