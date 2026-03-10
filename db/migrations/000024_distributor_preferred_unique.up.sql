-- Clean up any existing duplicates, keeping one preferred per team
-- (the first alphabetically by distributor name).
UPDATE distributor_configs
SET preferred = FALSE
WHERE preferred = TRUE
  AND id NOT IN (
    SELECT DISTINCT ON (dc.team_id) dc.id
    FROM distributor_configs dc
    JOIN distributors d ON d.id = dc.distributor_id
    WHERE dc.preferred = TRUE
    ORDER BY dc.team_id, d.name
  );

-- Enforce at most one preferred distributor per team.
CREATE UNIQUE INDEX distributor_configs_team_preferred_unique
    ON distributor_configs (team_id)
    WHERE preferred = TRUE;
