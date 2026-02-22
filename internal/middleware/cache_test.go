package middleware

import (
	"testing"
	"time"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func newTestUser(id string) *User {
	return &User{ID: id, DisplayName: "Test", Email: id + "@example.com", IsActive: true}
}

// ── Session cache ─────────────────────────────────────────────────────────────

func TestSessionCacheMissBeforeStore(t *testing.T) {
	c := NewAuthCache()
	_, ok := c.LookupSession("hash1")
	if ok {
		t.Fatal("expected miss before store")
	}
}

func TestSessionCacheHitAfterStore(t *testing.T) {
	c := NewAuthCache()
	user := newTestUser("u1")
	c.StoreSession("hash1", "sess1", "u1", user, time.Now().Add(time.Hour))
	e, ok := c.LookupSession("hash1")
	if !ok {
		t.Fatal("expected hit after store")
	}
	if e.user.ID != "u1" {
		t.Fatalf("wrong user ID: %s", e.user.ID)
	}
}

func TestSessionCacheMissAfterExpiry(t *testing.T) {
	c := NewAuthCache()
	c.StoreSession("hash1", "sess1", "u1", newTestUser("u1"), time.Now().Add(-time.Second))
	_, ok := c.LookupSession("hash1")
	if ok {
		t.Fatal("expected miss for expired session")
	}
}

func TestSessionCacheMissAfterEvict(t *testing.T) {
	c := NewAuthCache()
	c.StoreSession("hash1", "sess1", "u1", newTestUser("u1"), time.Now().Add(time.Hour))
	c.EvictSession("hash1")
	_, ok := c.LookupSession("hash1")
	if ok {
		t.Fatal("expected miss after evict")
	}
}

func TestSessionCacheEvictByID(t *testing.T) {
	c := NewAuthCache()
	c.StoreSession("hash1", "sess1", "u1", newTestUser("u1"), time.Now().Add(time.Hour))
	c.EvictSessionByID("sess1")
	_, ok := c.LookupSession("hash1")
	if ok {
		t.Fatal("expected miss after EvictSessionByID")
	}
}

func TestSessionCacheEvictAllForUser(t *testing.T) {
	c := NewAuthCache()
	exp := time.Now().Add(time.Hour)
	c.StoreSession("h1", "s1", "u1", newTestUser("u1"), exp)
	c.StoreSession("h2", "s2", "u1", newTestUser("u1"), exp)
	c.StoreSession("h3", "s3", "u2", newTestUser("u2"), exp)
	c.EvictAllSessionsForUser("u1")
	if _, ok := c.LookupSession("h1"); ok {
		t.Error("h1 should be evicted")
	}
	if _, ok := c.LookupSession("h2"); ok {
		t.Error("h2 should be evicted")
	}
	if _, ok := c.LookupSession("h3"); !ok {
		t.Error("h3 (other user) should still be present")
	}
}

func TestSessionCacheTouchSetsDirty(t *testing.T) {
	c := NewAuthCache()
	c.StoreSession("hash1", "sess1", "u1", newTestUser("u1"), time.Now().Add(time.Hour))

	// Verify dirty is false initially.
	c.sessmu.RLock()
	initialDirty := c.sessions["hash1"].dirty
	c.sessmu.RUnlock()
	if initialDirty {
		t.Fatal("expected dirty=false after store")
	}

	c.TouchSession("hash1")

	c.sessmu.RLock()
	afterDirty := c.sessions["hash1"].dirty
	c.sessmu.RUnlock()
	if !afterDirty {
		t.Fatal("expected dirty=true after Touch")
	}

	// Entry must still be lookupable.
	if _, ok := c.LookupSession("hash1"); !ok {
		t.Fatal("entry should still be present after Touch")
	}
}

// ── API token cache ───────────────────────────────────────────────────────────

func TestAPITokenCacheMissBeforeStore(t *testing.T) {
	c := NewAuthCache()
	_, ok := c.LookupAPIToken("hash1")
	if ok {
		t.Fatal("expected miss before store")
	}
}

func TestAPITokenCacheHitAfterStore(t *testing.T) {
	c := NewAuthCache()
	c.StoreAPIToken("hash1", "tok1", "u1", newTestUser("u1"), nil)
	e, ok := c.LookupAPIToken("hash1")
	if !ok {
		t.Fatal("expected hit after store")
	}
	if e.user.ID != "u1" {
		t.Fatalf("wrong user ID: %s", e.user.ID)
	}
}

func TestAPITokenCacheNoExpiry(t *testing.T) {
	c := NewAuthCache()
	// nil expiresAt — entry should live indefinitely until explicit eviction.
	c.StoreAPIToken("hash1", "tok1", "u1", newTestUser("u1"), nil)
	if _, ok := c.LookupAPIToken("hash1"); !ok {
		t.Fatal("expected hit for token with nil expiresAt")
	}
}

func TestAPITokenCacheMissAfterExpiry(t *testing.T) {
	c := NewAuthCache()
	past := time.Now().Add(-time.Second)
	c.StoreAPIToken("hash1", "tok1", "u1", newTestUser("u1"), &past)
	_, ok := c.LookupAPIToken("hash1")
	if ok {
		t.Fatal("expected miss for expired API token")
	}
}

func TestAPITokenCacheMissAfterEvict(t *testing.T) {
	c := NewAuthCache()
	c.StoreAPIToken("hash1", "tok1", "u1", newTestUser("u1"), nil)
	c.EvictAPIToken("hash1")
	_, ok := c.LookupAPIToken("hash1")
	if ok {
		t.Fatal("expected miss after EvictAPIToken")
	}
}

func TestAPITokenCacheEvictByID(t *testing.T) {
	c := NewAuthCache()
	c.StoreAPIToken("hash1", "tok1", "u1", newTestUser("u1"), nil)
	c.EvictAPITokenByID("tok1")
	_, ok := c.LookupAPIToken("hash1")
	if ok {
		t.Fatal("expected miss after EvictAPITokenByID")
	}
}

func TestAPITokenCacheEvictAllForUser(t *testing.T) {
	c := NewAuthCache()
	c.StoreAPIToken("h1", "t1", "u1", newTestUser("u1"), nil)
	c.StoreAPIToken("h2", "t2", "u1", newTestUser("u1"), nil)
	c.StoreAPIToken("h3", "t3", "u2", newTestUser("u2"), nil)
	c.EvictAllAPITokensForUser("u1")
	if _, ok := c.LookupAPIToken("h1"); ok {
		t.Error("h1 should be evicted")
	}
	if _, ok := c.LookupAPIToken("h2"); ok {
		t.Error("h2 should be evicted")
	}
	if _, ok := c.LookupAPIToken("h3"); !ok {
		t.Error("h3 (other user) should still be present")
	}
}

func TestAPITokenCacheTouchSetsDirty(t *testing.T) {
	c := NewAuthCache()
	c.StoreAPIToken("hash1", "tok1", "u1", newTestUser("u1"), nil)

	c.apimu.RLock()
	initialDirty := c.apiTokens["hash1"].dirty
	c.apimu.RUnlock()
	if initialDirty {
		t.Fatal("expected dirty=false after store")
	}

	c.TouchAPIToken("hash1")

	c.apimu.RLock()
	afterDirty := c.apiTokens["hash1"].dirty
	c.apimu.RUnlock()
	if !afterDirty {
		t.Fatal("expected dirty=true after Touch")
	}
}

// ── Membership cache ─────────────────────────────────────────────────────────

func TestMembershipCacheStoreAndLookup(t *testing.T) {
	c := NewAuthCache()
	c.StoreMembership("team1", "u1", "admin")
	role, ok := c.LookupMembership("team1", "u1")
	if !ok {
		t.Fatal("expected hit after store")
	}
	if role != "admin" {
		t.Fatalf("expected role admin, got %s", role)
	}
}

func TestMembershipCacheMissBeforeStore(t *testing.T) {
	c := NewAuthCache()
	_, ok := c.LookupMembership("team1", "u1")
	if ok {
		t.Fatal("expected miss before store")
	}
}

func TestMembershipCacheEvict(t *testing.T) {
	c := NewAuthCache()
	c.StoreMembership("team1", "u1", "member")
	c.EvictMembership("team1", "u1")
	_, ok := c.LookupMembership("team1", "u1")
	if ok {
		t.Fatal("expected miss after evict")
	}
}

func TestMembershipCacheEvictAllForUser(t *testing.T) {
	c := NewAuthCache()
	c.StoreMembership("t1", "u1", "admin")
	c.StoreMembership("t2", "u1", "viewer")
	c.StoreMembership("t1", "u2", "member")
	c.EvictAllMembershipsForUser("u1")
	if _, ok := c.LookupMembership("t1", "u1"); ok {
		t.Error("t1:u1 should be evicted")
	}
	if _, ok := c.LookupMembership("t2", "u1"); ok {
		t.Error("t2:u1 should be evicted")
	}
	if _, ok := c.LookupMembership("t1", "u2"); !ok {
		t.Error("t1:u2 (other user) should still be present")
	}
}

func TestMembershipCacheUpdateRole(t *testing.T) {
	c := NewAuthCache()
	c.StoreMembership("t1", "u1", "viewer")
	// Storing again with a different role must update the stored value.
	c.StoreMembership("t1", "u1", "admin")
	role, ok := c.LookupMembership("t1", "u1")
	if !ok {
		t.Fatal("expected hit")
	}
	if role != "admin" {
		t.Fatalf("expected role admin after update, got %s", role)
	}
}
