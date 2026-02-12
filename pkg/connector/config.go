package connector

import (
	_ "embed"

	up "go.mau.fi/util/configupgrade"
)

//go:embed example-config.yaml
var ExampleConfig string

type TeamsConfig struct {
	// OAuth client ID used by the Teams web app. This must match the ID used in MSAL localStorage keys.
	// If unset, the connector uses the default client ID from internal/teams/auth.
	ClientID string `yaml:"client_id"`
}

func upgradeConfig(helper up.Helper) {
	helper.Copy(up.Str, "client_id")
}

func (t *TeamsConnector) GetConfig() (string, any, up.Upgrader) {
	return ExampleConfig, &t.Config, up.SimpleUpgrader(upgradeConfig)
}
