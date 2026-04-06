package server

import (
	"log"
	"os/exec"
	"time"

	"raspberry-media-server/internal/config"
)

func (s *Server) startAutoScan() {
	cfg := s.config.Crawlers.AutoScan
	if !cfg.Enabled {
		return
	}

	interval := time.Duration(cfg.IntervalHours) * time.Hour
	log.Printf("Auto-scan enabled: every %dh (metadata=%v, subtitles=%v, thumbnails=%v)",
		cfg.IntervalHours, cfg.Metadata, cfg.Subtitles, cfg.Thumbnails)

	go func() {
		// Run once at startup after a short delay
		time.Sleep(30 * time.Second)
		s.runAutoScan()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			s.runAutoScan()
		}
	}()
}

func (s *Server) runAutoScan() {
	cfg := s.config.Crawlers.AutoScan
	log.Println("Auto-scan: starting...")

	configPath := findConfigPath()

	if cfg.Metadata {
		runCrawler("metacrawler", configPath, nil)
	}

	if cfg.Subtitles {
		runCrawler("subcrawler", configPath, nil)
	}

	if cfg.Thumbnails {
		runCrawler("metacrawler", configPath, []string{"--thumbnails"})
	}

	log.Println("Auto-scan: complete")
}

func runCrawler(name, configPath string, extraArgs []string) {
	bin, err := exec.LookPath(name)
	if err != nil {
		bin = "./" + name
	}

	args := []string{"-config", configPath}
	args = append(args, extraArgs...)

	log.Printf("Auto-scan: running %s %v", name, args)
	cmd := exec.Command(bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Auto-scan: %s failed: %v\n%s", name, err, string(output))
	} else if len(output) > 0 {
		log.Printf("Auto-scan: %s output:\n%s", name, string(output))
	}
}

func findConfigPath() string {
	// Try common locations
	for _, p := range []string{
		"/app/config/config.yml",
		"config/config.yml",
	} {
		if fileExists(p) {
			return p
		}
	}
	return "config/config.yml"
}

func fileExists(path string) bool {
	_, err := config.Load(path)
	return err == nil
}
