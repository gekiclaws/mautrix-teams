package auth

import (
	"encoding/json"
	"testing"
)

func TestExtractTokensFromMSALLocalStorage(t *testing.T) {
	storage := map[string]string{
		"msal.token.keys." + defaultClientID: `{"refreshToken":["rt"],"idToken":["idt"],"accessToken":["graph"]}`,
		"rt":                                 `{"secret":"refresh-secret","expiresOn":"1700000000"}`,
		"idt":                                `{"secret":"id-secret"}`,
		"graph":                              `{"secret":"graph-secret","expiresOn":"1700000200","target":"https://graph.microsoft.com/Files.ReadWrite User.Read"}`,
	}
	payload, err := json.Marshal(storage)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	state, err := ExtractTokensFromMSALLocalStorage(string(payload), defaultClientID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.RefreshToken != "refresh-secret" {
		t.Fatalf("unexpected refresh token: %s", state.RefreshToken)
	}
	if state.ExpiresAtUnix != 1700000000 {
		t.Fatalf("unexpected expires: %d", state.ExpiresAtUnix)
	}
	if state.IDToken != "id-secret" {
		t.Fatalf("unexpected id token: %s", state.IDToken)
	}
	if state.GraphAccessToken != "graph-secret" {
		t.Fatalf("unexpected graph token: %s", state.GraphAccessToken)
	}
	if state.GraphExpiresAt != 1700000200 {
		t.Fatalf("unexpected graph expiry: %d", state.GraphExpiresAt)
	}
}

func TestExtractTokensFromMSALLocalStorage_MSALVariantTokenKeys(t *testing.T) {
	storage := map[string]string{
		"msal." + defaultClientID + ".token.keys.tenant": `{"refreshToken":["rt"],"idToken":["idt"]}`,
		"rt":  `{"secret":"refresh-secret","expiresOn":"1700000000"}`,
		"idt": `{"secret":"id-secret"}`,
	}
	payload, err := json.Marshal(storage)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	state, err := ExtractTokensFromMSALLocalStorage(string(payload), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.RefreshToken != "refresh-secret" {
		t.Fatalf("unexpected refresh token: %s", state.RefreshToken)
	}
	if state.IDToken != "id-secret" {
		t.Fatalf("unexpected id token: %s", state.IDToken)
	}
}

func TestExtractTokensFromMSALLocalStorage_MissingMBIAccessTokenIsNonFatal(t *testing.T) {
	storage := map[string]string{
		"msal.token.keys." + defaultClientID: `{"refreshToken":["rt"],"accessToken":["at"],"idToken":["idt"]}`,
		"rt":                                 `{"secret":"refresh-secret","expiresOn":"1700000000"}`,
		"at":                                 `{"secret":"token-openid","expiresOn":"1700000100","target":"openid profile"}`,
		"idt":                                `{"secret":"id-secret"}`,
	}
	payload, err := json.Marshal(storage)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	state, err := ExtractTokensFromMSALLocalStorage(string(payload), defaultClientID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.RefreshToken != "refresh-secret" {
		t.Fatalf("unexpected refresh token: %s", state.RefreshToken)
	}
	if state.AccessToken != "" {
		t.Fatalf("expected empty access token, got %s", state.AccessToken)
	}
	if state.GraphAccessToken != "" {
		t.Fatalf("expected empty graph token, got %s", state.GraphAccessToken)
	}
}
