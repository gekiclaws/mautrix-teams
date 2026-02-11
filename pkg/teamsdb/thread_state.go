package teamsdb

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

type ThreadState struct {
	BridgeID     networkid.BridgeID
	UserLoginID  networkid.UserLoginID
	ThreadID     string
	Conversation string
	IsOneToOne   bool
	Name         string

	LastSequenceID string
	LastMessageTS  int64
}

type ThreadStateQuery struct {
	BridgeID networkid.BridgeID
	Database *dbutil.Database
}

var errMissingDB = errors.New("teamsdb missing database")

func (q *ThreadStateQuery) Upsert(ctx context.Context, state *ThreadState) error {
	if q == nil || q.Database == nil {
		return errMissingDB
	}
	if state == nil {
		return errors.New("thread state is nil")
	}
	state.ThreadID = strings.TrimSpace(state.ThreadID)
	state.Conversation = strings.TrimSpace(state.Conversation)
	if state.ThreadID == "" || state.Conversation == "" {
		return errors.New("missing thread_id or conversation_id")
	}
	_, err := q.Database.Exec(ctx, `
		INSERT INTO teams_thread_state (
			bridge_id, user_login_id, thread_id, conversation_id, is_one_to_one, name, last_sequence_id, last_message_ts
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (bridge_id, user_login_id, thread_id) DO UPDATE SET
			conversation_id=excluded.conversation_id,
			is_one_to_one=excluded.is_one_to_one,
			name=excluded.name
	`, q.BridgeID, state.UserLoginID, state.ThreadID, state.Conversation, state.IsOneToOne, state.Name, state.LastSequenceID, state.LastMessageTS)
	return err
}

func (q *ThreadStateQuery) UpdateCursor(ctx context.Context, userLoginID networkid.UserLoginID, threadID, lastSequenceID string, lastMessageTS int64) error {
	if q == nil || q.Database == nil {
		return errMissingDB
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return errors.New("missing thread id")
	}
	_, err := q.Database.Exec(ctx, `
		UPDATE teams_thread_state
		SET last_sequence_id=$1, last_message_ts=$2
		WHERE bridge_id=$3 AND user_login_id=$4 AND thread_id=$5
	`, lastSequenceID, lastMessageTS, q.BridgeID, userLoginID, threadID)
	return err
}

func (q *ThreadStateQuery) ListForLogin(ctx context.Context, userLoginID networkid.UserLoginID) ([]*ThreadState, error) {
	if q == nil || q.Database == nil {
		return nil, errMissingDB
	}
	rows, err := q.Database.Query(ctx, `
		SELECT thread_id, conversation_id, is_one_to_one, name, last_sequence_id, last_message_ts
		FROM teams_thread_state
		WHERE bridge_id=$1 AND user_login_id=$2
	`, q.BridgeID, userLoginID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ThreadState
	for rows.Next() {
		st := &ThreadState{
			BridgeID:    q.BridgeID,
			UserLoginID: userLoginID,
		}
		if err := rows.Scan(&st.ThreadID, &st.Conversation, &st.IsOneToOne, &st.Name, &st.LastSequenceID, &st.LastMessageTS); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (q *ThreadStateQuery) Get(ctx context.Context, userLoginID networkid.UserLoginID, threadID string) (*ThreadState, error) {
	if q == nil || q.Database == nil {
		return nil, errMissingDB
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, nil
	}
	row := q.Database.QueryRow(ctx, `
		SELECT thread_id, conversation_id, is_one_to_one, name, last_sequence_id, last_message_ts
		FROM teams_thread_state
		WHERE bridge_id=$1 AND user_login_id=$2 AND thread_id=$3
	`, q.BridgeID, userLoginID, threadID)
	st := &ThreadState{
		BridgeID:    q.BridgeID,
		UserLoginID: userLoginID,
	}
	err := row.Scan(&st.ThreadID, &st.Conversation, &st.IsOneToOne, &st.Name, &st.LastSequenceID, &st.LastMessageTS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return st, nil
}
