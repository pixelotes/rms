package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/crawlers/metadata"
)

// yearRe matches a 4-digit year in parentheses, e.g. "The Matrix (1999)".
var yearRe = regexp.MustCompile(`\((\d{4})\)`)

// parseTitleYear splits a directory name like "The Matrix (1999)" into
// title "The Matrix" and year 1999. Year is 0 when not present.
func parseTitleYear(name string) (title string, year int) {
	m := yearRe.FindStringSubmatch(name)
	if m != nil {
		year, _ = strconv.Atoi(m[1])
		title = strings.TrimSpace(name[:strings.Index(name, m[0])])
	} else {
		title = strings.TrimSpace(name)
	}
	return
}

func main() {
	configPath := flag.String("config", "config/config.yml", "path to config.yml")
	targetPath := flag.String("path", "", "crawl this path only (default: all libraries)")
	contentType := flag.String("type", "", "override content type: movies|tvseries|anime")
	force := flag.Bool("force", false, "re-fetch metadata even if NFO already exists")
	thumbnails := flag.Bool("thumbnails", false, "generate thumbnails only (no metadata fetch)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if *thumbnails {
		runThumbnails(cfg, *targetPath)
		return
	}

	runMetadata(cfg, *targetPath, *contentType, *force)
}

func runThumbnails(cfg *config.Config, targetPath string) {
	dirs := targetDirs(cfg, targetPath, "")
	total := 0
	for _, dir := range dirs {
		n := metadata.GenerateThumbnailsInDir(dir, true)
		total += n
		if n > 0 {
			log.Printf("thumbnails: %d generated under %s", n, dir)
		}
	}
	log.Printf("thumbnails: done (%d total)", total)
}

func runMetadata(cfg *config.Config, targetPath, contentTypeOverride string, force bool) {
	for _, lib := range cfg.Libraries {
		ct := lib.ContentType
		if contentTypeOverride != "" {
			ct = contentTypeOverride
		}

		// Determine which directories under this library to crawl.
		var dirs []string
		if targetPath != "" {
			absTarget, _ := filepath.Abs(targetPath)
			absLib, _ := filepath.Abs(lib.Path)
			if !strings.HasPrefix(absTarget, absLib) {
				continue
			}
			dirs = []string{absTarget}
		} else {
			dirs = subdirs(lib.Path)
		}

		for _, dir := range dirs {
			crawlDir(cfg, lib, dir, ct, force)
		}
	}
}

func crawlDir(cfg *config.Config, lib config.Library, dir, contentType string, force bool) {
	lang := lib.MetadataLang
	if lang == "" {
		lang = "en"
	}

	switch contentType {
	case "movies":
		crawlMovies(cfg, dir, lang, force)
	case "tvseries":
		crawlTVSeries(cfg, dir, lang, force)
	case "anime":
		crawlAnime(cfg, dir, lang, force)
	default:
		log.Printf("metacrawler: skipping %s (unsupported content type %q)", dir, contentType)
	}
}

func crawlMovies(cfg *config.Config, dir, lang string, force bool) {
	nfoPath := filepath.Join(dir, "movie.nfo")
	if !force {
		if _, err := os.Stat(nfoPath); err == nil {
			return
		}
	}

	title, year := parseTitleYear(filepath.Base(dir))
	if title == "" {
		return
	}

	client := metadata.NewTMDBClient(cfg.Crawlers.Metadata.TMDBKey, lang)
	result, err := client.SearchMovie(title, year)
	if err != nil {
		log.Printf("metacrawler: movie %q: %v", title, err)
		return
	}

	if err := metadata.WriteMovieNFO(dir, result); err != nil {
		log.Printf("metacrawler: write NFO for %q: %v", title, err)
		return
	}

	poster, backdrop, _ := client.GetMovieImages(0)
	if result.PosterURL != "" {
		poster = result.PosterURL
	}
	if result.BackdropURL != "" {
		backdrop = result.BackdropURL
	}
	metadata.DownloadImages(dir, poster, backdrop, "")
	log.Printf("metacrawler: movie %q done", title)
}

func crawlTVSeries(cfg *config.Config, dir, lang string, force bool) {
	nfoPath := filepath.Join(dir, "tvshow.nfo")
	if !force {
		if _, err := os.Stat(nfoPath); err == nil {
			return
		}
	}

	title, year := parseTitleYear(filepath.Base(dir))
	if title == "" {
		return
	}

	providers := cfg.Crawlers.Metadata.TVSeriesProviders
	if len(providers) == 0 {
		providers = []string{"tvmaze"}
	}

	var result *metadata.TVShowResult
	var err error
	for _, p := range providers {
		switch p {
		case "tvmaze":
			result, err = metadata.NewTVmazeClient().SearchTVShow(title)
		case "tmdb":
			result, err = metadata.NewTMDBClient(cfg.Crawlers.Metadata.TMDBKey, lang).SearchTVShow(title, year)
		}
		if err == nil && result != nil {
			break
		}
	}

	if result == nil {
		log.Printf("metacrawler: tvshow %q: not found", title)
		return
	}

	if err := metadata.WriteTVShowNFO(dir, result); err != nil {
		log.Printf("metacrawler: write tvshow NFO for %q: %v", title, err)
		return
	}

	metadata.DownloadImages(dir, result.PosterURL, result.BackdropURL, "")
	log.Printf("metacrawler: tvshow %q done", title)
}

func crawlAnime(cfg *config.Config, dir, lang string, force bool) {
	nfoPath := filepath.Join(dir, "tvshow.nfo")
	if !force {
		if _, err := os.Stat(nfoPath); err == nil {
			return
		}
	}

	title, _ := parseTitleYear(filepath.Base(dir))
	if title == "" {
		return
	}

	providers := cfg.Crawlers.Metadata.AnimeProviders
	if len(providers) == 0 {
		providers = []string{"anilist"}
	}

	var result *metadata.TVShowResult
	var err error
	for _, p := range providers {
		switch p {
		case "anilist":
			result, err = metadata.NewAniListClient().SearchAnime(title)
		case "kitsu":
			result, err = metadata.NewKitsuClient().SearchAnime(title)
		}
		if err == nil && result != nil {
			break
		}
	}

	if result == nil {
		log.Printf("metacrawler: anime %q: not found", title)
		return
	}

	if err := metadata.WriteTVShowNFO(dir, result); err != nil {
		log.Printf("metacrawler: write anime NFO for %q: %v", title, err)
		return
	}

	metadata.DownloadImages(dir, result.PosterURL, result.BackdropURL, "")
	log.Printf("metacrawler: anime %q done", title)
}

// targetDirs returns the root directories to process: the single targetPath
// if set and within a known library, or all library roots otherwise.
func targetDirs(cfg *config.Config, targetPath, contentType string) []string {
	if targetPath != "" {
		return []string{targetPath}
	}
	var dirs []string
	for _, lib := range cfg.Libraries {
		if contentType != "" && lib.ContentType != contentType {
			continue
		}
		dirs = append(dirs, lib.Path)
	}
	return dirs
}

// subdirs returns the immediate subdirectories of root (one per movie/show).
func subdirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []string{root}
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	if len(dirs) == 0 {
		return []string{root}
	}
	return dirs
}
