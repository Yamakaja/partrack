-- directory_managed marks a membership as owned by LDAP/directory group sync.
-- Directory sync only ever creates, updates, or removes rows where this is TRUE;
-- a manual add or role change (by a team/global admin) sets it FALSE, pinning
-- the membership so a later sync cannot revert the override. Existing rows were
-- all created manually, so the FALSE default is correct.
ALTER TABLE team_memberships
    ADD COLUMN directory_managed BOOLEAN NOT NULL DEFAULT FALSE;
