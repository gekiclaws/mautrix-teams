-- v28 (compatible with v19+): Teams send intent

CREATE TABLE teams_send_intent (
    thread_id TEXT NOT NULL,
    client_message_id TEXT PRIMARY KEY,
    timestamp BIGINT NOT NULL,
    status TEXT NOT NULL
);
