package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/pixelotes/scout"

	"raspberry-media-server/internal/media"
	"raspberry-media-server/internal/scoutclient"
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

// subtitleBackend returns the scout backend, built on first use.
func (s *Server) subtitleBackend() (scout.Backend, error) {
	s.scoutOnce.Do(func() { s.scout, s.scoutErr = scoutclient.New(s.config) })
	return s.scout, s.scoutErr
}

func (s *Server) handleSearchSubtitles(w http.ResponseWriter, r *http.Request) {
	filePath, ok := s.authorizeSubtitlePath(w, r)
	if !ok {
		return
	}

	var req searchSubtitlesRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional

	b, err := s.subtitleBackend()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	langs := req.Languages
	if len(langs) == 0 {
		langs = s.config.Crawlers.Subtitles.Languages
	}
	libRoot := filepath.Dir(filePath)
	if lib, ok := scoutclient.LibraryFor(s.config, filePath); ok {
		libRoot = lib.Path
	}
	q := scoutclient.SubtitleQuery(filePath, libRoot, langs)
	if text := strings.TrimSpace(req.Query); text != "" {
		// A typed query replaces the automatic one. An episode in it
		// ("Show S01E03") wins over the file's; providers add it back.
		season, episode := q.Season, q.Episode
		if s, e := scoutclient.ParseEpisode(text); s > 0 || e > 0 {
			season, episode = s, e
			text = strings.TrimSpace(scoutclient.StripEpisode(text))
		}
		q = scout.SubtitleQuery{Query: text, Languages: langs, Season: season, Episode: episode}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	results, err := b.SearchSubtitles(ctx, q)
	switch {
	case errors.Is(err, scout.ErrNotFound):
		results = []scout.Subtitle{}
	case errors.Is(err, scout.ErrUnsupported):
		respondError(w, http.StatusServiceUnavailable, "No subtitle provider configured (crawlers.subtitles)")
		return
	case err != nil:
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, results)
}

type downloadSubtitleRequest struct {
	Provider string          `json:"provider"`
	FileID   json.RawMessage `json:"file_id"` // string; numbers from older clients are accepted
	Language string          `json:"language"`
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	fileID := strings.Trim(string(req.FileID), `"`)
	if fileID == "" || fileID == "0" || fileID == "null" {
		respondError(w, http.StatusBadRequest, "Invalid request: file_id required")
		return
	}

	lang := strings.ToLower(strings.TrimSpace(req.Language))
	if lang == "" {
		lang = "en"
	}

	b, err := s.subtitleBackend()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	f, err := b.DownloadSubtitle(ctx, req.Provider, fileID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, scout.ErrQuotaExceeded) {
			status = http.StatusTooManyRequests
		}
		respondError(w, status, err.Error())
		return
	}

	videoBase := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	destName := videoBase + "." + lang + ".srt"
	if err := scoutclient.WriteFile(filepath.Join(filepath.Dir(filePath), destName), f.Data); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
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
