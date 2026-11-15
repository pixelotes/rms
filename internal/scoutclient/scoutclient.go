// Package scoutclient connects RMS to scout, the metadata and subtitle
// library shared with Reel. It turns RMS's config into a scout backend and
// holds the helpers the crawlers and the subtitle endpoints share.
package scoutclient

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pixelotes/scout"
	"github.com/pixelotes/scout/client"
	scoutcfg "github.com/pixelotes/scout/config"
	"github.com/pixelotes/scout/moviehash"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/media"
)

// New builds the scout backend from RMS's config.
func New(cfg *config.Config) (scout.Backend, error) {
	sc := scoutcfg.Default()
	sc.Backends = cfg.Crawlers.Backends
	sc.Service.URL = cfg.Crawlers.Service.URL
	sc.Service.Token = cfg.Crawlers.Service.Token
	sc.Service.Timeout = time.Duration(cfg.Crawlers.Service.Timeout) * time.Second

	m := cfg.Crawlers.Metadata
	sc.Local.Providers = map[scout.Kind][]string{
		scout.Movie: m.MovieProviders,
		scout.Show:  m.TVSeriesProviders,
		scout.Anime: m.AnimeProviders,
	}
	sc.Local.TMDB.APIKey = keyOrEmpty(m.TMDBKey)
	sc.Local.Trakt.ClientID = keyOrEmpty(m.TraktID)

	s := cfg.Crawlers.Subtitles
	sc.Local.OpenSubtitles.APIKey = keyOrEmpty(s.APIKey)
	sc.Local.SubDL.APIKey = keyOrEmpty(s.SubDLAPIKey)
	sc.Local.Subdivx.FlareSolverrURL = s.FlareSolverrURL
	if len(s.Providers) > 0 {
		sc.Local.SubtitleProviders = s.Providers
	}
	if len(s.Languages) > 0 {
		sc.Local.SubtitleLanguages = s.Languages
	}
	if cfg.App.CachePath != "" {
		sc.Local.Cache.SubtitleDir = filepath.Join(cfg.App.CachePath, "subtitles")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})).With("component", "scout")
	return client.New(sc, logger)
}

// keyOrEmpty drops unset keys and the REPLACE_WITH_... placeholders of the
// sample config, so scout does not register a provider with a fake key.
func keyOrEmpty(k string) string {
	k = strings.TrimSpace(k)
	if strings.HasPrefix(k, "REPLACE_WITH_") {
		return ""
	}
	return k
}

// Kind maps a library content_type to a scout kind.
func Kind(contentType string) (scout.Kind, bool) {
	switch contentType {
	case "movies":
		return scout.Movie, true
	case "tvseries":
		return scout.Show, true
	case "anime":
		return scout.Anime, true
	}
	return "", false
}

// Providers is the provider order for a library: its own providers list
// when set, otherwise crawlers.metadata.<type>_providers.
func Providers(cfg *config.Config, lib config.Library, contentType string) []string {
	if len(lib.Providers) > 0 {
		return lib.Providers
	}
	switch contentType {
	case "movies":
		return cfg.Crawlers.Metadata.MovieProviders
	case "tvseries":
		return cfg.Crawlers.Metadata.TVSeriesProviders
	case "anime":
		return cfg.Crawlers.Metadata.AnimeProviders
	}
	return nil
}

// LibraryFor returns the library that contains path.
func LibraryFor(cfg *config.Config, path string) (config.Library, bool) {
	abs, _ := filepath.Abs(path)
	for _, lib := range cfg.Libraries {
		root, _ := filepath.Abs(lib.Path)
		if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
			return lib, true
		}
	}
	return config.Library{}, false
}

// ShowInfo reads the movie.nfo or tvshow.nfo nearest to videoPath (its own
// folder, then parents up to libRoot) and returns its title and IDs.
func ShowInfo(videoPath, libRoot string) (title string, ids scout.IDs) {
	ids = scout.IDs{}
	root, _ := filepath.Abs(libRoot)
	dir, _ := filepath.Abs(filepath.Dir(videoPath))
	for {
		if nfo, err := media.ParseNFO(dir); err == nil {
			for _, u := range nfo.UniqueID {
				ids[strings.ToLower(u.Type)] = u.Value
			}
			return nfo.Title, ids
		}
		if dir == root || !strings.HasPrefix(dir, root) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", ids
}

var episodeRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)s(\d{1,2})[ ._-]?e(\d{1,3})`),
	regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`),
}

// ParseEpisode finds S01E02 or 1x02 in a file name; 0, 0 when absent.
func ParseEpisode(name string) (season, episode int) {
	for _, re := range episodeRes {
		if m := re.FindStringSubmatch(name); m != nil {
			season, _ = strconv.Atoi(m[1])
			episode, _ = strconv.Atoi(m[2])
			return
		}
	}
	return 0, 0
}

// StripEpisode removes an S01E02 / 1x02 marker from a query.
func StripEpisode(text string) string {
	for _, re := range episodeRes {
		text = re.ReplaceAllString(text, "")
	}
	return strings.Join(strings.Fields(text), " ")
}

// MissingLanguages returns the languages in langs with no subtitle file next
// to videoPath yet.
func MissingLanguages(videoPath string, langs []string) []string {
	have := map[string]bool{}
	for _, t := range media.FindSubtitles(videoPath) {
		have[strings.ToLower(t.Language)] = true
	}
	var missing []string
	for _, l := range langs {
		if !have[strings.ToLower(l)] {
			missing = append(missing, l)
		}
	}
	return missing
}

// SubtitleQuery builds the lookup for a video: the hash (computed here, so a
// scout service never needs the file), the IDs and title from the nearest
// NFO, and the episode from the file name.
func SubtitleQuery(videoPath, libRoot string, langs []string) scout.SubtitleQuery {
	title, ids := ShowInfo(videoPath, libRoot)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	q := scout.SubtitleQuery{Languages: langs, IMDbID: ids["imdb"], TMDbID: ids["tmdb"]}
	q.Season, q.Episode = ParseEpisode(base)
	if h, size, err := moviehash.File(videoPath); err == nil {
		q.Hash, q.Size = h, size
	}
	switch {
	case title != "":
		q.Query = title
	case q.Season > 0:
		// Show/Season 1/file.mkv or Show/file.mkv
		dir := filepath.Dir(videoPath)
		if strings.HasPrefix(strings.ToLower(filepath.Base(dir)), "season") {
			dir = filepath.Dir(dir)
		}
		q.Query = filepath.Base(dir)
	default:
		q.Query = filepath.Base(filepath.Dir(videoPath)) // "The Matrix (1999)"
	}
	return q
}

// FetchSubtitles downloads the best candidate for each of langs that has no
// subtitle yet, next to the video as <name>.<lang>.srt. It returns how many
// it wrote; scout.ErrQuotaExceeded means stop for today.
func FetchSubtitles(ctx context.Context, b scout.Backend, videoPath, libRoot string, langs []string) (int, error) {
	missing := MissingLanguages(videoPath, langs)
	if len(missing) == 0 {
		return 0, nil
	}
	q := SubtitleQuery(videoPath, libRoot, missing)
	subs, err := b.SearchSubtitles(ctx, q)
	if errors.Is(err, scout.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	written := 0
	for _, lang := range missing {
		best := scout.BestSubtitle(subs, lang)
		if best == nil {
			continue
		}
		f, err := b.DownloadSubtitle(ctx, best.Provider, best.FileID)
		if errors.Is(err, scout.ErrQuotaExceeded) {
			return written, err
		}
		if err != nil {
			log.Printf("subtitles: %s %s: %v", filepath.Base(videoPath), lang, err)
			continue
		}
		if err := WriteFile(base+"."+lang+".srt", f.Data); err != nil {
			return written, err
		}
		written++
		log.Printf("subtitles: %s.%s.srt from %s (matched by %s)", filepath.Base(base), lang, best.Provider, best.MatchedBy)
	}
	return written, nil
}

// WriteFile writes atomically, so a player never sees half a subtitle.
func WriteFile(path string, data []byte) error {
	tmp := path + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Rename(tmp, path)
}
