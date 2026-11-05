package server

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

// legacyPrefixes are the URL prefixes Jellyfin still accepts for clients that
// grew up on Emby (Jellyfin strips them in LegacyEmbyRouteRewriteMiddleware).
var legacyPrefixes = []string{"/emby", "/mediabrowser"}

// compatHandler wraps the router with the request rewrites Jellyfin performs
// before routing, so third-party clients that rely on them keep working:
//   - "/emby/..." and "/mediabrowser/..." are served as "/...".
//   - paths are matched case-insensitively (ASP.NET routing semantics): a
//     request that matches no route as written is retried against the
//     registered templates ignoring case, e.g. /users/me -> /Users/Me.
func (s *Server) compatHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if p, ok := stripLegacyPrefix(path); ok {
			path = p
		}
		// The router has a NotFoundHandler, so Match() cannot tell a miss from
		// a hit; canonicalPath is cheap (only templates with the same segment
		// count are compared) and reports ok=false when nothing changes.
		if p, ok := s.canonicalPath(path); ok {
			path = p
		}
		if path != r.URL.Path {
			r2 := r.Clone(r.Context())
			r2.URL.Path = path
			r2.URL.RawPath = ""
			r = r2
		}
		next.ServeHTTP(w, r)
	})
}

// routeTemplate is one registered route split into path segments;
// placeholders ("{itemId}") match any segment, literals match ignoring case.
type routeTemplate struct {
	segs         []string
	placeholders int
}

// buildRouteIndex snapshots the registered route templates, grouped by
// segment count, for case-insensitive lookups. Templates with custom
// regexps (":.+") or prefix matches are left to the router itself.
func (s *Server) buildRouteIndex() {
	s.routeIndex = map[int][]routeTemplate{}
	_ = s.router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		tpl, err := route.GetPathTemplate()
		if err != nil || tpl == "" || tpl == "/" || strings.HasSuffix(tpl, "/") || strings.Contains(tpl, ":") {
			return nil
		}
		segs := strings.Split(strings.TrimPrefix(tpl, "/"), "/")
		rt := routeTemplate{segs: segs}
		for _, seg := range segs {
			if strings.HasPrefix(seg, "{") {
				rt.placeholders++
			}
		}
		s.routeIndex[len(segs)] = append(s.routeIndex[len(segs)], rt)
		return nil
	})
}

// canonicalPath maps a path onto a registered route template ignoring the
// case of literal segments, returning the canonical spelling. ok is false
// when nothing matches or the path is already canonical.
func (s *Server) canonicalPath(path string) (string, bool) {
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var best *routeTemplate
	for i := range s.routeIndex[len(segs)] {
		rt := &s.routeIndex[len(segs)][i]
		if best != nil && rt.placeholders >= best.placeholders {
			continue
		}
		match := true
		for j, seg := range rt.segs {
			if strings.HasPrefix(seg, "{") {
				continue
			}
			if !strings.EqualFold(seg, segs[j]) {
				match = false
				break
			}
		}
		if match {
			best = rt
		}
	}
	if best == nil {
		return path, false
	}
	out := make([]string, len(segs))
	for j, seg := range best.segs {
		if strings.HasPrefix(seg, "{") {
			out[j] = segs[j]
		} else {
			out[j] = seg
		}
	}
	canonical := "/" + strings.Join(out, "/")
	return canonical, canonical != path
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
