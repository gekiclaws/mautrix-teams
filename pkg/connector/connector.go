package connector

import (
	"context"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"

	"go.mau.fi/mautrix-teams/pkg/teamsdb"
)

type TeamsConnector struct {
	Bridge *bridgev2.Bridge
	Config TeamsConfig
	DB     *teamsdb.Database
}

var _ bridgev2.NetworkConnector = (*TeamsConnector)(nil)

func (t *TeamsConnector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:      "Microsoft Teams",
		NetworkURL:       "https://www.microsoft.com/microsoft-teams",
		NetworkIcon:      "",
		NetworkID:        "msteams",
		BeeperBridgeType: "msteams",
		DefaultPort:      29340,
	}
}

func (t *TeamsConnector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{
		UserLogin: func() any { return &TeamsUserLoginMetadata{} },
	}
}

func (t *TeamsConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return &bridgev2.NetworkGeneralCapabilities{
		Provisioning: bridgev2.ProvisioningCapabilities{
			// Login flows are supported by default if GetLoginFlows/CreateLogin are implemented.
			// Other endpoints will return "not supported" unless explicitly implemented.
		},
	}
}

func (t *TeamsConnector) Init(br *bridgev2.Bridge) {
	t.Bridge = br
}

func (t *TeamsConnector) Start(ctx context.Context) error {
	// Initialize Teams-specific DB section.
	if t.Bridge == nil || t.Bridge.DB == nil || t.Bridge.DB.Database == nil {
		return nil
	}
	t.DB = teamsdb.New(t.Bridge.ID, t.Bridge.DB.Database, t.Bridge.Log.With().Str("db_section", "teams").Logger())
	if err := t.DB.Upgrade(ctx); err != nil {
		return bridgev2.DBUpgradeError{Err: err, Section: "teams"}
	}
	return nil
}

func (t *TeamsConnector) GetBridgeInfoVersion() (info, capabilities int) {
	return 0, 0
}

func (t *TeamsConnector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	// Ensure metadata is the expected concrete type (bridgev2 unmarshals into the type returned by GetDBMetaTypes).
	meta, _ := login.Metadata.(*TeamsUserLoginMetadata)
	login.Client = &TeamsClient{
		Main:  t,
		Login: login,
		Meta:  meta,
	}
	return nil
}

func (t *TeamsConnector) GetLoginFlows() []bridgev2.LoginFlow {
	return []bridgev2.LoginFlow{loginFlowWebviewLocalStorage, loginFlowMSALLocalStorage}
}

func (t *TeamsConnector) CreateLogin(ctx context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	switch flowID {
	case FlowIDWebviewLocalStorage:
		return &WebviewLocalStorageLogin{
			Main: t,
			User: user,
		}, nil
	case FlowIDMSALLocalStorage:
		return &MSALLocalStorageLogin{
			Main: t,
			User: user,
		}, nil
	default:
		return nil, bridgev2.ErrInvalidLoginFlowID
	}
}
