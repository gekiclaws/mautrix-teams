-- v27 (compatible with v19+): Teams profile cache

CREATE TABLE IF NOT EXISTS teams_profile (
    teams_user_id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    last_seen_ts BIGINT NOT NULL
);
