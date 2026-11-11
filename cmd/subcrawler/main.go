// Command subcrawler downloads missing subtitles for the videos in the
// libraries, using scout. Each video is hashed here, and its IMDb/TMDB IDs
// come from the nearest movie.nfo or tvshow.nfo.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pixelotes/scout"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/media"
	"raspberry-media-server/internal/scoutclient"
)

var errStop = errors.New("stop")

func main() {
	configPath := flag.String("config", "config/config.yml", "path to config.yml")
	targetPath := flag.String("path", "", "crawl this path only (default: all libraries)")
	recursive := flag.Bool("recursive", false, "recurse into subdirectories")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	langs := cfg.Crawlers.Subtitles.Languages
	if len(langs) == 0 {
		langs = []string{"en"}
	}
	backend, err := scoutclient.New(cfg)
	if err != nil {
		log.Fatalf("subcrawler: %v", err)
	}

	type root struct {
		path, lib string
	}
	var roots []root
	if *targetPath != "" {
		lib, ok := scoutclient.LibraryFor(cfg, *targetPath)
		if !ok {
			log.Fatalf("subcrawler: %s is not inside a library", *targetPath)
		}
		roots = []root{{*targetPath, lib.Path}}
	} else {
		*recursive = true // a full scan always walks the whole library
		for _, lib := range cfg.Libraries {
			// download_subtitles: false only stops the scheduled scan.
			if _, ok := scoutclient.Kind(lib.ContentType); ok && lib.WantsSubtitles() {
				roots = append(roots, root{lib.Path, lib.Path})
			}
		}
	}

	total, failed := 0, 0
	for _, r := range roots {
		err := walkVideos(r.path, *recursive, func(videoPath string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			n, err := scoutclient.FetchSubtitles(ctx, backend, videoPath, r.lib, langs)
			total += n
			if errors.Is(err, scout.ErrQuotaExceeded) {
				log.Printf("subcrawler: download quota used up, stopping until the next run")
				return errStop
			}
			if err != nil {
				failed++
				log.Printf("subcrawler: %s: %v", filepath.Base(videoPath), err)
			}
			return nil
		})
		if errors.Is(err, errStop) {
			break
		}
	}
	log.Printf("subcrawler: done — %d downloaded, %d errors", total, failed)
}

func walkVideos(root string, recursive bool, fn func(string) error) error {
	if st, err := os.Stat(root); err == nil && !st.IsDir() {
		if media.IsVideoFile(root) {
			return fn(root) // -path pointed at a single video
		}
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Printf("subcrawler: read dir %s: %v", root, err)
		return nil
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(root, e.Name())
		if e.IsDir() {
			if recursive {
				if err := walkVideos(full, true, fn); err != nil {
					return err
				}
			}
			continue
		}
		if media.IsVideoFile(e.Name()) {
			if err := fn(full); err != nil {
				return err
			}
		}
	}
	return nil
}
