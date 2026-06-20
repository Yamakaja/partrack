# Roles & permissions

partrack has two independent permission layers: your **role within each team**, and an optional **instance-admin** flag that applies across the entire deployment. Understanding both helps you grant the right level of access and troubleshoot why a particular action may be blocked.

## Two permission layers

**Team roles** control what you can do within one team. Because you can belong to multiple teams (see [Getting started](getting-started.md)), you hold a separate role in each one — you could be a purchaser in your main engineering team and a viewer in a partner team you observe but do not operate.

**Instance admin** is a deployment-wide flag that overrides team roles entirely. An instance admin can act as a team admin in any team regardless of whether they appear on its member list. This flag is intended for the person (or small group) who operates the partrack installation — not for ordinary users.

---

## Team roles

Within a team you hold exactly one role. Roles are cumulative: each role includes everything the roles below it can do.

| Role | Level | Description |
|---|---|---|
| **Viewer** | 1 | Read-only access. Browse everything — parts, inventory, storage, projects, BOMs, builds, orders, checkouts — but make no changes. |
| **Member** | 2 | Day-to-day operations. Everything a viewer can do, plus creating and modifying content: adjust stock, manage parts and inventory, import and resolve BOMs, create and check out builds, record general checkouts, and use the scanning tools. |
| **Purchaser** | 3 | Procurement. Everything a member can do, plus the cart and orders: view and modify the cart, convert it to orders, receive order items, manage OAuth connections to distributors, and control which pending builds contribute to purchase demand. |
| **Admin** | 4 | Team administration. Everything a purchaser can do, plus managing the team itself: roster changes, role assignments, team settings, and distributor configuration. |

---

## Permission matrix

The table below maps concrete actions to the minimum role required. Actions not listed here are available to any authenticated team member at the indicated base level (viewer for all read operations).

| Action | Minimum role |
|---|---|
| **Browse parts, inventory, projects, BOMs, builds, orders, checkouts** | Viewer |
| **Run a stock check** | Viewer |
| **Read activity log (own actions only)** | Viewer |
| **Read the team-wide activity log** | Purchaser |
| **Global search** | Viewer |
| **Live updates (SSE event stream)** | Viewer |
| **Adjust stock (manual correction, receive scan)** | Member |
| **Create and update inventory entries** | Member |
| **Create and manage storage locations** | Member |
| **Create and update projects** | Member |
| **Import a BOM revision** | Member |
| **Resolve BOM line items and manage substitutes** | Member |
| **Create and submit builds** | Member |
| **Check out / assemble a build** | Member |
| **Cancel a build** | Member |
| **Record a general checkout** | Member |
| **Add and manage part notes, flags, and documents** | Member |
| **Add a team-scoped part to the catalog** | Member |
| **Enrich a part from a distributor** | Member |
| **Manage distributor listings on a part** | Member |
| **Use scan sessions (mobile scanning)** | Member |
| **Use bulk-scan and kitting** | Member |
| **Add parts to the catalog (global / shared scope)** | Instance admin only |
| **Edit parameters on a global part** | Instance admin only |
| **Delete a part** | Instance admin only |
| **View and manage the shopping cart** | Purchaser |
| **Convert the cart to orders** | Purchaser |
| **Import, update, submit, and cancel orders** | Purchaser |
| **Receive order items** | Purchaser |
| **Export an order** | Purchaser |
| **Toggle whether a build contributes to cart demand** | Purchaser |
| **Connect / disconnect distributor OAuth** | Purchaser |
| **Manage scan-session context (purchaser push)** | Purchaser |
| **View and manage team members and roles** | Team admin |
| **Update team name and settings** | Team admin |
| **Configure team distributor credentials** | Team admin |
| **Deactivate a user account** | Team admin |
| **Create a new team** | Instance admin only |
| **Manage user accounts** | Instance admin only |
| **Manage parameter definitions** | Instance admin only |
| **View server metrics and diagnostics** | Instance admin only |
| **Configure and diagnose LDAP providers** | Instance admin only |

> **Note on the activity log:** `GET /teams/{teamId}/activity-log` without a scope parameter returns only your own activity (viewer+). Adding `?scope=team` to see the full team-wide feed requires purchaser or above. The server enforces this gate in addition to the route-level role middleware.

---

## Managing team members

Only a **team admin** (or an instance admin) can manage the team roster. Navigate to **Settings → Members** within the team to:

- **Add a member** — search for any existing partrack user by name or email and assign them a role. Users must already have a partrack account; account creation is an instance-admin task.
- **Change a role** — use the role dropdown next to any member. You cannot change your own role.
- **Remove a member** — click the remove button. The user loses access to the team immediately on their next request (the auth cache may keep the old role for a short period).

When assigning roles, follow the principle of least privilege:

- Most contributors only need **member**.
- Only people who actually place orders need **purchaser**.
- Keep **admin** to a small group — it controls the roster and financial configuration.

### Directory-managed memberships

When your installation is connected to an LDAP or Active Directory, some memberships are labelled **Directory** in the members list. This means partrack last set the role from a directory group mapping. Changing the role manually in the UI **pins** that membership: subsequent directory syncs will not override it. Once pinned, it behaves like any other manual membership and the Directory badge disappears.

See [LDAP / Active Directory assignment](#ldap--active-directory-assignment) below for how mappings work.

---

## Instance admin

The instance-admin flag is distinct from any team role. It:

- Bypasses all team-role checks, granting effective team-admin access to every team.
- Unlocks the **Administration** pages (user management, parameter definitions, metrics, LDAP diagnostics, and more).
- Is required to create new teams, create global (shared) parts, edit parameters on global parts, and delete parts.

The instance admin flag is set on a user account by another instance admin via **Administration → Users**. It is not a team role and does not appear on team member lists.

For a full description of what instance admins can do, see [Administration](administration.md).

> **Instance admin and the activity log scope gate:** the server documentation notes that instance admins also bypass the purchaser-gate on team-wide activity logs.

---

## API tokens

API tokens authenticate as the user who created them and carry exactly that user's permissions — the same team roles and instance-admin flag. A token issued by a member-level user cannot perform purchaser or admin actions, and a token issued by an instance admin has the same deployment-wide access as that user's browser session.

Manage your tokens under your account settings (accessible from the user menu). Each token is shown exactly once at creation; store it securely.

---

## LDAP / Active Directory assignment

When LDAP is configured, team roles can be assigned automatically from directory groups. The mapping works as follows:

- Each `group_mappings` entry in the LDAP configuration ties a directory group DN to a partrack team name and role.
- On every LDAP login, partrack sets the user's membership in each mapped team to the strongest role their current group membership grants. If the user is in no mapped group for a team, their directory-managed membership in that team is removed.
- The **instance-admin** flag can likewise be driven by an `admin_group` in the LDAP config: members of that group get the flag granted; non-members have it revoked on login.

Because the sync runs at login time, directory changes take effect on the user's **next sign-in**, not immediately.

**Manual overrides are preserved.** Once a team admin changes or adds a membership in the UI, that row is pinned as manual and directory syncs will not override it. This is the recommended workflow for catch-all provisioning: auto-assign everyone to a baseline role via the directory, then individually promote the few who need more access.

The exception is the instance-admin flag: when `admin_group` is configured it is fully directory-authoritative and is granted or revoked on every login — it cannot be manually pinned. Either manage admins through the directory group, or leave `admin_group` unset to control the flag entirely by hand.

For configuration details and the diagnostics tool, see [Administration](administration.md).

---

## Choosing roles in practice

| Persona | Recommended role |
|---|---|
| Stakeholder who needs to see stock and build status | Viewer |
| PCB designer importing BOMs and adjusting stock | Member |
| Lab technician doing builds and kitting | Member |
| Purchasing agent placing and receiving orders | Purchaser |
| Team lead managing rosters and distributor accounts | Admin |
| Site / IT administrator managing the deployment | Instance admin |
