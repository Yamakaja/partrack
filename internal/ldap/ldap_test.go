package ldap

import (
	"context"
	"errors"
	"testing"

	"github.com/Yamakaja/partrack/internal/config"
)

// TestAuthenticateRejectsEmptyCredentials guards against the RFC 4513
// "unauthenticated bind" pitfall: an empty password must be rejected before any
// network bind, so a valid DN with no password can never succeed as an
// anonymous bind. The URL points nowhere; if the guard regresses the test would
// instead fail trying to dial.
func TestAuthenticateRejectsEmptyCredentials(t *testing.T) {
	a := New(config.LDAPProvider{
		Name:       "test",
		URL:        "ldap://127.0.0.1:1", // never dialled when the guard holds
		UserBaseDN: "dc=example,dc=com",
		UserFilter: "(uid=%s)",
	})

	cases := []struct{ user, pass string }{
		{"alice", ""},
		{"", "secret"},
		{"", ""},
	}
	for _, c := range cases {
		if _, err := a.Authenticate(context.Background(), c.user, c.pass); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Authenticate(%q,%q): want ErrInvalidCredentials, got %v", c.user, c.pass, err)
		}
	}
}

func TestEvaluateGroups(t *testing.T) {
	cfg := config.LDAPProvider{
		AdminGroup: "cn=admins,dc=x",
		GroupMappings: []config.LDAPGroupMapping{
			{Group: "", Team: "Everyone", Role: "viewer"},                     // catch-all default
			{Group: "cn=eng,dc=x", Team: "Engineering", Role: "member"},       // baseline
			{Group: "cn=buyers,dc=x", Team: "Engineering", Role: "purchaser"}, // higher role, same team
			{Group: "cn=ops,dc=x", Team: "Ops", Role: "member"},               // not granted below
			{Group: "cn=eng,dc=x", Team: "Bad", Role: "superuser"},            // invalid role
		},
	}

	// User is in eng + buyers (case-insensitively) but not admins/ops.
	d := EvaluateGroups(cfg, []string{"CN=Eng,DC=x", "cn=buyers,dc=x"})

	if !d.AdminGroupConfigured || d.IsAdmin {
		t.Errorf("admin: configured=%v isAdmin=%v; want configured=true isAdmin=false", d.AdminGroupConfigured, d.IsAdmin)
	}
	if got := d.TeamRoles["Engineering"]; got != "purchaser" {
		t.Errorf("Engineering role = %q; want purchaser (highest wins)", got)
	}
	if got := d.TeamRoles["Everyone"]; got != "viewer" {
		t.Errorf("Everyone role = %q; want viewer (catch-all empty group)", got)
	}
	if _, ok := d.TeamRoles["Ops"]; ok {
		t.Errorf("Ops should not be granted (user not in cn=ops)")
	}
	if _, ok := d.TeamRoles["Bad"]; ok {
		t.Errorf("invalid role must not produce a grant")
	}
	if d.RequiredGroupConfigured || !d.LoginAllowed {
		t.Errorf("no required_group: want LoginAllowed=true, configured=false; got allowed=%v configured=%v", d.LoginAllowed, d.RequiredGroupConfigured)
	}
	// Ops and Bad are still "managed" (named by mappings) so sync would remove them.
	wantManaged := map[string]bool{"Everyone": true, "Engineering": true, "Ops": true, "Bad": true}
	if len(d.ManagedTeams) != len(wantManaged) {
		t.Errorf("ManagedTeams = %v; want keys %v", d.ManagedTeams, wantManaged)
	}
	for _, tn := range d.ManagedTeams {
		if !wantManaged[tn] {
			t.Errorf("unexpected managed team %q", tn)
		}
	}
}

func TestEvaluateGroupsLoginGate(t *testing.T) {
	cfg := config.LDAPProvider{RequiredGroup: "cn=partrack-users,dc=x"}

	in := EvaluateGroups(cfg, []string{"CN=Partrack-Users,DC=x"})
	if !in.RequiredGroupConfigured || !in.LoginAllowed {
		t.Errorf("member: want configured=true allowed=true; got configured=%v allowed=%v", in.RequiredGroupConfigured, in.LoginAllowed)
	}

	out := EvaluateGroups(cfg, []string{"cn=other,dc=x"})
	if !out.RequiredGroupConfigured || out.LoginAllowed {
		t.Errorf("non-member: want configured=true allowed=false; got configured=%v allowed=%v", out.RequiredGroupConfigured, out.LoginAllowed)
	}
}
