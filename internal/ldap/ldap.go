// Package ldap provides directory authentication (OpenLDAP / Active Directory)
// for partrack via the standard search-then-bind flow:
//
//  1. bind as a read-only service account,
//  2. search the user subtree for the supplied username,
//  3. re-bind as the found DN with the user's password to verify it,
//  4. read identity attributes (email, display name, stable id) and groups.
//
// A fresh connection is opened per Authenticate call and closed on return;
// LDAP connections are cheap and not safe to share across logins.
package ldap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Yamakaja/partrack/internal/config"
	"github.com/go-ldap/ldap/v3"
)

// ErrInvalidCredentials is returned for any authentication failure that must not
// be distinguishable to the caller (unknown user, wrong password, empty
// password). Operational failures (server unreachable, misconfiguration) return
// a different, wrapped error so they can be logged separately.
var ErrInvalidCredentials = errors.New("ldap: invalid credentials")

// Result is the normalized identity returned by a successful bind.
type Result struct {
	// Subject is the stable unique identifier (AttrUniqueID, e.g. entryUUID /
	// objectGUID). Binary values are encoded to a stable string (objectGUID to
	// its canonical form, other binary to hex). Falls back to the user's DN if
	// the attribute is absent.
	Subject string
	Email   string
	// DisplayName is the human name; falls back to the username if absent.
	DisplayName string
	// Groups are the DN strings from the member-of attribute or group search.
	Groups []string
	// DN is the user's distinguished name, recorded for diagnostics.
	DN string
	// SubjectFromDN is true when AttrUniqueID was empty and Subject fell back to
	// the DN (an unstable identifier). Surfaced as a diagnostics warning.
	SubjectFromDN bool
	// GroupSource notes how Groups were resolved ("memberOf" / "group search",
	// plus a "+ nested walk (N groups)" suffix when transitive expansion ran).
	GroupSource string
	// Via maps each resolved group DN to the child group DN it was first reached
	// through during nested expansion ("" means the user is a direct member). Used
	// by diagnostics to label membership as direct vs nested; login ignores it.
	Via map[string]string
	// GroupExistence reports, keyed by lower-cased DN, whether each probe group
	// (passed by the diagnostics path) exists in the directory. Nil on the
	// production login path, which never probes.
	GroupExistence map[string]DNCheck
}

// DNCheck reports whether a configured DN (group, base) exists in the directory.
type DNCheck struct {
	DN     string `json:"dn"`
	Exists bool   `json:"exists"`
	Err    string `json:"err,omitempty"`
}

// Step records one phase of a bind for the admin diagnostics tool. It is only
// populated when authentication runs via Diagnose; the production Authenticate
// path discards it.
type Step struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Err    string `json:"err,omitempty"`
}

// traceFunc receives each Step as it completes; nil on the production path.
type traceFunc func(Step)

// Authenticator binds against one configured directory.
type Authenticator struct {
	cfg     config.LDAPProvider
	timeout time.Duration
	caPool  *x509.CertPool // nil unless ca_cert_file is configured
	caErr   error          // deferred ca_cert_file load error, surfaced on dial
}

// New constructs an Authenticator for the given provider config, loading the CA
// certificate bundle if one is configured (errors are deferred to dial time so
// a bad path doesn't crash startup).
func New(cfg config.LDAPProvider) *Authenticator {
	a := &Authenticator{cfg: cfg, timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}
	if a.timeout <= 0 {
		a.timeout = 8 * time.Second
	}
	if cfg.CACertFile != "" {
		pem, err := os.ReadFile(cfg.CACertFile)
		switch {
		case err != nil:
			a.caErr = fmt.Errorf("read ca_cert_file: %w", err)
		default:
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				a.caErr = fmt.Errorf("ca_cert_file %s: no certificates could be parsed", cfg.CACertFile)
			} else {
				a.caPool = pool
			}
		}
	}
	return a
}

// Name returns the provider slug.
func (a *Authenticator) Name() string { return a.cfg.Name }

// Authenticate verifies username/password against the directory and returns the
// resolved identity. Credential failures return ErrInvalidCredentials; all
// other (operational) problems return a wrapped error.
func (a *Authenticator) Authenticate(ctx context.Context, username, password string) (*Result, error) {
	return a.authenticate(ctx, username, password, true, nil, nil)
}

// Diagnose runs the same flow as Authenticate but records a step-by-step trace
// for the admin diagnostics endpoint, and additionally checks the existence of
// each probeGroup DN (populating Result.GroupExistence). Steps are returned even
// on failure; the Result is nil unless the user bind succeeded. It performs no
// writes.
func (a *Authenticator) Diagnose(ctx context.Context, username, password string, probeGroups []string) (*Result, []Step, error) {
	var steps []Step
	res, err := a.authenticate(ctx, username, password, true, probeGroups, func(s Step) { steps = append(steps, s) })
	return res, steps, err
}

// Inspect resolves a user by username alone — search, identity, (transitive)
// groups, authorization and the probeGroup existence checks — but never binds as
// the user, so it needs no password. It lets an admin debug why a given account
// would or wouldn't be able to log in, without that user being present. Like
// Diagnose it is strictly read-only.
func (a *Authenticator) Inspect(ctx context.Context, username string, probeGroups []string) (*Result, []Step, error) {
	var steps []Step
	res, err := a.authenticate(ctx, username, "", false, probeGroups, func(s Step) { steps = append(steps, s) })
	return res, steps, err
}

// authenticate is the shared search-then-bind core. trace, when non-nil,
// receives a Step for each phase; production passes nil. probeGroups, when
// non-nil, requests existence checks for those DNs (diagnostics only). When
// verifyPassword is false (Inspect), the user re-bind is skipped and no password
// is required — every other lookup is identical, so the inspect, diagnose and
// login paths can never diverge.
func (a *Authenticator) authenticate(ctx context.Context, username, password string, verifyPassword bool, probeGroups []string, trace traceFunc) (*Result, error) {
	rec := func(name string, err error, detail string) {
		if trace == nil {
			return
		}
		s := Step{Name: name, OK: err == nil, Detail: detail}
		if err != nil {
			s.Err = err.Error()
		}
		trace(s)
	}

	// Reject empty credentials up front: RFC 4513 "unauthenticated bind" means a
	// valid DN with an empty password can succeed as an anonymous bind, which
	// would falsely authenticate the user. Inspect (verifyPassword=false) never
	// binds as the user, so it only needs a username.
	if username == "" || (verifyPassword && password == "") {
		rec("validate input", ErrInvalidCredentials, "username and password must both be non-empty")
		return nil, ErrInvalidCredentials
	}

	conn, err := a.dial(ctx)
	if err != nil {
		rec("connect", err, a.dialDetail())
		return nil, fmt.Errorf("ldap: dial %s: %w", a.cfg.URL, err)
	}
	defer conn.Close()
	rec("connect", nil, a.dialDetail())

	// Bind as the service account so we can search. An anonymous search is also
	// possible if bind_dn is empty.
	if a.cfg.BindDN != "" {
		if err := conn.Bind(a.cfg.BindDN, a.cfg.BindPassword); err != nil {
			rec("service bind", err, "bind_dn="+a.cfg.BindDN)
			return nil, fmt.Errorf("ldap: service bind failed: %w", err)
		}
		rec("service bind", nil, "bind_dn="+a.cfg.BindDN)
	} else {
		rec("service bind", nil, "anonymous (no bind_dn configured)")
	}

	// Search for the user. The username is escaped to prevent LDAP injection.
	filter := fmt.Sprintf(a.cfg.UserFilter, ldap.EscapeFilter(username))
	attrs := []string{a.cfg.AttrEmail, a.cfg.AttrDisplayName, a.cfg.AttrUniqueID, a.cfg.AttrMemberOf}
	search := ldap.NewSearchRequest(
		a.cfg.UserBaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, int(a.timeout.Seconds()), false,
		filter, attrs, nil,
	)
	res, err := conn.Search(search)
	if err != nil {
		rec("search", err, "base="+a.cfg.UserBaseDN+" filter="+filter)
		return nil, fmt.Errorf("ldap: search failed: %w", err)
	}
	// Zero or multiple matches: treat as an invalid login rather than leaking
	// which case occurred. The diagnostic trace records the true count.
	if len(res.Entries) != 1 {
		rec("search", ErrInvalidCredentials, fmt.Sprintf("base=%s filter=%s → %d entries (need exactly 1)", a.cfg.UserBaseDN, filter, len(res.Entries)))
		return nil, ErrInvalidCredentials
	}
	entry := res.Entries[0]
	rec("search", nil, fmt.Sprintf("filter=%s → 1 entry: %s", filter, entry.DN))

	// Re-bind as the user to verify the password — skipped in inspect mode, which
	// resolves the account without it.
	if verifyPassword {
		if err := conn.Bind(entry.DN, password); err != nil {
			if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
				rec("user bind", ErrInvalidCredentials, "password rejected for "+entry.DN)
				return nil, ErrInvalidCredentials
			}
			rec("user bind", err, "dn="+entry.DN)
			return nil, fmt.Errorf("ldap: user bind failed: %w", err)
		}
		rec("user bind", nil, "password accepted for "+entry.DN)
	} else {
		rec("user bind", nil, "skipped — inspection only, password not verified")
	}

	// The unique-id attribute may be binary (e.g. AD's objectGUID), so read raw
	// bytes and encode to a stable, valid-UTF-8 string before it becomes the
	// subject — a raw GUID stored in a TEXT column would be rejected as invalid
	// UTF-8 (or for its NUL bytes).
	subject := encodeUniqueID(a.cfg.AttrUniqueID, entry.GetRawAttributeValue(a.cfg.AttrUniqueID))
	subjectFromDN := subject == ""
	if subjectFromDN {
		subject = entry.DN // DNs can change, but better than no identity at all
	}
	displayName := entry.GetAttributeValue(a.cfg.AttrDisplayName)
	if displayName == "" {
		displayName = username
	}

	// The password is verified; everything that follows (group expansion,
	// existence probes) reads the directory as the service account, since the
	// user binding may lack read access to the group subtree.
	if err := a.bindService(conn); err != nil {
		rec("service re-bind", err, "bind_dn="+a.cfg.BindDN)
		return nil, fmt.Errorf("ldap: service re-bind failed: %w", err)
	}

	// Resolve groups (transitively, when enabled) and surface how it was done.
	groups, via, groupSource, gerr := a.resolveGroups(conn, entry)
	if gerr != nil {
		rec("group resolution", gerr, "base="+a.cfg.GroupBaseDN)
		return nil, fmt.Errorf("ldap: group resolution failed: %w", gerr)
	}
	rec("group resolution", nil, fmt.Sprintf("%d group(s) via %s", len(groups), groupSource))

	rec("read attributes", nil, fmt.Sprintf("%s=%s, %s=%s, %s=%s, %d group(s) via %s",
		a.cfg.AttrEmail, entry.GetAttributeValue(a.cfg.AttrEmail),
		a.cfg.AttrDisplayName, displayName,
		a.cfg.AttrUniqueID, subject, len(groups), groupSource))

	// Diagnostics existence probe: does each group partrack references actually
	// exist? De-duplicated case-insensitively; failures are recorded per DN, not
	// fatal.
	var existence map[string]DNCheck
	if probeGroups != nil {
		existence = make(map[string]DNCheck, len(probeGroups))
		for _, dn := range probeGroups {
			key := strings.ToLower(dn)
			if dn == "" || existence[key].DN != "" {
				continue
			}
			ok, derr := a.dnExists(conn, dn)
			c := DNCheck{DN: dn, Exists: ok}
			if derr != nil {
				c.Err = derr.Error()
			}
			existence[key] = c
		}
		rec("group existence", nil, fmt.Sprintf("checked %d group DN(s)", len(existence)))
	}

	return &Result{
		Subject:        subject,
		Email:          entry.GetAttributeValue(a.cfg.AttrEmail),
		DisplayName:    displayName,
		Groups:         groups,
		DN:             entry.DN,
		SubjectFromDN:  subjectFromDN,
		GroupSource:    groupSource,
		Via:            via,
		GroupExistence: existence,
	}, nil
}

// inChainMatchingRuleOID is Active Directory's LDAP_MATCHING_RULE_IN_CHAIN. When
// it appears in group_filter the reverse search already returns the transitive
// closure server-side, so partrack's own nested walk is redundant and skipped.
const inChainMatchingRuleOID = "1.2.840.113556.1.4.1941"

// Group-walk bounds: cap total groups discovered and nesting depth so a deep or
// pathological group graph can never make a login unboundedly slow.
const (
	maxGroupWalk  = 50
	maxGroupDepth = 10
)

// bindService binds the connection as the read-only service account, or no-ops
// when no bind_dn is configured (anonymous access).
func (a *Authenticator) bindService(conn *ldap.Conn) error {
	if a.cfg.BindDN == "" {
		return nil
	}
	return conn.Bind(a.cfg.BindDN, a.cfg.BindPassword)
}

// resolveGroups returns the user's group DNs, the "reached via" map for nested
// expansion, and a human label describing how resolution was done. The
// connection must already be bound as the service account. Direct membership is
// read from the memberOf attribute, or via a reverse group search when
// group_base_dn is set. When ExpandNestedGroups is on (and the filter is not
// already an in-chain rule) the direct set is expanded to its transitive closure
// of parent groups, which works on any directory.
func (a *Authenticator) resolveGroups(conn *ldap.Conn, entry *ldap.Entry) ([]string, map[string]string, string, error) {
	searchMode := a.cfg.GroupBaseDN != ""
	inChain := strings.Contains(a.cfg.GroupFilter, inChainMatchingRuleOID)
	expand := a.cfg.NestedGroupsEnabled() && !inChain

	var direct []string
	source := "memberOf"
	if searchMode {
		source = "group search"
		g, err := a.searchMembers(conn, entry.DN)
		if err != nil {
			return nil, nil, "", err
		}
		direct = g
	} else {
		direct = entry.GetAttributeValues(a.cfg.AttrMemberOf)
	}

	if !expand {
		via := make(map[string]string, len(direct))
		for _, d := range direct {
			via[d] = ""
		}
		if inChain {
			source += " (in-chain transitive)"
		}
		return direct, via, source, nil
	}

	var parentsOf func(string) ([]string, error)
	if searchMode {
		parentsOf = func(dn string) ([]string, error) { return a.searchMembers(conn, dn) }
	} else {
		parentsOf = func(dn string) ([]string, error) { return a.readGroupMemberOf(conn, dn) }
	}
	groups, via, soft := expandGroups(direct, parentsOf, maxGroupWalk, maxGroupDepth)
	source = fmt.Sprintf("%s + nested walk (%d groups)", source, len(groups))
	if soft != nil {
		source += " [partial: " + soft.Error() + "]"
	}
	return groups, via, source, nil
}

// searchMembers returns the DNs of groups that directly list memberDN, by
// applying group_filter under group_base_dn. The connection must already be
// service-bound. memberDN may be a user DN (direct groups) or a group DN
// (parent groups during nested expansion).
func (a *Authenticator) searchMembers(conn *ldap.Conn, memberDN string) ([]string, error) {
	filter := fmt.Sprintf(a.cfg.GroupFilter, ldap.EscapeFilter(memberDN))
	sr := ldap.NewSearchRequest(
		a.cfg.GroupBaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, int(a.timeout.Seconds()), false,
		filter, []string{"1.1"}, nil, // 1.1 = no attributes; we only want the DNs
	)
	res, err := conn.Search(sr)
	if err != nil {
		return nil, err
	}
	groups := make([]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		groups = append(groups, e.DN)
	}
	return groups, nil
}

// readGroupMemberOf returns the parent-group DNs listed in groupDN's memberOf
// attribute (memberOf-mode nested expansion). A missing entry yields no parents
// rather than an error, so one unreadable group never breaks the walk.
func (a *Authenticator) readGroupMemberOf(conn *ldap.Conn, groupDN string) ([]string, error) {
	sr := ldap.NewSearchRequest(
		groupDN,
		ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, int(a.timeout.Seconds()), false,
		"(objectClass=*)", []string{a.cfg.AttrMemberOf}, nil,
	)
	res, err := conn.Search(sr)
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			return nil, nil
		}
		return nil, err
	}
	if len(res.Entries) != 1 {
		return nil, nil
	}
	return res.Entries[0].GetAttributeValues(a.cfg.AttrMemberOf), nil
}

// expandGroups computes the transitive closure of parent groups starting from
// direct, using parentsOf to find the groups each group is a member of. It is
// bounded by maxGroups (total discovered) and maxDepth (nesting levels), and
// guards against cycles with a visited set keyed by lower-cased DN. The returned
// via map records, per discovered group (original-case DN), the child DN it was
// first reached through ("" for a direct membership). A non-nil error is the
// first parentsOf failure encountered; it is informational — expansion proceeds
// past failing nodes so a single unreadable group does not break login.
func expandGroups(direct []string, parentsOf func(dn string) ([]string, error), maxGroups, maxDepth int) ([]string, map[string]string, error) {
	via := map[string]string{}
	seen := map[string]bool{} // lower-cased DN
	var order []string        // discovery order, original case

	type qitem struct {
		dn    string
		depth int
	}
	var queue []qitem
	add := func(dn, from string, depth int) {
		key := strings.ToLower(dn)
		if seen[key] || len(order) >= maxGroups {
			return
		}
		seen[key] = true
		order = append(order, dn)
		via[dn] = from
		queue = append(queue, qitem{dn, depth})
	}

	for _, d := range direct {
		add(d, "", 0)
	}
	var firstErr error
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if it.depth >= maxDepth {
			continue
		}
		parents, err := parentsOf(it.dn)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, p := range parents {
			add(p, it.dn, it.depth+1)
		}
	}
	return order, via, firstErr
}

// TestConnection dials, negotiates TLS, and binds the service account — without
// any user credential — and optionally checks that each supplied DN exists. It
// returns the trace and per-DN results for the diagnostics tool.
func (a *Authenticator) TestConnection(ctx context.Context, dns []string) ([]Step, []DNCheck, error) {
	var steps []Step
	add := func(name string, err error, detail string) {
		s := Step{Name: name, OK: err == nil, Detail: detail}
		if err != nil {
			s.Err = err.Error()
		}
		steps = append(steps, s)
	}

	conn, err := a.dial(ctx)
	if err != nil {
		add("connect", err, a.dialDetail())
		return steps, nil, err
	}
	defer conn.Close()
	add("connect", nil, a.dialDetail())

	if a.cfg.BindDN != "" {
		if err := conn.Bind(a.cfg.BindDN, a.cfg.BindPassword); err != nil {
			add("service bind", err, "bind_dn="+a.cfg.BindDN)
			return steps, nil, err
		}
		add("service bind", nil, "bind_dn="+a.cfg.BindDN)
	} else {
		add("service bind", nil, "anonymous (no bind_dn configured)")
	}

	var checks []DNCheck
	for _, dn := range dns {
		if dn == "" {
			continue
		}
		c := DNCheck{DN: dn}
		exists, derr := a.dnExists(conn, dn)
		c.Exists = exists
		if derr != nil {
			c.Err = derr.Error()
		}
		checks = append(checks, c)
	}
	return steps, checks, nil
}

// dnExists reports whether dn resolves to an entry (base-scoped search).
func (a *Authenticator) dnExists(conn *ldap.Conn, dn string) (bool, error) {
	sr := ldap.NewSearchRequest(
		dn,
		ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, int(a.timeout.Seconds()), false,
		"(objectClass=*)", []string{"1.1"}, nil,
	)
	res, err := conn.Search(sr)
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			return false, nil
		}
		return false, err
	}
	return len(res.Entries) == 1, nil
}

// roleRank orders team roles so the strongest grant wins when multiple groups
// map to the same team.
var roleRank = map[string]int{"viewer": 0, "member": 1, "purchaser": 2, "admin": 3}

// ValidRole reports whether r is one of partrack's team roles.
func ValidRole(r string) bool { _, ok := roleRank[r]; return ok }

// encodeUniqueID turns a raw unique-id attribute value into a stable string
// safe to store in a TEXT column and to display. Already-printable text values
// (entryUUID, OIDC-style sub) pass through unchanged. AD's objectGUID is
// rendered in its canonical mixed-endian form so it matches AD tooling; any
// other binary value is hex-encoded. Empty input yields "" so the caller can
// fall back to the DN.
func encodeUniqueID(attr string, raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if isPrintableText(raw) {
		return string(raw)
	}
	if strings.EqualFold(attr, "objectGUID") && len(raw) == 16 {
		return formatADGUID(raw)
	}
	return hex.EncodeToString(raw)
}

// isPrintableText reports whether b is valid UTF-8 with no NUL or control
// characters (tab excepted) — i.e. usable as-is in a TEXT column.
func isPrintableText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r == 0 || (r < 0x20 && r != '\t') {
			return false
		}
	}
	return true
}

// formatADGUID renders a 16-byte Active Directory objectGUID as the canonical
// "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" string. AD stores the first three
// groups little-endian and the rest big-endian.
func formatADGUID(b []byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[3], b[2], b[1], b[0],
		b[5], b[4],
		b[7], b[6],
		b[8], b[9],
		b[10], b[11], b[12], b[13], b[14], b[15])
}

// MappingMatch is the per-rule outcome of evaluating group_mappings, for display
// in the diagnostics tool.
type MappingMatch struct {
	Group     string `json:"group"`
	Team      string `json:"team"`
	Role      string `json:"role"`
	Matched   bool   `json:"matched"`    // the user is a member of Group
	ValidRole bool   `json:"valid_role"` // Role is a known team role
}

// Decision is the authorization that the user's directory groups would produce,
// independent of the database. The handler applies it (subject to manual
// overrides); the admin tool displays it.
type Decision struct {
	// RequiredGroupConfigured is true when required_groups gates login.
	RequiredGroupConfigured bool `json:"required_group_configured"`
	// LoginAllowed is false only when required_groups is configured and the user
	// is in none of them; such a user must be denied login (and not provisioned).
	LoginAllowed bool `json:"login_allowed"`
	// AdminGroupConfigured is true when admin_group is set, meaning the instance
	// admin flag is directory-controlled.
	AdminGroupConfigured bool `json:"admin_group_configured"`
	IsAdmin              bool `json:"is_admin"`
	// TeamRoles maps team name → the highest role the user's groups grant.
	TeamRoles map[string]string `json:"team_roles"`
	// ManagedTeams is every team named by a mapping (sorted), whether or not the
	// user currently qualifies; teams here but absent from TeamRoles are the ones
	// directory sync would remove.
	ManagedTeams []string `json:"managed_teams"`
	// Mappings is the per-rule breakdown.
	Mappings []MappingMatch `json:"mappings"`
}

// EvaluateGroups computes the authorization a user's directory groups grant,
// using only the provider config — no database access. Group DNs are matched
// case-insensitively.
func EvaluateGroups(cfg config.LDAPProvider, groups []string) Decision {
	have := make(map[string]bool, len(groups))
	for _, g := range groups {
		have[strings.ToLower(g)] = true
	}

	d := Decision{
		RequiredGroupConfigured: len(cfg.RequiredGroups) > 0,
		AdminGroupConfigured:    cfg.AdminGroup != "",
		TeamRoles:               map[string]string{},
		// Non-nil so they marshal as [] not null; the diagnostics UI reads
		// .length on both. An install with no group_mappings has neither.
		ManagedTeams: []string{},
		Mappings:     []MappingMatch{},
	}
	// Login is allowed when ungated, or when the user is in any one of the
	// allowed groups (membership counts nested groups; see resolveGroups).
	d.LoginAllowed = !d.RequiredGroupConfigured
	for _, rg := range cfg.RequiredGroups {
		if have[strings.ToLower(rg)] {
			d.LoginAllowed = true
			break
		}
	}
	if d.AdminGroupConfigured {
		d.IsAdmin = have[strings.ToLower(cfg.AdminGroup)]
	}

	managed := map[string]bool{}
	for _, m := range cfg.GroupMappings {
		managed[m.Team] = true
		// An empty group is a catch-all: every authenticated directory user
		// qualifies. This lets an install auto-provision everyone to a baseline
		// team/role that admins can then individually override.
		matched := m.Group == "" || have[strings.ToLower(m.Group)]
		valid := ValidRole(m.Role)
		d.Mappings = append(d.Mappings, MappingMatch{
			Group: m.Group, Team: m.Team, Role: m.Role, Matched: matched, ValidRole: valid,
		})
		if !matched || !valid {
			continue
		}
		if cur, ok := d.TeamRoles[m.Team]; !ok || roleRank[m.Role] > roleRank[cur] {
			d.TeamRoles[m.Team] = m.Role
		}
	}
	for t := range managed {
		d.ManagedTeams = append(d.ManagedTeams, t)
	}
	sort.Strings(d.ManagedTeams)
	return d
}

// dialDetail describes the transport for the connect trace step.
func (a *Authenticator) dialDetail() string {
	d := a.cfg.URL
	if a.cfg.StartTLS {
		d += " (StartTLS)"
	}
	if a.cfg.InsecureSkipVerify {
		d += " [TLS verification disabled]"
	}
	return d
}

// dial opens a connection honouring ldaps:// vs ldap:// + StartTLS, the CA
// bundle, and the configured TLS verification policy.
func (a *Authenticator) dial(ctx context.Context) (*ldap.Conn, error) {
	if a.caErr != nil {
		return nil, a.caErr
	}
	tlsCfg := &tls.Config{ //nolint:gosec // InsecureSkipVerify is gated by explicit config flag
		InsecureSkipVerify: a.cfg.InsecureSkipVerify,
		RootCAs:            a.caPool,
	}
	conn, err := ldap.DialURL(a.cfg.URL, ldap.DialWithTLSConfig(tlsCfg))
	if err != nil {
		return nil, err
	}
	conn.SetTimeout(a.timeout)
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetTimeout(time.Until(deadline))
	}
	if a.cfg.StartTLS {
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close()
			return nil, fmt.Errorf("starttls: %w", err)
		}
	}
	return conn, nil
}
