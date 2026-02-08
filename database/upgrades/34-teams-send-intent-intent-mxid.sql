-- v34 (compatible with v31+): Teams send intent matrix intent mxid

ALTER TABLE teams_send_intent ADD COLUMN intent_mxid TEXT NOT NULL DEFAULT '';
