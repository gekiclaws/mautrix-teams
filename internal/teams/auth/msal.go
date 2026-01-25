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

	if len(keys.AccessToken) > 0 {
		accessToken, expiresAt := selectMBIAccessToken(storage, keys.AccessToken)
		if accessToken == "" {
			targets := collectAccessTokenTargets(storage, keys.AccessToken, 6)
			if len(targets) == 0 {
				return nil, errors.New("MBI_SSL access token not found in localStorage")
			}
			return nil, errors.New("MBI_SSL access token not found in localStorage; observed targets: " + strings.Join(targets, ", "))
		}
		state.AccessToken = accessToken
		if expiresAt != 0 {
			state.ExpiresAtUnix = expiresAt
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

func collectAccessTokenTargets(storage map[string]string, keys []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	out := make([]string, 0, limit)
	seen := make(map[string]struct{})
	for _, key := range keys {
		if len(out) >= limit {
			break
		}
		raw, ok := storage[key]
		if !ok || raw == "" {
			continue
		}
		var entry msalTokenEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			continue
		}
		if entry.Target == "" {
			continue
		}
		if _, ok := seen[entry.Target]; ok {
			continue
		}
		seen[entry.Target] = struct{}{}
		out = append(out, entry.Target)
	}
	return out
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
	return "", errors.New("msal token keys entry not found")
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
