package connector

import "maunium.net/go/mautrix/bridgev2"

var teamsGeneralCaps = &bridgev2.NetworkGeneralCapabilities{
	Provisioning: bridgev2.ProvisioningCapabilities{
		// Login flows are supported via GetLoginFlows/CreateLogin.
	},
}

func (t *TeamsConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return teamsGeneralCaps
}

func (t *TeamsConnector) GetBridgeInfoVersion() (info, capabilities int) {
	return 1, 9
}
