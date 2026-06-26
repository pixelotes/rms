package server

import (
	"net/http"

	"raspberry-media-server/internal/media"
)

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	libs := s.librariesForRequest(r)

	// TV libraries (content_type: tv) are browsed from the in-memory channel
	// store, not the filesystem: library root → categories → channels.
	if path != "" && path != "." {
		if s.browseTV(w, r, path) {
			return
		}
	}

	var resp media.BrowseResponse
	if path == "" || path == "." {
		resp = media.BrowseLibraries(libs)
		resp.Items = s.hideEmptyTVLibraries(resp.Items, libs)
	} else {
		resp = media.BrowseDirectory(path, libs)
		if len(resp.Items) == 0 && resp.CurrentFolder == nil {
			respondError(w, http.StatusForbidden, "Path denied")
			return
		}
	}

	respondJSON(w, http.StatusOK, resp)
}
