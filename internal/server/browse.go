package server

import (
	"net/http"

	"raspberry-media-server/internal/media"
)

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	libs := s.librariesForRequest(r)

	var resp media.BrowseResponse
	if path == "" || path == "." {
		resp = media.BrowseLibraries(libs)
	} else {
		resp = media.BrowseDirectory(path, libs)
		if len(resp.Items) == 0 && resp.CurrentFolder == nil {
			respondError(w, http.StatusForbidden, "Path denied")
			return
		}
	}

	respondJSON(w, http.StatusOK, resp)
}
