package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/media"
	"raspberry-media-server/internal/tv"
)

func (s *Server) jfPlaybackInfo(w http.ResponseWriter, r *http.Request) {
	itemID := mux.Vars(r)["itemId"]

	// TV channel: return its HLS MediaSource (infinite, non-seekable).
	if ch, ok := tv.LookupChannel(itemID); ok {
		if !s.tvLibraryVisible(ch.LibKey, r) {
			respondProblem(w, http.StatusNotFound, "Item not found")
			return
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"MediaSources":  []map[string]interface{}{s.channelMediaSource(ch)},
			"PlaySessionId": "ps-" + ch.ID[:8],
		})
		return
	}

	path, err := media.ItemPath(itemID)
	if err != nil {
		respondProblem(w, http.StatusNotFound, "Item not found")
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		respondProblem(w, http.StatusNotFound, "Item not found")
		return
	}
	if info.IsDir() {
		videoPath := media.FindVideoFile(path)
		if videoPath == "" {
			respondProblem(w, http.StatusNotFound, "No video file found")
			return
		}
		path = videoPath
	}

	playSessionID := fmt.Sprintf("ps-%s", itemID[:8])
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"MediaSources":  s.buildMediaSources(path, true, playSessionID),
		"PlaySessionId": playSessionID,
	})
}

func (s *Server) jfVideoStream(w http.ResponseWriter, r *http.Request) {
	itemID := mux.Vars(r)["itemId"]

	// TV channel: redirect to the upstream HLS stream.
	if ch, ok := tv.LookupChannel(itemID); ok {
		s.handleTVStream(w, r, ch.ID)
		return
	}

	path, err := media.ItemPath(itemID)
	if err != nil {
		s.logDebug("VideoStream: ItemPath(%s) failed: %v", itemID, err)
		respondProblem(w, http.StatusNotFound, "Item not found")
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		s.logDebug("VideoStream: Stat(%s) failed: %v", path, err)
		respondProblem(w, http.StatusNotFound, "Item not found")
		return
	}
	if info.IsDir() {
		videoPath := media.FindVideoFile(path)
		if videoPath == "" {
			s.logDebug("VideoStream: no video file in dir %s", path)
			respondProblem(w, http.StatusNotFound, "No video file found")
			return
		}
		path = videoPath
	}

	static := queryParam(r, "static", "Static")
	s.logDebug("VideoStream: serving %s (static=%s)", path, static)

	// Transcoding is only offered when player.stream_strategy lists
	// "transcode": buildMediaSources then advertises a TranscodingUrl that
	// carries rmsStrategy=transcode. Any other request (direct play, direct
	// stream, or a client trying to force a transcode) is served as-is.
	if queryParam(r, "rmsStrategy") == "transcode" && !strings.EqualFold(static, "true") &&
		s.config.Player.AllowsStrategy("transcode") {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "video/mp4")
			w.WriteHeader(http.StatusOK)
			return
		}
		s.streamTranscode(w, r, path)
		return
	}
	s.streamDirect(w, r, path)
}

func (s *Server) jfSubtitleStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	itemID := vars["itemId"]
	subtitleIndex, _ := strconv.Atoi(vars["index"])
	format := vars["format"]

	path, err := media.ItemPath(itemID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	videoPath := path
	if info.IsDir() {
		videoPath = media.FindVideoFile(path)
		if videoPath == "" {
			http.NotFound(w, r)
			return
		}
	}

	subs := media.FindSubtitles(videoPath)

	// External subtitles are numbered after the container's own streams (see
	// buildMediaSources), so map the MediaStream index back to the sidecar list.
	probeInfo, _ := media.Probe(videoPath)
	subIdx := subtitleIndex - externalSubtitleBase(probeInfo)
	if subIdx < 0 || subIdx >= len(subs) {
		http.NotFound(w, r)
		return
	}

	sub := subs[subIdx]

	if format == "vtt" {
		vttContent, err := media.ConvertSRTToVTT(sub.FilePath)
		if err != nil {
			respondProblem(w, http.StatusInternalServerError, "Failed to convert subtitle")
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeContent(w, r, "subtitles.vtt", time.Now(), vttContent)
	} else {
		w.Header().Set("Content-Type", "application/x-subrip; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeFile(w, r, sub.FilePath)
	}
}
