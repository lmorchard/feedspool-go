package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

// requireAuth gates every API route when a token is configured.
//
// Auth is off by default: with no token set, the middleware is a pass-through.
// That is the chosen posture for a personal tool bound to localhost; cmd/serve
// warns when the API is reachable off-box without one.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token == "" {
			next(w, r)
			return
		}

		header := r.Header.Get("Authorization")
		presented := ""
		if strings.HasPrefix(header, bearerPrefix) {
			presented = header[len(bearerPrefix):]
		}

		// Constant-time even when the prefix was missing, so a malformed
		// header and a wrong token take the same path.
		if subtle.ConstantTimeCompare([]byte(presented), []byte(s.cfg.Token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "a valid bearer token is required")
			return
		}
		next(w, r)
	}
}
