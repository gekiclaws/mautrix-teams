-- v36 (compatible with v19+): unify reaction mapping state

DROP TABLE IF EXISTS teams_reaction_map;
DROP TABLE IF EXISTS teams_reaction;

CREATE TABLE reaction_map (
    thread_id TEXT NOT NULL,
    teams_message_id TEXT NOT NULL,
    teams_user_id TEXT NOT NULL,
    reaction_key TEXT NOT NULL,
    matrix_room_id TEXT NOT NULL,
    matrix_target_event_id TEXT NOT NULL,
    matrix_reaction_event_id TEXT NOT NULL,
    updated_ts_ms BIGINT NOT NULL,
    PRIMARY KEY (thread_id, teams_message_id, teams_user_id, reaction_key)
);

CREATE INDEX reaction_map_matrix_event_idx ON reaction_map (matrix_room_id, matrix_reaction_event_id);
