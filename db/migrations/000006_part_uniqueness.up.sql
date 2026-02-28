-- Prevent duplicate parts with the same MPN+manufacturer within the same visibility scope.
-- Uses LOWER() for case-insensitive uniqueness, matching ILIKE queries in the application.

-- Global parts: one entry per (manufacturer, mpn) pair globally.
CREATE UNIQUE INDEX parts_global_mpn_mfr_uniq
    ON parts (LOWER(manufacturer), mpn)
    WHERE scope = 'global' AND mpn IS NOT NULL AND manufacturer IS NOT NULL;

-- Team parts: one entry per (manufacturer, mpn) pair per team.
CREATE UNIQUE INDEX parts_team_mpn_mfr_uniq
    ON parts (LOWER(manufacturer), mpn, team_id)
    WHERE scope = 'team' AND mpn IS NOT NULL AND manufacturer IS NOT NULL;
