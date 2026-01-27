-- v32 (compatible with v19+): Teams reaction ingest state
CREATE TABLE teams_reaction (
    thread_id TEXT NOT NULL,
    teams_message_id TEXT NOT NULL,
    emotion_key TEXT NOT NULL,
    user_mri TEXT NOT NULL,
    matrix_event_id TEXT NOT NULL,
    PRIMARY KEY (thread_id, teams_message_id, emotion_key, user_mri)
);
