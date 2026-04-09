package handler

import "sync"

// ScanHub tracks live SSE connections from phone clients.
// It is keyed by scan session UUID string.
// Phone info (IP, user-agent) is stored in-memory alongside each channel.
type ScanHub struct {
	mu    sync.RWMutex
	conns map[string]*scanConn
}

type scanConn struct {
	ch        chan []byte
	ip        string
	userAgent string
}

// NewScanHub returns an initialised ScanHub.
func NewScanHub() *ScanHub {
	return &ScanHub{conns: make(map[string]*scanConn)}
}

// Register opens a channel for the given session and returns it together with
// a cleanup func that must be deferred by the caller.
func (h *ScanHub) Register(sessionID, ip, userAgent string) (<-chan []byte, func()) {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.conns[sessionID] = &scanConn{ch: ch, ip: ip, userAgent: userAgent}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if c, ok := h.conns[sessionID]; ok && c.ch == ch {
			delete(h.conns, sessionID)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Send pushes a message to a connected phone. Returns false if the session is
// not currently connected (non-blocking drop on a full buffer).
func (h *ScanHub) Send(sessionID string, msg []byte) bool {
	h.mu.RLock()
	c, ok := h.conns[sessionID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case c.ch <- msg:
		return true
	default:
		return false
	}
}

// IsConnected returns true if the phone for the given session has an open SSE
// connection.
func (h *ScanHub) IsConnected(sessionID string) bool {
	h.mu.RLock()
	_, ok := h.conns[sessionID]
	h.mu.RUnlock()
	return ok
}

// PhoneInfo returns the IP and User-Agent stored at registration time.
func (h *ScanHub) PhoneInfo(sessionID string) (ip, userAgent string) {
	h.mu.RLock()
	if c, ok := h.conns[sessionID]; ok {
		ip, userAgent = c.ip, c.userAgent
	}
	h.mu.RUnlock()
	return
}
