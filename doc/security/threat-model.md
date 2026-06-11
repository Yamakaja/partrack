# partrack — Threat Model

Status: draft, basis for the security review. Source code is assumed fully public
(open source); secrecy of code is **not** a control.

## 1. Scope & method

Scope: the partrack server (Go API + embedded SPA), its PostgreSQL store, the
auth/session layer, team RBAC, the phone-scan channel, and outbound distributor
integrations. We enumerate assets, trust boundaries, actors, entry points, and
the controls/gaps per STRIDE. Infra below the reverse proxy (OS, Postgres
hardening, network) is assumed but its config is out of scope except where
partrack depends on it.

## 2. System overview

Single Go binary, single process, no clustering. Serves a JSON API under
`/api/v1` and the embedded Svelte SPA at `/`. State lives in one external
PostgreSQL. An in-process worker queue runs async jobs (BOM import, enrichment,
part resolution) and a per-team SSE hub pushes change events. The server calls
out to distributor APIs (e.g. DigiKey) using per-team credentials / OAuth tokens.

```
                          trust boundary (reverse proxy terminates TLS)
  Browser SPA  ─┐                    │
  API clients  ─┤── HTTPS ──▶  [ reverse proxy ] ──HTTP──▶ [ partrack binary ]
  Phone (scan) ─┘                    │                         │   │
                                     │                         │   └─▶ PostgreSQL
                                     │                         └─────▶ Distributor APIs
                                                                       (DigiKey, OIDC IdP)
```

Tenancy: multi-team within a single instance. A `team` is the primary
authorization boundary; an instance-level `is_admin` flag transcends all teams.

## 3. Assets

| Asset | Why it matters |
|---|---|
| User credentials (`user_credentials.password_hash`, bcrypt) | Account takeover |
| Session tokens / API tokens / scan tokens (SHA-256 hash stored) | Impersonation |
| Password-reset tokens | Account takeover |
| `session_secret` (HMAC key for OAuth state & link tokens) | Forge signed state/links |
| Distributor API credentials (`distributor_configs.api_credentials`, **plaintext JSONB**) | Abuse of paid distributor accounts |
| Distributor OAuth access/refresh tokens (`oauth_tokens`, **plaintext**) | Same; long-lived refresh tokens |
| OIDC client secrets (config/env) | IdP impersonation |
| Business data: parts, BOMs, inventory, orders, builds, costs | Confidentiality / integrity per team |
| Audit/ledger data (stock_adjustments, scan audit log) | Integrity of the immutable record |

## 4. Actors

- **Anonymous** — can reach login, OIDC initiate/callback, OAuth callback,
  password-reset endpoints, public category/parameter reads, the SPA, and the
  SSE scan stream (`/scan/stream`, token in query string).
- **Authenticated user (no team)** — valid session/API token, no membership.
- **Team member** — `viewer < member < purchaser < admin`, strictly hierarchical,
  scoped per team via `{teamId}` in the URL.
- **Instance admin** (`is_admin`) — implicit `admin` on every team; manages users,
  passwords, reset links, the parameter registry.
- **Phone scanner** — a `scan_`-prefixed session token bound to one (user, team).
- **Malicious distributor / IdP / network position** — upstream API or OIDC
  provider returns hostile data; or an attacker MITMs outbound calls.
- **Source-code-aware external attacker** — knows the full implementation.

## 5. Trust boundaries

1. **Internet ↔ reverse proxy** — TLS terminates here; partrack speaks plain HTTP
   behind it. `RealIP` only honours `X-Forwarded-For`/`X-Real-IP` from configured
   `trusted_proxies` CIDRs.
2. **Reverse proxy ↔ partrack** — assumed private; partrack trusts proxy headers
   only within the CIDR allowlist.
3. **partrack ↔ PostgreSQL** — full trust; DB holds all secrets, some plaintext.
4. **partrack ↔ distributor / OIDC** — outbound; responses are untrusted input.
5. **Authn boundary** — `Authenticate` middleware (cookie or Bearer; scan token).
6. **Authz boundary** — `RequireTeamRole` / `RequireAdmin`. Within a team, handlers
   must additionally enforce that nested resources (project/order/build/etc.)
   belong to the `{teamId}` in the path — this is per-handler, not centralized.

## 6. Environmental assumptions

- A reverse proxy terminates TLS and is correctly configured; partrack sets no
  HSTS/CSP/security headers itself.
- `trusted_proxies` is set to the real proxy. If left empty, `RemoteAddr` is used
  (safe default); if set too broadly, clients can spoof source IP.
- `session_secret` is ≥32 bytes, random, and secret. A weak/empty secret lets an
  attacker forge OAuth `state` and HMAC link tokens.
- PostgreSQL is on a private network with least-privilege credentials and
  encrypted at rest (partrack does not encrypt secret columns itself).
- First admin is bootstrapped by direct SQL (`UPDATE users SET is_admin`).
- No production data / migration concerns yet (early-stage).

## 7. Entry points (attack surface)

- Unauthenticated: `POST /auth/login`, `GET/POST /auth/reset-password`,
  `GET /auth/oidc/{provider}` + `/callback`, `GET /oauth/callback/{apiType}`,
  `GET /categories`, `GET /parameter-definitions`, `GET /scan/stream?token=…`,
  `GET /health`, the SPA static handler.
- Authenticated, team-scoped JSON API (bulk of routes).
- Async job inputs: BOM/order import payloads, scan submissions, bulk-scan items.
- Outbound responses parsed as input: distributor search/enrich JSON, OIDC claims,
  OAuth token responses, barcode payloads from the phone.

## 8. Existing controls

- Passwords: bcrypt (`DefaultCost`). Tokens: 32-byte CSPRNG, only SHA-256 hash
  stored; raw shown once.
- Session cookie: `HttpOnly`, `SameSite=Lax`, `Expires` set; Bearer supported for
  API clients.
- OIDC: `state` CSRF cookie (constant-time compare), `nonce` bound to the login
  attempt, PKCE (S256) on the code exchange; `id_token` verified against the
  provider with issuer + audience pinning; redirect URI built from operator-set
  `app.base_url`, never request headers. Auto-link and auto-provision require
  `email_verified=true`; provisioning gated per provider.
- OAuth state & link tokens: HMAC-SHA256 with constant-time compare; link tokens
  carry expiry.
- RBAC: hierarchical roles enforced by middleware; queries are sqlc-generated and
  parameterized (low SQLi exposure).
- `Recoverer` catches panics; auth/membership cache reduces DB load.

## 9. Threats (STRIDE, terse)

| # | Threat | Vector | Current posture / risk |
|---|---|---|---|
| S1 | Credential brute force / password spray | `POST /auth/login`, reset endpoints | **No rate limiting / lockout** seen → open. |
| S2 | Session/API token theft | XSS, log leakage, network | HttpOnly + hashed-at-rest; SSE scan token is in the **URL query** → may land in proxy/access logs. |
| S3 | Forged OAuth `state` / link tokens | weak/empty `session_secret` | HMAC sound; hinges on secret strength (assumption). |
| S4 | OIDC assertion forgery / mis-binding | hostile IdP or token swap | Mitigated: id_token verified with issuer + audience pinning; nonce binds it to the browser's login attempt; PKCE on the exchange; linking/provisioning require a verified email (closes email-collision takeover). |
| T1 | Cross-team access (IDOR) | nested IDs (`projectId`, `orderId`, `itemId`) under a team the user *is* in, but resource belongs to another team | RequireTeamRole guards `{teamId}` only; **per-handler ownership checks must be verified** — primary review focus. |
| T2 | Privilege escalation across roles | calling higher-role route | Hierarchical middleware; verify every mutating route has the right `RequireTeamRole`. |
| T3 | Ledger / inventory tampering | adjustment, build, receive flows | Append-only ledger by design; verify no direct quantity overwrite path. |
| R1 | Repudiation | actions without attribution | Scan audit log exists; coverage of other mutations unverified. |
| I1 | Secret disclosure at rest | DB read, backup leak | Distributor `api_credentials` + OAuth tokens stored **plaintext** → high impact if DB/backup leaks. |
| I2 | Error/stack leakage | verbose errors | Generic error bodies used; confirm no internal detail in 500s. |
| I3 | SSRF / outbound abuse | issuer/base URLs, distributor calls | OIDC issuer & `app.base_url` are operator config; distributor endpoints fixed in code — low, confirm no user-controlled URL fetch. |
| D1 | DoS via unauthenticated/async work | login bcrypt, BOM/enrich jobs, SSE fan-out | In-process queue, no rate limits; single process → resource exhaustion possible. |
| E1 | Instance-admin abuse / bootstrap | `is_admin` grants all teams + password reset links | By design; ensure reset-link generation is admin-only and links are scoped + expiring. |
| Sp1 | Source IP spoofing | `X-Forwarded-For` with bad `trusted_proxies` | Mitigated by CIDR allowlist; misconfig risk only. |
| Ta1 | Hostile distributor/barcode data | parsed into parts/params, rendered in SPA | Validate output-encoding (stored XSS) and parser robustness in review. |

## 10. Out of scope

- Reverse proxy, OS, network, and PostgreSQL hardening configuration.
- Physical security and DB-at-rest encryption (assumed at infra layer).
- Supply-chain integrity of Go/npm dependencies (tracked separately).
- Availability guarantees / HA (single-process by design).

## 11. Open questions for the review

1. Do team-scoped handlers consistently verify nested resources belong to
   `{teamId}`? (T1 — highest priority.)
2. Should distributor credentials and OAuth tokens be encrypted at rest (I1)?
3. Add rate limiting / lockout on login and password-reset (S1)?
4. Scan SSE token in the query string (S2). EventSource can't set headers, and
   the phone joins by scanning a QR code that encodes the URL, so the token must
   survive in a form the QR/EventSource flow can carry. Options to weigh:
   (a) keep it in the URL but make scan tokens **short-lived** (e.g. minutes) so
   log exposure is low-value — possibly ephemeral / not persisted to the DB at
   all; (b) one-time bootstrap token in the URL that's immediately exchanged for
   an HttpOnly cookie used by subsequent calls; (c) scrub the token from access
   logs at the proxy. Whatever is chosen must not break QR-code pairing.
5. ~~Confirm OIDC issuer/audience pinning and email-collision handling on
   auto-provision (S4).~~ Resolved: issuer/audience pinned by go-oidc verifier;
   nonce + PKCE added; auto-link/auto-provision require `email_verified=true`.
6. Confirm no unauthenticated handler leaks user existence (login vs. reset
   responses) beyond acceptable levels.
</content>
</invoke>
