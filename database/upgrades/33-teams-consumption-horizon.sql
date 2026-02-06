-- v32 -> v33: Teams consumption horizons + message metadata

DROP TABLE IF EXISTS teams_message_map_old;
ALTER TABLE teams_message_map RENAME TO teams_message_map_old;

CREATE TABLE teams_message_map (
    mxid TEXT PRIMARY KEY,
    thread_id TEXT NOT NULL,
    teams_message_id TEXT NOT NULL,
    message_ts BIGINT,
    sender_id TEXT
);

INSERT INTO teams_message_map (mxid, thread_id, teams_message_id)
SELECT mxid, thread_id, teams_message_id FROM teams_message_map_old;

DROP TABLE teams_message_map_old;

DROP TABLE IF EXISTS teams_consumption_horizon;

CREATE TABLE teams_consumption_horizon (
    thread_id TEXT NOT NULL,
    teams_user_id TEXT NOT NULL,
    last_read_ts BIGINT NOT NULL,
    PRIMARY KEY (thread_id, teams_user_id)
);

CREATE INDEX teams_message_map_thread_ts_idx ON teams_message_map (thread_id, message_ts);
