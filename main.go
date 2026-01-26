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
	"context"
	_ "embed"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"go.mau.fi/util/configupgrade"
	"go.mau.fi/util/exsync"
	"golang.org/x/sync/semaphore"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/commands"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/config"
	"go.mau.fi/mautrix-teams/database"
	teamsbridge "go.mau.fi/mautrix-teams/internal/bridge"
	"go.mau.fi/mautrix-teams/teams"
	teamsauth "go.mau.fi/mautrix-teams/teams/auth"
	"go.mau.fi/mautrix-teams/teams/poll"
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

type DiscordBridge struct {
	bridge.Bridge

	Config *config.Config
	DB     *database.Database

	DMA          *DirectMediaAPI
	provisioning *ProvisioningAPI

	usersByMXID map[id.UserID]*User
	usersByID   map[string]*User
	usersLock   sync.Mutex

	managementRooms     map[id.RoomID]*User
	managementRoomsLock sync.Mutex

	portalsByMXID map[id.RoomID]*Portal
	portalsByID   map[database.PortalKey]*Portal
	portalsLock   sync.Mutex

	threadsByID                 map[string]*Thread
	threadsByRootMXID           map[id.EventID]*Thread
	threadsByCreationNoticeMXID map[id.EventID]*Thread
	threadsLock                 sync.Mutex

	guildsByMXID map[id.RoomID]*Guild
	guildsByID   map[string]*Guild
	guildsLock   sync.Mutex

	puppets             map[string]*Puppet
	puppetsByCustomMXID map[id.UserID]*Puppet
	puppetsLock         sync.Mutex

	attachmentTransfers         *exsync.Map[attachmentKey, *exsync.ReturnableOnce[*database.File]]
	parallelAttachmentSemaphore *semaphore.Weighted

	TeamsThreadStore    *teamsbridge.TeamsThreadStore
	TeamsConsumerSender *teamsbridge.TeamsConsumerSender
}

func (br *DiscordBridge) GetExampleConfig() string {
	return ExampleConfig
}

func (br *DiscordBridge) GetConfigPtr() interface{} {
	br.Config = &config.Config{
		BaseConfig: &br.Bridge.Config,
	}
	br.Config.BaseConfig.Bridge = &br.Config.Bridge
	return br.Config
}

func (br *DiscordBridge) Init() {
	br.CommandProcessor = commands.NewProcessor(&br.Bridge)
	br.RegisterCommands()
	br.EventProcessor.On(event.StateTombstone, br.HandleTombstone)

	matrixHTMLParser.PillConverter = br.pillConverter

	br.DB = database.New(br.Bridge.DB, br.Log.Sub("Database"))
	discordLog = br.ZLog.With().Str("component", "discordgo").Logger()
}

func (br *DiscordBridge) Start() {
	if br.Config.Bridge.Provisioning.SharedSecret != "disable" {
		br.provisioning = newProvisioningAPI(br)
	}
	if br.Config.Bridge.PublicAddress != "" {
		br.AS.Router.HandleFunc("/mautrix-discord/avatar/{server}/{mediaID}/{checksum}", br.serveMediaProxy).Methods(http.MethodGet)
	}
	br.DMA = newDirectMediaAPI(br)
	br.WaitWebsocketConnected()
	go br.startUsers()
	br.startTeamsConsumerRoomSync()
	br.startTeamsConsumerSender()
}

func (br *DiscordBridge) Stop() {
	for _, user := range br.usersByMXID {
		if user.Session == nil {
			continue
		}

		br.Log.Debugln("Disconnecting", user.MXID)
		user.Session.Close()
	}
}

func (br *DiscordBridge) GetIPortal(mxid id.RoomID) bridge.Portal {
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

func (br *DiscordBridge) GetIUser(mxid id.UserID, create bool) bridge.User {
	p := br.GetUserByMXID(mxid)
	if p == nil {
		return nil
	}
	return p
}

func (br *DiscordBridge) IsGhost(mxid id.UserID) bool {
	_, isGhost := br.ParsePuppetMXID(mxid)
	return isGhost
}

func (br *DiscordBridge) GetIGhost(mxid id.UserID) bridge.Ghost {
	p := br.GetPuppetByMXID(mxid)
	if p == nil {
		return nil
	}
	return p
}

func (br *DiscordBridge) CreatePrivatePortal(id id.RoomID, user bridge.User, ghost bridge.Ghost) {
	//TODO implement
}

func main() {
	if handled, exitCode := runDevSendIfRequested(os.Args[1:]); handled {
		os.Exit(exitCode)
	}
	runTeamsAuthTestIfRequested(os.Args)
	runTeamsPollTestIfRequested(os.Args)
	br := &DiscordBridge{
		usersByMXID: make(map[id.UserID]*User),
		usersByID:   make(map[string]*User),

		managementRooms: make(map[id.RoomID]*User),

		portalsByMXID: make(map[id.RoomID]*Portal),
		portalsByID:   make(map[database.PortalKey]*Portal),

		threadsByID:                 make(map[string]*Thread),
		threadsByRootMXID:           make(map[id.EventID]*Thread),
		threadsByCreationNoticeMXID: make(map[id.EventID]*Thread),

		guildsByID:   make(map[string]*Guild),
		guildsByMXID: make(map[id.RoomID]*Guild),

		puppets:             make(map[string]*Puppet),
		puppetsByCustomMXID: make(map[id.UserID]*Puppet),

		attachmentTransfers:         exsync.NewMap[attachmentKey, *exsync.ReturnableOnce[*database.File]](),
		parallelAttachmentSemaphore: semaphore.NewWeighted(3),
	}
	br.Bridge = bridge.Bridge{
		Name:              "mautrix-teams",
		URL:               "https://github.com/mautrix/teams",
		Description:       "A Matrix-Discord puppeting bridge.",
		Version:           "0.7.5",
		ProtocolName:      "Discord",
		BeeperServiceName: "discordgo",
		BeeperNetworkName: "discord",

		CryptoPickleKey: "maunium.net/go/mautrix-whatsapp",

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

func runTeamsAuthTestIfRequested(args []string) {
	if !shouldRunTeamsAuthTest(args) && !envFlagEnabled("GO_TEAMS_AUTH_TEST") {
		return
	}
	log := zerolog.New(os.Stdout).With().Timestamp().Str("component", "teams-auth-test").Logger()
	if err := teamsauth.RunGraphAuthTest(context.Background(), log); err != nil {
		log.Error().Err(err).Msg("Graph auth test failed")
		os.Exit(1)
	}
	os.Exit(0)
}

func runTeamsPollTestIfRequested(args []string) {
	if !shouldRunTeamsPollTest(args) && !envFlagEnabled("GO_TEAMS_POLL_TEST") {
		return
	}
	log := zerolog.New(os.Stdout).With().Timestamp().Str("component", "teams-poll-test").Logger()

	creds, err := teams.LoadGraphCredentialsFromEnv(".env")
	if err != nil {
		log.Error().Err(err).Msg("Failed to load Graph credentials")
		os.Exit(1)
	}
	userID := os.Getenv(teams.EnvGraphUserID)
	if userID == "" {
		log.Error().Msg("Missing required env var: " + teams.EnvGraphUserID)
		os.Exit(1)
	}

	client, err := teams.NewGraphClient(context.Background(), creds)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create Graph client")
		os.Exit(1)
	}

	poller := &poll.Poller{
		GraphClient: client,
		UserID:      userID,
		Cursor:      make(map[string]string),
	}
	if err := poller.RunOnce(context.Background(), log); err != nil {
		log.Error().Err(err).Msg("Teams poll test failed")
		os.Exit(1)
	}
	os.Exit(0)
}

func envFlagEnabled(key string) bool {
	value, ok := os.LookupEnv(key)
	if !ok {
		return false
	}
	value = strings.TrimSpace(strings.ToLower(value))
	return value != "" && value != "0" && value != "false"
}

func shouldRunTeamsAuthTest(args []string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, "--teams-auth-test") {
			return true
		}
	}
	return false
}

func shouldRunTeamsPollTest(args []string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, "--teams-poll-test") {
			return true
		}
	}
	return false
}
