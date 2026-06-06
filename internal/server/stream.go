package server

import (
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
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
	// ?start=N bypasses the cache: the cached file is always full-length
	// starting at 0, so it can't satisfy a partial-stream request from N.
	// "Nuclear" client recovery (handleVideoError) is the main caller.
	if ffmpegStartSeconds(r) > 0 {
		s.streamRemuxPipe(w, r, filePath)
		return
	}
	// If caching is configured, check it before falling back to live remux.
	// A finished cache entry is served via http.ServeFile (full Range/seek
	// support). A missing entry triggers a background cache build while the
	// current client gets the live pipe (current behavior, no seek).
	if s.streamCache != nil {
		if key, err := s.streamCache.key(filePath, "remux"); err == nil {
			cachedPath, isLeader := s.streamCache.claim(key)
			if cachedPath != "" {
				log.Printf("remux cache hit: %s", filePath)
				http.ServeFile(w, r, cachedPath)
				return
			}
			if isLeader {
				go s.buildRemuxCache(filePath, key)
			}
		}
	}
	s.streamRemuxPipe(w, r, filePath)
}

// streamRemuxPipe pipes ffmpeg stdout straight to the client. This is the
// only path when the cache is disabled or the cache entry is still being
// built. No seek support — the response has no Content-Length and no Range.
// Honors ?start=N (seconds) for "nuclear" client-side recovery from a
// corrupted region: ffmpeg is reseeked at the input with -ss N.
func (s *Server) streamRemuxPipe(w http.ResponseWriter, r *http.Request, filePath string) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		log.Println("FFmpeg not found, falling back to direct play")
		http.ServeFile(w, r, filePath)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Connection", "keep-alive")

	// Tolerance flags: drop corrupt packets and regenerate timestamps instead
	// of aborting. Lets a single bad chunk in the source cause artifacts in
	// the browser decoder rather than killing the whole pipe.
	args := []string{
		"-fflags", "+genpts+discardcorrupt",
		"-err_detect", "ignore_err",
	}
	if start := ffmpegStartSeconds(r); start > 0 {
		args = append(args, "-ss", strconv.FormatFloat(start, 'f', 3, 64))
	}
	args = append(args,
		"-i", filePath,
		"-c:v", "copy",
		"-c:a", "aac", "-ac", "2",
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof+separate_moof",
		"pipe:1",
	)

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

// ffmpegStartSeconds parses ?start= from the request. Returns 0 (no seek)
// for unset/invalid/negative values.
func ffmpegStartSeconds(r *http.Request) float64 {
	raw := r.URL.Query().Get("start")
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

// buildRemuxCache runs in its own goroutine, decoupled from any client
// request. It produces a faststart-indexed MP4 suitable for serving with
// http.ServeFile. If ffmpeg fails, the .partial file is removed.
func (s *Server) buildRemuxCache(filePath, key string) {
	outPath := s.streamCache.partialPath(key)
	log.Printf("remux cache: building %s -> %s", filePath, outPath)

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		s.streamCache.complete(key, false)
		return
	}

	args := []string{
		"-fflags", "+genpts+discardcorrupt",
		"-err_detect", "ignore_err",
		"-i", filePath,
		"-c:v", "copy",
		"-c:a", "aac", "-ac", "2",
		"-f", "mp4",
		"-movflags", "+faststart",
		"-y", outPath,
	}
	cmd := exec.Command(ffmpegPath, args...)
	cmd.Stderr = log.Writer()

	if err := cmd.Run(); err != nil {
		log.Printf("remux cache: build failed for %s: %v", filePath, err)
		// best-effort cleanup; complete() also tries to remove .partial
		os.Remove(outPath)
		s.streamCache.complete(key, false)
		return
	}

	log.Printf("remux cache: built %s", filePath)
	s.streamCache.complete(key, true)
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

	// Tolerance flags + decoder error concealment: -ec deblock papers over
	// lost macroblocks at the H.264 decoder so corruption manifests as brief
	// visual artifacts instead of aborting the transcode.
	args := []string{
		"-fflags", "+genpts+discardcorrupt",
		"-err_detect", "ignore_err",
		"-ec", "deblock",
	}
	if start := ffmpegStartSeconds(r); start > 0 {
		args = append(args, "-ss", strconv.FormatFloat(start, 'f', 3, 64))
	}
	args = append(args,
		"-i", filePath,
		"-c:v", "libx264", "-preset", "ultrafast", "-profile:v", "main", "-level", "4.0", "-pix_fmt", "yuv420p", "-crf", "23",
		"-c:a", "aac", "-ac", "2",
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof+separate_moof",
		"pipe:1",
	)

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
