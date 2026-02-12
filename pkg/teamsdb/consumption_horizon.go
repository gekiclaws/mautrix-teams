package teamsdb

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

type ConsumptionHorizonState struct {
	BridgeID    networkid.BridgeID
	UserLoginID networkid.UserLoginID
	ThreadID    string
	TeamsUserID string
	LastReadTS  int64
}

type ConsumptionHorizonQuery struct {
	BridgeID networkid.BridgeID
	Database *dbutil.Database
}

func (q *ConsumptionHorizonQuery) Get(ctx context.Context, userLoginID networkid.UserLoginID, threadID string, teamsUserID string) (*ConsumptionHorizonState, error) {
	if q == nil || q.Database == nil {
		return nil, errMissingDB
	}
	threadID = strings.TrimSpace(threadID)
	teamsUserID = strings.TrimSpace(teamsUserID)
	if threadID == "" || teamsUserID == "" {
		return nil, nil
	}
	row := q.Database.QueryRow(ctx, `
		SELECT thread_id, teams_user_id, last_read_ts
		FROM teams_consumption_horizon_state
		WHERE bridge_id=$1 AND user_login_id=$2 AND thread_id=$3 AND teams_user_id=$4
	`, q.BridgeID, userLoginID, threadID, teamsUserID)
	state := &ConsumptionHorizonState{
		BridgeID:    q.BridgeID,
		UserLoginID: userLoginID,
	}
	if err := row.Scan(&state.ThreadID, &state.TeamsUserID, &state.LastReadTS); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return state, nil
}

func (q *ConsumptionHorizonQuery) UpsertLastRead(ctx context.Context, userLoginID networkid.UserLoginID, threadID string, teamsUserID string, lastReadTS int64) error {
	if q == nil || q.Database == nil {
		return errMissingDB
	}
	threadID = strings.TrimSpace(threadID)
	teamsUserID = strings.TrimSpace(teamsUserID)
	if threadID == "" || teamsUserID == "" {
		return errors.New("missing thread id or teams user id")
	}
	_, err := q.Database.Exec(ctx, `
		INSERT INTO teams_consumption_horizon_state (
			bridge_id, user_login_id, thread_id, teams_user_id, last_read_ts
		) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (bridge_id, user_login_id, thread_id, teams_user_id) DO UPDATE SET
			last_read_ts=excluded.last_read_ts
	`, q.BridgeID, userLoginID, threadID, teamsUserID, lastReadTS)
	return err
}
