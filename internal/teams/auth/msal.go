package auth

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

type msalTokenKeys struct {
	RefreshToken []string `json:"refreshToken"`
	IDToken      []string `json:"idToken"`
	AccessToken  []string `json:"accessToken"`
}

type msalTokenEntry struct {
	Secret    string `json:"secret"`
	ExpiresOn string `json:"expiresOn"`
	Target    string `json:"target"`
}

func ExtractTokensFromMSALLocalStorage(raw string, clientID string) (*AuthState, error) {
	storage, err := parseStorage(raw)
	if err != nil {
		return nil, err
	}
	keysEntry, err := findMSALKeys(storage, clientID)
	if err != nil {
		return nil, err
	}

	var keys msalTokenKeys
	if err := json.Unmarshal([]byte(keysEntry), &keys); err != nil {
		return nil, err
	}
	if len(keys.RefreshToken) == 0 {
		return nil, errors.New("no refresh token keys in msal token keys")
	}

	refreshEntry, ok := storage[keys.RefreshToken[0]]
	if !ok {
		return nil, errors.New("refresh token entry not found in localStorage")
	}

	var refresh msalTokenEntry
	if err := json.Unmarshal([]byte(refreshEntry), &refresh); err != nil {
		return nil, err
	}
	if refresh.Secret == "" {
		return nil, errors.New("refresh token secret missing")
	}

	state := &AuthState{
		RefreshToken: refresh.Secret,
	}
	if refresh.ExpiresOn != "" {
		if parsed, ok := parseMSALExpires(refresh.ExpiresOn); ok {
			state.ExpiresAtUnix = parsed
		}
	}

	// Extract tenant ID from the account that matches the selected token keys.
	homeAccountID := extractHomeAccountIDFromMSALCredentialKeys(keys.RefreshToken)
	if homeAccountID == "" {
		homeAccountID = extractHomeAccountIDFromMSALCredentialKeys(keys.AccessToken)
	}
	state.TenantID = extractTenantIDFromMSALStorage(storage, homeAccountID)

	if len(keys.AccessToken) > 0 {
		accessToken, expiresAt := selectMBIAccessToken(storage, keys.AccessToken)
		if accessToken != "" {
			state.AccessToken = accessToken
			if expiresAt != 0 {
				state.ExpiresAtUnix = expiresAt
			}
		}
		// If no Consumer MBI token found, try Enterprise scope pattern as fallback.
		if state.AccessToken == "" {
			enterpriseToken, enterpriseExpiry := selectEnterpriseAccessToken(storage, keys.AccessToken)
			if enterpriseToken != "" {
				state.AccessToken = enterpriseToken
				state.AccountType = string(AccountTypeEnterprise)
				if enterpriseExpiry != 0 {
					state.ExpiresAtUnix = enterpriseExpiry
				}
			}
		}
		graphAccessToken, graphExpiresAt := selectGraphAccessToken(storage, keys.AccessToken)
		if graphAccessToken != "" {
			state.GraphAccessToken = graphAccessToken
			if graphExpiresAt != 0 {
				state.GraphExpiresAt = graphExpiresAt
			}
		}
	}

	if len(keys.IDToken) > 0 {
		if idEntry, ok := storage[keys.IDToken[0]]; ok {
			var idToken msalTokenEntry
			if err := json.Unmarshal([]byte(idEntry), &idToken); err == nil {
				state.IDToken = idToken.Secret
			}
		}
	}

	return state, nil
}

const mbiAccessTokenMarker = "service::api.fl.spaces.skype.com::mbi_ssl"
const enterpriseAccessTokenMarker = "api.spaces.skype.com"

func selectMBIAccessToken(storage map[string]string, keys []string) (string, int64) {
	var bestToken string
	var bestExpiry int64
	for _, key := range keys {
		raw, ok := storage[key]
		if !ok || raw == "" {
			continue
		}
		var entry msalTokenEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			continue
		}
		if entry.Secret == "" || !matchesMBITarget(entry.Target) {
			continue
		}
		expiry, _ := parseMSALExpires(entry.ExpiresOn)
		if bestToken == "" || expiry > bestExpiry {
			bestToken = entry.Secret
			bestExpiry = expiry
		}
	}
	return bestToken, bestExpiry
}

func matchesMBITarget(target string) bool {
	if target == "" {
		return false
	}
	lower := strings.ToLower(target)
	return strings.Contains(lower, mbiAccessTokenMarker)
}

func selectEnterpriseAccessToken(storage map[string]string, keys []string) (string, int64) {
	var bestToken string
	var bestExpiry int64
	for _, key := range keys {
		raw, ok := storage[key]
		if !ok || raw == "" {
			continue
		}
		var entry msalTokenEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			continue
		}
		if entry.Secret == "" || !matchesEnterpriseTarget(entry.Target) {
			continue
		}
		expiry, _ := parseMSALExpires(entry.ExpiresOn)
		if bestToken == "" || expiry > bestExpiry {
			bestToken = entry.Secret
			bestExpiry = expiry
		}
	}
	return bestToken, bestExpiry
}

func matchesEnterpriseTarget(target string) bool {
	if target == "" {
		return false
	}
	lower := strings.ToLower(target)
	return strings.Contains(lower, enterpriseAccessTokenMarker) && !strings.Contains(lower, mbiAccessTokenMarker)
}

func selectGraphAccessToken(storage map[string]string, keys []string) (string, int64) {
	var bestToken string
	var bestExpiry int64
	for _, key := range keys {
		raw, ok := storage[key]
		if !ok || raw == "" {
			continue
		}
		var entry msalTokenEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			continue
		}
		if entry.Secret == "" || !matchesGraphTarget(entry.Target) {
			continue
		}
		expiry, _ := parseMSALExpires(entry.ExpiresOn)
		if bestToken == "" || expiry > bestExpiry {
			bestToken = entry.Secret
			bestExpiry = expiry
		}
	}
	return bestToken, bestExpiry
}

func matchesGraphTarget(target string) bool {
	if target == "" {
		return false
	}
	return strings.Contains(strings.ToLower(target), "graph.microsoft.com")
}

func parseStorage(raw string) (map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty localStorage payload")
	}

	var stringMap map[string]string
	if err := json.Unmarshal([]byte(trimmed), &stringMap); err == nil {
		return stringMap, nil
	}

	var anyMap map[string]any
	if err := json.Unmarshal([]byte(trimmed), &anyMap); err != nil {
		return nil, err
	}

	out := make(map[string]string, len(anyMap))
	for key, val := range anyMap {
		switch typed := val.(type) {
		case string:
			out[key] = typed
		default:
			payload, err := json.Marshal(typed)
			if err != nil {
				continue
			}
			out[key] = string(payload)
		}
	}
	return out, nil
}

func findMSALKeys(storage map[string]string, clientID string) (string, error) {
	if storage == nil {
		return "", errors.New("localStorage is empty")
	}
	if clientID != "" {
		key := "msal.token.keys." + clientID
		if val, ok := storage[key]; ok {
			return val, nil
		}
	}

	for key, val := range storage {
		if strings.HasPrefix(key, "msal.token.keys.") {
			return val, nil
		}
	}
	for key, val := range storage {
		if strings.HasPrefix(key, "msal.") && strings.Contains(key, ".token.keys.") {
			return val, nil
		}
	}
	return "", errors.New("msal token keys entry not found")
}

// extractTenantIDFromMSALStorage scans MSAL account entries in localStorage for the tenant ID.
// MSAL account keys follow the pattern "<uid>.<tenantid>-login.windows.net-..." and account
// entries contain a "realm" field with the tenant ID.
func extractTenantIDFromMSALStorage(storage map[string]string, preferredHomeAccountID string) string {
	preferredHomeAccountID = strings.ToLower(strings.TrimSpace(preferredHomeAccountID))
	var fallbackTenantID string

	for key, val := range storage {
		// Account entry keys contain "login.windows.net" or "login.microsoftonline.com".
		lowerKey := strings.ToLower(key)
		if !strings.Contains(lowerKey, "login.windows.net") && !strings.Contains(lowerKey, "login.microsoftonline.com") {
			continue
		}
		// Skip token keys (they contain "-accesstoken-", "-refreshtoken-", "-idtoken-").
		if strings.Contains(lowerKey, "token") {
			continue
		}
		var account struct {
			Realm         string `json:"realm"`
			HomeAccountID string `json:"home_account_id"`
		}
		if err := json.Unmarshal([]byte(val), &account); err != nil {
			continue
		}
		accountHomeID := strings.ToLower(strings.TrimSpace(account.HomeAccountID))
		if accountHomeID == "" {
			accountHomeID = strings.ToLower(extractHomeAccountIDFromMSALKey(key))
		}
		tenantID := strings.TrimSpace(account.Realm)
		if tenantID == "" {
			tenantID = tenantIDFromHomeAccountID(accountHomeID)
		}
		if tenantID == "" {
			continue
		}
		if preferredHomeAccountID != "" && accountHomeID == preferredHomeAccountID {
			return tenantID
		}
		if fallbackTenantID == "" {
			fallbackTenantID = tenantID
		}
	}
	return fallbackTenantID
}

func extractHomeAccountIDFromMSALCredentialKeys(keys []string) string {
	for _, key := range keys {
		if homeAccountID := extractHomeAccountIDFromMSALKey(key); homeAccountID != "" {
			return homeAccountID
		}
	}
	return ""
}

func extractHomeAccountIDFromMSALKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	lowerKey := strings.ToLower(key)
	for _, marker := range []string{"-login.windows.net-", "-login.microsoftonline.com-"} {
		idx := strings.Index(lowerKey, marker)
		if idx > 0 {
			return key[:idx]
		}
	}
	return ""
}

func tenantIDFromHomeAccountID(homeAccountID string) string {
	if homeAccountID == "" {
		return ""
	}
	parts := strings.SplitN(homeAccountID, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func parseMSALExpires(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		return unix, true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Unix(), true
	}
	return 0, false
}
