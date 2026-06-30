package media

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var runtimeRe = regexp.MustCompile(`<runtime>\d*</runtime>`)

// NFOData holds parsed metadata from a Kodi movie/tvshow NFO file.
type NFOData struct {
	Title         string
	OriginalTitle string
	Year          int
	Plot          string
	Rating        float64
	Runtime       int // minutes
	Studio        string
	Genres        []string
	MPAA          string
	Tagline       string
	UniqueID      []UniqueIDEntry
}

// UniqueIDEntry holds a single external ID (tmdb, imdb, tvdb, anilist…).
type UniqueIDEntry struct {
	Type  string
	Value string
}

// EpisodeNFOData holds parsed metadata from a Kodi episode NFO file.
type EpisodeNFOData struct {
	Title         string
	Season        int
	Episode       int
	Plot          string
	Rating        float64
	Year          int
	Aired         string
	Runtime       int // minutes
	ShowTitle     string
	Genres        []string
	StreamDetails *StreamDetails
}

// StreamDetails holds ffprobe-style stream info embedded in NFO.
type StreamDetails struct {
	DurationSeconds int
}

// nfoRaw is the raw XML structure — works for both <movie> and <tvshow> roots.
type nfoRaw struct {
	Title         string      `xml:"title"`
	OriginalTitle string      `xml:"originaltitle"`
	Year          int         `xml:"year"`
	Plot          string      `xml:"plot"`
	Rating        float64     `xml:"rating"`
	Runtime       int         `xml:"runtime"`
	Studio        string      `xml:"studio"`
	Genres        []string    `xml:"genre"`
	MPAA          string      `xml:"mpaa"`
	Tagline       string      `xml:"tagline"`
	UniqueIDs     []rawUnique `xml:"uniqueid"`
	Ratings       struct {
		Rating struct {
			Value float64 `xml:"value"`
		} `xml:"rating"`
	} `xml:"ratings"`
}

type rawUnique struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

// episodeNFORaw is the raw XML structure for <episodedetails>.
type episodeNFORaw struct {
	Title     string   `xml:"title"`
	Season    int      `xml:"season"`
	Episode   int      `xml:"episode"`
	Plot      string   `xml:"plot"`
	Rating    float64  `xml:"rating"`
	Year      int      `xml:"year"`
	Aired     string   `xml:"aired"`
	Runtime   int      `xml:"runtime"`
	ShowTitle string   `xml:"showtitle"`
	Genres    []string `xml:"genre"`
	FileInfo  struct {
		StreamDetails struct {
			Video struct {
				DurationInSeconds int `xml:"durationinseconds"`
			} `xml:"video"`
		} `xml:"streamdetails"`
	} `xml:"fileinfo"`
	Ratings struct {
		Rating struct {
			Value float64 `xml:"value"`
		} `xml:"rating"`
	} `xml:"ratings"`
}

// ParseNFO parses a Kodi movie or tvshow NFO from the given directory.
// It tries common NFO filenames (movie.nfo, tvshow.nfo, any *.nfo).
func ParseNFO(dir string) (NFOData, error) {
	nfoPath := findNFOInDir(dir)
	if nfoPath == "" {
		return NFOData{}, fmt.Errorf("no NFO in %s", dir)
	}

	f, err := os.Open(nfoPath)
	if err != nil {
		return NFOData{}, err
	}
	defer f.Close()

	var raw nfoRaw
	if err := xml.NewDecoder(f).Decode(&raw); err != nil {
		return NFOData{}, err
	}
	if raw.Title == "" && raw.Plot == "" {
		return NFOData{}, fmt.Errorf("empty NFO")
	}

	rating := raw.Rating
	if rating == 0 && raw.Ratings.Rating.Value > 0 {
		rating = raw.Ratings.Rating.Value
	}

	uids := make([]UniqueIDEntry, 0, len(raw.UniqueIDs))
	for _, u := range raw.UniqueIDs {
		if u.Value != "" {
			uids = append(uids, UniqueIDEntry{Type: u.Type, Value: strings.TrimSpace(u.Value)})
		}
	}

	return NFOData{
		Title:         raw.Title,
		OriginalTitle: raw.OriginalTitle,
		Year:          raw.Year,
		Plot:          raw.Plot,
		Rating:        rating,
		Runtime:       raw.Runtime,
		Studio:        raw.Studio,
		Genres:        raw.Genres,
		MPAA:          raw.MPAA,
		Tagline:       raw.Tagline,
		UniqueID:      uids,
	}, nil
}

// ParseEpisodeNFO parses a Kodi episode NFO alongside the given video file.
// The NFO is expected at <videobase>.nfo.
func ParseEpisodeNFO(videoPath string) (EpisodeNFOData, error) {
	nfoPath := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".nfo"
	f, err := os.Open(nfoPath)
	if err != nil {
		return EpisodeNFOData{}, err
	}
	defer f.Close()

	var raw episodeNFORaw
	if err := xml.NewDecoder(f).Decode(&raw); err != nil {
		return EpisodeNFOData{}, err
	}

	rating := raw.Rating
	if rating == 0 && raw.Ratings.Rating.Value > 0 {
		rating = raw.Ratings.Rating.Value
	}

	var sd *StreamDetails
	if secs := raw.FileInfo.StreamDetails.Video.DurationInSeconds; secs > 0 {
		sd = &StreamDetails{DurationSeconds: secs}
	}

	return EpisodeNFOData{
		Title:         raw.Title,
		Season:        raw.Season,
		Episode:       raw.Episode,
		Plot:          raw.Plot,
		Rating:        rating,
		Year:          raw.Year,
		Aired:         raw.Aired,
		Runtime:       raw.Runtime,
		ShowTitle:     raw.ShowTitle,
		Genres:        raw.Genres,
		StreamDetails: sd,
	}, nil
}

// UpdateEpisodeRuntime updates the <runtime> tag in an episode NFO.
// It is a best-effort write; errors are silently ignored.
func UpdateEpisodeRuntime(videoPath string, minutes int) {
	nfoPath := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".nfo"
	data, err := os.ReadFile(nfoPath)
	if err != nil {
		return
	}

	replacement := fmt.Sprintf("<runtime>%d</runtime>", minutes)

	if existing := runtimeRe.Find(data); string(existing) == replacement {
		return
	}

	var updated []byte
	if runtimeRe.Match(data) {
		updated = runtimeRe.ReplaceAll(data, []byte(replacement))
	} else {
		// Append before closing tag — handles both episodedetails and movie roots.
		for _, closing := range []string{"</episodedetails>", "</movie>", "</tvshow>"} {
			if bytes := strings.Replace(string(data), closing, replacement+"\n"+closing, 1); bytes != string(data) {
				updated = []byte(bytes)
				break
			}
		}
	}
	if updated == nil {
		return
	}
	os.WriteFile(nfoPath, updated, 0644)
}

// findNFOInDir returns the path of a suitable NFO file inside dir.
// Priority: movie.nfo → tvshow.nfo → any single *.nfo
func findNFOInDir(dir string) string {
	for _, name := range []string{"movie.nfo", "tvshow.nfo"} {
		p := filepath.Join(dir, name)
		if fileExists(p) {
			return p
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".nfo") {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}
