package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-teams/internal/teams/auth"
	"go.mau.fi/mautrix-teams/pkg/teamsid"
)

const (
	FlowIDMSALLocalStorage    = "msal_localstorage"
	FlowIDWebviewLocalStorage = "webview_localstorage"

	LoginStepIDMSALLocalStorage    = "go.mau.teams.msal_localstorage"
	LoginStepIDWebviewLocalStorage = "go.mau.teams.webview_localstorage"
)

var loginFlowMSALLocalStorage = bridgev2.LoginFlow{
	Name:        "teams.live.com (browser)",
	Description: "Login using Teams web app tokens from browser localStorage.",
	ID:          FlowIDMSALLocalStorage,
}

var loginFlowWebviewLocalStorage = bridgev2.LoginFlow{
	Name:        "teams.live.com (in-app browser)",
	Description: "Login using an embedded browser and automatic localStorage extraction.",
	ID:          FlowIDWebviewLocalStorage,
}

type MSALLocalStorageLogin struct {
	Main *TeamsConnector
	User *bridgev2.User
}

var _ bridgev2.LoginProcessUserInput = (*MSALLocalStorageLogin)(nil)

func (l *MSALLocalStorageLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	_ = ctx
	clientID := ""
	if l.Main != nil {
		clientID = strings.TrimSpace(l.Main.Config.ClientID)
	}
	if clientID == "" {
		clientID = auth.NewClient(nil).ClientID
	}
	instructions := "1. Open https://teams.live.com/v2 in your browser and log in.\n" +
		"2. Open browser devtools console.\n" +
		"3. Run:\n" +
		"   copy(JSON.stringify(Object.fromEntries(Object.entries(localStorage))))\n" +
		"4. Paste the copied JSON below.\n\n" +
		"Note: This flow looks for MSAL keys for client_id " + clientID + "."

	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       LoginStepIDMSALLocalStorage,
		Instructions: instructions,
		UserInputParams: &bridgev2.LoginUserInputParams{
			Fields: []bridgev2.LoginInputDataField{{
				Type:        bridgev2.LoginInputFieldTypeToken,
				ID:          "storage",
				Name:        "localStorage JSON",
				Description: "Paste the JSON dump of localStorage from https://teams.live.com/v2",
			}},
		},
	}, nil
}

func (l *MSALLocalStorageLogin) Cancel() {}

func (l *MSALLocalStorageLogin) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	if l == nil || l.Main == nil || l.User == nil {
		return nil, errors.New("missing login state")
	}
	raw := strings.TrimSpace(input["storage"])
	if raw == "" {
		return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_STORAGE", Err: "Missing localStorage payload", StatusCode: http.StatusBadRequest}
	}

	clientID := strings.TrimSpace(l.Main.Config.ClientID)
	if clientID == "" {
		clientID = auth.NewClient(nil).ClientID
	}

	meta, err := extractMetaFromStorage(ctx, raw, clientID)
	if err != nil {
		return nil, err
	}
	loginID := networkid.UserLoginID(meta.TeamsUserID)
	ul, err := l.User.NewLogin(ctx, &database.UserLogin{
		ID:         loginID,
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

type WebviewLocalStorageLogin struct {
	Main *TeamsConnector
	User *bridgev2.User
}

var _ bridgev2.LoginProcessCookies = (*WebviewLocalStorageLogin)(nil)

func (l *WebviewLocalStorageLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	_ = ctx
	instructions := "Log in to Teams in the embedded browser. The bridge will automatically extract localStorage when available."
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeCookies,
		StepID:       LoginStepIDWebviewLocalStorage,
		Instructions: instructions,
		CookiesParams: &bridgev2.LoginCookiesParams{
			URL: "https://teams.live.com/v2",
			Fields: []bridgev2.LoginCookieField{{
				ID:       "storage",
				Required: true,
				Sources: []bridgev2.LoginCookieFieldSource{{
					Type: bridgev2.LoginCookieTypeLocalStorage,
					// The client will fill this field using ExtractJS instead of this name.
					Name: "msal.token.keys.*",
				}},
			}},
			WaitForURLPattern: "^https://teams\\.live\\.com/v2(?:/.*)?$",
			ExtractJS: `(async () => {
  function dump() {
    try { return JSON.stringify(Object.fromEntries(Object.entries(localStorage))); } catch (e) { return ""; }
  }
  function hasMSALKeys() {
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k && k.startsWith("msal.token.keys.")) return true;
    }
    return false;
  }
  for (let i = 0; i < 240; i++) { // ~2 minutes
    if (hasMSALKeys()) {
      const storage = dump();
      if (storage) return { storage };
    }
    await new Promise(r => setTimeout(r, 500));
  }
  throw new Error("Timed out waiting for MSAL tokens in localStorage");
})()`,
		},
	}, nil
}

func (l *WebviewLocalStorageLogin) Cancel() {}

func (l *WebviewLocalStorageLogin) SubmitCookies(ctx context.Context, cookies map[string]string) (*bridgev2.LoginStep, error) {
	if l == nil || l.Main == nil || l.User == nil {
		return nil, errors.New("missing login state")
	}
	raw := strings.TrimSpace(cookies["storage"])
	if raw == "" {
		return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_STORAGE", Err: "Missing localStorage payload", StatusCode: http.StatusBadRequest}
	}
	clientID := strings.TrimSpace(l.Main.Config.ClientID)
	if clientID == "" {
		clientID = auth.NewClient(nil).ClientID
	}
	meta, err := extractMetaFromStorage(ctx, raw, clientID)
	if err != nil {
		return nil, err
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

func extractMetaFromStorage(ctx context.Context, raw string, clientID string) (*teamsid.UserLoginMetadata, error) {
	state, err := auth.ExtractTokensFromMSALLocalStorage(raw, clientID)
	if err != nil {
		return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_INVALID_STORAGE", Err: fmt.Sprintf("Failed to extract tokens: %v", err), StatusCode: http.StatusBadRequest}
	}
	if strings.TrimSpace(state.AccessToken) == "" {
		return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_ACCESS_TOKEN", Err: "Access token missing from extracted state", StatusCode: http.StatusBadRequest}
	}

	authClient := auth.NewClient(nil)
	token, expiresAt, skypeID, err := authClient.AcquireSkypeToken(ctx, state.AccessToken)
	if err != nil {
		return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_SKYPETOKEN_FAILED", Err: fmt.Sprintf("Failed to acquire skypetoken: %v", err), StatusCode: http.StatusBadRequest}
	}

	teamsUserID := auth.NormalizeTeamsUserID(skypeID)
	if teamsUserID == "" {
		return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_USER_ID", Err: "Teams user ID missing from skypetoken response", StatusCode: http.StatusBadRequest}
	}

	return &teamsid.UserLoginMetadata{
		RefreshToken:         state.RefreshToken,
		AccessTokenExpiresAt: state.ExpiresAtUnix,
		SkypeToken:           token,
		SkypeTokenExpiresAt:  expiresAt,
		TeamsUserID:          teamsUserID,
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
