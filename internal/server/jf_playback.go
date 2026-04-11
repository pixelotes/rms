package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/media"
)

func (s *Server) jfPlaybackInfo(w http.ResponseWriter, r *http.Request) {
	itemID := mux.Vars(r)["itemId"]

	path, err := media.ItemPath(itemID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Item not found")
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		respondError(w, http.StatusNotFound, "Item not found")
		return
	}
	if info.IsDir() {
		videoPath := media.FindVideoFile(path)
		if videoPath == "" {
			respondError(w, http.StatusNotFound, "No video file found")
			return
		}
		path = videoPath
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"MediaSources":  s.buildMediaSources(path),
		"PlaySessionId": fmt.Sprintf("ps-%s", itemID[:8]),
	})
}

func (s *Server) jfVideoStream(w http.ResponseWriter, r *http.Request) {
	itemID := mux.Vars(r)["itemId"]

	path, err := media.ItemPath(itemID)
	if err != nil {
		s.logDebug("VideoStream: ItemPath(%s) failed: %v", itemID, err)
		respondError(w, http.StatusNotFound, "Item not found")
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		s.logDebug("VideoStream: Stat(%s) failed: %v", path, err)
		respondError(w, http.StatusNotFound, "Item not found")
		return
	}
	if info.IsDir() {
		videoPath := media.FindVideoFile(path)
		if videoPath == "" {
			s.logDebug("VideoStream: no video file in dir %s", path)
			respondError(w, http.StatusNotFound, "No video file found")
			return
		}
		path = videoPath
	}

	s.logDebug("VideoStream: serving %s (static=%s)", path, queryParam(r, "static", "Static"))

	// Treat static=false same as direct play — we don't do real transcoding.
	// Clients that request static=false expect a stream; serve direct play.
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

	// The subtitle index in MediaStreams starts after video (0) and audio (1) streams,
	// so subtitle index 2 = first subtitle, 3 = second, etc.
	subIdx := subtitleIndex - 2
	if subIdx < 0 || subIdx >= len(subs) {
		http.NotFound(w, r)
		return
	}

	sub := subs[subIdx]

	if format == "vtt" {
		vttContent, err := media.ConvertSRTToVTT(sub.FilePath)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to convert subtitle")
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

func (s *Server) buildMediaSources(videoPath string) []map[string]interface{} {
	info, err := os.Stat(videoPath)
	if err != nil {
		return make([]map[string]interface{}, 0)
	}

	ext := strings.ToLower(filepath.Ext(videoPath))
	container := strings.TrimPrefix(ext, ".")

	videoCodec := "h264"
	audioCodec := "aac"

	streamIndex := 0
	mediaStreams := []map[string]interface{}{
		{
			"Type":                   "Video",
			"Index":                  streamIndex,
			"Codec":                  videoCodec,
			"IsDefault":              true,
			"IsExternal":             false,
			"IsForced":               false,
			"IsHearingImpaired":      false,
			"IsInterlaced":           false,
			"IsTextSubtitleStream":   false,
			"SupportsExternalStream": false,
			"VideoRange":             "SDR",
			"VideoRangeType":         "SDR",
			"AudioSpatialFormat":     "None",
		},
	}
	streamIndex++
	mediaStreams = append(mediaStreams, map[string]interface{}{
		"Type":                   "Audio",
		"Index":                  streamIndex,
		"Codec":                  audioCodec,
		"IsDefault":              true,
		"IsExternal":             false,
		"IsForced":               false,
		"IsHearingImpaired":      false,
		"IsInterlaced":           false,
		"IsTextSubtitleStream":   false,
		"SupportsExternalStream": false,
		"VideoRange":             "Unknown",
		"VideoRangeType":         "Unknown",
		"AudioSpatialFormat":     "None",
		"Language":               "und",
	})
	streamIndex++

	subtitles := media.FindSubtitles(videoPath)
	for i, sub := range subtitles {
		mediaStreams = append(mediaStreams, map[string]interface{}{
			"Type":                   "Subtitle",
			"Index":                  streamIndex,
			"Codec":                  "srt",
			"Language":               sub.Language,
			"DisplayTitle":           sub.Label,
			"IsDefault":              i == 0,
			"IsForced":               false,
			"IsExternal":             true,
			"IsHearingImpaired":      false,
			"IsInterlaced":           false,
			"IsTextSubtitleStream":   true,
			"SupportsExternalStream": true,
			"VideoRange":             "Unknown",
			"VideoRangeType":         "Unknown",
			"AudioSpatialFormat":     "None",
			"DeliveryMethod":         "External",
			"DeliveryUrl":            fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/0/Stream.srt", media.ItemID(videoPath), media.ItemID(videoPath), streamIndex),
			"Path":                   sub.FilePath,
		})
		streamIndex++
	}

	sourceID := media.ItemID(videoPath)

	var runTimeTicks int64
	if nfo, err := media.ParseEpisodeNFO(videoPath); err == nil {
		if nfo.StreamDetails != nil && nfo.StreamDetails.DurationSeconds > 0 {
			runTimeTicks = int64(nfo.StreamDetails.DurationSeconds) * 10000000
		} else if nfo.Runtime > 0 {
			runTimeTicks = int64(nfo.Runtime) * 600000000
		}
	}
	if runTimeTicks == 0 {
		if nfo, err := media.ParseNFO(filepath.Dir(videoPath)); err == nil && nfo.Runtime > 0 {
			runTimeTicks = int64(nfo.Runtime) * 600000000
		}
	}
	if runTimeTicks == 0 {
		if seconds := media.ProbeDuration(videoPath); seconds > 0 {
			runTimeTicks = int64(seconds * 10000000)
		}
	}

	source := map[string]interface{}{
		"Id":                     sourceID,
		"Path":                   videoPath,
		"Protocol":               "File",
		"Type":                   "Default",
		"Container":              container,
		"Size":                   info.Size(),
		"Name":                   filepath.Base(videoPath),
		"IsRemote":               false,
		"SupportsDirectPlay":     true,
		"SupportsDirectStream":   true,
		"SupportsTranscoding":    true,
		"ReadAtNativeFramerate":  false,
		"IgnoreDts":              false,
		"IgnoreIndex":            false,
		"GenPtsInput":            false,
		"IsInfiniteStream":       false,
		"RequiresOpening":        false,
		"RequiresClosing":        false,
		"RequiresLooping":        false,
		"SupportsProbing":        true,
		"HasSegments":            false,
		"TranscodingSubProtocol": "http",
		"MediaStreams":           mediaStreams,
		"DirectStreamUrl":        fmt.Sprintf("/Videos/%s/stream?static=true&mediaSourceId=%s", sourceID, sourceID),
	}

	if runTimeTicks > 0 {
		source["RunTimeTicks"] = runTimeTicks
	}

	return []map[string]interface{}{source}
}
