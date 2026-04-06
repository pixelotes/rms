package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"raspberry-media-server/internal/media"
)

type crawlRequest struct {
	Path string `json:"path"`
}

func (s *Server) handleCrawlMetadata(w http.ResponseWriter, r *http.Request) {
	var req crawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if !media.IsPathAllowed(req.Path, s.librariesForRequest(r)) {
		respondError(w, http.StatusForbidden, "Path denied")
		return
	}

	bin, err := exec.LookPath("metacrawler")
	if err != nil {
		// Try in same directory as current binary
		bin = "./metacrawler"
	}

	args := []string{"--force", "--path", req.Path}

	// Detect content type from library config
	for _, lib := range s.librariesForRequest(r) {
		absLib, _ := absPath(lib.Path)
		absReq, _ := absPath(req.Path)
		if strings.HasPrefix(absReq, absLib) {
			args = append(args, "--type", lib.ContentType)
			break
		}
	}

	cmd := exec.Command(bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"output":  string(output),
			"error":   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"output":  string(output),
	})
}

func (s *Server) handleCrawlSubtitles(w http.ResponseWriter, r *http.Request) {
	var req crawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if !media.IsPathAllowed(req.Path, s.librariesForRequest(r)) {
		respondError(w, http.StatusForbidden, "Path denied")
		return
	}

	bin, err := exec.LookPath("subcrawler")
	if err != nil {
		bin = "./subcrawler"
	}

	args := []string{"--recursive", "--path", req.Path}

	cmd := exec.Command(bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"output":  string(output),
			"error":   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"output":  string(output),
	})
}

func (s *Server) handleCrawlThumbnails(w http.ResponseWriter, r *http.Request) {
	var req crawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if !media.IsPathAllowed(req.Path, s.librariesForRequest(r)) {
		respondError(w, http.StatusForbidden, "Path denied")
		return
	}

	bin, err := exec.LookPath("metacrawler")
	if err != nil {
		bin = "./metacrawler"
	}

	args := []string{"--thumbnails", "--path", req.Path}

	cmd := exec.Command(bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"output":  string(output),
			"error":   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"output":  string(output),
	})
}

func (s *Server) handleSubtitlesList(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Path[len("/api/v1/subtitles-list/"):]

	// Decode if base64
	if decoded, err := media.ItemPath(filePath); err == nil && strings.Contains(decoded, "/") {
		filePath = decoded
	}

	// Ensure absolute path
	if !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}

	if !media.IsPathAllowed(filePath, s.librariesForRequest(r)) {
		respondError(w, http.StatusForbidden, "Path denied")
		return
	}

	subs := media.FindSubtitles(filePath)

	type subInfo struct {
		Language string `json:"language"`
		Label    string `json:"label"`
		Filename string `json:"filename"`
	}

	result := make([]subInfo, len(subs))
	for i, sub := range subs {
		parts := strings.Split(sub.FilePath, "/")
		result[i] = subInfo{
			Language: sub.Language,
			Label:    sub.Label,
			Filename: parts[len(parts)-1],
		}
	}

	respondJSON(w, http.StatusOK, result)
}

func absPath(p string) (string, error) {
	return fmt.Sprintf("%s", p), nil
}
