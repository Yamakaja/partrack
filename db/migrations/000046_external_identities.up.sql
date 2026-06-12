-- Generalise oidc_identities into external_identities so the same (provider,
-- subject) linking model serves both OIDC SSO and LDAP/AD directory logins.
-- A `kind` discriminator records which mechanism minted the identity.

ALTER TABLE oidc_identities RENAME TO external_identities;

ALTER TABLE external_identities
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'oidc';

COMMENT ON COLUMN external_identities.kind IS 'identity mechanism: ''oidc'' or ''ldap''';
COMMENT ON COLUMN external_identities.provider IS 'config-file provider slug (oidc[].name or ldap[].name)';
COMMENT ON COLUMN external_identities.subject IS 'stable IdP identifier: OIDC ''sub'' claim or LDAP unique-id attribute (e.g. entryUUID/objectGUID)';
