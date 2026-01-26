package upgrades

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
)

func TestUpgradesIncludeTeamsSendIntent(t *testing.T) {
	db, err := dbutil.NewWithDialect(":memory:", "sqlite3")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	db.UpgradeTable = Table
	db.Log = dbutil.ZeroLogger(zerolog.Nop())
	if err := db.Upgrade(context.Background()); err != nil {
		t.Fatalf("failed to apply upgrades: %v", err)
	}
	var name string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='teams_send_intent'").Scan(&name); err != nil {
		t.Fatalf("teams_send_intent table missing: %v", err)
	}
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='teams_message_map'").Scan(&name); err != nil {
		t.Fatalf("teams_message_map table missing: %v", err)
	}
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='teams_reaction_map'").Scan(&name); err != nil {
		t.Fatalf("teams_reaction_map table missing: %v", err)
	}
}
