package connector

import (
	"context"
	"testing"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-teams/pkg/teamsid"
)

func TestGetDBMetaTypes_UserLogin(t *testing.T) {
	var c TeamsConnector
	mt := c.GetDBMetaTypes()
	v := mt.UserLogin()
	if _, ok := v.(*teamsid.UserLoginMetadata); !ok {
		t.Fatalf("expected UserLogin meta type *teamsid.UserLoginMetadata, got %T", v)
	}
}

func TestLoadUserLogin_AttachesClientAndMetadata(t *testing.T) {
	var c TeamsConnector
	login := &bridgev2.UserLogin{
		UserLogin: &database.UserLogin{
			ID:       networkid.UserLoginID("test"),
			Metadata: &teamsid.UserLoginMetadata{},
		},
	}
	err := c.LoadUserLogin(context.TODO(), login)
	if err != nil {
		t.Fatalf("LoadUserLogin failed: %v", err)
	}
	tc, ok := login.Client.(*TeamsClient)
	if !ok || tc == nil {
		t.Fatalf("expected login.Client to be *TeamsClient, got %T", login.Client)
	}
	if tc.Meta == nil {
		t.Fatalf("expected TeamsClient.Meta to be non-nil")
	}
}
