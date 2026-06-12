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
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

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
	// objectGUID). Falls back to the user's DN if the attribute is absent.
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
	// GroupSource notes how Groups were resolved ("memberOf" or "group search").
	GroupSource string
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
	return a.authenticate(ctx, username, password, nil)
}

// Diagnose runs the same flow as Authenticate but records a step-by-step trace
// for the admin diagnostics endpoint. Steps are returned even on failure; the
// Result is nil unless the user bind succeeded. It performs no writes.
func (a *Authenticator) Diagnose(ctx context.Context, username, password string) (*Result, []Step, error) {
	var steps []Step
	res, err := a.authenticate(ctx, username, password, func(s Step) { steps = append(steps, s) })
	return res, steps, err
}

// authenticate is the shared search-then-bind core. trace, when non-nil,
// receives a Step for each phase; production passes nil. Return values and
// error semantics are identical regardless of trace, so diagnostics never alter
// real login behaviour.
func (a *Authenticator) authenticate(ctx context.Context, username, password string, trace traceFunc) (*Result, error) {
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
	// would falsely authenticate the user.
	if username == "" || password == "" {
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

	// Re-bind as the user to verify the password.
	if err := conn.Bind(entry.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			rec("user bind", ErrInvalidCredentials, "password rejected for "+entry.DN)
			return nil, ErrInvalidCredentials
		}
		rec("user bind", err, "dn="+entry.DN)
		return nil, fmt.Errorf("ldap: user bind failed: %w", err)
	}
	rec("user bind", nil, "password accepted for "+entry.DN)

	subject := entry.GetAttributeValue(a.cfg.AttrUniqueID)
	subjectFromDN := subject == ""
	if subjectFromDN {
		subject = entry.DN // DNs can change, but better than no identity at all
	}
	displayName := entry.GetAttributeValue(a.cfg.AttrDisplayName)
	if displayName == "" {
		displayName = username
	}

	// Resolve groups: a reverse search (group_base_dn) when configured — needed
	// for directories that don't populate memberOf or to expand nested groups —
	// otherwise the memberOf attribute read off the user entry.
	var groups []string
	groupSource := "memberOf"
	if a.cfg.GroupBaseDN != "" {
		groupSource = "group search"
		g, gerr := a.searchGroups(conn, entry.DN)
		if gerr != nil {
			rec("group search", gerr, "base="+a.cfg.GroupBaseDN)
			return nil, fmt.Errorf("ldap: group search failed: %w", gerr)
		}
		groups = g
		rec("group search", nil, fmt.Sprintf("base=%s → %d group(s)", a.cfg.GroupBaseDN, len(groups)))
	} else {
		groups = entry.GetAttributeValues(a.cfg.AttrMemberOf)
	}

	rec("read attributes", nil, fmt.Sprintf("%s=%s, %s=%s, %s=%s, %d group(s) via %s",
		a.cfg.AttrEmail, entry.GetAttributeValue(a.cfg.AttrEmail),
		a.cfg.AttrDisplayName, displayName,
		a.cfg.AttrUniqueID, subject, len(groups), groupSource))

	return &Result{
		Subject:       subject,
		Email:         entry.GetAttributeValue(a.cfg.AttrEmail),
		DisplayName:   displayName,
		Groups:        groups,
		DN:            entry.DN,
		SubjectFromDN: subjectFromDN,
		GroupSource:   groupSource,
	}, nil
}

// searchGroups resolves a user's groups by reverse search under GroupBaseDN. The
// connection is re-bound as the service account first (the user binding may lack
// read access to the group subtree). The user DN is escaped into GroupFilter.
func (a *Authenticator) searchGroups(conn *ldap.Conn, userDN string) ([]string, error) {
	if a.cfg.BindDN != "" {
		if err := conn.Bind(a.cfg.BindDN, a.cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("re-bind service account: %w", err)
		}
	}
	filter := fmt.Sprintf(a.cfg.GroupFilter, ldap.EscapeFilter(userDN))
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
	// RequiredGroupConfigured is true when required_group gates login.
	RequiredGroupConfigured bool `json:"required_group_configured"`
	// LoginAllowed is false only when a required_group is configured and the user
	// is not a member; such a user must be denied login (and not provisioned).
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
		RequiredGroupConfigured: cfg.RequiredGroup != "",
		AdminGroupConfigured:    cfg.AdminGroup != "",
		TeamRoles:               map[string]string{},
	}
	d.LoginAllowed = !d.RequiredGroupConfigured || have[strings.ToLower(cfg.RequiredGroup)]
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
