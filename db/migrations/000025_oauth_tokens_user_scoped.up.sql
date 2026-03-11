TRUNCATE oauth_tokens;
ALTER TABLE oauth_tokens
  ADD COLUMN user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE oauth_tokens
  DROP CONSTRAINT oauth_tokens_team_id_distributor_id_key;
ALTER TABLE oauth_tokens
  ADD CONSTRAINT oauth_tokens_team_user_distributor_unique
    UNIQUE (team_id, user_id, distributor_id);
