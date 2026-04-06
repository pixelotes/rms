package server

import (
	"net/http"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/media"
)

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	imageID := mux.Vars(r)["imageId"]

	filePath, err := media.ItemPath(imageID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid image ID")
		return
	}

	if !media.IsPathAllowed(filePath, s.librariesForRequest(r)) {
		respondError(w, http.StatusForbidden, "Path denied")
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filePath)
}
