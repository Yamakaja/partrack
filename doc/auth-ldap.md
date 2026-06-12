# LDAP / Active Directory Authentication

partrack can authenticate users against an LDAP directory (OpenLDAP, Active
Directory, FreeIPA, etc.) in addition to local passwords and OIDC SSO. This
document describes how it works, how to configure it, and the security model.

## How it fits in

partrack has one canonical identity — the `users` row — and several ways to
prove you are that user (local password, OIDC, API token, and now LDAP). All of
them converge on the same session cookie once a user is resolved.

Unlike OIDC (a browser redirect to an identity provider), **LDAP login is
credential-based**: the user types their username and password into partrack's
normal login form and partrack verifies them against the directory. There is
therefore **no separate LDAP route and no SSO button** — directory login rides
the existing `POST /api/v1/auth/login` endpoint.

The login handler resolves credentials in this order:

1. **Local password.** If the identifier matches a user that has a stored
   password (`user_credentials`), it is verified with bcrypt as usual.
2. **Directory bind.** If the user has *no* local password and at least one LDAP
   provider is configured, the identifier is treated as a directory username and
   verified by a [search-then-bind](#the-search-then-bind-flow).

Because local users are checked first, the bootstrap `admin@localhost` account
(and any other local-password user) keeps working even with LDAP enabled.

## The search-then-bind flow

For each configured directory, on a login attempt partrack:

1. Connects to the directory (`ldaps://` or `ldap://` + StartTLS).
2. Binds as the read-only **service account** (`bind_dn` / `bind_password`).
3. Searches `user_base_dn` with `user_filter`, substituting the (escaped)
   username for `%s` — e.g. `(uid=alice)`.
4. **Re-binds as the found user's DN** using the password they supplied. A
   successful bind proves the password.
5. Reads identity attributes (email, display name, a stable unique id) and the
   user's group memberships off the entry.

A fresh connection is opened per login attempt and closed immediately after;
LDAP connections are not pooled.

## Restricting who may log in

By default any user who binds successfully may log in (and be provisioned). Set
`required_group` to a group DN to gate this: a user who authenticates but is
**not** a member of that group is denied exactly like a wrong password — no
session, and no account is provisioned. The gate is checked on every login, so
removing a user from the group also locks out their existing account.

## Account linking and provisioning

After a successful bind, the directory identity is mapped to a partrack user,
mirroring the OIDC model:

1. **Linked identity** — if an `external_identities` row already exists for this
   `(provider, subject)`, that user is logged in. `subject` is the directory's
   stable unique id (`attr_unique_id`, default `entryUUID`; use `objectGUID` for
   Active Directory).
2. **Auto-link by email** — otherwise, if a user with the same email already
   exists, the directory identity is linked to it.
3. **Auto-provision** — otherwise, if `auto_provision: true`, a new user is
   created from the directory's email and display name.

If none apply (no existing user and provisioning disabled), the login is
rejected. The directory's `mail` attribute is read via the service account, so
it is treated as authoritative — there is no separate "verified email" gate as
there is for OIDC id-token claims.

## Group → team-role mapping

On **every** LDAP login, partrack reconciles the user's authorization from their
current directory groups:

- **Instance admin.** When `admin_group` is set, the user's `is_admin` flag is
  set to exactly reflect membership in that group — granted to members, revoked
  from non-members.
- **Team memberships.** Each entry in `group_mappings` binds a directory group
  DN to a partrack **team name** and **role**. For every team named by a
  mapping, the user's membership is set to the strongest role their groups grant
  (`viewer` < `member` < `purchaser` < `admin`), or removed if no mapped group
  applies.

Only teams referenced by a mapping are managed this way; memberships in any
other team are left untouched, so manually assigned memberships are preserved.
Teams are matched by name; a mapping that names a team which does not exist is
logged and skipped (create the team first).

Because the sync runs at login time, directory changes take effect on the user's
**next sign-in**, not in real time.

### Default (catch-all) provisioning

A `group_mapping` whose `group` is **empty** matches every authenticated
directory user, regardless of their groups. This lets an installation
auto-provision everyone to a baseline team and role — typically `viewer` or
`member` — without creating a dedicated directory group:

```yaml
group_mappings:
  - { group: "", team: "Engineering", role: "viewer" }   # everyone → viewer
```

### Manual overrides win

Each membership is flagged as either **directory-managed** or **manual**. Group
sync only ever creates, updates, or removes *directory-managed* rows. The moment
a team or global admin changes a member's role (or adds them) in the team
settings UI, that membership becomes **manual** and is pinned — a later sync will
neither change its role nor remove it, even if the directory says otherwise.

This is the intended workflow for the catch-all pattern above: provision all
users at `viewer` via the directory, then individually promote the ones who need
more access. Their elevated role survives every subsequent login. In the team
members list, directory-managed memberships show a **Directory** badge;
overriding the role removes it.

> The instance **admin** flag (`admin_group`) is *not* subject to manual-override
> pinning: when `admin_group` is configured it is authoritative and is granted or
> revoked on every login to match group membership. Manage admins via the
> directory group, or leave `admin_group` unset to control the flag entirely by
> hand.

## Configuration

Add one or more providers under `ldap:` in `config.yaml`:

```yaml
ldap:
  - name: corp                          # slug stored in external_identities.provider
    display_name: "Company Directory"   # informational

    url: "ldaps://ldap.corp.example:636"  # or ldap://… with start_tls: true
    start_tls: false                      # upgrade a plaintext ldap:// connection
    insecure_skip_verify: false           # disable TLS cert checks — dev only
    ca_cert_file: ""                      # PEM bundle to trust a corporate CA
    timeout_seconds: 8                    # connect/bind timeout

    bind_dn: "cn=svc-partrack,ou=svc,dc=corp,dc=example"
    bind_password: "..."                  # or env PARTRACK_LDAP_CORP_PASSWORD

    user_base_dn: "ou=people,dc=corp,dc=example"
    user_filter: "(uid=%s)"               # %s = escaped login username

    email_domains: ["corp.example"]       # route logins for this domain here (optional)
    strip_domain: false                   # send only the local part to user_filter

    # Attribute names read off the user entry (defaults shown):
    attr_email: "mail"
    attr_display_name: "cn"
    attr_unique_id: "entryUUID"           # objectGUID for Active Directory
    attr_member_of: "memberOf"

    # Group resolution by reverse search (optional) — use when the directory
    # does not populate memberOf, or to expand AD nested groups:
    group_base_dn: ""                     # e.g. ou=groups,dc=corp,dc=example
    group_filter: "(member=%s)"           # %s = escaped user DN

    required_group: "cn=partrack-users,ou=groups,dc=corp,dc=example"  # gate login
    auto_provision: true                  # create a user on first login

    admin_group: "cn=partrack-admins,ou=groups,dc=corp,dc=example"
    group_mappings:
      - { group: "cn=eng,ou=groups,dc=corp,dc=example",    team: "Engineering", role: "member" }
      - { group: "cn=buyers,ou=groups,dc=corp,dc=example",  team: "Engineering", role: "purchaser" }
```

### Multiple directories and email-domain routing

You can configure several `ldap:` providers. By default a login is attempted
against each in turn — which, with multiple directories, wastes binds and risks
tripping account-lockout policies on directories where the user exists. Set
`email_domains` on a provider to route instead: a login is only attempted
against providers whose `email_domains` include the identifier's email domain. A
provider with no `email_domains` stays a catch-all (always attempted), so adding
domains is backward-compatible.

```yaml
ldap:
  - name: institute
    email_domains: ["institute.com"]      # john.doe@institute.com → here only
    user_filter: "(mail=%s)"
    # …
  - name: partner
    email_domains: ["partner.org"]        # jane@partner.org → here only
    # …
```

`admin@localhost` matches no directory's domain and has a local password, so it
stays a local login. If a domain-routed provider uses a uid-style
`user_filter` (`(uid=%s)`) rather than an email one (`(mail=%s)`), set
`strip_domain: true` to pass only the local part of the email into the filter.

### Group resolution: memberOf vs search

By default groups come from the user entry's `memberOf` attribute. Two common
directories need `group_base_dn` instead:

- **OpenLDAP without the `memberof` overlay** doesn't populate `memberOf` at all
  — group sync would silently find nothing. Use a reverse search:
  `group_base_dn: ou=groups,…`, `group_filter: "(member=%s)"`.
- **Active Directory nested groups** aren't expanded in `memberOf`. Use AD's
  in-chain matching rule: `group_filter: "(member:1.2.840.113556.1.4.1941:=%s)"`.

When `group_base_dn` is set, a user's groups are the DNs of entries under it that
match `group_filter` (with the escaped user DN substituted for `%s`).

### Worked example: gated login + baseline team access + manual upgrades

This is the common "everyone in a group gets viewer; promote individuals as
needed" setup:

```yaml
ldap:
  - name: corp
    url: "ldaps://ldap.corp.example:636"
    bind_dn: "cn=svc-partrack,ou=svc,dc=corp,dc=example"
    bind_password: "..."                 # or PARTRACK_LDAP_CORP_PASSWORD
    user_base_dn: "ou=people,dc=corp,dc=example"
    user_filter: "(uid=%s)"

    # 1. Only members of this group may log in (and be provisioned).
    required_group: "cn=partrack-users,ou=groups,dc=corp,dc=example"
    auto_provision: true

    # 2. Those users get viewer access to the Engineering team.
    group_mappings:
      - { group: "cn=partrack-users,ou=groups,dc=corp,dc=example", team: "Engineering", role: "viewer" }
```

With this config: a user must be in `partrack-users` to sign in at all; on first
login they are provisioned and given **viewer** on Engineering; and an admin can
then open the team's members page and raise any individual to member / purchaser
/ admin — **3.** that elevated role is pinned and survives every future login,
no directory change required.

### Active Directory notes

- `url`: use `ldaps://` (port 636) or `ldap://` with `start_tls: true`.
- `user_filter`: `(sAMAccountName=%s)` or `(userPrincipalName=%s)`.
- `attr_unique_id`: `objectGUID`.
- AD populates `memberOf` with the user's group DNs, which is what
  `admin_group` / `group_mappings` match against.

### Required vs optional fields

`name`, `url`, `user_base_dn` and `user_filter` are required (startup fails
otherwise), and `user_filter` must contain exactly one `%s`. `bind_dn` /
`bind_password` may be omitted if the directory permits anonymous search.
Attribute names default as shown above. Group handling is entirely optional.

### Environment overrides

| Variable | Equivalent config key |
|---|---|
| `PARTRACK_LDAP_<NAME>_PASSWORD` | `ldap[name].bind_password` |

`<NAME>` is the uppercased provider slug — e.g. `PARTRACK_LDAP_CORP_PASSWORD`
for `name: corp`. Prefer this over committing the bind password to the config
file.

## Login page behaviour

`GET /api/v1/auth/providers` returns `ldap_enabled: true` when any directory is
configured. The login page uses this to relabel the identifier field
"Email or username" and accept a non-email value. OIDC providers still render as
separate sign-in buttons; LDAP does not, because it uses the main form.

## Diagnostics tool

Instance admins get an in-app **LDAP Diagnostics** page (Administration → LDAP,
backed by `POST /api/v1/admin/ldap/diagnose`). Enter a username and password and
it runs the *real* search-then-bind and group evaluation against the configured
directory, then reports:

- a step-by-step **bind trace** (connect/TLS, service bind, the exact search
  filter and match count, user re-bind) with the precise failure point and LDAP
  error when something goes wrong;
- the **resolved identity** (DN, subject, email, display name, group DNs);
- the **authorization** the user's groups produce (admin flag, per-mapping
  matches, resulting team roles);
- the **account resolution** — whether login would use an already-linked
  identity, link to an existing user by email, or auto-provision — plus, per
  team, whether the mapped role would be applied, removed, or **preserved**
  because a manual override is in place.

It is strictly **read-only**: it performs DB reads to make the report precise but
never creates a user, identity, membership, or session. Use it to validate a new
`ldap:` block (after restarting), debug connectivity/TLS, or confirm that a
user's groups map to the teams and roles you expect.

The page also offers a **Test connection** button (backed by
`POST /api/v1/admin/ldap/test-connection`) that needs *no* user credentials. It:

- dials the directory, negotiates TLS, and binds the service account — so you can
  validate URL / TLS / CA / `bind_dn` before you have a test user;
- checks that every configured DN (`user_base_dn`, `group_base_dn`,
  `required_group`, `admin_group`, and each `group_mappings` group) actually
  **exists** in the directory — catching the single most common misconfig, a
  mistyped group DN;
- echoes the **effective configuration** (bind password redacted) so you can
  confirm env-var overrides such as `PARTRACK_LDAP_<NAME>_PASSWORD` took effect.

The per-user test additionally surfaces **warnings** for likely mistakes — e.g.
no groups returned while group mappings are configured (suggesting you need
`group_base_dn`), `attr_unique_id` returning empty (subject falling back to the
unstable DN), a missing email, or an email-routed provider using a uid filter.

## Security model

The directory path holds to the same bar as the rest of partrack's auth:

- **No unauthenticated bind.** Empty usernames or passwords are rejected before
  any network bind. This closes the RFC 4513 pitfall where a valid DN with an
  empty password succeeds as an *anonymous* bind and would otherwise falsely
  authenticate the user.
- **No LDAP injection.** The username is escaped (`ldap.EscapeFilter`) before
  being placed into the search filter.
- **No account enumeration.** Every failure — unknown user, wrong password,
  zero or multiple search matches, unreachable server — returns the same generic
  `INVALID_CREDENTIALS` 401 as local login. Operational errors are logged
  server-side only.
- **Bounded latency.** Connection and bind are time-limited (8s) so a slow or
  unreachable directory cannot hang a login request.
- **Use TLS.** Prefer `ldaps://` or `start_tls: true` with a valid certificate;
  `insecure_skip_verify` is for development only. The bind and user passwords
  cross the wire on every login.

## Data model

LDAP and OIDC identities share one table, `external_identities`, keyed on
`(provider, subject)` with a `kind` discriminator (`'oidc'` or `'ldap'`).
`provider` is the config slug; `subject` is the stable directory id. The
`users` row remains the single source of truth for email, display name, and the
admin flag.

## Limitations

- Group/role changes apply on next login, not live.
- A user removed from `admin_group` is demoted to a regular user on their next
  login (local users are never affected by group sync).
- Manual team-role overrides are preserved, but the instance admin flag is not
  pinned when `admin_group` is configured (see above).
- One identity per `(provider, subject)`; renaming a directory's
  `attr_unique_id` source would orphan existing links.
