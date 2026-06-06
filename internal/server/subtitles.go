package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/crawlers/subtitles"
	"raspberry-media-server/internal/media"
)

func (s *Server) handleSubtitles(w http.ResponseWriter, r *http.Request) {
	filePath, ok := s.authorizeSubtitlePath(w, r)
	if !ok {
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

type searchSubtitlesRequest struct {
	Query     string   `json:"query"`
	Languages []string `json:"languages"`
}

func (s *Server) handleSearchSubtitles(w http.ResponseWriter, r *http.Request) {
	filePath, ok := s.authorizeSubtitlePath(w, r)
	if !ok {
		return
	}

	var req searchSubtitlesRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional

	client := s.subtitleClient(req.Languages)
	if client == nil {
		respondError(w, http.StatusServiceUnavailable, "OpenSubtitles API key not configured")
		return
	}

	results, err := client.Search(subtitles.SearchOpts{
		VideoPath: filePath,
		Query:     req.Query,
		Languages: req.Languages,
	})
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	if results == nil {
		results = []subtitles.SearchResult{}
	}
	respondJSON(w, http.StatusOK, results)
}

type downloadSubtitleRequest struct {
	FileID   int    `json:"file_id"`
	Language string `json:"language"`
}

type downloadSubtitleResponse struct {
	Filename string `json:"filename"`
	Language string `json:"language"`
}

func (s *Server) handleDownloadSubtitle(w http.ResponseWriter, r *http.Request) {
	filePath, ok := s.authorizeSubtitlePath(w, r)
	if !ok {
		return
	}

	var req downloadSubtitleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == 0 {
		respondError(w, http.StatusBadRequest, "Invalid request: file_id required")
		return
	}

	lang := strings.ToLower(strings.TrimSpace(req.Language))
	if lang == "" {
		lang = "en"
	}

	client := s.subtitleClient(nil)
	if client == nil {
		respondError(w, http.StatusServiceUnavailable, "OpenSubtitles API key not configured")
		return
	}

	videoBase := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	destName := videoBase + "." + lang + ".srt"
	destPath := filepath.Join(filepath.Dir(filePath), destName)

	if err := client.Download(req.FileID, destPath); err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, downloadSubtitleResponse{
		Filename: destName,
		Language: lang,
	})
}

// authorizeSubtitlePath extracts the {filePath} mux var, decodes it if it's a
// base64 item ID, and rejects the request when the path is outside the user's
// allowed libraries. Returns the resolved absolute path or false if a response
// was already written.
func (s *Server) authorizeSubtitlePath(w http.ResponseWriter, r *http.Request) (string, bool) {
	filePath := mux.Vars(r)["filePath"]
	if decoded, err := media.ItemPath(filePath); err == nil && strings.Contains(decoded, "/") {
		filePath = decoded
	}
	if !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}
	if !media.IsPathAllowed(filePath, s.librariesForRequest(r)) {
		respondError(w, http.StatusForbidden, "Path denied")
		return "", false
	}
	return filePath, true
}

// subtitleClient constructs an OpenSubtitles client using the server's configured
// API key. Returns nil if no key is set. languages is optional; an empty slice
// falls back to the config-level default.
func (s *Server) subtitleClient(languages []string) *subtitles.Client {
	key := s.config.Crawlers.Subtitles.APIKey
	if key == "" {
		return nil
	}
	if len(languages) == 0 {
		languages = s.config.Crawlers.Subtitles.Languages
	}
	return subtitles.NewClient(key, languages)
}
