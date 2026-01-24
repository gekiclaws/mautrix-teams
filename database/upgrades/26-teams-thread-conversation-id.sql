-- v26 (compatible with v25+): Add conversation ID to teams_thread
ALTER TABLE teams_thread ADD COLUMN conversation_id TEXT;
