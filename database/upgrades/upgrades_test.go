package upgrades

import (
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
	if err := db.Upgrade(); err != nil {
		t.Fatalf("failed to apply upgrades: %v", err)
	}
	var name string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='teams_send_intent'").Scan(&name); err != nil {
		t.Fatalf("teams_send_intent table missing: %v", err)
	}
	var intentMXIDCol string
	if err := db.QueryRow("SELECT name FROM pragma_table_info('teams_send_intent') WHERE name='intent_mxid'").Scan(&intentMXIDCol); err != nil {
		t.Fatalf("teams_send_intent.intent_mxid column missing: %v", err)
	}
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='teams_message_map'").Scan(&name); err != nil {
		t.Fatalf("teams_message_map table missing: %v", err)
	}
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='reaction_map'").Scan(&name); err != nil {
		t.Fatalf("reaction_map table missing: %v", err)
	}
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name='reaction_map_matrix_event_idx'").Scan(&name); err != nil {
		t.Fatalf("reaction_map_matrix_event_idx missing: %v", err)
	}
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='teams_consumption_horizon'").Scan(&name); err != nil {
		t.Fatalf("teams_consumption_horizon table missing: %v", err)
	}
	var remoteIDCol string
	if err := db.QueryRow("SELECT name FROM pragma_table_info('user') WHERE name='remote_id'").Scan(&remoteIDCol); err != nil {
		t.Fatalf("user.remote_id column missing: %v", err)
	}
	var authTokenCol string
	if err := db.QueryRow("SELECT name FROM pragma_table_info('user') WHERE name='auth_token'").Scan(&authTokenCol); err != nil {
		t.Fatalf("user.auth_token column missing: %v", err)
	}
}
