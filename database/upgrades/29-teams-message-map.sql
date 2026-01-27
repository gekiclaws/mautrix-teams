-- v29 (compatible with v19+): Teams message map for consumer reactions

CREATE TABLE teams_message_map (
    mxid TEXT PRIMARY KEY,
    thread_id TEXT NOT NULL,
    teams_message_id TEXT NOT NULL
);
