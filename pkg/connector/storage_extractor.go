package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.mau.fi/mautrix-teams/internal/teams/auth"
	"go.mau.fi/mautrix-teams/pkg/teamsid"

	"maunium.net/go/mautrix/bridgev2"
)

const mbiRefreshScope = "service::api.fl.spaces.skype.com::MBI_SSL"
const enterpriseRefreshScope = "https://api.spaces.skype.com/.default"
const graphFilesReadWriteScope = "https://graph.microsoft.com/Files.ReadWrite"

var newAuthClient = auth.NewClient
var newEnterpriseAuthClient = auth.NewEnterpriseClient

// ExtractTeamsLoginMetadataFromLocalStorage parses the MSAL localStorage payload
// and exchanges its access token for a Teams skypetoken (Consumer flow).
func ExtractTeamsLoginMetadataFromLocalStorage(ctx context.Context, rawStorage, clientID string) (*teamsid.UserLoginMetadata, error) {
	return extractTeamsLoginMetadata(ctx, rawStorage, clientID, auth.AccountTypeConsumer)
}

// ExtractEnterpriseTeamsLoginMetadataFromLocalStorage parses the MSAL localStorage payload
// for an Enterprise (Work/School) account and exchanges its access token for a Teams skypetoken.
func ExtractEnterpriseTeamsLoginMetadataFromLocalStorage(ctx context.Context, rawStorage, clientID string) (*teamsid.UserLoginMetadata, error) {
	return extractTeamsLoginMetadata(ctx, rawStorage, clientID, auth.AccountTypeEnterprise)
}

func extractTeamsLoginMetadata(ctx context.Context, rawStorage, clientID string, accountType auth.AccountType) (*teamsid.UserLoginMetadata, error) {
	state, err := auth.ExtractTokensFromMSALLocalStorage(rawStorage, clientID)
	if err != nil {
		return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_INVALID_STORAGE", Err: fmt.Sprintf("Failed to extract tokens: %v", err), StatusCode: http.StatusBadRequest}
	}

	isEnterprise := accountType == auth.AccountTypeEnterprise

	var authClient *auth.Client
	if isEnterprise {
		authClient = newEnterpriseAuthClient(state.TenantID, nil)
	} else {
		authClient = newAuthClient(nil)
		if id := strings.TrimSpace(clientID); id != "" {
			authClient.ClientID = id
		}
	}

	accessToken := strings.TrimSpace(state.AccessToken)
	if accessToken == "" {
		refreshToken := strings.TrimSpace(state.RefreshToken)
		if refreshToken == "" {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_ACCESS_TOKEN", Err: "Access token missing from extracted state", StatusCode: http.StatusBadRequest}
		}
		var refreshed *auth.AuthState
		var refreshErr error
		if isEnterprise {
			refreshed, refreshErr = refreshAccessTokenForEnterpriseScope(ctx, authClient, refreshToken)
		} else {
			refreshed, refreshErr = refreshAccessTokenForSkypeScope(ctx, authClient, refreshToken)
		}
		if refreshErr != nil {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_ACCESS_TOKEN", Err: fmt.Sprintf("Access token missing from localStorage and refresh failed: %v", refreshErr), StatusCode: http.StatusBadRequest}
		}
		accessToken = strings.TrimSpace(refreshed.AccessToken)
		if accessToken == "" {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_ACCESS_TOKEN", Err: "Access token missing after refresh-token exchange", StatusCode: http.StatusBadRequest}
		}
		if rt := strings.TrimSpace(refreshed.RefreshToken); rt != "" {
			state.RefreshToken = rt
		}
		if refreshed.ExpiresAtUnix != 0 {
			state.ExpiresAtUnix = refreshed.ExpiresAtUnix
		}
		if token := strings.TrimSpace(refreshed.GraphAccessToken); token != "" {
			state.GraphAccessToken = token
			state.GraphExpiresAt = refreshed.GraphExpiresAt
		}
	}

	// For Consumer, also try to obtain a Graph access token if not already present.
	if !isEnterprise && strings.TrimSpace(state.GraphAccessToken) == "" {
		refreshToken := strings.TrimSpace(state.RefreshToken)
		if refreshToken != "" {
			graphState, graphErr := refreshAccessTokenForGraphScope(ctx, authClient, refreshToken)
			if graphErr == nil {
				if token := strings.TrimSpace(graphState.GraphAccessToken); token != "" {
					state.GraphAccessToken = token
					state.GraphExpiresAt = graphState.GraphExpiresAt
				}
				if rt := strings.TrimSpace(graphState.RefreshToken); rt != "" {
					state.RefreshToken = rt
				}
			}
		}
	}

	meta := &teamsid.UserLoginMetadata{
		RefreshToken:         state.RefreshToken,
		AccessTokenExpiresAt: state.ExpiresAtUnix,
		GraphAccessToken:     strings.TrimSpace(state.GraphAccessToken),
		GraphExpiresAt:       state.GraphExpiresAt,
	}

	if isEnterprise {
		token, expiresAt, regionGTMs, err := authClient.AcquireEnterpriseSkypeToken(ctx, accessToken)
		if err != nil {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_SKYPETOKEN_FAILED", Err: fmt.Sprintf("Failed to acquire enterprise skypetoken: %v", err), StatusCode: http.StatusBadRequest}
		}
		meta.SkypeToken = token
		meta.SkypeTokenExpiresAt = expiresAt
		meta.AccountType = string(auth.AccountTypeEnterprise)
		meta.TenantID = state.TenantID

		if regionGTMs != nil {
			gtms := auth.ParseRegionGTMs(regionGTMs)
			if gtms != nil && gtms.ChatService != "" {
				meta.ChatService = gtms.ChatService
			}
		}

		// Enterprise authz doesn't return a skypeID in the same way as Consumer.
		// Extract user ID from the Skype token JWT's skypeid or oid claim.
		teamsUserID := auth.NormalizeTeamsUserID(extractUserIDFromSkypeToken(token))
		if teamsUserID == "" {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_USER_ID", Err: "Teams user ID missing from enterprise skypetoken", StatusCode: http.StatusBadRequest}
		}
		meta.TeamsUserID = teamsUserID
	} else {
		token, expiresAt, skypeID, err := authClient.AcquireSkypeToken(ctx, accessToken)
		if err != nil {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_SKYPETOKEN_FAILED", Err: fmt.Sprintf("Failed to acquire skypetoken: %v", err), StatusCode: http.StatusBadRequest}
		}
		meta.SkypeToken = token
		meta.SkypeTokenExpiresAt = expiresAt
		meta.AccountType = string(auth.AccountTypeConsumer)

		teamsUserID := auth.NormalizeTeamsUserID(skypeID)
		if teamsUserID == "" {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_USER_ID", Err: "Teams user ID missing from skypetoken response", StatusCode: http.StatusBadRequest}
		}
		meta.TeamsUserID = teamsUserID
	}

	return meta, nil
}

// extractUserIDFromSkypeToken attempts to extract the user identifier from a Skype JWT token.
// Enterprise Skype tokens are JWTs; the "skypeid" or "oid" claim contains the user ID.
func extractUserIDFromSkypeToken(token string) string {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	// Try standard base64 then raw URL encoding.
	payload := decodeBase64Segment(parts[1])
	if payload == nil {
		return ""
	}
	// Look for skypeid first, then oid.
	type jwtClaims struct {
		SkypeID string `json:"skypeid"`
		OID     string `json:"oid"`
	}
	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	if claims.SkypeID != "" {
		return claims.SkypeID
	}
	if claims.OID != "" {
		return "8:orgid:" + claims.OID
	}
	return ""
}

func refreshAccessTokenForSkypeScope(ctx context.Context, client *auth.Client, refreshToken string) (*auth.AuthState, error) {
	// Prefer requesting the Skype MBI scope explicitly for skypetoken bootstrap.
	retryClient := *client
	retryClient.Scopes = []string{mbiRefreshScope, "offline_access"}
	refreshed, err := retryClient.RefreshAccessToken(ctx, refreshToken)
	if err == nil {
		return refreshed, nil
	}

	// Fallback to default scopes for environments that don't accept MBI scope on refresh.
	fallbackClient := *client
	fallbackClient.Scopes = []string{"openid", "profile", "offline_access"}
	refreshed, fallbackErr := fallbackClient.RefreshAccessToken(ctx, refreshToken)
	if fallbackErr == nil {
		return refreshed, nil
	}

	return nil, fmt.Errorf("MBI scope refresh failed (%v); default scopes failed (%v)", err, fallbackErr)
}

func refreshAccessTokenForEnterpriseScope(ctx context.Context, client *auth.Client, refreshToken string) (*auth.AuthState, error) {
	retryClient := *client
	retryClient.Scopes = []string{enterpriseRefreshScope, "offline_access"}
	refreshed, err := retryClient.RefreshAccessToken(ctx, refreshToken)
	if err == nil {
		return refreshed, nil
	}

	// Fallback to default Enterprise scopes.
	fallbackClient := *client
	fallbackClient.Scopes = []string{"openid", "profile", "offline_access", enterpriseRefreshScope}
	refreshed, fallbackErr := fallbackClient.RefreshAccessToken(ctx, refreshToken)
	if fallbackErr == nil {
		return refreshed, nil
	}

	return nil, fmt.Errorf("enterprise scope refresh failed (%v); fallback scopes failed (%v)", err, fallbackErr)
}

func refreshAccessTokenForGraphScope(ctx context.Context, client *auth.Client, refreshToken string) (*auth.AuthState, error) {
	retryClient := *client
	retryClient.Scopes = []string{graphFilesReadWriteScope, "offline_access"}
	refreshed, err := retryClient.RefreshAccessToken(ctx, refreshToken)
	if err == nil {
		return refreshed, nil
	}

	fallbackClient := *client
	fallbackClient.Scopes = []string{"openid", "profile", "offline_access", graphFilesReadWriteScope}
	refreshed, fallbackErr := fallbackClient.RefreshAccessToken(ctx, refreshToken)
	if fallbackErr == nil {
		return refreshed, nil
	}

	return nil, fmt.Errorf("graph scope refresh failed (%v); fallback scopes failed (%v)", err, fallbackErr)
}

func resolveClientID(main *TeamsConnector, accountType auth.AccountType) string {
	if accountType == auth.AccountTypeEnterprise {
		// Enterprise uses a fixed client ID; operator-configured ClientID is for Consumer only.
		return auth.EnterpriseClientID
	}
	if main != nil {
		if id := strings.TrimSpace(main.Config.ClientID); id != "" {
			return id
		}
	}
	return auth.DefaultClientID
}

// LoginFromRefreshToken bootstraps a full login using only a refresh token (Consumer flow).
func LoginFromRefreshToken(ctx context.Context, refreshToken, clientID string) (*teamsid.UserLoginMetadata, error) {
	return loginFromRefreshToken(ctx, refreshToken, clientID, auth.AccountTypeConsumer)
}

// EnterpriseLoginFromRefreshToken bootstraps a full login using a refresh token and tenant ID (Enterprise flow).
func EnterpriseLoginFromRefreshToken(ctx context.Context, refreshToken, tenantID, clientID string) (*teamsid.UserLoginMetadata, error) {
	return loginFromRefreshToken(ctx, refreshToken, clientID, auth.AccountTypeEnterprise, tenantID)
}

func loginFromRefreshToken(ctx context.Context, refreshToken, clientID string, accountType auth.AccountType, optTenantID ...string) (*teamsid.UserLoginMetadata, error) {
	isEnterprise := accountType == auth.AccountTypeEnterprise

	var authClient *auth.Client
	tenantID := ""
	if len(optTenantID) > 0 {
		tenantID = optTenantID[0]
	}
	if isEnterprise {
		if tenantID == "" {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_TENANT_ID", Err: "Tenant ID is required for enterprise login", StatusCode: http.StatusBadRequest}
		}
		authClient = newEnterpriseAuthClient(tenantID, nil)
	} else {
		authClient = newAuthClient(nil)
		if id := strings.TrimSpace(clientID); id != "" {
			authClient.ClientID = id
		}
	}

	// Refresh access token from refresh token.
	var refreshed *auth.AuthState
	var refreshErr error
	if isEnterprise {
		refreshed, refreshErr = refreshAccessTokenForEnterpriseScope(ctx, authClient, refreshToken)
	} else {
		refreshed, refreshErr = refreshAccessTokenForSkypeScope(ctx, authClient, refreshToken)
	}
	if refreshErr != nil {
		return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_REFRESH_FAILED", Err: fmt.Sprintf("Failed to refresh access token: %v", refreshErr), StatusCode: http.StatusBadRequest}
	}
	accessToken := strings.TrimSpace(refreshed.AccessToken)
	if accessToken == "" {
		return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_ACCESS_TOKEN", Err: "Access token missing after refresh", StatusCode: http.StatusBadRequest}
	}

	// Update refresh token if a new one was returned.
	currentRefreshToken := refreshToken
	if rt := strings.TrimSpace(refreshed.RefreshToken); rt != "" {
		currentRefreshToken = rt
	}

	meta := &teamsid.UserLoginMetadata{
		RefreshToken:         currentRefreshToken,
		AccessTokenExpiresAt: refreshed.ExpiresAtUnix,
		GraphAccessToken:     strings.TrimSpace(refreshed.GraphAccessToken),
		GraphExpiresAt:       refreshed.GraphExpiresAt,
	}

	// Try to obtain Graph token for consumer if not already present.
	if !isEnterprise && strings.TrimSpace(meta.GraphAccessToken) == "" {
		graphState, graphErr := refreshAccessTokenForGraphScope(ctx, authClient, currentRefreshToken)
		if graphErr == nil {
			if token := strings.TrimSpace(graphState.GraphAccessToken); token != "" {
				meta.GraphAccessToken = token
				meta.GraphExpiresAt = graphState.GraphExpiresAt
			}
			if rt := strings.TrimSpace(graphState.RefreshToken); rt != "" {
				meta.RefreshToken = rt
			}
		}
	}

	if isEnterprise {
		token, expiresAt, regionGTMs, err := authClient.AcquireEnterpriseSkypeToken(ctx, accessToken)
		if err != nil {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_SKYPETOKEN_FAILED", Err: fmt.Sprintf("Failed to acquire enterprise skypetoken: %v", err), StatusCode: http.StatusBadRequest}
		}
		meta.SkypeToken = token
		meta.SkypeTokenExpiresAt = expiresAt
		meta.AccountType = string(auth.AccountTypeEnterprise)
		meta.TenantID = tenantID

		if regionGTMs != nil {
			gtms := auth.ParseRegionGTMs(regionGTMs)
			if gtms != nil && gtms.ChatService != "" {
				meta.ChatService = gtms.ChatService
			}
		}

		teamsUserID := auth.NormalizeTeamsUserID(extractUserIDFromSkypeToken(token))
		if teamsUserID == "" {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_USER_ID", Err: "Teams user ID missing from enterprise skypetoken", StatusCode: http.StatusBadRequest}
		}
		meta.TeamsUserID = teamsUserID
	} else {
		token, expiresAt, skypeID, err := authClient.AcquireSkypeToken(ctx, accessToken)
		if err != nil {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_SKYPETOKEN_FAILED", Err: fmt.Sprintf("Failed to acquire skypetoken: %v", err), StatusCode: http.StatusBadRequest}
		}
		meta.SkypeToken = token
		meta.SkypeTokenExpiresAt = expiresAt
		meta.AccountType = string(auth.AccountTypeConsumer)

		teamsUserID := auth.NormalizeTeamsUserID(skypeID)
		if teamsUserID == "" {
			return nil, bridgev2.RespError{ErrCode: "FI.MAU.TEAMS_MISSING_USER_ID", Err: "Teams user ID missing from skypetoken response", StatusCode: http.StatusBadRequest}
		}
		meta.TeamsUserID = teamsUserID
	}

	return meta, nil
}

func decodeBase64Segment(seg string) []byte {
	// JWT uses raw URL encoding without padding.
	data, err := base64.RawURLEncoding.DecodeString(seg)
	if err == nil {
		return data
	}
	// Fallback: try standard encoding with padding.
	data, err = base64.StdEncoding.DecodeString(seg)
	if err == nil {
		return data
	}
	return nil
}
