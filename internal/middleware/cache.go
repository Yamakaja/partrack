package middleware

import (
	"context"
	"log"
	"sync"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sessionEntry holds a cached session token lookup result.
// expiresAt mirrors the DB value; dirty signals a pending last_seen_at write.
type sessionEntry struct {
	user      *User
	sessionID string // UUID string; used by the flusher to call UpdateSessionLastSeen
	expiresAt time.Time
	dirty     bool
}

// apiTokenEntry holds a cached API token lookup result.
// expiresAt is nil when the token has no expiry.
type apiTokenEntry struct {
	user      *User
	tokenID   string // UUID string; used by the flusher to call UpdateAPITokenLastUsed
	expiresAt *time.Time
	dirty     bool
}

// scanTokenEntry holds a cached scan session token lookup result.
// No dirty flag — scan sessions have no last_seen_at to track.
type scanTokenEntry struct {
	user      *User
	sessionID string // UUID string; used by EvictScanTokenBySessionID
	expiresAt time.Time
}

// membershipEntry holds a cached team membership role.
type membershipEntry struct {
	role string
}

// AuthCache is an in-process cache for session tokens, API tokens, and team
// memberships. Each section has its own RWMutex so contention is isolated.
// All public methods are safe for concurrent use.
type AuthCache struct {
	sessmu        sync.RWMutex
	sessions      map[string]*sessionEntry // tokenHash → entry
	sessionByID   map[string]string        // sessionID → tokenHash
	sessionByUser map[string][]string      // userID → []tokenHash

	apimu          sync.RWMutex
	apiTokens      map[string]*apiTokenEntry // tokenHash → entry
	apiTokenByID   map[string]string         // tokenID → tokenHash
	apiTokenByUser map[string][]string       // userID → []tokenHash

	scanmu     sync.RWMutex
	scanTokens map[string]*scanTokenEntry // tokenHash → entry
	scanByID   map[string]string          // sessionID → tokenHash

	membmu      sync.RWMutex
	memberships map[string]membershipEntry // "teamID:userID" → entry
	membsByUser map[string][]string        // userID → []"teamID:userID"

	resmu         sync.RWMutex
	resourceOwner map[string]string // resourceID (project/revision/line-item UUID) → owning teamID
}

// NewAuthCache creates an empty AuthCache ready for use.
func NewAuthCache() *AuthCache {
	return &AuthCache{
		sessions:       make(map[string]*sessionEntry),
		sessionByID:    make(map[string]string),
		sessionByUser:  make(map[string][]string),
		apiTokens:      make(map[string]*apiTokenEntry),
		apiTokenByID:   make(map[string]string),
		apiTokenByUser: make(map[string][]string),
		scanTokens:     make(map[string]*scanTokenEntry),
		scanByID:       make(map[string]string),
		memberships:    make(map[string]membershipEntry),
		membsByUser:    make(map[string][]string),
		resourceOwner:  make(map[string]string),
	}
}

// ── Session cache ─────────────────────────────────────────────────────────────

// LookupSession returns the cached entry for hash if it exists and has not expired.
func (c *AuthCache) LookupSession(hash string) (*sessionEntry, bool) {
	c.sessmu.RLock()
	defer c.sessmu.RUnlock()
	e, ok := c.sessions[hash]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e, true
}

// StoreSession populates the session cache and both secondary indexes.
func (c *AuthCache) StoreSession(hash, sessionID, userID string, user *User, expiresAt time.Time) {
	c.sessmu.Lock()
	defer c.sessmu.Unlock()
	if _, exists := c.sessions[hash]; !exists {
		c.sessionByID[sessionID] = hash
		c.sessionByUser[userID] = append(c.sessionByUser[userID], hash)
	}
	c.sessions[hash] = &sessionEntry{user: user, sessionID: sessionID, expiresAt: expiresAt}
}

// TouchSession marks a session as dirty so the flusher will write last_seen_at.
func (c *AuthCache) TouchSession(hash string) {
	c.sessmu.Lock()
	if e, ok := c.sessions[hash]; ok {
		e.dirty = true
	}
	c.sessmu.Unlock()
}

// EvictSession removes a session entry by its token hash.
func (c *AuthCache) EvictSession(hash string) {
	c.sessmu.Lock()
	c.evictSessionLocked(hash)
	c.sessmu.Unlock()
}

// EvictSessionByID removes a session entry by its session UUID.
func (c *AuthCache) EvictSessionByID(sessionID string) {
	c.sessmu.Lock()
	if hash, ok := c.sessionByID[sessionID]; ok {
		c.evictSessionLocked(hash)
	}
	c.sessmu.Unlock()
}

// EvictAllSessionsForUser removes every cached session belonging to a user.
func (c *AuthCache) EvictAllSessionsForUser(userID string) {
	c.sessmu.Lock()
	hashes := append([]string(nil), c.sessionByUser[userID]...) // snapshot before mutating
	for _, h := range hashes {
		c.evictSessionLocked(h)
	}
	c.sessmu.Unlock()
}

// evictSessionLocked must be called with sessmu write-locked.
func (c *AuthCache) evictSessionLocked(hash string) {
	e, ok := c.sessions[hash]
	if !ok {
		return
	}
	delete(c.sessions, hash)
	delete(c.sessionByID, e.sessionID)
	userID := e.user.ID
	hashes := c.sessionByUser[userID]
	for i, h := range hashes {
		if h == hash {
			c.sessionByUser[userID] = append(hashes[:i], hashes[i+1:]...)
			break
		}
	}
	if len(c.sessionByUser[userID]) == 0 {
		delete(c.sessionByUser, userID)
	}
}

// ── API token cache ───────────────────────────────────────────────────────────

// LookupAPIToken returns the cached entry for hash if it exists and has not expired.
func (c *AuthCache) LookupAPIToken(hash string) (*apiTokenEntry, bool) {
	c.apimu.RLock()
	defer c.apimu.RUnlock()
	e, ok := c.apiTokens[hash]
	if !ok {
		return nil, false
	}
	if e.expiresAt != nil && time.Now().After(*e.expiresAt) {
		return nil, false
	}
	return e, true
}

// StoreAPIToken populates the API token cache and both secondary indexes.
func (c *AuthCache) StoreAPIToken(hash, tokenID, userID string, user *User, expiresAt *time.Time) {
	c.apimu.Lock()
	defer c.apimu.Unlock()
	if _, exists := c.apiTokens[hash]; !exists {
		c.apiTokenByID[tokenID] = hash
		c.apiTokenByUser[userID] = append(c.apiTokenByUser[userID], hash)
	}
	c.apiTokens[hash] = &apiTokenEntry{user: user, tokenID: tokenID, expiresAt: expiresAt}
}

// TouchAPIToken marks an API token as dirty so the flusher will write last_used_at.
func (c *AuthCache) TouchAPIToken(hash string) {
	c.apimu.Lock()
	if e, ok := c.apiTokens[hash]; ok {
		e.dirty = true
	}
	c.apimu.Unlock()
}

// EvictAPIToken removes an API token entry by its token hash.
func (c *AuthCache) EvictAPIToken(hash string) {
	c.apimu.Lock()
	c.evictAPITokenLocked(hash)
	c.apimu.Unlock()
}

// EvictAPITokenByID removes an API token entry by its token UUID.
func (c *AuthCache) EvictAPITokenByID(tokenID string) {
	c.apimu.Lock()
	if hash, ok := c.apiTokenByID[tokenID]; ok {
		c.evictAPITokenLocked(hash)
	}
	c.apimu.Unlock()
}

// EvictAllAPITokensForUser removes every cached API token belonging to a user.
func (c *AuthCache) EvictAllAPITokensForUser(userID string) {
	c.apimu.Lock()
	hashes := append([]string(nil), c.apiTokenByUser[userID]...)
	for _, h := range hashes {
		c.evictAPITokenLocked(h)
	}
	c.apimu.Unlock()
}

// evictAPITokenLocked must be called with apimu write-locked.
func (c *AuthCache) evictAPITokenLocked(hash string) {
	e, ok := c.apiTokens[hash]
	if !ok {
		return
	}
	delete(c.apiTokens, hash)
	delete(c.apiTokenByID, e.tokenID)
	userID := e.user.ID
	hashes := c.apiTokenByUser[userID]
	for i, h := range hashes {
		if h == hash {
			c.apiTokenByUser[userID] = append(hashes[:i], hashes[i+1:]...)
			break
		}
	}
	if len(c.apiTokenByUser[userID]) == 0 {
		delete(c.apiTokenByUser, userID)
	}
}

// ── Scan token cache ──────────────────────────────────────────────────────────

// LookupScanToken returns the cached user for the token hash if present and not expired.
func (c *AuthCache) LookupScanToken(hash string) (*User, bool) {
	c.scanmu.RLock()
	defer c.scanmu.RUnlock()
	e, ok := c.scanTokens[hash]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.user, true
}

// StoreScanToken populates the scan token cache and the sessionID → hash index.
func (c *AuthCache) StoreScanToken(hash, sessionID string, user *User, expiresAt time.Time) {
	c.scanmu.Lock()
	defer c.scanmu.Unlock()
	if _, exists := c.scanTokens[hash]; !exists {
		c.scanByID[sessionID] = hash
	}
	c.scanTokens[hash] = &scanTokenEntry{user: user, sessionID: sessionID, expiresAt: expiresAt}
}

// EvictScanToken removes a scan token entry by its token hash.
func (c *AuthCache) EvictScanToken(hash string) {
	c.scanmu.Lock()
	c.evictScanTokenLocked(hash)
	c.scanmu.Unlock()
}

// EvictScanTokenBySessionID removes a scan token entry by its session UUID.
func (c *AuthCache) EvictScanTokenBySessionID(sessionID string) {
	c.scanmu.Lock()
	if hash, ok := c.scanByID[sessionID]; ok {
		c.evictScanTokenLocked(hash)
	}
	c.scanmu.Unlock()
}

// evictScanTokenLocked must be called with scanmu write-locked.
func (c *AuthCache) evictScanTokenLocked(hash string) {
	e, ok := c.scanTokens[hash]
	if !ok {
		return
	}
	delete(c.scanTokens, hash)
	delete(c.scanByID, e.sessionID)
}

// ── Membership cache ──────────────────────────────────────────────────────────

func membershipKey(teamID, userID string) string { return teamID + ":" + userID }

// LookupMembership returns the cached role for (teamID, userID), or "" and false.
func (c *AuthCache) LookupMembership(teamID, userID string) (string, bool) {
	c.membmu.RLock()
	e, ok := c.memberships[membershipKey(teamID, userID)]
	c.membmu.RUnlock()
	if !ok {
		return "", false
	}
	return e.role, true
}

// StoreMembership stores a team membership role in the cache.
func (c *AuthCache) StoreMembership(teamID, userID, role string) {
	key := membershipKey(teamID, userID)
	c.membmu.Lock()
	defer c.membmu.Unlock()
	if _, exists := c.memberships[key]; !exists {
		c.membsByUser[userID] = append(c.membsByUser[userID], key)
	}
	c.memberships[key] = membershipEntry{role: role}
}

// EvictMembership removes a membership entry for (teamID, userID).
func (c *AuthCache) EvictMembership(teamID, userID string) {
	key := membershipKey(teamID, userID)
	c.membmu.Lock()
	c.evictMembershipLocked(key, userID)
	c.membmu.Unlock()
}

// EvictAllMembershipsForUser removes every cached membership belonging to a user.
func (c *AuthCache) EvictAllMembershipsForUser(userID string) {
	c.membmu.Lock()
	keys := append([]string(nil), c.membsByUser[userID]...)
	for _, key := range keys {
		c.evictMembershipLocked(key, userID)
	}
	c.membmu.Unlock()
}

// evictMembershipLocked must be called with membmu write-locked.
func (c *AuthCache) evictMembershipLocked(key, userID string) {
	if _, ok := c.memberships[key]; !ok {
		return
	}
	delete(c.memberships, key)
	keys := c.membsByUser[userID]
	for i, k := range keys {
		if k == key {
			c.membsByUser[userID] = append(keys[:i], keys[i+1:]...)
			break
		}
	}
	if len(c.membsByUser[userID]) == 0 {
		delete(c.membsByUser, userID)
	}
}

// ── Resource-owner cache ──────────────────────────────────────────────────────
//
// Maps a project / BOM revision / BOM line-item UUID to its owning team. These
// relationships are immutable for the lifetime of the resource (a project never
// changes team, a revision never changes project, a line item never changes
// revision), so entries need no expiry: a stale entry for a deleted resource is
// harmless because the comparison still rejects cross-team access and the
// handler's own query 404s. Keyed by globally-unique UUID, so the three resource
// types share one map without collision.

// LookupResourceOwner returns the cached owning teamID for a resource UUID.
func (c *AuthCache) LookupResourceOwner(resourceID string) (string, bool) {
	c.resmu.RLock()
	team, ok := c.resourceOwner[resourceID]
	c.resmu.RUnlock()
	return team, ok
}

// StoreResourceOwner records the owning teamID for a resource UUID.
func (c *AuthCache) StoreResourceOwner(resourceID, teamID string) {
	c.resmu.Lock()
	c.resourceOwner[resourceID] = teamID
	c.resmu.Unlock()
}

// ── Flusher ───────────────────────────────────────────────────────────────────

// StartFlusher starts a background goroutine that periodically:
//   - writes dirty last_seen_at / last_used_at values to the database, and
//   - evicts expired session and API token entries.
//
// The goroutine stops when ctx is cancelled. Loss of up to one interval's
// worth of last-seen precision on crash is acceptable by design.
func (c *AuthCache) StartFlusher(ctx context.Context, db *pgxpool.Pool, interval time.Duration) {
	q := dbgen.New(db)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.flush(ctx, q)
			case <-ctx.Done():
				return
			}
		}
	}()
}

type flushSession struct{ sessionID string }
type flushToken struct{ tokenID string }

func (c *AuthCache) flush(ctx context.Context, q *dbgen.Queries) {
	now := time.Now()

	// Snapshot dirty sessions; evict expired ones.
	var sessions []flushSession
	c.sessmu.Lock()
	for hash, e := range c.sessions {
		if now.After(e.expiresAt) {
			c.evictSessionLocked(hash)
			continue
		}
		if e.dirty {
			sessions = append(sessions, flushSession{e.sessionID})
			e.dirty = false
		}
	}
	c.sessmu.Unlock()

	// Snapshot dirty API tokens; evict expired ones.
	var tokens []flushToken
	c.apimu.Lock()
	for hash, e := range c.apiTokens {
		if e.expiresAt != nil && now.After(*e.expiresAt) {
			c.evictAPITokenLocked(hash)
			continue
		}
		if e.dirty {
			tokens = append(tokens, flushToken{e.tokenID})
			e.dirty = false
		}
	}
	c.apimu.Unlock()

	// Evict expired scan session tokens.
	c.scanmu.Lock()
	for hash, e := range c.scanTokens {
		if now.After(e.expiresAt) {
			c.evictScanTokenLocked(hash)
		}
	}
	c.scanmu.Unlock()

	// Write to DB outside all locks.
	for _, s := range sessions {
		var id pgtype.UUID
		if err := id.Scan(s.sessionID); err != nil {
			continue
		}
		if err := q.UpdateSessionLastSeen(ctx, id); err != nil {
			log.Printf("authcache: flush session last_seen %s: %v", s.sessionID, err)
		}
	}
	for _, t := range tokens {
		var id pgtype.UUID
		if err := id.Scan(t.tokenID); err != nil {
			continue
		}
		if err := q.UpdateAPITokenLastUsed(ctx, id); err != nil {
			log.Printf("authcache: flush api_token last_used %s: %v", t.tokenID, err)
		}
	}
}
