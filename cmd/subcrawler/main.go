package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/crawlers/subtitles"
	"raspberry-media-server/internal/media"
)

func main() {
	configPath := flag.String("config", "config/config.yml", "path to config.yml")
	targetPath := flag.String("path", "", "crawl this path only (default: all libraries)")
	recursive := flag.Bool("recursive", false, "recurse into subdirectories")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if cfg.Crawlers.Subtitles.APIKey == "" {
		log.Fatal("subcrawler: crawlers.subtitles.api_key is not set in config")
	}

	client := subtitles.NewClient(
		cfg.Crawlers.Subtitles.APIKey,
		cfg.Crawlers.Subtitles.Languages,
	)

	var roots []string
	if *targetPath != "" {
		roots = []string{*targetPath}
	} else {
		for _, lib := range cfg.Libraries {
			roots = append(roots, lib.Path)
		}
	}

	total, skipped, failed := 0, 0, 0
	for _, root := range roots {
		walkVideos(root, *recursive, func(videoPath string) {
			got, need, err := client.ProcessFile(videoPath)
			if err != nil {
				log.Printf("subcrawler: %s: %v", videoPath, err)
				failed++
				return
			}
			if need == 0 {
				skipped++
				return
			}
			total += got
			log.Printf("subcrawler: %s: %d/%d subtitles downloaded", filepath.Base(videoPath), got, need)
		})
	}

	log.Printf("subcrawler: done — %d downloaded, %d already complete, %d errors", total, skipped, failed)
}

func walkVideos(root string, recursive bool, fn func(string)) {
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Printf("subcrawler: read dir %s: %v", root, err)
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(root, e.Name())
		if e.IsDir() {
			if recursive {
				walkVideos(full, true, fn)
			}
			continue
		}
		if media.IsVideoFile(e.Name()) {
			fn(full)
		}
	}
}
