ALTER TABLE oauth_tokens
  DROP CONSTRAINT oauth_tokens_team_user_distributor_unique;
ALTER TABLE oauth_tokens
  DROP COLUMN user_id;
ALTER TABLE oauth_tokens
  ADD CONSTRAINT oauth_tokens_team_id_distributor_id_key
    UNIQUE (team_id, distributor_id);
