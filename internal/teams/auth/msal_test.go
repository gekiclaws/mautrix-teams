package auth

import "testing"

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
