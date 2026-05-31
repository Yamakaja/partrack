package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// CSRF provides defense-in-depth against cross-site request forgery for
// cookie-authenticated, state-changing requests.
//
// The session cookie is SameSite=Lax, which already blocks cross-site POST/PATCH/
// DELETE; this adds an explicit same-origin check so a single missing/old browser
// protection does not expose mutations. It applies only when the request is
// cookie-authenticated — Bearer/API-token clients set the Authorization header
// explicitly and are not CSRF-able, so they are exempt (and so is the
// unauthenticated login POST, which carries no session cookie yet).
func (m *Middleware) CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
			next.ServeHTTP(w, r)
			return
		}
		// Not cookie-authenticated → not CSRF-able.
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := r.Cookie("session"); err != nil {
			next.ServeHTTP(w, r)
			return
		}

		// Prefer the Fetch Metadata signal when the browser sends it.
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
			if site == "cross-site" {
				writeForbidden(w, "cross-site request blocked")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		// Fallback: if an Origin is present it must match the request host.
		if origin := r.Header.Get("Origin"); origin != "" {
			if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
				writeForbidden(w, "cross-origin request blocked")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
