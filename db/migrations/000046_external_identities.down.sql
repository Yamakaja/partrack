ALTER TABLE external_identities DROP COLUMN kind;

ALTER TABLE external_identities RENAME TO oidc_identities;
