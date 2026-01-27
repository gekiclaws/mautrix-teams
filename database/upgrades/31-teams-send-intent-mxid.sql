-- v31 (compatible with v28+): Teams send intent matrix event id

ALTER TABLE teams_send_intent ADD COLUMN mxid TEXT NOT NULL DEFAULT '';
