-- v0 -> v1: initial schema for mautrix-teams (bridgev2 rewrite)

CREATE TABLE IF NOT EXISTS teams_thread_state (
    bridge_id TEXT NOT NULL,
    user_login_id TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    is_one_to_one BOOLEAN NOT NULL,
    name TEXT NOT NULL,
    last_sequence_id TEXT NOT NULL DEFAULT '',
    last_message_ts BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bridge_id, user_login_id, thread_id)
);

CREATE INDEX IF NOT EXISTS teams_thread_state_login_idx ON teams_thread_state (bridge_id, user_login_id);

CREATE TABLE IF NOT EXISTS teams_profile (
    bridge_id TEXT NOT NULL,
    teams_user_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    last_seen_ts BIGINT NOT NULL,
    PRIMARY KEY (bridge_id, teams_user_id)
);

CREATE INDEX IF NOT EXISTS teams_profile_seen_idx ON teams_profile (bridge_id, last_seen_ts);

CREATE TABLE IF NOT EXISTS teams_consumption_horizon_state (
    bridge_id TEXT NOT NULL,
    user_login_id TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    teams_user_id TEXT NOT NULL,
    last_read_ts BIGINT NOT NULL,
    PRIMARY KEY (bridge_id, user_login_id, thread_id, teams_user_id)
);

CREATE INDEX IF NOT EXISTS teams_consumption_horizon_idx ON teams_consumption_horizon_state (bridge_id, user_login_id, thread_id);
