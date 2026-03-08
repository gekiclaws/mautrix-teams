package auth

import (
	"encoding/json"
	"testing"
)

func TestSelectMBIAccessToken(t *testing.T) {
	storage := map[string]string{
		"access1": `{"secret":"token-openid","expiresOn":"1700000000","target":"openid profile"}`,
		"access2": `{"secret":"token-mbi","expiresOn":"1700000100","target":"service::api.fl.spaces.skype.com::MBI_SSL"}`,
		"access3": `{"secret":"token-mbi-2","expiresOn":"1700000200","target":"service::api.fl.spaces.skype.com::MBI_SSL"}`,
	}
	keys := []string{"access1", "access2", "access3"}

	token, expiry := selectMBIAccessToken(storage, keys)
	if token != "token-mbi-2" {
		t.Fatalf("unexpected token: %s", token)
	}
	if expiry != 1700000200 {
		t.Fatalf("unexpected expiry: %d", expiry)
	}
}

func TestSelectGraphAccessToken(t *testing.T) {
	storage := map[string]string{
		"access1": `{"secret":"token-openid","expiresOn":"1700000000","target":"openid profile"}`,
		"access2": `{"secret":"token-graph-1","expiresOn":"1700000100","target":"https://graph.microsoft.com/Files.ReadWrite User.Read"}`,
		"access3": `{"secret":"token-graph-2","expiresOn":"1700000200","target":"https://graph.microsoft.com/User.Read Files.ReadWrite"}`,
	}
	keys := []string{"access1", "access2", "access3"}

	token, expiry := selectGraphAccessToken(storage, keys)
	if token != "token-graph-2" {
		t.Fatalf("unexpected token: %s", token)
	}
	if expiry != 1700000200 {
		t.Fatalf("unexpected expiry: %d", expiry)
	}
}

func TestSelectGraphAccessTokenMissing(t *testing.T) {
	storage := map[string]string{
		"access1": `{"secret":"token-openid","expiresOn":"1700000000","target":"openid profile"}`,
	}
	keys := []string{"access1"}

	token, expiry := selectGraphAccessToken(storage, keys)
	if token != "" || expiry != 0 {
		t.Fatalf("expected no token, got %q with expiry %d", token, expiry)
	}
}

func TestSelectMBIAccessTokenMissing(t *testing.T) {
	storage := map[string]string{
		"access1": `{"secret":"token-openid","expiresOn":"1700000000","target":"openid profile"}`,
	}
	keys := []string{"access1"}

	token, expiry := selectMBIAccessToken(storage, keys)
	if token != "" || expiry != 0 {
		t.Fatalf("expected no token, got %q with expiry %d", token, expiry)
	}
}

func TestExtractTenantIDFromMSALStorage_PrefersMatchingHomeAccount(t *testing.T) {
	storage := map[string]string{
		"uid1.tid-one-login.windows.net-account-client--": `{"realm":"tid-one","home_account_id":"uid1.tid-one"}`,
		"uid2.tid-two-login.windows.net-account-client--": `{"realm":"tid-two","home_account_id":"uid2.tid-two"}`,
	}

	tenantID := extractTenantIDFromMSALStorage(storage, "uid2.tid-two")
	if tenantID != "tid-two" {
		t.Fatalf("unexpected tenant id: %s", tenantID)
	}
}

func TestExtractTokensFromMSALLocalStorage_BindsTenantToSelectedTokenAccount(t *testing.T) {
	const clientID = "test-client-id"
	refreshKey := "uid2.tid-two-login.windows.net-refreshtoken-test-client-id--"
	storage := map[string]string{
		"msal.token.keys." + clientID: `{"refreshToken":["` + refreshKey + `"],"accessToken":[]}`,
		refreshKey:                    `{"secret":"refresh-token","expiresOn":"1700000000"}`,
		"uid1.tid-one-login.windows.net-account-test-client-id--": `{"realm":"tid-one","home_account_id":"uid1.tid-one"}`,
		"uid2.tid-two-login.windows.net-account-test-client-id--": `{"realm":"tid-two","home_account_id":"uid2.tid-two"}`,
	}
	payload, err := json.Marshal(storage)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	state, err := ExtractTokensFromMSALLocalStorage(string(payload), clientID)
	if err != nil {
		t.Fatalf("unexpected extraction error: %v", err)
	}
	if state.TenantID != "tid-two" {
		t.Fatalf("unexpected tenant id: %s", state.TenantID)
	}
}
