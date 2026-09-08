package media

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// ProbeStream is one container stream as reported by ffprobe.
type ProbeStream struct {
	Index         int
	Type          string // "video" | "audio" | "subtitle"
	Codec         string
	Profile       string
	Width         int
	Height        int
	AspectRatio   string
	PixFmt        string
	ColorTransfer string
	FrameRate     float64
	Channels      int
	ChannelLayout string
	SampleRate    int
	BitRate       int64
	Language      string
	Title         string
	Default       bool
	Forced        bool
}

// ProbeInfo is the cached ffprobe result for one media file.
type ProbeInfo struct {
	Duration float64 // seconds
	BitRate  int64   // container bit rate (bits/s), 0 if unknown
	Streams  []ProbeStream
}

type probeEntry struct {
	size  int64
	mtime int64
	info  *ProbeInfo // nil when ffprobe failed for this file version
}

var (
	probeMu    sync.Mutex
	probeCache = map[string]*probeEntry{}
	// probeSem caps concurrent ffprobe processes: a recursive listing on a
	// Raspberry Pi must never fan out into dozens of probes at once.
	probeSem = make(chan struct{}, 2)
)

// CachedProbe returns the cached probe for videoPath without ever spawning
// ffprobe. ok is false when the file has not been probed yet (or changed).
func CachedProbe(videoPath string) (info *ProbeInfo, ok bool) {
	st, err := os.Stat(videoPath)
	if err != nil {
		return nil, false
	}
	probeMu.Lock()
	defer probeMu.Unlock()
	e, hit := probeCache[videoPath]
	if !hit || e.size != st.Size() || e.mtime != st.ModTime().UnixNano() {
		return nil, false
	}
	return e.info, e.info != nil
}

// Probe returns stream/format information for videoPath, running ffprobe at
// most once per file version (size+mtime). Failures are cached too, so a
// broken file or a missing ffprobe binary does not trigger repeated spawns.
// ok is false when no information is available.
func Probe(videoPath string) (info *ProbeInfo, ok bool) {
	st, err := os.Stat(videoPath)
	if err != nil {
		return nil, false
	}
	size, mtime := st.Size(), st.ModTime().UnixNano()

	probeMu.Lock()
	if e, hit := probeCache[videoPath]; hit && e.size == size && e.mtime == mtime {
		probeMu.Unlock()
		return e.info, e.info != nil
	}
	probeMu.Unlock()

	probeSem <- struct{}{}
	info = runFFprobe(videoPath)
	<-probeSem

	probeMu.Lock()
	probeCache[videoPath] = &probeEntry{size: size, mtime: mtime, info: info}
	probeMu.Unlock()
	return info, info != nil
}

// ProbeDuration returns the duration of a video file in seconds, or 0 if
// ffprobe is unavailable or fails. Results are cached (see Probe).
func ProbeDuration(videoPath string) float64 {
	if info, ok := Probe(videoPath); ok {
		return info.Duration
	}
	return 0
}

func runFFprobe(videoPath string) *ProbeInfo {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil
	}
	out, err := exec.Command(ffprobe,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-i", videoPath,
	).Output()
	if err != nil {
		return nil
	}

	var raw struct {
		Format struct {
			Duration string `json:"duration"`
			BitRate  string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			Index              int               `json:"index"`
			CodecType          string            `json:"codec_type"`
			CodecName          string            `json:"codec_name"`
			Profile            string            `json:"profile"`
			Width              int               `json:"width"`
			Height             int               `json:"height"`
			DisplayAspectRatio string            `json:"display_aspect_ratio"`
			PixFmt             string            `json:"pix_fmt"`
			ColorTransfer      string            `json:"color_transfer"`
			RFrameRate         string            `json:"r_frame_rate"`
			AvgFrameRate       string            `json:"avg_frame_rate"`
			Channels           int               `json:"channels"`
			ChannelLayout      string            `json:"channel_layout"`
			SampleRate         string            `json:"sample_rate"`
			BitRate            string            `json:"bit_rate"`
			Tags               map[string]string `json:"tags"`
			Disposition        map[string]int    `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}

	info := &ProbeInfo{}
	info.Duration, _ = strconv.ParseFloat(raw.Format.Duration, 64)
	info.BitRate, _ = strconv.ParseInt(raw.Format.BitRate, 10, 64)
	for _, s := range raw.Streams {
		if s.CodecType != "video" && s.CodecType != "audio" && s.CodecType != "subtitle" {
			continue
		}
		// Attached pictures (cover art) are reported as video streams.
		if s.CodecType == "video" && s.Disposition["attached_pic"] == 1 {
			continue
		}
		ps := ProbeStream{
			Index:         s.Index,
			Type:          s.CodecType,
			Codec:         s.CodecName,
			Profile:       s.Profile,
			Width:         s.Width,
			Height:        s.Height,
			AspectRatio:   s.DisplayAspectRatio,
			PixFmt:        s.PixFmt,
			ColorTransfer: s.ColorTransfer,
			FrameRate:     parseFrameRate(s.AvgFrameRate),
			Channels:      s.Channels,
			ChannelLayout: s.ChannelLayout,
			Language:      s.Tags["language"],
			Title:         s.Tags["title"],
			Default:       s.Disposition["default"] == 1,
			Forced:        s.Disposition["forced"] == 1,
		}
		if ps.FrameRate == 0 {
			ps.FrameRate = parseFrameRate(s.RFrameRate)
		}
		ps.SampleRate, _ = strconv.Atoi(s.SampleRate)
		ps.BitRate, _ = strconv.ParseInt(s.BitRate, 10, 64)
		if ps.Language == "" {
			ps.Language = "und"
		}
		info.Streams = append(info.Streams, ps)
	}
	return info
}

// parseFrameRate turns an ffprobe rational ("24000/1001") into a float.
func parseFrameRate(s string) float64 {
	if s == "" || s == "0/0" {
		return 0
	}
	if num, den, ok := strings.Cut(s, "/"); ok {
		n, err1 := strconv.ParseFloat(num, 64)
		d, err2 := strconv.ParseFloat(den, 64)
		if err1 != nil || err2 != nil || d == 0 {
			return 0
		}
		return n / d
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
