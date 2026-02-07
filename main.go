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
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/configupgrade"
	"go.mau.fi/util/exsync"
	"golang.org/x/sync/semaphore"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/bridge/commands"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/config"
	"go.mau.fi/mautrix-teams/database"
	teamsbridge "go.mau.fi/mautrix-teams/internal/bridge"
	auth "go.mau.fi/mautrix-teams/internal/teams/auth"
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

type TeamsBridge struct {
	bridge.Bridge

	Config *config.Config
	DB     *database.Database

	DMA *DirectMediaAPI

	teamsAuthLock  sync.RWMutex
	teamsAuthState *auth.AuthState
	teamsRunLock   sync.Mutex
	teamsRunning   bool

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

	TeamsThreadStore     *teamsbridge.TeamsThreadStore
	TeamsConsumerSender  *teamsbridge.TeamsConsumerSender
	TeamsConsumerReactor *teamsbridge.TeamsConsumerReactor
	TeamsConsumerTyper   *teamsbridge.TeamsConsumerTyper
	TeamsUnreadCycles    *teamsbridge.UnreadCycleTracker
	TeamsConsumerReceipt *teamsbridge.TeamsConsumerReceiptSender
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
	br.ZLog.Info().
		Str("context", "implicit-dm-registration").
		Msg("IMPLICIT DM REGISTRATION CODE PATH EXECUTED")
	br.CommandProcessor = commands.NewProcessor(&br.Bridge)
	br.ZLog.Info().Msg("commands: RegisterCommands() starting")
	br.RegisterCommands()
	br.ZLog.Info().Msg("commands: RegisterCommands() done")
	br.EventProcessor.On(event.StateTombstone, br.HandleTombstone)
	br.EventProcessor.On(event.StateMember, br.handleBotInviteManagementRoomClaim)
	for _, subtype := range []event.Type{
		event.EventMessage,
		event.EventEncrypted,
		event.EventSticker,
		event.EventReaction,
		event.EventRedaction,
		event.StateMember,
		event.StateRoomName,
		event.StateRoomAvatar,
		event.StateTopic,
		event.StateEncryption,
		event.EphemeralEventReceipt,
		event.EphemeralEventTyping,
		event.StateTombstone,
	} {
		br.EventProcessor.PrependHandler(subtype, br.handleImplicitDMManagementRoomClaim)
		br.ZLog.Info().
			Str("processor_ptr", fmt.Sprintf("%p", br.EventProcessor)).
			Str("event_type", subtype.String()).
			Msg("REGISTER implicit-dm pre-handler")
	}

	matrixHTMLParser.PillConverter = br.pillConverter

	br.DB = database.New(br.Bridge.DB, br.Log.Sub("Database"))
	discordLog = br.ZLog.With().Str("component", "discordgo").Logger()
}

func (br *TeamsBridge) Start() {
	fmt.Println("PROBE: registering implicit DM pre-handler (Start)")
	br.EventProcessor.PrependHandler(
		event.EventMessage,
		br.handleImplicitDMManagementRoomClaim,
	)

	if br.Config.Bridge.PublicAddress != "" {
		br.AS.Router.HandleFunc("/mautrix-discord/avatar/{server}/{mediaID}/{checksum}", br.serveMediaProxy).Methods(http.MethodGet)
	}
	br.DMA = newDirectMediaAPI(br)
	br.WaitWebsocketConnected()
	go br.startUsers()

	state, err := br.LoadTeamsAuth(time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, ErrTeamsAuthExpiredToken):
			br.ZLog.Warn().Err(err).Msg("Teams auth expired; bridge is idle. Re-run teams-login and then `!login`")
		case errors.Is(err, ErrTeamsAuthMissingFile), errors.Is(err, ErrTeamsAuthMissingState), errors.Is(err, ErrTeamsAuthMissingToken):
			br.ZLog.Warn().Err(err).Msg("Teams auth missing; bridge is idle. Run teams-login and then `!login`")
		default:
			br.ZLog.Warn().Err(err).Msg("Teams auth unavailable; bridge is idle. Run `!login` after completing teams-login")
		}
		return
	}

	br.setTeamsAuthState(state)
	br.ZLog.Info().Msg("Teams auth OK")
	if err := br.StartTeamsConsumers(context.Background(), state); err != nil {
		br.ZLog.Warn().Err(err).Msg("Teams auth present but failed to start consumers")
	}
}

func (br *TeamsBridge) Stop() {
	for _, user := range br.usersByMXID {
		if user.Client == nil {
			continue
		}

		br.Log.Debugln("Disconnecting", user.MXID)
		user.Client.Close()
	}
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

func (br *TeamsBridge) isAlreadyJoinedJoinError(err error) bool {
	if err == nil {
		return false
	}
	checkHTTPError := func(httpErr mautrix.HTTPError) bool {
		if httpErr.RespError == nil {
			return false
		}
		errMsg := strings.ToLower(httpErr.RespError.Err)
		return strings.Contains(errMsg, "already in the room") ||
			strings.Contains(errMsg, "already joined to room") ||
			strings.Contains(errMsg, "already joined to this room")
	}
	switch typedErr := err.(type) {
	case mautrix.HTTPError:
		if checkHTTPError(typedErr) {
			return true
		}
	case *mautrix.HTTPError:
		if typedErr != nil && checkHTTPError(*typedErr) {
			return true
		}
	}
	var httpErr mautrix.HTTPError
	if errors.As(err, &httpErr) && checkHTTPError(httpErr) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "already joined")
}

func (br *TeamsBridge) isGhostForManagementRoomClaim(userID id.UserID) (isGhost bool) {
	if br == nil || userID == "" {
		return false
	}
	defer func() {
		if recover() != nil {
			isGhost = false
		}
	}()
	return br.IsGhost(userID)
}

func (br *TeamsBridge) ensureBotJoinedManagementRoom(bot *appservice.IntentAPI, roomID id.RoomID, user *User, trigger string) bool {
	if br == nil || bot == nil || user == nil || roomID == "" {
		return false
	}

	if _, err := bot.JoinRoomByID(roomID); err != nil {
		if br.isAlreadyJoinedJoinError(err) {
			members, membersErr := bot.JoinedMembers(roomID)
			if membersErr != nil {
				br.ZLog.Warn().
					Err(membersErr).
					Str("trigger", trigger).
					Str("room_id", roomID.String()).
					Str("user", user.MXID.String()).
					Msg("Failed to verify bot membership after already-joined join response")
				return false
			}
			_, hasBot := members.Joined[bot.UserID]
			return hasBot
		}
		br.ZLog.Warn().
			Err(err).
			Str("trigger", trigger).
			Str("room_id", roomID.String()).
			Str("user", user.MXID.String()).
			Msg("Failed to join management room")
		return false
	}
	return true
}

func (br *TeamsBridge) claimManagementRoomAfterJoin(bot *appservice.IntentAPI, roomID id.RoomID, user *User, trigger string) {
	if br == nil || bot == nil || user == nil || roomID == "" {
		return
	}

	br.ZLog.Info().
		Str("trigger", trigger).
		Str("room_id", roomID.String()).
		Str("user", user.MXID.String()).
		Msg("Claiming management room")

	user.SetManagementRoom(roomID)

	_, err := bot.SendMessageEvent(roomID, event.EventMessage, &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    "Teams bridge ready. Use !login to activate.",
	})
	if err != nil {
		br.ZLog.Warn().
			Err(err).
			Str("trigger", trigger).
			Str("room_id", roomID.String()).
			Str("user", user.MXID.String()).
			Msg("Failed to send management room readiness message")
	}
}

func (br *TeamsBridge) claimManagementRoom(bot *appservice.IntentAPI, roomID id.RoomID, user *User, trigger string) {
	if !br.ensureBotJoinedManagementRoom(bot, roomID, user, trigger) {
		return
	}
	br.claimManagementRoomAfterJoin(bot, roomID, user, trigger)
}

func (br *TeamsBridge) isImplicitDMClaimCandidate(members *mautrix.RespJoinedMembers, sender id.UserID) bool {
	if br == nil || br.Bot == nil || members == nil || sender == "" {
		return false
	}
	if len(members.Joined) != 2 {
		return false
	}
	_, hasSender := members.Joined[sender]
	_, hasBot := members.Joined[br.Bot.UserID]
	return hasSender && hasBot
}

func (br *TeamsBridge) CreatePrivatePortal(roomID id.RoomID, user bridge.User, _ bridge.Ghost) {
	typedUser, ok := user.(*User)
	if !ok {
		return
	}
	br.claimManagementRoom(br.Bot, roomID, typedUser, "invite")
}

var registerImplicitDMOnce sync.Once

func (br *TeamsBridge) handleBotInviteManagementRoomClaim(evt *event.Event) {
	registerImplicitDMOnce.Do(func() {
		fmt.Println("PROBE: registering implicit DM pre-handler (lazy)")
		br.EventProcessor.PrependHandler(
			event.EventMessage,
			br.handleImplicitDMManagementRoomClaim,
		)
	})

	if br == nil || evt == nil || br.Bot == nil {
		return
	}

	memberContent := evt.Content.AsMember()
	if memberContent.Membership != event.MembershipInvite || !memberContent.IsDirect {
		return
	}
	if id.UserID(evt.GetStateKey()) != br.Bot.UserID {
		return
	}

	user := br.getUserForManagementRoomClaim(evt.Sender)
	if user == nil {
		return
	}
	if user.GetPermissionLevel() < bridgeconfig.PermissionLevelUser {
		return
	}

	br.claimManagementRoom(br.Bot, evt.RoomID, user, "invite")
}

func (br *TeamsBridge) handleImplicitDMManagementRoomClaim(evt *event.Event) {
	if br == nil || evt == nil || br.Bot == nil {
		return
	}
	br.ZLog.Info().
		Str("processor_ptr", fmt.Sprintf("%p", br.EventProcessor)).
		Str("event_type", evt.Type.String()).
		Str("room", evt.RoomID.String()).
		Str("sender", evt.Sender.String()).
		Msg("HANDLE message entered")
	if evt.Type != event.EventMessage || evt.Sender == br.Bot.UserID || br.isGhostForManagementRoomClaim(evt.Sender) {
		return
	}

	user := br.getUserForManagementRoomClaim(evt.Sender)
	if user == nil || user.GetPermissionLevel() < bridgeconfig.PermissionLevelUser || user.GetManagementRoomID() != "" {
		return
	}

	if !br.ensureBotJoinedManagementRoom(br.Bot, evt.RoomID, user, "implicit_dm") {
		return
	}

	members, err := br.Bot.JoinedMembers(evt.RoomID)
	if err != nil {
		br.ZLog.Warn().
			Err(err).
			Str("trigger", "implicit_dm").
			Str("room_id", evt.RoomID.String()).
			Str("user", user.MXID.String()).
			Msg("Failed to fetch room members for implicit DM claim")
		return
	}
	if !br.isImplicitDMClaimCandidate(members, evt.Sender) {
		br.ZLog.Debug().
			Str("trigger", "implicit_dm").
			Str("room_id", evt.RoomID.String()).
			Str("user", user.MXID.String()).
			Int("joined_member_count", len(members.Joined)).
			Msg("Skipping implicit DM claim: room is not a direct chat between sender and bot")
		return
	}

	br.claimManagementRoomAfterJoin(br.Bot, evt.RoomID, user, "implicit_dm")
}

func (br *TeamsBridge) getUserForManagementRoomClaim(userID id.UserID) *User {
	if userID == "" || br.Bot == nil || userID == br.Bot.UserID || br.isGhostForManagementRoomClaim(userID) {
		return nil
	}

	br.usersLock.Lock()
	defer br.usersLock.Unlock()

	user, ok := br.usersByMXID[userID]
	if ok {
		return user
	}
	return br.loadUser(br.DB.User.GetByMXID(userID), &userID)
}

func main() {
	br := &TeamsBridge{
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
