CREATE TABLE oauth_tokens (
    id              UUID        PRIMARY KEY DEFAULT uuidv7(),
    team_id         UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    distributor_id  UUID        NOT NULL REFERENCES distributors(id),
    access_token    TEXT        NOT NULL,
    refresh_token   TEXT        NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, distributor_id)
);
