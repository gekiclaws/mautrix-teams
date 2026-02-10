package database

import (
	"database/sql"
	"go.mau.fi/util/dbutil"
	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix/id"
)

type UserQuery struct {
	db  *Database
	log log.Logger
}

func (uq *UserQuery) New() *User {
	return &User{
		db:  uq.db,
		log: uq.log,
	}
}

func (uq *UserQuery) GetByMXID(userID id.UserID) *User {
	query := `SELECT mxid, remote_id, auth_token, management_room, space_room, dm_space_room, read_state_version, heartbeat_session FROM "user" WHERE mxid=$1`
	return uq.New().Scan(uq.db.QueryRow(query, userID))
}

func (uq *UserQuery) GetByID(id string) *User {
	query := `SELECT mxid, remote_id, auth_token, management_room, space_room, dm_space_room, read_state_version, heartbeat_session FROM "user" WHERE remote_id=$1`
	return uq.New().Scan(uq.db.QueryRow(query, id))
}

func (uq *UserQuery) GetAllWithToken() []*User {
	query := `
		SELECT mxid, remote_id, auth_token, management_room, space_room, dm_space_room, read_state_version, heartbeat_session
		FROM "user" WHERE auth_token IS NOT NULL
	`
	rows, err := uq.db.Query(query)
	if err != nil || rows == nil {
		return nil
	}

	var users []*User
	for rows.Next() {
		user := uq.New().Scan(rows)
		if user != nil {
			users = append(users, user)
		}
	}
	return users
}

type User struct {
	db  *Database
	log log.Logger

	MXID             id.UserID
	RemoteID         string
	AuthToken        string
	ManagementRoom   id.RoomID
	SpaceRoom        id.RoomID
	DMSpaceRoom      id.RoomID
	HeartbeatSession map[string]any

	ReadStateVersion int
}

func (u *User) Scan(row dbutil.Scannable) *User {
	var remoteID, managementRoom, spaceRoom, dmSpaceRoom, authToken sql.NullString
	err := row.Scan(&u.MXID, &remoteID, &authToken, &managementRoom, &spaceRoom, &dmSpaceRoom, &u.ReadStateVersion, dbutil.JSON{Data: &u.HeartbeatSession})
	if err != nil {
		if err != sql.ErrNoRows {
			u.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}
	u.RemoteID = remoteID.String
	u.AuthToken = authToken.String
	u.ManagementRoom = id.RoomID(managementRoom.String)
	u.SpaceRoom = id.RoomID(spaceRoom.String)
	u.DMSpaceRoom = id.RoomID(dmSpaceRoom.String)
	return u
}

func (u *User) Insert() {
	query := `INSERT INTO "user" (mxid, remote_id, auth_token, management_room, space_room, dm_space_room, read_state_version, heartbeat_session) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := u.db.Exec(query, u.MXID, strPtr(u.RemoteID), strPtr(u.AuthToken), strPtr(string(u.ManagementRoom)), strPtr(string(u.SpaceRoom)), strPtr(string(u.DMSpaceRoom)), u.ReadStateVersion, dbutil.JSON{Data: u.HeartbeatSession})
	if err != nil {
		u.log.Warnfln("Failed to insert %s: %v", u.MXID, err)
		panic(err)
	}
}

func (u *User) Update() {
	query := `UPDATE "user" SET remote_id=$1, auth_token=$2, management_room=$3, space_room=$4, dm_space_room=$5, read_state_version=$6, heartbeat_session=$7 WHERE mxid=$8`
	_, err := u.db.Exec(query, strPtr(u.RemoteID), strPtr(u.AuthToken), strPtr(string(u.ManagementRoom)), strPtr(string(u.SpaceRoom)), strPtr(string(u.DMSpaceRoom)), u.ReadStateVersion, dbutil.JSON{Data: u.HeartbeatSession}, u.MXID)
	if err != nil {
		u.log.Warnfln("Failed to update %q: %v", u.MXID, err)
		panic(err)
	}
}
