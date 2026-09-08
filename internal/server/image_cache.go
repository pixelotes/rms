package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/mux"
)

// Image variants (resized/re-encoded posters) are generated with ffmpeg once
// and stored under <cache_path>/images. Without app.cache_path the original
// file is always served, as before.
var (
	imageSem      = make(chan struct{}, 2) // resizing is short-lived; allow a little parallelism
	imageBuildMu  sync.Mutex
	imageBuilding = map[string]*sync.Mutex{}
)

// imageParam reads a Jellyfin image parameter from the query string or, for
// the long path form (/Images/{type}/{index}/{tag}/{format}/{maxWidth}/...),
// from the route variables. Empty when absent.
func imageParam(r *http.Request, names ...string) string {
	vars := mux.Vars(r)
	for _, n := range names {
		if v := r.URL.Query().Get(n); v != "" {
			return v
		}
		if v := vars[n]; v != "" {
			return v
		}
	}
	return ""
}

func imageIntParam(r *http.Request, names ...string) int {
	v, _ := strconv.Atoi(imageParam(r, names...))
	if v < 0 {
		return 0
	}
	return v
}

// imageVariant returns the path of a cached copy of imgPath that honours the
// request's sizing/format parameters (maxWidth/maxHeight, width/height,
// fillWidth/fillHeight, quality, format). It returns "" when no variant is
// wanted or possible (no cache configured, no sizing requested, ffmpeg
// missing or failing) so the caller serves the original file.
func (s *Server) imageVariant(r *http.Request, imgPath string) string {
	if s.streamCache == nil {
		return ""
	}
	maxW := imageIntParam(r, "maxWidth", "MaxWidth", "width", "Width", "fillWidth", "FillWidth")
	maxH := imageIntParam(r, "maxHeight", "MaxHeight", "height", "Height", "fillHeight", "FillHeight")
	format := strings.ToLower(imageParam(r, "format", "Format"))
	quality := imageIntParam(r, "quality", "Quality")
	if quality <= 0 || quality > 100 {
		quality = 90
	}
	// Ignore absurd or unhelpful sizes: nothing to gain below 16px, and a
	// request larger than any poster is effectively "the original".
	if maxW < 16 {
		maxW = 0
	}
	if maxH < 16 {
		maxH = 0
	}
	ext := strings.ToLower(filepath.Ext(imgPath))
	switch format {
	case "webp":
		ext = ".webp"
	case "jpg", "jpeg":
		ext = ".jpg"
	case "png":
		ext = ".png"
	case "", "original":
		format = ""
	default:
		format = "" // unsupported target (gif, bmp, svg...) -> keep the source
	}
	if maxW == 0 && maxH == 0 && format == "" {
		return ""
	}

	st, err := os.Stat(imgPath)
	if err != nil {
		return ""
	}
	if out := s.buildImageVariant(imgPath, st, maxW, maxH, quality, ext); out != "" {
		return out
	}
	// The requested encoder may be missing from this ffmpeg build (webp is
	// optional); a JPEG of the right size still beats the full-size original.
	if ext == ".webp" {
		return s.buildImageVariant(imgPath, st, maxW, maxH, quality, ".jpg")
	}
	return ""
}

// buildImageVariant returns the cached variant path, generating it on first
// use. A failed generation leaves a marker so it is not retried on every
// request; "" means unavailable.
func (s *Server) buildImageVariant(imgPath string, st os.FileInfo, maxW, maxH, quality int, ext string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d|%d|%d|%d|%s", imgPath, st.Size(), st.ModTime().UnixNano(), maxW, maxH, quality, ext)))
	dir := filepath.Join(s.streamCache.root, "images")
	out := filepath.Join(dir, hex.EncodeToString(sum[:])[:32]+ext)
	failed := out + ".failed"

	// Serialize builders of the same variant so a burst of identical
	// requests (a grid of posters) runs ffmpeg once.
	imageBuildMu.Lock()
	mu, ok := imageBuilding[out]
	if !ok {
		mu = &sync.Mutex{}
		imageBuilding[out] = mu
	}
	imageBuildMu.Unlock()
	mu.Lock()
	defer mu.Unlock()

	if _, err := os.Stat(out); err == nil {
		return out
	}
	if _, err := os.Stat(failed); err == nil {
		return ""
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ""
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ""
	}

	// Never upscale: clamp each requested dimension to the source size and
	// keep the aspect ratio. -2 keeps the free dimension even for yuv420.
	var scale string
	switch {
	case maxW > 0 && maxH > 0:
		scale = fmt.Sprintf("scale='min(%d,iw)':'min(%d,ih)':force_original_aspect_ratio=decrease", maxW, maxH)
	case maxW > 0:
		scale = fmt.Sprintf("scale='min(%d,iw)':-2", maxW)
	case maxH > 0:
		scale = fmt.Sprintf("scale=-2:'min(%d,ih)'", maxH)
	default:
		scale = "scale=iw:ih"
	}
	args := []string{"-loglevel", "error", "-y", "-i", imgPath, "-frames:v", "1", "-vf", scale}
	switch ext {
	case ".jpg", ".jpeg":
		// mjpeg q:v runs 2 (best) .. 31 (worst); map Jellyfin's 0-100 quality onto it.
		args = append(args, "-c:v", "mjpeg", "-q:v", strconv.Itoa(2+(100-quality)*29/100))
	case ".webp":
		args = append(args, "-c:v", "libwebp", "-quality", strconv.Itoa(quality))
	case ".png":
		args = append(args, "-c:v", "png")
	}
	tmp := out + ".partial"
	args = append(args, "-f", "image2", tmp)

	imageSem <- struct{}{}
	err = exec.Command(ffmpegPath, args...).Run()
	<-imageSem
	if err != nil {
		os.Remove(tmp)
		os.WriteFile(failed, nil, 0644)
		log.Printf("image cache: ffmpeg failed for %s (%s -> %s): %v", imgPath, scale, ext, err)
		return ""
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return ""
	}
	return out
}
