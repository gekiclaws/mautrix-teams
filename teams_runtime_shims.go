package main

import (
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
)

// Teams-only runtime user model.
type User struct {
	*database.User
	bridge          *DiscordBridge
	PermissionLevel bridgeconfig.PermissionLevel
}

func (user *User) GetRemoteID() string { return "" }
func (user *User) GetRemoteName() string {
	return user.MXID.String()
}
func (user *User) GetPermissionLevel() bridgeconfig.PermissionLevel { return user.PermissionLevel }
func (user *User) IsLoggedIn() bool                                 { return true }
func (user *User) GetManagementRoomID() id.RoomID                   { return user.ManagementRoom }
func (user *User) SetManagementRoom(roomID id.RoomID)               { user.ManagementRoom = roomID }
func (user *User) GetMXID() id.UserID                               { return user.MXID }
func (user *User) GetCommandState() map[string]interface{}          { return nil }
func (user *User) GetIDoublePuppet() bridge.DoublePuppet            { return nil }
func (user *User) GetIGhost() bridge.Ghost                          { return nil }

func (br *DiscordBridge) loadUser(dbUser *database.User, mxid *id.UserID) *User {
	if dbUser == nil {
		if mxid == nil || br.DB == nil || br.DB.User == nil {
			return nil
		}
		dbUser = br.DB.User.New()
		dbUser.MXID = *mxid
		dbUser.Insert()
	}
	user := &User{
		User:            dbUser,
		bridge:          br,
		PermissionLevel: br.Config.Bridge.Permissions.Get(dbUser.MXID),
	}
	br.usersByMXID[user.MXID] = user
	return user
}

func (br *DiscordBridge) GetUserByMXID(userID id.UserID) *User {
	if userID == br.Bot.UserID || br.IsGhost(userID) {
		return nil
	}
	br.usersLock.Lock()
	defer br.usersLock.Unlock()
	if user, ok := br.usersByMXID[userID]; ok {
		return user
	}
	return br.loadUser(br.DB.User.GetByMXID(userID), &userID)
}

func (br *DiscordBridge) GetPortalByMXID(mxid id.RoomID) *TeamsConsumerPortal {
	return nil
}

func (br *DiscordBridge) GetAllIPortals() []bridge.Portal {
	if br == nil || br.DB == nil || br.DB.TeamsThread == nil {
		return nil
	}
	rows := br.DB.TeamsThread.GetAll()
	portals := make([]bridge.Portal, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.RoomID == "" {
			continue
		}
		portals = append(portals, &TeamsConsumerPortal{bridge: br, roomID: row.RoomID})
	}
	return portals
}

func (br *DiscordBridge) HandleTombstone(evt *event.Event) {}

func (br *DiscordBridge) ParsePuppetMXID(mxid id.UserID) (string, bool) { return "", false }

type Puppet struct{}

func (p *Puppet) CustomIntent() *appservice.IntentAPI {
	return nil
}
func (p *Puppet) SwitchCustomMXID(accessToken string, userID id.UserID) error {
	return nil
}
func (p *Puppet) ClearCustomMXID() {}
func (p *Puppet) DefaultIntent() *appservice.IntentAPI {
	return nil
}
func (p *Puppet) GetMXID() id.UserID {
	return ""
}

func (br *DiscordBridge) GetPuppetByMXID(mxid id.UserID) *Puppet { return nil }
func (br *DiscordBridge) GetPuppetByCustomMXID(mxid id.UserID) *Puppet { return nil }
