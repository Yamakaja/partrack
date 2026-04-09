package middleware

import (
	"net"
	"net/http"
	"strings"
)

// RealIP is HTTP middleware that rewrites r.RemoteAddr to the value of
// X-Real-IP or X-Forwarded-For, but only when the direct peer matches a
// configured trusted proxy CIDR. If no trusted proxies are configured, or the
// peer is not trusted, RemoteAddr is left unchanged.
//
// This runs once at the top of the stack; all downstream code (handlers,
// logging, auth) can rely on r.RemoteAddr being the real client address.
func (m *Middleware) RealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(m.trustedNets) > 0 {
			peer, _, _ := net.SplitHostPort(r.RemoteAddr)
			if isTrustedPeer(peer, m.trustedNets) {
				if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
					r = r.WithContext(r.Context()) // shallow clone
					r2 := r.Clone(r.Context())
					r2.RemoteAddr = ip + ":0"
					r = r2
				} else if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
					ip = strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0])
					r2 := r.Clone(r.Context())
					r2.RemoteAddr = ip + ":0"
					r = r2
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isTrustedPeer(ipStr string, nets []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
