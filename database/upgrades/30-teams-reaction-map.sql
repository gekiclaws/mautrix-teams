-- v30 (compatible with v19+): Teams reaction map for consumer reactions

CREATE TABLE teams_reaction_map (
    reaction_mxid TEXT PRIMARY KEY,
    target_mxid TEXT NOT NULL,
    emotion_key TEXT NOT NULL
);
