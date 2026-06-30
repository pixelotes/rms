package server

import (
	"io"
	"net/http"
	"strings"
	"time"


	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/media"
	"raspberry-media-server/internal/tv"
)

// tvChanPathPrefix marks a synthetic browse/stream path that resolves to a TV
// channel (e.g. "tv:chan:<channelID>"). Category folders use the library's own
// path plus "/<group-title>", so the WebUI's existing breadcrumb/back logic
// (which splits on "/") keeps working unchanged.
const tvChanPathPrefix = "tv:chan:"

// tvHTTPClient fetches remote channel logos. Logos are proxied (not redirected)
// so the WebUI's fetch()+blob() isn't blocked by cross-origin policy.
var tvHTTPClient = &http.Client{Timeout: 10 * time.Second}

// tvLibraryVisible reports whether the library identified by its config Path is
// accessible to the requesting user.
func (s *Server) tvLibraryVisible(libKey string, r *http.Request) bool {
	for _, lib := range s.librariesForRequest(r) {
		if lib.ContentType == "tv" && lib.Path == libKey {
			return true
		}
	}
	return false
}

// tvLibraryForPath returns the TV library whose root browse path matches, and
// the remaining group suffix ("" when path is the library root). ok is false
// when path is not under any visible TV library.
func (s *Server) tvLibraryForPath(path string, libs []config.Library) (lib config.Library, group string, ok bool) {
	for _, l := range libs {
		if l.ContentType != "tv" {
			continue
		}
		if path == l.Path {
			return l, "", true
		}
		if strings.HasPrefix(path, l.Path+"/") {
			return l, path[len(l.Path)+1:], true
		}
	}
	return config.Library{}, "", false
}

// browseTV handles the TV branch of GET /api/v1/browse. It returns false when
// path is not a TV path, letting the caller fall back to filesystem browsing.
func (s *Server) browseTV(w http.ResponseWriter, r *http.Request, path string) bool {
	libs := s.librariesForRequest(r)

	lib, group, ok := s.tvLibraryForPath(path, libs)
	if !ok {
		return false
	}

	if group == "" {
		respondJSON(w, http.StatusOK, s.browseTVCategories(lib))
	} else {
		respondJSON(w, http.StatusOK, s.browseTVChannels(lib, group))
	}
	return true
}

// browseTVCategories lists a TV library's groups as folders.
func (s *Server) browseTVCategories(lib config.Library) media.BrowseResponse {
	groups := tv.GroupsForLibrary(lib.Path)
	items := make([]media.BrowseItem, 0, len(groups))
	for _, g := range groups {
		items = append(items, media.BrowseItem{
			Name:  g.Name,
			Path:  lib.Path + "/" + g.Name,
			IsDir: true,
		})
	}
	return media.BrowseResponse{
		CurrentFolder: &media.BrowseFolder{Path: lib.Path, Name: lib.FriendlyName},
		Items:         items,
	}
}

// browseTVChannels lists the channels of one category.
func (s *Server) browseTVChannels(lib config.Library, group string) media.BrowseResponse {
	gid := tv.GroupID(lib.Path, group)
	channels := tv.ChannelsForGroup(gid)
	items := make([]media.BrowseItem, 0, len(channels))
	for _, ch := range channels {
		item := media.BrowseItem{
			Name:       ch.Name,
			Path:       tvChanPathPrefix + ch.ID,
			IsDir:      false,
			StreamType: "hls",
		}
		if ch.Logo != "" {
			item.Thumbnail = "/api/v1/tv/logo/" + ch.ID
		}
		items = append(items, item)
	}
	return media.BrowseResponse{
		CurrentFolder: &media.BrowseFolder{Path: lib.Path + "/" + group, Name: group},
		Items:         items,
	}
}

// hideEmptyTVLibraries drops TV libraries with no parsed channels from a
// library listing (ADR-015 visibility rule).
func (s *Server) hideEmptyTVLibraries(items []media.BrowseItem, libs []config.Library) []media.BrowseItem {
	empty := map[string]bool{}
	for _, lib := range libs {
		if lib.ContentType == "tv" && tv.ChannelCount(lib.Path) == 0 {
			empty[lib.Path] = true
		}
	}
	if len(empty) == 0 {
		return items
	}
	out := items[:0]
	for _, it := range items {
		if !empty[it.Path] {
			out = append(out, it)
		}
	}
	return out
}

// handleTVStream resolves a channel ID to its primary source and redirects.
// Proxy mode (custom headers / CORS) is deferred to a later phase.
func (s *Server) handleTVStream(w http.ResponseWriter, r *http.Request, channelID string) {
	ch, ok := tv.LookupChannel(channelID)
	if !ok {
		respondError(w, http.StatusNotFound, "Channel not found")
		return
	}
	if !s.tvLibraryVisible(ch.LibKey, r) {
		respondError(w, http.StatusForbidden, "Access denied")
		return
	}
	if len(ch.Sources) == 0 {
		respondError(w, http.StatusNotFound, "Channel has no source")
		return
	}
	http.Redirect(w, r, ch.Sources[0].URL, http.StatusFound)
}

// handleTVLogo proxies a channel's remote logo so the WebUI can load it without
// hitting cross-origin restrictions.
func (s *Server) handleTVLogo(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("channelId")
	ch, ok := tv.LookupChannel(channelID)
	if !ok || ch.Logo == "" {
		http.NotFound(w, r)
		return
	}
	if !s.tvLibraryVisible(ch.LibKey, r) {
		respondError(w, http.StatusForbidden, "Access denied")
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, ch.Logo, nil)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	resp, err := tvHTTPClient.Do(req)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.NotFound(w, r)
		return
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	io.Copy(w, resp.Body)
}
