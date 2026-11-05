package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"raspberry-media-server/internal/media"
)

// mediaStream builds a Jellyfin MediaStream DTO with every non-nullable
// boolean/enum field present, then overlays the given fields. Video, audio,
// Live TV and subtitle streams all go through here so the field set cannot
// drift between builders.
func (s *Server) mediaStream(kind string, index int, fields map[string]interface{}) map[string]interface{} {
	videoRange := "Unknown"
	if kind == "Video" {
		videoRange = "SDR"
	}
	stream := map[string]interface{}{
		"Type":                   kind,
		"Index":                  index,
		"IsDefault":              false,
		"IsExternal":             false,
		"IsForced":               false,
		"IsHearingImpaired":      false,
		"IsInterlaced":           false,
		"IsTextSubtitleStream":   false,
		"SupportsExternalStream": false,
		"VideoRange":             videoRange,
		"VideoRangeType":         videoRange,
		"AudioSpatialFormat":     "None",
	}
	// IsOriginal was introduced in Jellyfin 12.0 and is not part of the
	// 10.x schemas, so it is only emitted when the configured version is >= 12.0.
	if s.jfVersionAtLeast(12, 0) {
		stream["IsOriginal"] = false
	}
	for k, v := range fields {
		stream[k] = v
	}
	return stream
}

// textSubtitleCodecs are the embedded subtitle codecs a player can render as text.
var textSubtitleCodecs = map[string]bool{
	"subrip": true, "srt": true, "ass": true, "ssa": true, "webvtt": true,
	"mov_text": true, "text": true, "ttml": true,
}

// probedMediaStreams converts an ffprobe result into MediaStream DTOs.
func (s *Server) probedMediaStreams(info *media.ProbeInfo) []map[string]interface{} {
	streams := make([]map[string]interface{}, 0, len(info.Streams))
	seenDefaultAudio := false
	for _, ps := range info.Streams {
		switch ps.Type {
		case "video":
			f := map[string]interface{}{
				"Codec":     ps.Codec,
				"IsDefault": true,
				"Language":  ps.Language,
			}
			if ps.Profile != "" {
				f["Profile"] = ps.Profile
			}
			if ps.Width > 0 && ps.Height > 0 {
				f["Width"] = ps.Width
				f["Height"] = ps.Height
			}
			if ps.AspectRatio != "" {
				f["AspectRatio"] = ps.AspectRatio
			}
			if ps.PixFmt != "" {
				f["PixelFormat"] = ps.PixFmt
			}
			if ps.FrameRate > 0 {
				f["AverageFrameRate"] = ps.FrameRate
				f["RealFrameRate"] = ps.FrameRate
			}
			if ps.BitRate > 0 {
				f["BitRate"] = ps.BitRate
			}
			switch ps.ColorTransfer {
			case "smpte2084":
				f["VideoRange"], f["VideoRangeType"] = "HDR", "HDR10"
			case "arib-std-b67":
				f["VideoRange"], f["VideoRangeType"] = "HDR", "HLG"
			}
			f["DisplayTitle"] = strings.TrimSpace(fmt.Sprintf("%dp %s", ps.Height, strings.ToUpper(ps.Codec)))
			streams = append(streams, s.mediaStream("Video", ps.Index, f))
		case "audio":
			isDefault := ps.Default || !seenDefaultAudio
			seenDefaultAudio = seenDefaultAudio || isDefault
			f := map[string]interface{}{
				"Codec":     ps.Codec,
				"Language":  ps.Language,
				"IsDefault": isDefault,
			}
			if ps.Channels > 0 {
				f["Channels"] = ps.Channels
			}
			if ps.ChannelLayout != "" {
				f["ChannelLayout"] = ps.ChannelLayout
			}
			if ps.SampleRate > 0 {
				f["SampleRate"] = ps.SampleRate
			}
			if ps.BitRate > 0 {
				f["BitRate"] = ps.BitRate
			}
			if ps.Profile != "" {
				f["Profile"] = ps.Profile
			}
			if ps.Title != "" {
				f["Title"] = ps.Title
			}
			f["DisplayTitle"] = audioDisplayTitle(ps)
			streams = append(streams, s.mediaStream("Audio", ps.Index, f))
		case "subtitle":
			isText := textSubtitleCodecs[ps.Codec]
			f := map[string]interface{}{
				"Codec":                ps.Codec,
				"Language":             ps.Language,
				"IsDefault":            ps.Default,
				"IsForced":             ps.Forced,
				"IsTextSubtitleStream": isText,
				// RMS does not extract embedded subtitles: the player renders
				// them from the container during direct play.
				"DeliveryMethod": "Embed",
			}
			if ps.Title != "" {
				f["Title"] = ps.Title
				f["DisplayTitle"] = ps.Title
			} else {
				f["DisplayTitle"] = strings.TrimSpace(ps.Language + " " + strings.ToUpper(ps.Codec))
			}
			streams = append(streams, s.mediaStream("Subtitle", ps.Index, f))
		}
	}
	return streams
}

func audioDisplayTitle(ps media.ProbeStream) string {
	parts := []string{ps.Language, strings.ToUpper(ps.Codec)}
	if ps.ChannelLayout != "" {
		parts = append(parts, ps.ChannelLayout)
	} else if ps.Channels > 0 {
		parts = append(parts, fmt.Sprintf("%dch", ps.Channels))
	}
	if ps.Title != "" {
		parts = append([]string{ps.Title}, parts...)
	}
	return strings.Join(parts, " - ")
}

// fallbackMediaStreams is used when no probe information is available
// (ffprobe missing, or a listing that must not spawn processes and whose
// file has not been probed yet). Codecs are a best guess.
func (s *Server) fallbackMediaStreams() []map[string]interface{} {
	return []map[string]interface{}{
		s.mediaStream("Video", 0, map[string]interface{}{"Codec": "h264", "IsDefault": true}),
		s.mediaStream("Audio", 1, map[string]interface{}{"Codec": "aac", "IsDefault": true, "Language": "und"}),
	}
}

// externalSubtitleBase returns the MediaStream index of the first external
// subtitle for a file: container streams come first, sidecar files after.
func externalSubtitleBase(info *media.ProbeInfo) int {
	if info != nil && len(info.Streams) > 0 {
		return len(info.Streams)
	}
	return 2 // fallback layout: video (0) + audio (1)
}

// buildMediaSources builds the MediaSourceInfo list for a video file.
//
// probe=true runs (cached) ffprobe so codecs, resolution, bitrate and embedded
// tracks are real — used by PlaybackInfo and single-item GETs, where clients
// decide whether they can direct play. probe=false never spawns a process:
// listings reuse the cache when the file was probed before and fall back to
// generic h264/aac streams otherwise.
func (s *Server) buildMediaSources(videoPath string, probe bool, playSessionID string) []map[string]interface{} {
	fi, err := os.Stat(videoPath)
	if err != nil {
		return make([]map[string]interface{}, 0)
	}

	var info *media.ProbeInfo
	if probe {
		info, _ = media.Probe(videoPath)
	} else {
		info, _ = media.CachedProbe(videoPath)
	}

	var mediaStreams []map[string]interface{}
	if info != nil && len(info.Streams) > 0 {
		mediaStreams = s.probedMediaStreams(info)
	} else {
		mediaStreams = s.fallbackMediaStreams()
	}

	sourceID := media.ItemID(videoPath)
	streamIndex := externalSubtitleBase(info)
	for i, sub := range media.FindSubtitles(videoPath) {
		mediaStreams = append(mediaStreams, s.mediaStream("Subtitle", streamIndex, map[string]interface{}{
			"Codec":                  "srt",
			"Language":               sub.Language,
			"DisplayTitle":           sub.Label,
			"IsDefault":              i == 0,
			"IsExternal":             true,
			"IsTextSubtitleStream":   true,
			"SupportsExternalStream": true,
			"DeliveryMethod":         "External",
			"DeliveryUrl":            fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/0/Stream.srt", sourceID, sourceID, streamIndex),
			"Path":                   sub.FilePath,
		}))
		streamIndex++
	}

	defaultAudio := -1
	for _, st := range mediaStreams {
		if st["Type"] == "Audio" && st["IsDefault"] == true {
			defaultAudio = st["Index"].(int)
			break
		}
	}

	sessionID := fmt.Sprintf("ps-%s", sourceID[:8])
	if playSessionID != "" {
		sessionID = playSessionID
	}

	// Duration: ffprobe is exact; NFO values are rounded to minutes.
	var runTimeTicks int64
	if info != nil && info.Duration > 0 {
		runTimeTicks = int64(info.Duration * 10000000)
	}
	if runTimeTicks == 0 {
		if nfo, err := media.ParseEpisodeNFO(videoPath); err == nil {
			if nfo.StreamDetails != nil && nfo.StreamDetails.DurationSeconds > 0 {
				runTimeTicks = int64(nfo.StreamDetails.DurationSeconds) * 10000000
			} else if nfo.Runtime > 0 {
				runTimeTicks = int64(nfo.Runtime) * 600000000
			}
		}
	}
	if runTimeTicks == 0 {
		if nfo, err := media.ParseNFO(filepath.Dir(videoPath)); err == nil && nfo.Runtime > 0 {
			runTimeTicks = int64(nfo.Runtime) * 600000000
		}
	}

	var bitrate int64
	if info != nil && info.BitRate > 0 {
		bitrate = info.BitRate
	} else if runTimeTicks > 0 {
		bitrate = fi.Size() * 8 * 10000000 / runTimeTicks
	}

	container := strings.TrimPrefix(strings.ToLower(filepath.Ext(videoPath)), ".")
	source := map[string]interface{}{
		"Id":                                  sourceID,
		"Path":                                videoPath,
		"Protocol":                            "File",
		"Type":                                "Default",
		"Container":                           container,
		"Size":                                fi.Size(),
		"Name":                                filepath.Base(videoPath),
		"IsRemote":                            false,
		"ETag":                                sourceID,
		"SupportsDirectPlay":                  true,
		"SupportsDirectStream":                true,
		"SupportsTranscoding":                 false,
		"SupportsProbing":                     true,
		"ReadAtNativeFramerate":               false,
		"IgnoreDts":                           false,
		"IgnoreIndex":                         false,
		"GenPtsInput":                         false,
		"IsInfiniteStream":                    false,
		"RequiresOpening":                     false,
		"RequiresClosing":                     false,
		"RequiresLooping":                     false,
		"HasSegments":                         false,
		"UseMostCompatibleTranscodingProfile": false,
		"DefaultSubtitleStreamIndex":          nil,
		"RequiredHttpHeaders":                 map[string]string{},
		"MediaStreams":                        mediaStreams,
		"TranscodingUrl":                      nil,
		"TranscodingSubProtocol":              "http", // non-nullable MediaStreamProtocol enum
		"TranscodingContainer":                nil,
	}
	if defaultAudio >= 0 {
		source["DefaultAudioStreamIndex"] = defaultAudio
	}
	if runTimeTicks > 0 {
		source["RunTimeTicks"] = runTimeTicks
	}
	if bitrate > 0 {
		source["Bitrate"] = bitrate
	}

	// Opt-in transcoding (player.stream_strategy contains "transcode"):
	// advertise a progressive (http) H.264/AAC MP4 transcode so clients that
	// cannot direct play the source fall back to it instead of failing.
	if s.config.Player.AllowsStrategy("transcode") {
		source["SupportsTranscoding"] = true
		source["TranscodingContainer"] = "mp4"
		source["TranscodingUrl"] = fmt.Sprintf(
			"/Videos/%s/stream.mp4?mediaSourceId=%s&playSessionId=%s&static=false&rmsStrategy=transcode&videoCodec=h264&audioCodec=aac&transcodingContainer=mp4",
			sourceID, sourceID, sessionID)
	}

	return []map[string]interface{}{source}
}
