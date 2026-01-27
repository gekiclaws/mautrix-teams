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
	"time"

	"github.com/rs/zerolog"
	deflog "github.com/rs/zerolog/log"
	"go.mau.fi/util/exzerolog"
	"gopkg.in/yaml.v3"
	flag "maunium.net/go/mauflag"
	"maunium.net/go/mautrix/bridge/bridgeconfig"

	"go.mau.fi/mautrix-teams/config"
	"go.mau.fi/mautrix-teams/internal/teams/auth"
)

var configPath = flag.MakeFull("c", "config", "The path to your config file.", "config.yaml").String()
var manualMode = flag.MakeFull("m", "manual", "Manual paste mode.", "false").Bool()
var noBrowser = flag.MakeFull("n", "no-browser", "Do not open a browser automatically.", "false").Bool()
var noProbe = flag.MakeFull("", "no-probe", "Disable the Teams auth probe sanity check.", "false").Bool()

const probeEndpoint = "https://teams.live.com/api/chatsvc/consumer/v1/users/ME/properties"

func main() {
	flag.SetHelpTitles("teams-login (auth-only)", "teams-login [-c <path>] [--manual] [--no-browser] [--no-probe]")
	if err := flag.Parse(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		flag.PrintHelp()
		os.Exit(1)
	}

	cfg, cfgErr := loadConfig(*configPath)
	log, err := setupLogger(cfg)
	if cfgErr != nil || err != nil {
		log = defaultLogger()
		if cfgErr != nil {
			log.Warn().Err(cfgErr).Msg("Failed to load config; falling back to default logger")
		}
		if err != nil {
			log.Warn().Err(err).Msg("Failed to initialize logger from config; using default logger")
		}
	}

	stateDir := filepath.Dir(*configPath)
	authPath := filepath.Join(stateDir, "auth.json")

	stateStore := auth.NewStateStore(authPath)

	client := auth.NewClient(nil)
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
		if shouldRunProbe(*noProbe) {
			runProbe(ctx, log, client, savedState)
		}
		log.Info().Msg("Authentication complete. Exiting (auth-only mode).")
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
	if shouldRunProbe(*noProbe) {
		runProbe(ctx, log, client, state)
	}
	log.Info().Msg("Authentication complete. Exiting (auth-only mode).")
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
	if cfg == nil {
		return nil, errors.New("missing config")
	}
	log, err := cfg.Logging.Compile()
	if err != nil {
		return nil, err
	}
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.CallerMarshalFunc = exzerolog.CallerWithFunctionName
	deflog.Logger = log.With().Bool("global_log", true).Caller().Logger()
	return log, nil
}

func defaultLogger() *zerolog.Logger {
	log := zerolog.New(os.Stdout).With().Timestamp().Bool("global_log", true).Logger()
	return &log
}

func shouldRunProbe(disabled bool) bool {
	return !disabled
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
