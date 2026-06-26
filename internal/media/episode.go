package media

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// episodePatterns are tried in order; the first match wins.
// Each must have exactly one numeric capture group for the episode number.
var episodePatterns = []*regexp.Regexp{
	// S01E05, s01e05, S01E05E06 (takes first episode)
	regexp.MustCompile(`(?i)[Ss]\d{1,2}[Ee](\d{1,3})`),
	// Episode 5, episode05, Ep.5, ep_05
	regexp.MustCompile(`(?i)(?:episode|ep)[._\s-]?(\d{1,3})`),
	// e05, E05 standalone (not preceded by a season marker)
	regexp.MustCompile(`(?i)(?:^|[^Ss\d])E(\d{1,3})(?:\D|$)`),
	// Leading zero-padded number at the start of the name: "05 - Title.mkv"
	regexp.MustCompile(`^(\d{1,3})\s*[-_\s]`),
	// Number at the very end before extension: "Title 05.mkv"
	regexp.MustCompile(`[-_\s.](\d{1,3})$`),
}

// ExtractEpisodeNumber attempts to parse an episode number from a filename.
// Returns 0 if no number can be found.
func ExtractEpisodeNumber(filename string) int {
	// Work on the base name without extension.
	name := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))

	for _, re := range episodePatterns {
		m := re.FindStringSubmatch(name)
		if len(m) >= 2 {
			if n, err := strconv.Atoi(m[1]); err == nil {
				return n
			}
		}
	}
	return 0
}
