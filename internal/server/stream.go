package server

import (
	"log"
	"net/http"
	"os/exec"
	"strings"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/media"
)

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	filePath := mux.Vars(r)["filePath"]

	// Decode if it's a base64 item ID, otherwise treat as path
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

	strategy := r.URL.Query().Get("strategy")
	if strategy == "" {
		strategy = "direct"
	}

	switch strategy {
	case "direct":
		s.streamDirect(w, r, filePath)
	case "remux":
		s.streamRemux(w, r, filePath)
	case "transcode":
		s.streamTranscode(w, r, filePath)
	default:
		s.streamDirect(w, r, filePath)
	}
}

func (s *Server) streamDirect(w http.ResponseWriter, r *http.Request, filePath string) {
	http.ServeFile(w, r, filePath)
}

func (s *Server) streamRemux(w http.ResponseWriter, r *http.Request, filePath string) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		log.Println("FFmpeg not found, falling back to direct play")
		http.ServeFile(w, r, filePath)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Connection", "keep-alive")

	args := []string{
		"-i", filePath,
		"-c:v", "copy",
		"-c:a", "aac", "-ac", "2",
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof+separate_moof",
		"pipe:1",
	}

	cmd := exec.CommandContext(r.Context(), ffmpegPath, args...)
	cmd.Stdout = w
	cmd.Stderr = log.Writer()

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start FFmpeg remux: %v", err)
		return
	}

	if err := cmd.Wait(); err != nil {
		if r.Context().Err() == nil {
			log.Printf("FFmpeg remux interrupted unexpectedly: %v", err)
		}
		// Client closed connection during remux (broken pipe). This is normal.
	}
}

func (s *Server) streamTranscode(w http.ResponseWriter, r *http.Request, filePath string) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		log.Println("FFmpeg not found, falling back to direct play")
		http.ServeFile(w, r, filePath)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Connection", "keep-alive")

	args := []string{
		"-i", filePath,
		"-c:v", "libx264", "-preset", "ultrafast", "-profile:v", "main", "-level", "4.0", "-pix_fmt", "yuv420p", "-crf", "23",
		"-c:a", "aac", "-ac", "2",
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof+separate_moof",
		"pipe:1",
	}

	cmd := exec.CommandContext(r.Context(), ffmpegPath, args...)
	cmd.Stdout = w
	cmd.Stderr = log.Writer()

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start FFmpeg transcode: %v", err)
		return
	}

	if err := cmd.Wait(); err != nil {
		if r.Context().Err() == nil {
			log.Printf("FFmpeg transcode interrupted unexpectedly: %v", err)
		}
		// Client closed connection during transcode (broken pipe). This is normal.
	}
}
