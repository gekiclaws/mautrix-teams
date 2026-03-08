package connector

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"go.mau.fi/mautrix-teams/internal/teams/auth"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

const (
	FlowIDWebviewLocalStorage           = "webview_localstorage"
	FlowIDEnterpriseWebviewLocalStorage = "enterprise_webview_localstorage"

	LoginStepIDWebviewLocalStorage           = "go.mau.teams.webview_localstorage"
	LoginStepIDEnterpriseWebviewLocalStorage = "go.mau.teams.enterprise_webview_localstorage"

	teamsLoginSpecialStorage = "go.mau.teams.storage"
	teamsLoginSpecialDebug   = "go.mau.teams.debug"

	fullStorageKey = "__mautrix_teams_full_storage"
)

// msalExtractJS is the shared JavaScript that polls for MSAL localStorage keys
// and returns the full localStorage dump once tokens are detected.
const msalExtractJS = `(async () => {
  const trace = [];
  const addTrace = (msg) => {
    if (trace.length < 80) {
      trace.push(msg);
    }
  };
  const traceValue = () => trace.join(" | ");
  addTrace("start url=" + location.href);

  // Force fallback auth path before passkey/WebAuthn prompts.
  try {
    Object.defineProperty(Navigator.prototype, "credentials", {
      get() {
        return {
          get: async () => {
            throw new DOMException("User cancelled", "NotAllowedError");
          }
        };
      }
    });
    addTrace("webauthn_override=ok");
  } catch (e) {
    addTrace("webauthn_override=failed:" + String((e && e.message) || e));
  }

  function dump() {
    try { return JSON.stringify(Object.fromEntries(Object.entries(localStorage))); } catch (e) { return ""; }
  }
  function trySet(key, value) {
    try { localStorage.setItem(key, value); return true; } catch (e) { return false; }
  }
  function findMSALKey() {
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (!k) continue;
      if (k.startsWith("msal.token.keys.")) return k;
      if (k.startsWith("msal.") && k.includes(".token.keys.")) return k;
    }
    return "";
  }
  for (let i = 0; i < 1200; i++) { // ~2 minutes
    if (i % 50 === 0) {
      addTrace("poll i=" + i + " ls_len=" + localStorage.length + " url=" + location.href);
    }
    const key = findMSALKey();
    if (key) {
      addTrace("msal_key_found=" + key);
      const storage = dump();
      addTrace("dump_len=" + storage.length);
      if (storage) {
        const debug = traceValue();
        const storageSaved = trySet("__mautrix_teams_full_storage", storage);
        const debugSaved = trySet("__mautrix_teams_debug", debug);
        addTrace("stash_storage=" + (storageSaved ? "ok" : "fail") + " stash_debug=" + (debugSaved ? "ok" : "fail"));
        return { storage, debug };
      }
      addTrace("dump_empty");
    }
    await new Promise(r => setTimeout(r, 100));
  }
  const finalDump = dump();
  addTrace("timeout final_dump_len=" + finalDump.length + " url=" + location.href);
  return { storage: finalDump || "{}", debug: traceValue() };
})()`

var loginFlowWebviewLocalStorage = bridgev2.LoginFlow{
	Name:        "teams.live.com (in-app browser)",
	Description: "Login using an embedded browser and automatic localStorage extraction.",
	ID:          FlowIDWebviewLocalStorage,
}

var loginFlowEnterpriseWebview = bridgev2.LoginFlow{
	Name:        "teams.microsoft.com (Work/School)",
	Description: "Login using an embedded browser for Work/School accounts.",
	ID:          FlowIDEnterpriseWebviewLocalStorage,
}

type WebviewLocalStorageLogin struct {
	Main      *TeamsConnector
	User      *bridgev2.User
	submitted atomic.Bool
	canceled  atomic.Bool
}

var _ bridgev2.LoginProcessCookies = (*WebviewLocalStorageLogin)(nil)

func (l *WebviewLocalStorageLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	_ = ctx
	if l != nil && l.User != nil {
		l.User.Log.Info().
			Str("local_storage_key", fullStorageKey).
			Msg("Starting Teams webview login flow with auto localStorage extraction")
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for i := 1; i <= 8; i++ { // up to ~2 minutes
				<-ticker.C
				if l.submitted.Load() || l.canceled.Load() {
					return
				}
				l.User.Log.Warn().
					Int("elapsed_seconds", i*15).
					Msg("Teams webview login still waiting for cookie submission")
			}
			if !l.submitted.Load() && !l.canceled.Load() {
				l.User.Log.Warn().Msg("Teams webview login has not submitted cookies after 2 minutes; extraction may be stalled")
			}
		}()
	}
	instructions := "Log in to Teams in the embedded browser. The bridge will automatically extract localStorage, close the window, and return you to Beeper."
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeCookies,
		StepID:       LoginStepIDWebviewLocalStorage,
		Instructions: instructions,
		CookiesParams: &bridgev2.LoginCookiesParams{
			URL: "https://teams.live.com/v2",
			Fields: []bridgev2.LoginCookieField{
				{
					ID:       "storage",
					Required: true,
					Sources: []bridgev2.LoginCookieFieldSource{
						{
							// Primary path: direct ExtractJS output.
							Type: bridgev2.LoginCookieTypeSpecial,
							Name: teamsLoginSpecialStorage,
						},
						{
							// Fallback path: value persisted by ExtractJS.
							Type: bridgev2.LoginCookieTypeLocalStorage,
							Name: fullStorageKey,
						},
					},
				},
				{
					ID:       "debug",
					Required: false,
					Sources: []bridgev2.LoginCookieFieldSource{
						{
							Type: bridgev2.LoginCookieTypeSpecial,
							Name: teamsLoginSpecialDebug,
						},
						{
							Type: bridgev2.LoginCookieTypeLocalStorage,
							Name: "__mautrix_teams_debug",
						},
					},
				},
			},
			WaitForURLPattern: ".*",
			ExtractJS:         msalExtractJS,
		},
	}, nil
}

func (l *WebviewLocalStorageLogin) Cancel() {
	if l != nil {
		l.canceled.Store(true)
		if l.User != nil {
			l.User.Log.Warn().Msg("Teams webview login was canceled before cookie submission")
		}
	}
}

func (l *WebviewLocalStorageLogin) SubmitCookies(ctx context.Context, cookies map[string]string) (*bridgev2.LoginStep, error) {
	if l == nil || l.Main == nil || l.User == nil {
		return nil, errors.New("missing login state")
	}
	l.submitted.Store(true)
	cookieKeys := make([]string, 0, len(cookies))
	for key := range cookies {
		cookieKeys = append(cookieKeys, key)
	}
	sort.Strings(cookieKeys)
	l.User.Log.Info().
		Int("cookie_fields", len(cookies)).
		Strs("cookie_keys", cookieKeys).
		Msg("Teams webview login submitted cookie payload")
	debugInfo := strings.TrimSpace(cookies["debug"])
	if debugInfo != "" {
		l.User.Log.Info().
			Str("teams_login_cookie_debug", truncateForLog(debugInfo, 4000)).
			Msg("Teams login extraction breadcrumbs")
	}
	raw := strings.TrimSpace(cookies["storage"])
	if raw == "" {
		return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_STORAGE", Err: "Missing localStorage payload", StatusCode: http.StatusBadRequest}
	}
	clientID := resolveClientID(l.Main, auth.AccountTypeConsumer)
	meta, err := ExtractTeamsLoginMetadataFromLocalStorage(ctx, raw, clientID)
	if err != nil {
		return nil, err
	}
	l.User.Log.Info().
		Bool("graph_token_present", strings.TrimSpace(meta.GraphAccessToken) != "").
		Msg("Teams login extracted Graph token state")
	if meta.GraphExpiresAt != 0 {
		l.User.Log.Debug().
			Time("graph_expires_at", time.Unix(meta.GraphExpiresAt, 0).UTC()).
			Msg("Teams login Graph token expiry")
	}
	ul, err := l.User.NewLogin(ctx, &database.UserLogin{
		ID:         networkid.UserLoginID(meta.TeamsUserID),
		RemoteName: meta.TeamsUserID,
		Metadata:   meta,
	}, &bridgev2.NewLoginParams{DeleteOnConflict: true})
	if err != nil {
		return nil, err
	}
	startLoginConnect(ul, loginConnectBaseCtx(l.Main))
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       "go.mau.teams.complete",
		Instructions: "Login complete.",
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: ul.ID,
			UserLogin:   ul,
		},
	}, nil
}

func loginConnectBaseCtx(main *TeamsConnector) context.Context {
	if main != nil && main.Bridge != nil && main.Bridge.BackgroundCtx != nil {
		return main.Bridge.BackgroundCtx
	}
	return context.Background()
}

func startLoginConnect(login *bridgev2.UserLogin, baseCtx context.Context) {
	if login == nil || login.Client == nil {
		return
	}
	ctx := baseCtx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = login.Log.WithContext(ctx)
	go login.Client.Connect(ctx)
}

func truncateForLog(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "...(truncated)"
}

// EnterpriseWebviewLogin handles the Enterprise (Work/School) login flow.
type EnterpriseWebviewLogin struct {
	Main      *TeamsConnector
	User      *bridgev2.User
	submitted atomic.Bool
	canceled  atomic.Bool
}

var _ bridgev2.LoginProcessCookies = (*EnterpriseWebviewLogin)(nil)

func (l *EnterpriseWebviewLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	_ = ctx
	if l != nil && l.User != nil {
		l.User.Log.Info().
			Str("local_storage_key", fullStorageKey).
			Msg("Starting Enterprise Teams webview login flow with auto localStorage extraction")
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for i := 1; i <= 8; i++ {
				<-ticker.C
				if l.submitted.Load() || l.canceled.Load() {
					return
				}
				l.User.Log.Warn().
					Int("elapsed_seconds", i*15).
					Msg("Enterprise Teams webview login still waiting for cookie submission")
			}
			if !l.submitted.Load() && !l.canceled.Load() {
				l.User.Log.Warn().Msg("Enterprise Teams webview login has not submitted cookies after 2 minutes; extraction may be stalled")
			}
		}()
	}
	instructions := "Log in to Teams in the embedded browser using your Work/School account. The bridge will automatically extract localStorage, close the window, and return you to Beeper."
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeCookies,
		StepID:       LoginStepIDEnterpriseWebviewLocalStorage,
		Instructions: instructions,
		CookiesParams: &bridgev2.LoginCookiesParams{
			URL: "https://teams.microsoft.com",
			Fields: []bridgev2.LoginCookieField{
				{
					ID:       "storage",
					Required: true,
					Sources: []bridgev2.LoginCookieFieldSource{
						{
							Type: bridgev2.LoginCookieTypeSpecial,
							Name: teamsLoginSpecialStorage,
						},
						{
							Type: bridgev2.LoginCookieTypeLocalStorage,
							Name: fullStorageKey,
						},
					},
				},
				{
					ID:       "debug",
					Required: false,
					Sources: []bridgev2.LoginCookieFieldSource{
						{
							Type: bridgev2.LoginCookieTypeSpecial,
							Name: teamsLoginSpecialDebug,
						},
						{
							Type: bridgev2.LoginCookieTypeLocalStorage,
							Name: "__mautrix_teams_debug",
						},
					},
				},
			},
			WaitForURLPattern: ".*",
			ExtractJS:         msalExtractJS,
		},
	}, nil
}

func (l *EnterpriseWebviewLogin) Cancel() {
	if l != nil {
		l.canceled.Store(true)
		if l.User != nil {
			l.User.Log.Warn().Msg("Enterprise Teams webview login was canceled before cookie submission")
		}
	}
}

func (l *EnterpriseWebviewLogin) SubmitCookies(ctx context.Context, cookies map[string]string) (*bridgev2.LoginStep, error) {
	if l == nil || l.Main == nil || l.User == nil {
		return nil, errors.New("missing login state")
	}
	l.submitted.Store(true)
	cookieKeys := make([]string, 0, len(cookies))
	for key := range cookies {
		cookieKeys = append(cookieKeys, key)
	}
	sort.Strings(cookieKeys)
	l.User.Log.Info().
		Int("cookie_fields", len(cookies)).
		Strs("cookie_keys", cookieKeys).
		Msg("Enterprise Teams webview login submitted cookie payload")
	debugInfo := strings.TrimSpace(cookies["debug"])
	if debugInfo != "" {
		l.User.Log.Info().
			Str("teams_login_cookie_debug", truncateForLog(debugInfo, 4000)).
			Msg("Enterprise Teams login extraction breadcrumbs")
	}
	raw := strings.TrimSpace(cookies["storage"])
	if raw == "" {
		return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_STORAGE", Err: "Missing localStorage payload", StatusCode: http.StatusBadRequest}
	}
	clientID := resolveClientID(l.Main, auth.AccountTypeEnterprise)
	meta, err := ExtractEnterpriseTeamsLoginMetadataFromLocalStorage(ctx, raw, clientID)
	if err != nil {
		return nil, err
	}
	l.User.Log.Info().
		Str("account_type", meta.AccountType).
		Str("chat_service", meta.ChatService).
		Bool("graph_token_present", strings.TrimSpace(meta.GraphAccessToken) != "").
		Msg("Enterprise Teams login extracted token state")
	if meta.GraphExpiresAt != 0 {
		l.User.Log.Debug().
			Time("graph_expires_at", time.Unix(meta.GraphExpiresAt, 0).UTC()).
			Msg("Enterprise Teams login Graph token expiry")
	}
	ul, err := l.User.NewLogin(ctx, &database.UserLogin{
		ID:         networkid.UserLoginID(meta.TeamsUserID),
		RemoteName: meta.TeamsUserID,
		Metadata:   meta,
	}, &bridgev2.NewLoginParams{DeleteOnConflict: true})
	if err != nil {
		return nil, err
	}
	startLoginConnect(ul, loginConnectBaseCtx(l.Main))
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       "go.mau.teams.complete",
		Instructions: "Login complete.",
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: ul.ID,
			UserLogin:   ul,
		},
	}, nil
}
