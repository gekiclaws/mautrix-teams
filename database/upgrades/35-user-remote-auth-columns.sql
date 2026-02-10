-- v35 (compatible with v19+): Rename legacy user identifiers to neutral naming
ALTER TABLE "user" RENAME COLUMN dcid TO remote_id;
ALTER TABLE "user" RENAME COLUMN discord_token TO auth_token;
