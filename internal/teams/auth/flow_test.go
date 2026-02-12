package auth

import (
	"encoding/json"
	"testing"
)

func TestExtractTokensFromMSALLocalStorage(t *testing.T) {
	storage := map[string]string{
		"msal.token.keys." + defaultClientID: `{"refreshToken":["rt"],"idToken":["idt"]}`,
		"rt":                                 `{"secret":"refresh-secret","expiresOn":"1700000000"}`,
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
	if state.ExpiresAtUnix != 1700000000 {
		t.Fatalf("unexpected expires: %d", state.ExpiresAtUnix)
	}
	if state.IDToken != "id-secret" {
		t.Fatalf("unexpected id token: %s", state.IDToken)
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
