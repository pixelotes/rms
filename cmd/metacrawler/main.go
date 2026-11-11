// Command metacrawler writes movie.nfo / tvshow.nfo and artwork for each
// title folder in the libraries, using scout for the lookups. RMS runs it as a
// separate, low-priority process so the server keeps its small footprint.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pixelotes/scout"
	"github.com/pixelotes/scout/nfo"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/media"
	"raspberry-media-server/internal/scoutclient"
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

	backend, err := scoutclient.New(cfg)
	if err != nil {
		log.Fatalf("metacrawler: %v", err)
	}
	runMetadata(cfg, backend, *targetPath, *contentType, *force)
}

func runThumbnails(cfg *config.Config, targetPath string) {
	dirs := targetDirs(cfg, targetPath, "")
	total := 0
	for _, dir := range dirs {
		n := media.GenerateThumbnailsInDir(dir, true)
		total += n
		if n > 0 {
			log.Printf("thumbnails: %d generated under %s", n, dir)
		}
	}
	log.Printf("thumbnails: done (%d total)", total)
}

func runMetadata(cfg *config.Config, b scout.Backend, targetPath, contentTypeOverride string, force bool) {
	done, failed := 0, 0
	for _, lib := range cfg.Libraries {
		ct := lib.ContentType
		if contentTypeOverride != "" {
			ct = contentTypeOverride
		}
		kind, ok := scoutclient.Kind(ct)
		if !ok {
			continue // e.g. "tv" (IPTV) libraries
		}

		// Determine which directories under this library to crawl.
		var dirs []string
		if targetPath != "" {
			absTarget, _ := filepath.Abs(targetPath)
			absLib, _ := filepath.Abs(lib.Path)
			if !strings.HasPrefix(absTarget, absLib) {
				continue
			}
			if absTarget == absLib {
				dirs = subdirs(lib.Path)
			} else {
				dirs = []string{absTarget}
			}
		} else {
			// download_metadata: false only stops the scheduled crawl; a
			// crawl asked for from the UI still runs.
			if !lib.WantsMetadata() {
				continue
			}
			dirs = subdirs(lib.Path)
		}

		lang := lib.MetadataLang
		if lang == "" {
			lang = "en"
		}
		providers := scoutclient.Providers(cfg, lib, ct)
		for _, dir := range dirs {
			switch err := crawlDir(b, dir, kind, lang, providers, force); {
			case err == nil:
				done++
			case errors.Is(err, errSkipped):
			default:
				failed++
				log.Printf("metacrawler: %s: %v", filepath.Base(dir), err)
			}
		}
	}
	log.Printf("metacrawler: done — %d written, %d failed", done, failed)
}

var errSkipped = errors.New("skipped")

// crawlDir identifies one title folder and writes its NFO and artwork.
func crawlDir(b scout.Backend, dir string, kind scout.Kind, lang string, providers []string, force bool) error {
	nfoName := "tvshow.nfo"
	if kind == scout.Movie {
		nfoName = "movie.nfo"
	}
	if !force {
		if _, err := os.Stat(filepath.Join(dir, nfoName)); err == nil {
			return errSkipped
		}
	}
	title, year := parseTitleYear(filepath.Base(dir))
	if title == "" {
		return errSkipped
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	items, err := b.Search(ctx, scout.Query{Kind: kind, Title: title, Year: year, Language: lang, Providers: providers, Limit: 1})
	if err != nil {
		return err
	}
	item, err := b.Get(ctx, scout.Lookup{Kind: kind, Provider: items[0].Provider, ID: items[0].ID, Language: lang})
	if err != nil {
		return err
	}
	// Keep IDs only the search result had (e.g. Cinemeta's TMDB ID).
	if item.IDs == nil {
		item.IDs = scout.IDs{}
	}
	for k, v := range items[0].IDs {
		if _, ok := item.IDs[k]; !ok {
			item.IDs[k] = v
		}
	}

	if kind == scout.Movie {
		err = nfo.WriteMovie(dir, item)
	} else {
		err = nfo.WriteShow(dir, item)
	}
	if err != nil {
		return err
	}
	files, artErr := nfo.DownloadArtwork(ctx, dir, item)
	log.Printf("metacrawler: %s → %s (%d) via %s %v, %d artwork file(s)", filepath.Base(dir), item.Title, item.Year, item.Provider, item.IDs, len(files))
	if artErr != nil {
		log.Printf("metacrawler: %s: %v", filepath.Base(dir), artErr)
	}
	return nil
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
