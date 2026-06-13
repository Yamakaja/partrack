package ldap

import (
	"context"
	"errors"
	"strings"
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

	// Inspect needs no password (it never binds as the user) but still requires a
	// username; an empty one is rejected before any dial, same as above.
	if _, _, err := a.Inspect(context.Background(), "", nil); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Inspect(\"\"): want ErrInvalidCredentials, got %v", err)
	}
}

// TestEncodeUniqueID covers the three subject-encoding paths: printable text
// passes through, AD's binary objectGUID becomes its canonical string (with the
// mixed-endian byte order), and any other binary value is hex-encoded.
func TestEncodeUniqueID(t *testing.T) {
	// Canonical GUID 00112233-4455-6677-8899-aabbccddeeff as AD stores it:
	// first three groups little-endian, the rest big-endian.
	guidRaw := []byte{
		0x33, 0x22, 0x11, 0x00,
		0x55, 0x44,
		0x77, 0x66,
		0x88, 0x99,
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff,
	}

	cases := []struct {
		name string
		attr string
		raw  []byte
		want string
	}{
		{"empty", "objectGUID", nil, ""},
		{"printable entryUUID", "entryUUID", []byte("9f8e7d6c-1234-5678-9abc-def012345678"), "9f8e7d6c-1234-5678-9abc-def012345678"},
		{"objectGUID canonical", "objectGUID", guidRaw, "00112233-4455-6677-8899-aabbccddeeff"},
		{"objectGUID case-insensitive attr", "objectguid", guidRaw, "00112233-4455-6677-8899-aabbccddeeff"},
		{"other binary → hex", "entryUUID", []byte{0x00, 0xde, 0xad, 0xbe, 0xef}, "00deadbeef"},
		{"non-16-byte binary → hex", "objectGUID", []byte{0xff, 0x00, 0x01}, "ff0001"},
	}
	for _, c := range cases {
		if got := encodeUniqueID(c.attr, c.raw); got != c.want {
			t.Errorf("%s: encodeUniqueID(%q, %v) = %q; want %q", c.name, c.attr, c.raw, got, c.want)
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
	cfg := config.LDAPProvider{RequiredGroups: []string{"cn=partrack-users,dc=x"}}

	in := EvaluateGroups(cfg, []string{"CN=Partrack-Users,DC=x"})
	if !in.RequiredGroupConfigured || !in.LoginAllowed {
		t.Errorf("member: want configured=true allowed=true; got configured=%v allowed=%v", in.RequiredGroupConfigured, in.LoginAllowed)
	}

	out := EvaluateGroups(cfg, []string{"cn=other,dc=x"})
	if !out.RequiredGroupConfigured || out.LoginAllowed {
		t.Errorf("non-member: want configured=true allowed=false; got configured=%v allowed=%v", out.RequiredGroupConfigured, out.LoginAllowed)
	}
}

// TestEvaluateGroupsAllowedGroupsAny verifies the array form: login is allowed
// when the user is in ANY one of the allowed groups, and denied when in none.
func TestEvaluateGroupsAllowedGroupsAny(t *testing.T) {
	cfg := config.LDAPProvider{RequiredGroups: []string{
		"cn=staff,dc=x",
		"cn=contractors,dc=x",
	}}

	// In the second allowed group only → allowed.
	any := EvaluateGroups(cfg, []string{"CN=Contractors,DC=x"})
	if !any.LoginAllowed {
		t.Errorf("member of one allowed group: want LoginAllowed=true, got false")
	}

	// In neither → denied.
	none := EvaluateGroups(cfg, []string{"cn=visitors,dc=x"})
	if none.LoginAllowed {
		t.Errorf("member of no allowed group: want LoginAllowed=false, got true")
	}
}

// TestExpandGroups exercises the directory-agnostic nested-group walk in
// isolation, with a fake parentsOf graph, covering transitive expansion, the
// cycle guard, and the size cap. No live LDAP is needed.
func TestExpandGroups(t *testing.T) {
	// Graph: user ∈ eng; eng ∈ all-staff; all-staff ∈ everyone; plus a cycle
	// everyone → all-staff to prove the visited-set guard holds.
	parents := map[string][]string{
		"cn=eng,dc=x":       {"cn=all-staff,dc=x"},
		"cn=all-staff,dc=x": {"cn=everyone,dc=x"},
		"cn=everyone,dc=x":  {"cn=all-staff,dc=x"}, // cycle back
	}
	parentsOf := func(dn string) ([]string, error) { return parents[strings.ToLower(dn)], nil }

	groups, via, err := expandGroups([]string{"cn=eng,dc=x"}, parentsOf, 50, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]bool{"cn=eng,dc=x": true, "cn=all-staff,dc=x": true, "cn=everyone,dc=x": true}
	if len(groups) != len(want) {
		t.Fatalf("groups = %v; want the 3 transitive groups", groups)
	}
	for _, g := range groups {
		if !want[g] {
			t.Errorf("unexpected group %q", g)
		}
	}
	if via["cn=eng,dc=x"] != "" {
		t.Errorf("eng should be direct (via \"\"), got %q", via["cn=eng,dc=x"])
	}
	if via["cn=all-staff,dc=x"] != "cn=eng,dc=x" {
		t.Errorf("all-staff via = %q; want cn=eng,dc=x", via["cn=all-staff,dc=x"])
	}

	// Cap: a fan-out larger than the cap is truncated, never unbounded.
	fanout := func(dn string) ([]string, error) {
		if dn == "cn=root,dc=x" {
			out := make([]string, 100)
			for i := range out {
				out[i] = "cn=g" + string(rune('A'+i%26)) + string(rune('0'+i/26)) + ",dc=x"
			}
			return out, nil
		}
		return nil, nil
	}
	capped, _, _ := expandGroups([]string{"cn=root,dc=x"}, fanout, 10, 10)
	if len(capped) > 10 {
		t.Errorf("cap not enforced: got %d groups, want <= 10", len(capped))
	}
}
