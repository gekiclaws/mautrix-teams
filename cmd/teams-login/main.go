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

const probeEndpoint = "https://teams.live.com/api/mt/Me"

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

	token, expiresAt, err := client.AcquireSkypeToken(ctx, state.AccessToken)
	if err != nil {
		log.Error().Err(err).Msg("Failed to acquire skypetoken")
		os.Exit(1)
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
	log.Info().
		Int("status", result.StatusCode).
		Str("body_snippet", result.BodySnippet).
		Interface("auth_headers", result.AuthHeaders).
		Msg("Teams probe response")

	interpretation := "probe result unclear"
	if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden {
		interpretation = "401/403 -> expected (missing Teams-native token)"
	} else if result.StatusCode == http.StatusOK && looksJSON(result.BodySnippet) {
		interpretation = "200/JSON -> cookies OK"
	}
	log.Info().Msg(interpretation)
}

func looksJSON(body string) bool {
	trimmed := strings.TrimSpace(body)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}
