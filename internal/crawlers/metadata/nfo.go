package metadata

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
)

// WriteMovieNFO generates a Kodi-compatible movie.nfo file.
func WriteMovieNFO(dir string, m *MovieResult) error {
	type uniqueID struct {
		XMLName xml.Name `xml:"uniqueid"`
		Type    string   `xml:"type,attr"`
		Default bool     `xml:"default,attr"`
		Value   string   `xml:",chardata"`
	}

	type movieNFO struct {
		XMLName       xml.Name   `xml:"movie"`
		Title         string     `xml:"title"`
		OriginalTitle string     `xml:"originaltitle,omitempty"`
		Year          int        `xml:"year,omitempty"`
		Plot          string     `xml:"plot,omitempty"`
		Runtime       int        `xml:"runtime,omitempty"`
		Rating        float64    `xml:"rating,omitempty"`
		Studio        string     `xml:"studio,omitempty"`
		Tagline       string     `xml:"tagline,omitempty"`
		Genres        []string   `xml:"genre"`
		UniqueIDs     []uniqueID `xml:"uniqueid"`
	}

	nfo := movieNFO{
		Title:   m.Title,
		Year:    m.Year,
		Plot:    m.Overview,
		Runtime: m.Runtime,
		Rating:  m.Rating,
		Studio:  m.Studio,
		Tagline: m.Tagline,
		Genres:  m.Genres,
	}

	if m.IMDBID != "" {
		nfo.UniqueIDs = append(nfo.UniqueIDs, uniqueID{Type: "imdb", Default: true, Value: m.IMDBID})
	}
	if m.TMDBID != "" {
		nfo.UniqueIDs = append(nfo.UniqueIDs, uniqueID{Type: "tmdb", Default: m.IMDBID == "", Value: m.TMDBID})
	}

	return writeNFO(filepath.Join(dir, "movie.nfo"), nfo)
}

// WriteTVShowNFO generates a Kodi-compatible tvshow.nfo file.
func WriteTVShowNFO(dir string, s *TVShowResult) error {
	type tvshowNFO struct {
		XMLName xml.Name `xml:"tvshow"`
		Title   string   `xml:"title"`
		Year    int      `xml:"year,omitempty"`
		Plot    string   `xml:"plot,omitempty"`
		Rating  float64  `xml:"rating,omitempty"`
		Studio  string   `xml:"studio,omitempty"`
		Status  string   `xml:"status,omitempty"`
		Genres  []string `xml:"genre"`
	}

	nfo := tvshowNFO{
		Title:  s.Title,
		Year:   s.Year,
		Plot:   s.Overview,
		Rating: s.Rating,
		Studio: s.Studio,
		Status: s.Status,
		Genres: s.Genres,
	}

	return writeNFO(filepath.Join(dir, "tvshow.nfo"), nfo)
}

// WriteEpisodeNFO generates a Kodi-compatible episode NFO file next to the video.
func WriteEpisodeNFO(videoPath string, showTitle string, season int, ep Episode) error {
	type episodeNFO struct {
		XMLName xml.Name `xml:"episodedetails"`
		Title   string   `xml:"title"`
		Season  int      `xml:"season"`
		Episode int      `xml:"episode"`
		Aired   string   `xml:"aired,omitempty"`
		Plot    string   `xml:"plot,omitempty"`
	}

	nfo := episodeNFO{
		Title:   ep.Title,
		Season:  season,
		Episode: ep.Number,
		Aired:   ep.AirDate,
	}

	nfoPath := videoPath[:len(videoPath)-len(filepath.Ext(videoPath))] + ".nfo"
	return writeNFO(nfoPath, nfo)
}

func writeNFO(path string, data interface{}) error {
	out, err := xml.MarshalIndent(data, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal NFO: %w", err)
	}

	content := []byte(xml.Header)
	content = append(content, out...)
	content = append(content, '\n')

	return os.WriteFile(path, content, 0644)
}
