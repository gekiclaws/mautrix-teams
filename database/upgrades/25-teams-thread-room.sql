-- v25 (compatible with v19+): Teams thread -> Matrix room mapping
CREATE TABLE teams_thread (
    thread_id TEXT PRIMARY KEY,
    room_id TEXT UNIQUE,
    conversation_id TEXT,
    last_sequence_id TEXT,
    last_message_ts BIGINT,
    last_message_id TEXT
);
