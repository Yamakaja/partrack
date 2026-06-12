package handler

import (
	"testing"

	"github.com/Yamakaja/partrack/internal/config"
)

func TestLdapProviderHandles(t *testing.T) {
	catchAll := config.LDAPProvider{Name: "any"}
	institute := config.LDAPProvider{Name: "inst", EmailDomains: []string{"institute.com", "Sub.Institute.com"}}

	cases := []struct {
		name       string
		p          config.LDAPProvider
		identifier string
		want       bool
	}{
		{"catch-all takes any email", catchAll, "john@institute.com", true},
		{"catch-all takes bare username", catchAll, "john", true},
		{"domain match", institute, "john.doe@institute.com", true},
		{"domain match case-insensitive", institute, "john@INSTITUTE.COM", true},
		{"second domain", institute, "x@sub.institute.com", true},
		{"domain miss", institute, "john@other.com", false},
		{"no domain on scoped provider", institute, "john", false},
	}
	for _, c := range cases {
		if got := ldapProviderHandles(c.p, c.identifier); got != c.want {
			t.Errorf("%s: ldapProviderHandles(%q) = %v, want %v", c.name, c.identifier, got, c.want)
		}
	}
}

func TestLdapUsername(t *testing.T) {
	strip := config.LDAPProvider{StripDomain: true}
	keep := config.LDAPProvider{}

	if got := ldapUsername(strip, "john.doe@institute.com"); got != "john.doe" {
		t.Errorf("strip_domain: got %q, want john.doe", got)
	}
	if got := ldapUsername(strip, "john.doe"); got != "john.doe" {
		t.Errorf("strip_domain on bare username: got %q, want john.doe", got)
	}
	if got := ldapUsername(keep, "john.doe@institute.com"); got != "john.doe@institute.com" {
		t.Errorf("no strip: got %q, want full email", got)
	}
}
