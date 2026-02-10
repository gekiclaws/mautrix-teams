// mautrix-teams - A Matrix-Teams puppeting bridge.
// Copyright (C) 2022 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	_ "embed"
	"sync"

	"go.mau.fi/util/configupgrade"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/commands"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/config"
	"go.mau.fi/mautrix-teams/database"
	teamsbridge "go.mau.fi/mautrix-teams/internal/bridge"
)

// Information to find out exactly which commit the bridge was built from.
// These are filled at build time with the -X linker flag.
var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

//go:embed example-config.yaml
var ExampleConfig string

type TeamsBridge struct {
	bridge.Bridge

	Config *config.Config
	DB     *database.Database

	usersByMXID map[id.UserID]*User
	usersByID   map[string]*User
	usersLock   sync.Mutex

	managementRooms     map[id.RoomID]*User
	managementRoomsLock sync.Mutex

	TeamsThreadStore     *teamsbridge.TeamsThreadStore
	TeamsConsumerSender  *teamsbridge.TeamsConsumerSender
	TeamsConsumerReactor *teamsbridge.TeamsConsumerReactor
	TeamsConsumerTyper   *teamsbridge.TeamsConsumerTyper
	TeamsUnreadCycles    *teamsbridge.UnreadCycleTracker
	TeamsConsumerReceipt *teamsbridge.TeamsConsumerReceiptSender
	teamsAdminInviteWarn sync.Once
}

func (br *TeamsBridge) GetExampleConfig() string {
	return ExampleConfig
}

func (br *TeamsBridge) GetConfigPtr() interface{} {
	br.Config = &config.Config{
		BaseConfig: &br.Bridge.Config,
	}
	br.Config.BaseConfig.Bridge = &br.Config.Bridge
	return br.Config
}

func (br *TeamsBridge) Init() {
	br.CommandProcessor = commands.NewProcessor(&br.Bridge)
	br.EventProcessor.On(event.StateTombstone, br.HandleTombstone)
	br.EventProcessor.On(event.EventReaction, br.HandleTeamsConsumerReaction)

	br.DB = database.New(br.Bridge.DB, br.Log.Sub("Database"))
}

func (br *TeamsBridge) Start() {
	br.WaitWebsocketConnected()
	br.startTeamsConsumerRoomSync()
	br.startTeamsConsumerMessageSync()
	br.startTeamsConsumerSender()
}

func (br *TeamsBridge) Stop() {
}

func (br *TeamsBridge) GetIPortal(mxid id.RoomID) bridge.Portal {
	p := br.GetPortalByMXID(mxid)
	if p == nil {
		if br.TeamsConsumerSender == nil || br.TeamsThreadStore == nil {
			return nil
		}
		if _, ok := br.TeamsThreadStore.GetThreadID(mxid); !ok {
			return nil
		}
		return &TeamsConsumerPortal{bridge: br, roomID: mxid}
	}
	return p
}

func (br *TeamsBridge) GetIUser(mxid id.UserID, create bool) bridge.User {
	p := br.GetUserByMXID(mxid)
	if p == nil {
		return nil
	}
	return p
}

func (br *TeamsBridge) IsGhost(mxid id.UserID) bool {
	_, isGhost := br.ParsePuppetMXID(mxid)
	return isGhost
}

func (br *TeamsBridge) GetIGhost(mxid id.UserID) bridge.Ghost {
	p := br.GetPuppetByMXID(mxid)
	if p == nil {
		return nil
	}
	return p
}

func (br *TeamsBridge) CreatePrivatePortal(id id.RoomID, user bridge.User, ghost bridge.Ghost) {
	//TODO implement
}

func main() {
	br := &TeamsBridge{
		usersByMXID: make(map[id.UserID]*User),
		usersByID:   make(map[string]*User),

		managementRooms: make(map[id.RoomID]*User),
	}
	br.Bridge = bridge.Bridge{
		Name:              "mautrix-teams",
		URL:               "https://github.com/mautrix/teams",
		Description:       "A Matrix-Teams puppeting bridge.",
		Version:           "0.7.5",
		ProtocolName:      "Teams",
		BeeperServiceName: "teams",
		BeeperNetworkName: "msteams",

		CryptoPickleKey: "maunium.net/go/mautrix-teams",

		ConfigUpgrader: &configupgrade.StructUpgrader{
			SimpleUpgrader: configupgrade.SimpleUpgrader(config.DoUpgrade),
			Blocks:         config.SpacedBlocks,
			Base:           ExampleConfig,
		},

		Child: br,
	}
	br.InitVersion(Tag, Commit, BuildTime)

	br.Main()
}
