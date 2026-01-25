package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCookieStoreRoundTrip(t *testing.T) {
	store, err := NewCookieStore()
	if err != nil {
		t.Fatalf("NewCookieStore failed: %v", err)
	}

	u, _ := url.Parse("https://login.live.com/")
	store.TrackURL(u)
	cookie := &http.Cookie{
		Name:     "session",
		Value:    "abc123",
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		Expires:  time.Now().Add(2 * time.Hour).UTC(),
	}
	store.Jar.SetCookies(u, []*http.Cookie{cookie})

	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.json")
	if err := store.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat failed: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected permissions: %v", info.Mode().Perm())
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var payload storedCookieFile
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}
	if len(payload.Origins) == 0 || len(payload.Origins[0].Cookies) == 0 {
		t.Fatalf("expected serialized cookies")
	}
	serialized := payload.Origins[0].Cookies[0]
	if serialized.Path != "/" {
		t.Fatalf("unexpected serialized path: %s", serialized.Path)
	}
	if !serialized.Secure || !serialized.HTTPOnly {
		t.Fatalf("unexpected serialized flags: secure=%v httpOnly=%v", serialized.Secure, serialized.HTTPOnly)
	}
	if serialized.ExpiresUnix == 0 {
		t.Fatalf("expected serialized expiry")
	}

	loaded, err := LoadCookieStore(path)
	if err != nil {
		t.Fatalf("LoadCookieStore failed: %v", err)
	}

	cookies := loaded.Jar.Cookies(u)
	if len(cookies) == 0 {
		t.Fatalf("expected cookies, got none")
	}
	found := false
	for _, got := range cookies {
		if got.Name == "session" {
			found = true
			if got.Value != "abc123" {
				t.Fatalf("unexpected value: %s", got.Value)
			}
		}
	}
	if !found {
		t.Fatalf("expected session cookie not found")
	}
}
