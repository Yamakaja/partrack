package handler

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Single-use, short-lived scan tickets keep durable scan tokens out of URLs.
//
//   - A *pairing* ticket is encoded into the desktop's QR code; the phone hands it
//     to POST /scan/pair to obtain the durable session token (used as Bearer
//     thereafter). Refreshed periodically by the desktop so it never goes stale.
//   - A *stream* ticket is minted by the phone (Bearer) right before opening the
//     SSE connection at GET /scan/stream?ticket=…, then consumed on connect.
//
// Because tickets are single-use and expire in seconds, one captured in an access
// log / proxy log / browser history / Referer is already worthless. The store is
// in-process (matching the single-binary, no-clustering design); a ticket only
// outlives a process restart by being useless afterwards.
const (
	pairTicketTTL   = 60 * time.Second
	streamTicketTTL = 30 * time.Second
)

type ticketKind string

const (
	ticketPair   ticketKind = "pt"
	ticketStream ticketKind = "st"
)

type ticketEntry struct {
	kind          ticketKind
	scanSessionID string
	expiresAt     time.Time
}

// ticketStore holds tickets keyed by their SHA-256 hash. All access is guarded by
// one mutex; consume is atomic (lookup + delete) so a ticket cannot be used twice.
type ticketStore struct {
	mu sync.Mutex
	m  map[string]ticketEntry
}

func newTicketStore() *ticketStore { return &ticketStore{m: make(map[string]ticketEntry)} }

// mint creates a ticket of kind bound to scanSessionID and returns the raw ticket.
func (s *ticketStore) mint(kind ticketKind, scanSessionID string, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	raw := string(kind) + "_" + hex.EncodeToString(b)

	s.mu.Lock()
	s.m[hashToken(raw)] = ticketEntry{kind: kind, scanSessionID: scanSessionID, expiresAt: time.Now().Add(ttl)}
	s.sweepLocked()
	s.mu.Unlock()
	return raw, nil
}

// consume atomically validates and removes a ticket, returning the bound scan
// session id. ok is false if the ticket is unknown, the wrong kind, or expired.
func (s *ticketStore) consume(raw string, kind ticketKind) (sessionID string, ok bool) {
	h := hashToken(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, found := s.m[h]
	if !found {
		return "", false
	}
	delete(s.m, h) // single-use: remove on any lookup
	if e.kind != kind || time.Now().After(e.expiresAt) {
		return "", false
	}
	return e.scanSessionID, true
}

// sweepLocked drops expired entries; cheap because the store is small and tickets
// are short-lived. Caller must hold s.mu.
func (s *ticketStore) sweepLocked() {
	now := time.Now()
	for k, e := range s.m {
		if now.After(e.expiresAt) {
			delete(s.m, k)
		}
	}
}
