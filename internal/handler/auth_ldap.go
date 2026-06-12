package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/Yamakaja/partrack/internal/config"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	pkgldap "github.com/Yamakaja/partrack/internal/ldap"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ldapEntry holds a configured directory and its authenticator. It parallels
// oidcEntry but needs no lazy discovery — connections are opened per login.
type ldapEntry struct {
	cfg  config.LDAPProvider
	auth *pkgldap.Authenticator
}

// initLDAP registers all configured LDAP providers.
func (h *Handler) initLDAP() {
	h.ldapEntries = make(map[string]*ldapEntry)
	for _, p := range h.Config.LDAP {
		h.ldapEntries[p.Name] = &ldapEntry{cfg: p, auth: pkgldap.New(p)}
	}
}

// ldapConfigured reports whether any directory is configured.
func (h *Handler) ldapConfigured() bool { return len(h.ldapEntries) > 0 }

// emailDomain returns the lower-cased domain part of an email-shaped identifier,
// or "" if it has no '@'.
func emailDomain(identifier string) string {
	if i := strings.LastIndex(identifier, "@"); i >= 0 {
		return strings.ToLower(identifier[i+1:])
	}
	return ""
}

// ldapProviderHandles reports whether a provider should be attempted for this
// identifier. A provider with no email_domains is a catch-all; one with domains
// is attempted only when the identifier's email domain matches.
func ldapProviderHandles(p config.LDAPProvider, identifier string) bool {
	if len(p.EmailDomains) == 0 {
		return true
	}
	domain := emailDomain(identifier)
	if domain == "" {
		return false
	}
	for _, d := range p.EmailDomains {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return false
}

// ldapUsername derives the value substituted into the provider's user_filter.
// With strip_domain, an email identifier is reduced to its local part so a
// domain-routed login still works with a uid-style filter.
func ldapUsername(p config.LDAPProvider, identifier string) string {
	if p.StripDomain {
		if i := strings.LastIndex(identifier, "@"); i >= 0 {
			return identifier[:i]
		}
	}
	return identifier
}

var (
	errLDAPNotProvisioned = errors.New("ldap: auto-provisioning disabled")
	errLDAPNoEmail        = errors.New("ldap: directory entry has no email")
)

// tryLDAPLogin attempts to authenticate username/password against every
// configured directory in turn. On the first success it resolves (and, if
// enabled, provisions) the partrack user, syncs directory groups, and returns
// the user row. The bool is false when no directory accepted the credentials;
// callers must surface a generic error in that case so account existence and
// the local-vs-directory path are not disclosed.
func (h *Handler) tryLDAPLogin(ctx context.Context, identifier, password string) (dbgen.GetUserWithMembershipsRow, bool) {
	var zero dbgen.GetUserWithMembershipsRow
	for _, p := range h.Config.LDAP {
		if !ldapProviderHandles(p, identifier) {
			continue
		}
		username := ldapUsername(p, identifier)
		entry := h.ldapEntries[p.Name]
		res, err := entry.auth.Authenticate(ctx, username, password)
		if err != nil {
			// Invalid credentials → try the next directory. Operational errors
			// (server down, misconfig) are logged but still fall through to a
			// generic failure for the user.
			if !errors.Is(err, pkgldap.ErrInvalidCredentials) {
				log.Printf("handler: LDAP provider %q error: %v", p.Name, err)
			}
			continue
		}

		// Login gate: when required_group is configured, a successful bind is not
		// enough — the user must also be a member. Deny (and do not provision)
		// otherwise, returning the same generic failure as a bad password.
		if d := pkgldap.EvaluateGroups(entry.cfg, res.Groups); !d.LoginAllowed {
			log.Printf("handler: LDAP login denied for provider %q: user %q is not in required_group", p.Name, username)
			return zero, false
		}

		user, err := h.resolveLDAPUser(ctx, entry.cfg, res)
		if err != nil {
			log.Printf("handler: LDAP user resolution failed for provider %q: %v", p.Name, err)
			return zero, false
		}
		if err := h.syncLDAPGroups(ctx, entry.cfg, user.ID, res.Groups); err != nil {
			// A group-sync failure must not block an otherwise valid login;
			// memberships simply remain as they were.
			log.Printf("handler: LDAP group sync failed for provider %q: %v", p.Name, err)
		}
		return user, true
	}
	return zero, false
}

// resolveLDAPUser maps a directory bind result to a partrack user, mirroring
// resolveOIDCUser. Resolution order:
//  1. an identity already linked to this (provider, subject)
//  2. auto-link to an existing user with the same email
//  3. auto-provision a new user, if enabled for the provider
//
// The directory's email attribute is read via the service account (not
// user-supplied), so it is authoritative — no separate "verified" gate is
// needed as it is for OIDC id_token claims.
func (h *Handler) resolveLDAPUser(ctx context.Context, cfg config.LDAPProvider, res *pkgldap.Result) (dbgen.GetUserWithMembershipsRow, error) {
	var zero dbgen.GetUserWithMembershipsRow

	identity, err := h.Queries.GetExternalIdentity(ctx, dbgen.GetExternalIdentityParams{
		Provider: cfg.Name,
		Subject:  res.Subject,
	})
	if err == nil {
		return h.Queries.GetUserWithMemberships(ctx, identity.UserID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, err
	}

	if res.Email == "" {
		return zero, errLDAPNoEmail
	}

	// Auto-link: attach this identity to an existing user with the same email.
	existing, err := h.Queries.GetUserByEmail(ctx, res.Email)
	if err == nil {
		email := res.Email
		if _, err := h.Queries.CreateExternalIdentity(ctx, dbgen.CreateExternalIdentityParams{
			UserID:        existing.ID,
			Provider:      cfg.Name,
			Subject:       res.Subject,
			ProviderEmail: &email,
			Kind:          "ldap",
		}); err != nil {
			return zero, fmt.Errorf("link ldap identity: %w", err)
		}
		return h.Queries.GetUserWithMemberships(ctx, existing.ID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, err
	}

	if !cfg.AutoProvision {
		return zero, errLDAPNotProvisioned
	}

	displayName := res.DisplayName
	if displayName == "" {
		displayName = res.Email
	}

	tx, err := h.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := h.Queries.WithTx(tx)

	user, err := qtx.CreateUser(ctx, dbgen.CreateUserParams{
		DisplayName: displayName,
		Email:       res.Email,
	})
	if err != nil {
		return zero, fmt.Errorf("create user: %w", err)
	}
	email := res.Email
	if _, err := qtx.CreateExternalIdentity(ctx, dbgen.CreateExternalIdentityParams{
		UserID:        user.ID,
		Provider:      cfg.Name,
		Subject:       res.Subject,
		ProviderEmail: &email,
		Kind:          "ldap",
	}); err != nil {
		return zero, fmt.Errorf("create ldap identity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	return dbgen.GetUserWithMembershipsRow{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		IsActive:    user.IsActive,
		IsAdmin:     user.IsAdmin,
		CreatedAt:   user.CreatedAt,
	}, nil
}

// syncLDAPGroups reconciles the user's instance-admin flag and team memberships
// against their current directory groups, using the shared pkgldap.EvaluateGroups
// decision so the diagnostics tool and real login agree.
//
//   - When admin_group is configured, the instance admin flag is set to exactly
//     reflect membership in that group (granted or revoked).
//   - Teams named by any group_mapping are "directory-managed": the user's
//     membership in them is set to the highest role their groups grant, or
//     removed when no group grants access. Crucially, only directory-managed
//     rows are touched — a membership a team/global admin added or changed by
//     hand (directory_managed = FALSE) is preserved, letting installs provision
//     everyone to a baseline role and then override individuals.
func (h *Handler) syncLDAPGroups(ctx context.Context, cfg config.LDAPProvider, userID pgtype.UUID, groups []string) error {
	d := pkgldap.EvaluateGroups(cfg, groups)

	if d.AdminGroupConfigured {
		isAdmin := d.IsAdmin
		if _, err := h.Queries.AdminUpdateUser(ctx, dbgen.AdminUpdateUserParams{
			ID:      userID,
			IsAdmin: &isAdmin,
		}); err != nil {
			return fmt.Errorf("update admin flag: %w", err)
		}
	}

	for _, m := range d.Mappings {
		if !m.ValidRole {
			log.Printf("handler: LDAP provider %q mapping for group %q has invalid role %q; skipping", cfg.Name, m.Group, m.Role)
		}
	}

	for _, teamName := range d.ManagedTeams {
		team, err := h.Queries.GetTeamByName(ctx, teamName)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				log.Printf("handler: LDAP group mapping references unknown team %q; skipping", teamName)
				continue
			}
			return fmt.Errorf("resolve team %q: %w", teamName, err)
		}
		if role, ok := d.TeamRoles[teamName]; ok {
			if err := h.Queries.SyncDirectoryMembership(ctx, dbgen.SyncDirectoryMembershipParams{
				TeamID: team.ID,
				UserID: userID,
				Role:   dbgen.TeamRole(role),
			}); err != nil {
				return fmt.Errorf("sync membership for team %q: %w", teamName, err)
			}
		} else {
			if err := h.Queries.RemoveDirectoryMembership(ctx, dbgen.RemoveDirectoryMembershipParams{
				TeamID: team.ID,
				UserID: userID,
			}); err != nil {
				return fmt.Errorf("remove membership for team %q: %w", teamName, err)
			}
		}
	}
	return nil
}
