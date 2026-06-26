package media

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SubtitleTrack describes a single subtitle file alongside a video.
type SubtitleTrack struct {
	FilePath string
	Language string // BCP-47 / ISO 639-1 code (e.g. "en", "es")
	Label    string // human-readable label (e.g. "English")
}

// FindSubtitles returns all SRT subtitle tracks found alongside videoPath.
// Filenames follow the Kodi/Jellyfin convention: <videoname>.<lang>.srt
func FindSubtitles(videoPath string) []SubtitleTrack {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var tracks []SubtitleTrack
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.EqualFold(filepath.Ext(name), ".srt") {
			continue
		}

		// Must start with the video basename
		nameLower := strings.ToLower(name)
		baseLower := strings.ToLower(base)
		if !strings.HasPrefix(nameLower, baseLower) {
			continue
		}

		// Extract language from the part after the base: "video.en.srt" → "en"
		suffix := name[len(base):]                      // e.g. ".en.srt" or ".srt"
		suffix = strings.TrimSuffix(suffix, filepath.Ext(suffix)) // e.g. ".en" or ""
		suffix = strings.TrimPrefix(suffix, ".")

		lang, label := parseLangCode(suffix)
		tracks = append(tracks, SubtitleTrack{
			FilePath: filepath.Join(dir, name),
			Language: lang,
			Label:    label,
		})
	}
	return tracks
}

// ConvertSRTToVTT reads an SRT file and returns its WebVTT equivalent.
func ConvertSRTToVTT(srtPath string) (io.ReadSeeker, error) {
	data, err := os.ReadFile(srtPath)
	if err != nil {
		return nil, err
	}

	text := string(data)
	// Strip UTF-8 BOM and normalize line endings.
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var sb strings.Builder
	sb.WriteString("WEBVTT\n\n")

	for _, line := range strings.Split(text, "\n") {
		// Convert SRT timestamps: commas → dots ("00:00:01,000" → "00:00:01.000")
		if strings.Contains(line, "-->") {
			line = strings.ReplaceAll(line, ",", ".")
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	return strings.NewReader(sb.String()), nil
}

// langNameToCode maps common language names/codes to ISO 639-1 codes.
var langNameToCode = map[string]string{
	"english": "en", "en": "en",
	"spanish": "es", "español": "es", "es": "es",
	"french": "fr", "français": "fr", "fr": "fr",
	"german": "de", "deutsch": "de", "de": "de",
	"italian": "it", "italiano": "it", "it": "it",
	"portuguese": "pt", "português": "pt", "pt": "pt",
	"dutch": "nl", "nl": "nl",
	"russian": "ru", "ru": "ru",
	"japanese": "ja", "ja": "ja",
	"chinese": "zh", "zh": "zh",
	"korean": "ko", "ko": "ko",
	"arabic": "ar", "ar": "ar",
	"hindi": "hi", "hi": "hi",
	"swedish": "sv", "sv": "sv",
	"norwegian": "no", "no": "no",
	"danish": "da", "da": "da",
	"finnish": "fi", "fi": "fi",
	"polish": "pl", "pl": "pl",
	"turkish": "tr", "tr": "tr",
	"czech": "cs", "cs": "cs",
	"hungarian": "hu", "hu": "hu",
	"romanian": "ro", "ro": "ro",
	"catalan": "ca", "ca": "ca",
	"greek": "el", "el": "el",
	"hebrew": "he", "he": "he",
}

var langCodeToName = map[string]string{
	"en": "English", "es": "Spanish", "fr": "French", "de": "German",
	"it": "Italian", "pt": "Portuguese", "nl": "Dutch", "ru": "Russian",
	"ja": "Japanese", "zh": "Chinese", "ko": "Korean", "ar": "Arabic",
	"hi": "Hindi", "sv": "Swedish", "no": "Norwegian", "da": "Danish",
	"fi": "Finnish", "pl": "Polish", "tr": "Turkish", "cs": "Czech",
	"hu": "Hungarian", "ro": "Romanian", "ca": "Catalan", "el": "Greek",
	"he": "Hebrew",
}

func parseLangCode(s string) (code, label string) {
	if s == "" {
		return "und", "Unknown"
	}
	key := strings.ToLower(s)
	if c, ok := langNameToCode[key]; ok {
		if name, ok2 := langCodeToName[c]; ok2 {
			return c, name
		}
		return c, strings.Title(s)
	}
	// Unknown code: use as-is with title case label
	return key, strings.Title(s)
}
