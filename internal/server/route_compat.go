package server

import (
	"net/http"
	"strings"
)

// legacyPrefixes are the URL prefixes Jellyfin still accepts for clients that
// grew up on Emby (Jellyfin strips them in LegacyEmbyRouteRewriteMiddleware).
var legacyPrefixes = []string{"/emby", "/mediabrowser"}

// compatHandler wraps the router with the request rewrites Jellyfin performs
// before routing, so third-party clients that rely on them keep working:
//   - "/emby/..." and "/mediabrowser/..." are served as "/...".
func (s *Server) compatHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := stripLegacyPrefix(r.URL.Path); ok {
			r2 := r.Clone(r.Context())
			r2.URL.Path = p
			r2.URL.RawPath = ""
			r = r2
		}
		next.ServeHTTP(w, r)
	})
}

// stripLegacyPrefix removes a leading /emby or /mediabrowser segment
// (case-insensitive). "/emby" alone maps to "/".
func stripLegacyPrefix(path string) (string, bool) {
	for _, prefix := range legacyPrefixes {
		if len(path) < len(prefix) || !strings.EqualFold(path[:len(prefix)], prefix) {
			continue
		}
		rest := path[len(prefix):]
		if rest == "" {
			return "/", true
		}
		if rest[0] == '/' {
			return rest, true
		}
	}
	return path, false
}
