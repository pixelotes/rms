package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/media"
)

func (s *Server) handleSubtitles(w http.ResponseWriter, r *http.Request) {
	filePath := mux.Vars(r)["filePath"]

	// Decode if it's a base64 item ID
	if decoded, err := media.ItemPath(filePath); err == nil && strings.Contains(decoded, "/") {
		filePath = decoded
	}

	if !media.IsPathAllowed(filePath, s.librariesForRequest(r)) {
		respondError(w, http.StatusForbidden, "Path denied")
		return
	}

	requestedLang := r.URL.Query().Get("lang")
	if requestedLang == "" {
		requestedLang = "en"
	}

	subtitles := media.FindSubtitles(filePath)
	if len(subtitles) == 0 {
		respondError(w, http.StatusNotFound, "No subtitle files found")
		return
	}

	// Find requested language
	var selected *media.SubtitleTrack
	for i, sub := range subtitles {
		if sub.Language == requestedLang {
			selected = &subtitles[i]
			break
		}
	}
	if selected == nil {
		selected = &subtitles[0]
	}

	vttContent, err := media.ConvertSRTToVTT(selected.FilePath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to process subtitles")
		return
	}

	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	http.ServeContent(w, r, "subtitles.vtt", time.Now(), vttContent)
}
