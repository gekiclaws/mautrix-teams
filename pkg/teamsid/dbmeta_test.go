package teamsid

import (
	"errors"
	"testing"
	"time"
)

func TestGraphTokenValid(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	meta := &UserLoginMetadata{
		GraphAccessToken: "graph-token",
		GraphExpiresAt:   now.Add(90 * time.Second).Unix(),
	}
	if !meta.GraphTokenValid(now) {
		t.Fatalf("expected token to be valid")
	}

	meta.GraphExpiresAt = now.Add(50 * time.Second).Unix()
	if meta.GraphTokenValid(now) {
		t.Fatalf("expected near-expiry token to be invalid with skew")
	}

	meta.GraphExpiresAt = now.Add(-10 * time.Second).Unix()
	if meta.GraphTokenValid(now) {
		t.Fatalf("expected expired token to be invalid")
	}

	meta.GraphAccessToken = ""
	if meta.GraphTokenValid(now) {
		t.Fatalf("expected empty token to be invalid")
	}
}

func TestGetGraphAccessToken(t *testing.T) {
	meta := &UserLoginMetadata{}
	if _, err := meta.GetGraphAccessToken(); !errors.Is(err, ErrGraphAccessTokenMissing) {
		t.Fatalf("expected missing token error, got %v", err)
	}

	meta.GraphAccessToken = "graph-token"
	meta.GraphExpiresAt = time.Now().UTC().Add(-time.Minute).Unix()
	if _, err := meta.GetGraphAccessToken(); !errors.Is(err, ErrGraphAccessTokenExpired) {
		t.Fatalf("expected expired token error, got %v", err)
	}

	meta.GraphExpiresAt = time.Now().UTC().Add(2 * time.Minute).Unix()
	got, err := meta.GetGraphAccessToken()
	if err != nil {
		t.Fatalf("expected valid token, got error %v", err)
	}
	if got != "graph-token" {
		t.Fatalf("unexpected token: %s", got)
	}
}
