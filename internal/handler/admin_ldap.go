package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Yamakaja/partrack/internal/config"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	pkgldap "github.com/Yamakaja/partrack/internal/ldap"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// AdminLDAPProviders handles GET /api/v1/admin/ldap/providers
// Lists configured directories so the diagnostics page can offer a picker.
func (h *Handler) AdminLDAPProviders(w http.ResponseWriter, r *http.Request) {
	type item struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		URL         string `json:"url"`
	}
	items := make([]item, 0, len(h.Config.LDAP))
	for _, p := range h.Config.LDAP {
		dn := p.DisplayName
		if dn == "" {
			dn = p.Name
		}
		items = append(items, item{Name: p.Name, DisplayName: dn, URL: p.URL})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": items})
}

// ldapTeamResolution is the per-team outcome a real login would produce.
type ldapTeamResolution struct {
	Team                    string `json:"team"`
	Exists                  bool   `json:"exists"`
	IntendedRole            string `json:"intended_role,omitempty"` // empty ⇒ directory would remove
	Action                  string `json:"action"`
	CurrentRole             string `json:"current_role,omitempty"`
	CurrentDirectoryManaged *bool  `json:"current_directory_managed,omitempty"`
}

// ldapTeamMembership is one of the resolved user's current DB team memberships,
// shown so an admin can compare the live state against what login would do.
type ldapTeamMembership struct {
	Team             string `json:"team"`
	Role             string `json:"role"`
	DirectoryManaged bool   `json:"directory_managed"`
}

// ldapResolution is the read-only account/authorization outcome of a login.
type ldapResolution struct {
	Subject              string               `json:"subject"`
	Outcome              string               `json:"outcome"`
	LinkedUserID         string               `json:"linked_user_id,omitempty"`
	LinkedUserEmail      string               `json:"linked_user_email,omitempty"`
	ExistingUserByEmail  string               `json:"existing_user_by_email_id,omitempty"`
	ExistingUserDisplay  string               `json:"existing_user_display_name,omitempty"`
	AutoProvisionEnabled bool                 `json:"auto_provision_enabled"`
	Teams                []ldapTeamResolution `json:"teams"`
	// TeamMemberships is the resolved user's current memberships (empty when no
	// existing user is matched). Always non-nil so the UI can read .length.
	TeamMemberships []ldapTeamMembership `json:"team_memberships"`
}

// ldapGroupCheck answers, for one group DN partrack references, the two
// questions an admin debugs: does the group exist in the directory, and is this
// user a member of it (directly or via nesting)?
type ldapGroupCheck struct {
	DN        string   `json:"dn"`
	Purposes  []string `json:"purposes"` // why partrack cares: "login", "admin", "team: X (role)"
	Exists    bool     `json:"exists"`
	ExistsErr string   `json:"exists_err,omitempty"`
	Member    bool     `json:"member"`
	MemberVia string   `json:"member_via,omitempty"` // "direct" or "nested via <dn>"
}

// AdminLDAPDiagnose handles POST /api/v1/admin/ldap/diagnose
// Body: {provider, username, password}. Runs the real search-then-bind and
// group evaluation and returns a step-by-step report. It is strictly
// read-only: no user, identity, membership, or session is ever created — it
// only reports what a real login *would* do.
func (h *Handler) AdminLDAPDiagnose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Provider == "" || req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "provider, username and password are required", "BAD_REQUEST")
		return
	}

	entry, ok := h.ldapEntries[req.Provider]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown LDAP provider", "NOT_FOUND")
		return
	}

	result, steps, err := entry.auth.Diagnose(r.Context(), req.Username, req.Password, ldapProbeGroups(entry.cfg))
	writeJSON(w, http.StatusOK, h.ldapReport(r.Context(), req.Provider, entry.cfg, result, steps, err, true))
}

// AdminLDAPInspect handles POST /api/v1/admin/ldap/inspect
// Body: {provider, username}. Resolves an account by username alone — search,
// identity, groups, authorization, group existence and account resolution —
// without binding as the user, so no password is needed and the user need not
// be present. For debugging why a specific account can or cannot log in. Like
// diagnose it is strictly read-only.
func (h *Handler) AdminLDAPInspect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Provider == "" || req.Username == "" {
		writeError(w, http.StatusBadRequest, "provider and username are required", "BAD_REQUEST")
		return
	}

	entry, ok := h.ldapEntries[req.Provider]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown LDAP provider", "NOT_FOUND")
		return
	}

	result, steps, err := entry.auth.Inspect(r.Context(), req.Username, ldapProbeGroups(entry.cfg))
	writeJSON(w, http.StatusOK, h.ldapReport(r.Context(), req.Provider, entry.cfg, result, steps, err, false))
}

// ldapReport renders the shared diagnose/inspect response from a directory
// lookup. verified records whether the user's password was checked (diagnose)
// vs skipped (inspect); it is surfaced as password_verified so the UI can label
// the outcome. On error the report carries only the trace and a message.
func (h *Handler) ldapReport(ctx context.Context, provider string, cfg config.LDAPProvider, result *pkgldap.Result, steps []pkgldap.Step, err error, verified bool) map[string]any {
	resp := map[string]any{
		"provider":          provider,
		"password_verified": verified,
		"steps":             steps,
	}
	if err != nil {
		switch {
		case !errors.Is(err, pkgldap.ErrInvalidCredentials):
			// The admin owns this directory, so surfacing the operational error
			// (TLS, unreachable host, misconfigured bind DN) is the whole point.
			resp["error"] = err.Error()
		case verified:
			resp["error"] = "invalid credentials"
		default:
			// Inspect never checks a password, so this only means the search did
			// not resolve to exactly one entry.
			resp["error"] = "user not found, or the filter matched zero or multiple entries (see trace)"
		}
		return resp
	}

	decision := pkgldap.EvaluateGroups(cfg, result.Groups)
	groups := result.Groups
	if groups == nil {
		// Marshal as [] not null — the diagnostics UI reads groups.length.
		groups = []string{}
	}
	resp["identity"] = map[string]any{
		"subject":      result.Subject,
		"email":        result.Email,
		"display_name": result.DisplayName,
		"dn":           result.DN,
		"groups":       groups,
	}
	resp["authorization"] = decision
	resp["group_checks"] = ldapGroupChecks(cfg, result)
	resp["resolution"] = h.resolveLDAPDiagnostics(ctx, cfg, result, decision)
	resp["warnings"] = ldapWarnings(cfg, result, decision)
	return resp
}

// ldapProbeGroups is the set of group DNs partrack references (login gate, admin
// group, team mappings), de-duplicated case-insensitively, whose existence the
// diagnostics tool checks against the directory.
func ldapProbeGroups(cfg config.LDAPProvider) []string {
	seen := map[string]bool{}
	var out []string
	add := func(dn string) {
		key := strings.ToLower(dn)
		if dn == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, dn)
	}
	for _, g := range cfg.RequiredGroups {
		add(g)
	}
	add(cfg.AdminGroup)
	for _, m := range cfg.GroupMappings {
		add(m.Group)
	}
	return out
}

// ldapGroupChecks joins directory existence (from result.GroupExistence) with
// the user's resolved (transitive) membership to answer, per referenced group,
// "does it exist and is this user in it?" Groups are keyed case-insensitively;
// a single group may serve multiple purposes (e.g. login gate and a team map).
func ldapGroupChecks(cfg config.LDAPProvider, result *pkgldap.Result) []ldapGroupCheck {
	member := make(map[string]bool, len(result.Groups))
	for _, g := range result.Groups {
		member[strings.ToLower(g)] = true
	}
	via := make(map[string]string, len(result.Via))
	for dn, from := range result.Via {
		via[strings.ToLower(dn)] = from
	}

	byKey := map[string]*ldapGroupCheck{}
	order := []*ldapGroupCheck{}
	add := func(dn, purpose string) {
		if dn == "" {
			return
		}
		key := strings.ToLower(dn)
		gc := byKey[key]
		if gc == nil {
			memberVia := ""
			if member[key] {
				if v := via[key]; v == "" {
					memberVia = "direct"
				} else {
					memberVia = "nested via " + v
				}
			}
			ex := result.GroupExistence[key]
			gc = &ldapGroupCheck{
				DN:        dn,
				Purposes:  []string{},
				Exists:    ex.Exists,
				ExistsErr: ex.Err,
				Member:    member[key],
				MemberVia: memberVia,
			}
			byKey[key] = gc
			order = append(order, gc)
		}
		gc.Purposes = append(gc.Purposes, purpose)
	}

	for _, g := range cfg.RequiredGroups {
		add(g, "login")
	}
	add(cfg.AdminGroup, "admin")
	for _, m := range cfg.GroupMappings {
		add(m.Group, fmt.Sprintf("team: %s (%s)", m.Team, m.Role))
	}

	out := make([]ldapGroupCheck, 0, len(order))
	for _, gc := range order {
		out = append(out, *gc)
	}
	return out
}

// ldapWarnings flags likely misconfigurations from an otherwise-successful test.
func ldapWarnings(cfg config.LDAPProvider, result *pkgldap.Result, decision pkgldap.Decision) []string {
	var w []string
	usesGroups := decision.RequiredGroupConfigured || decision.AdminGroupConfigured || len(cfg.GroupMappings) > 0
	if usesGroups && len(result.Groups) == 0 {
		hint := "the directory returned no groups for this user"
		if cfg.GroupBaseDN == "" {
			hint += "; if it does not populate memberOf, configure group_base_dn/group_filter"
		}
		w = append(w, hint)
	}
	if result.SubjectFromDN {
		w = append(w, "attr_unique_id ("+cfg.AttrUniqueID+") returned no value; using the DN as the stable subject, which breaks identity linking if the entry is moved or renamed")
	}
	if result.Email == "" {
		w = append(w, "no email attribute ("+cfg.AttrEmail+"); auto-linking and provisioning are impossible without it")
	}
	if len(cfg.EmailDomains) > 0 && !cfg.StripDomain && strings.Contains(cfg.UserFilter, "uid=") {
		w = append(w, "provider routes by email_domains but user_filter matches uid; set strip_domain: true or use an email-based filter like (mail=%s)")
	}
	return w
}

// AdminLDAPTestConnection handles POST /api/v1/admin/ldap/test-connection
// Body: {provider}. Verifies connectivity (dial + TLS + service bind) without
// any user credential, checks that the configured group/base DNs exist, and
// echoes the resolved provider config (password redacted) so env overrides can
// be confirmed. Strictly read-only.
func (h *Handler) AdminLDAPTestConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required", "BAD_REQUEST")
		return
	}
	entry, ok := h.ldapEntries[req.Provider]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown LDAP provider", "NOT_FOUND")
		return
	}

	// DNs worth confirming: the user/group search bases and every group DN
	// referenced by the access/admin gates and team mappings.
	dnSet := map[string]bool{}
	addDN := func(dn string) {
		if dn != "" {
			dnSet[dn] = true
		}
	}
	addDN(entry.cfg.UserBaseDN)
	addDN(entry.cfg.GroupBaseDN)
	for _, g := range entry.cfg.RequiredGroups {
		addDN(g)
	}
	addDN(entry.cfg.AdminGroup)
	for _, m := range entry.cfg.GroupMappings {
		addDN(m.Group)
	}
	dns := make([]string, 0, len(dnSet))
	for dn := range dnSet {
		dns = append(dns, dn)
	}

	steps, checks, err := entry.auth.TestConnection(r.Context(), dns)
	resp := map[string]any{
		"provider":  req.Provider,
		"connected": err == nil,
		"steps":     steps,
		"dn_checks": checks,
		"config":    redactedLDAPConfig(entry.cfg),
	}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// redactedLDAPConfig echoes the effective provider config with the bind password
// reduced to a set/unset marker, so an admin can confirm env overrides applied.
func redactedLDAPConfig(cfg config.LDAPProvider) map[string]any {
	bindPw := "unset"
	if cfg.BindPassword != "" {
		bindPw = "set"
	}
	return map[string]any{
		"url":                  cfg.URL,
		"start_tls":            cfg.StartTLS,
		"insecure_skip_verify": cfg.InsecureSkipVerify,
		"ca_cert_file":         cfg.CACertFile,
		"timeout_seconds":      cfg.TimeoutSeconds,
		"bind_dn":              cfg.BindDN,
		"bind_password":        bindPw,
		"user_base_dn":         cfg.UserBaseDN,
		"user_filter":          cfg.UserFilter,
		"email_domains":        cfg.EmailDomains,
		"strip_domain":         cfg.StripDomain,
		"attr_email":           cfg.AttrEmail,
		"attr_display_name":    cfg.AttrDisplayName,
		"attr_unique_id":       cfg.AttrUniqueID,
		"attr_member_of":       cfg.AttrMemberOf,
		"group_base_dn":        cfg.GroupBaseDN,
		"group_filter":         cfg.GroupFilter,
		"required_groups":      cfg.RequiredGroups,
		"expand_nested_groups": cfg.NestedGroupsEnabled(),
		"admin_group":          cfg.AdminGroup,
		"auto_provision":       cfg.AutoProvision,
		"group_mappings":       cfg.GroupMappings,
	}
}

// resolveLDAPDiagnostics performs read-only DB lookups to turn the directory
// result into the concrete outcome a login would have. It never writes.
func (h *Handler) resolveLDAPDiagnostics(ctx context.Context, cfg config.LDAPProvider, result *pkgldap.Result, decision pkgldap.Decision) ldapResolution {
	res := ldapResolution{
		Subject:              result.Subject,
		AutoProvisionEnabled: cfg.AutoProvision,
		// Non-nil so they marshal as [] not null — the frontend reads .length
		// unconditionally, and a user in no mapped team has zero of each.
		Teams:           []ldapTeamResolution{},
		TeamMemberships: []ldapTeamMembership{},
	}

	// Login gate short-circuits everything: a denied user is neither resolved nor
	// provisioned, and no team sync runs.
	if decision.RequiredGroupConfigured && !decision.LoginAllowed {
		res.Outcome = "rejected_not_in_login_group"
		return res
	}

	var targetUser pgtype.UUID
	userKnown := false

	// 1. An identity already linked to (provider, subject) → that user logs in.
	identity, err := h.Queries.GetExternalIdentity(ctx, dbgen.GetExternalIdentityParams{
		Provider: cfg.Name,
		Subject:  result.Subject,
	})
	switch {
	case err == nil:
		res.Outcome = "login_existing_identity"
		res.LinkedUserID = identity.UserID.String()
		targetUser, userKnown = identity.UserID, true
		if u, uerr := h.Queries.GetUserWithMemberships(ctx, identity.UserID); uerr == nil {
			res.LinkedUserEmail = u.Email
		}
	case !errors.Is(err, pgx.ErrNoRows):
		res.Outcome = "error"
	case result.Email == "":
		// No identity and no email: cannot link or provision.
		res.Outcome = "rejected_no_email"
	default:
		// 2. Auto-link to an existing user with the same email, else
		// 3. auto-provision (if enabled).
		existing, eerr := h.Queries.GetUserByEmail(ctx, result.Email)
		switch {
		case eerr == nil:
			res.Outcome = "link_existing_user"
			res.ExistingUserByEmail = existing.ID.String()
			res.ExistingUserDisplay = existing.DisplayName
			targetUser, userKnown = existing.ID, true
		case !errors.Is(eerr, pgx.ErrNoRows):
			res.Outcome = "error"
		case cfg.AutoProvision:
			res.Outcome = "provision_new_user"
		default:
			res.Outcome = "rejected_provisioning_disabled"
		}
	}

	// Per-team resolution for every directory-managed team.
	for _, teamName := range decision.ManagedTeams {
		tr := ldapTeamResolution{Team: teamName}
		role, granted := decision.TeamRoles[teamName]
		tr.IntendedRole = role

		team, terr := h.Queries.GetTeamByName(ctx, teamName)
		if errors.Is(terr, pgx.ErrNoRows) {
			tr.Action = "skip — team does not exist"
			res.Teams = append(res.Teams, tr)
			continue
		}
		if terr != nil {
			tr.Action = "error resolving team"
			res.Teams = append(res.Teams, tr)
			continue
		}
		tr.Exists = true
		if granted {
			tr.Action = "grant/update to " + role
		} else {
			tr.Action = "remove (directory-managed only)"
		}

		// If we know the target user, show their current membership so the admin
		// can see when a manual override would be preserved.
		if userKnown {
			if md, merr := h.Queries.GetMembershipDetail(ctx, dbgen.GetMembershipDetailParams{
				TeamID: team.ID,
				UserID: targetUser,
			}); merr == nil {
				tr.CurrentRole = string(md.Role)
				dm := md.DirectoryManaged
				tr.CurrentDirectoryManaged = &dm
				if !dm {
					tr.Action = "manual override — preserved (no change)"
				}
			}
		}
		res.Teams = append(res.Teams, tr)
	}

	// The resolved user's current memberships (the live DB truth), so the admin
	// can compare what login would do against where the user already sits.
	if userKnown {
		if mships, merr := h.Queries.ListUserTeams(ctx, targetUser); merr == nil {
			for _, m := range mships {
				res.TeamMemberships = append(res.TeamMemberships, ldapTeamMembership{
					Team:             m.Name,
					Role:             string(m.Role),
					DirectoryManaged: m.DirectoryManaged,
				})
			}
		}
	}
	return res
}
