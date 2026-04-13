package server

import (
	"log"
	"net/http"
	"os/exec"
	"time"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/media"
)

func (s *Server) startAutoScan() {
	cfg := s.config.Crawlers.AutoScan
	if !cfg.Enabled {
		return
	}

	if cfg.Schedule != "" {
		s.startScheduledScan(cfg)
	} else {
		s.startIntervalScan(cfg)
	}
}

func (s *Server) startScheduledScan(cfg config.AutoScanConfig) {
	log.Printf("Auto-scan scheduled daily at %s (metadata=%v, subtitles=%v, thumbnails=%v)",
		cfg.Schedule, cfg.Metadata, cfg.Subtitles, cfg.Thumbnails)

	go func() {
		for {
			now := time.Now()
			next, err := nextScheduleTime(now, cfg.Schedule)
			if err != nil {
				log.Printf("Auto-scan: invalid schedule %q: %v", cfg.Schedule, err)
				return
			}
			log.Printf("Auto-scan: next run at %s", next.Format("2006-01-02 15:04"))
			time.Sleep(time.Until(next))
			s.runAutoScan()
		}
	}()
}

func nextScheduleTime(now time.Time, schedule string) (time.Time, error) {
	t, err := time.Parse("15:04", schedule)
	if err != nil {
		return time.Time{}, err
	}

	next := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next, nil
}

func (s *Server) startIntervalScan(cfg config.AutoScanConfig) {
	interval := time.Duration(cfg.IntervalHours) * time.Hour
	log.Printf("Auto-scan enabled: every %dh (metadata=%v, subtitles=%v, thumbnails=%v)",
		cfg.IntervalHours, cfg.Metadata, cfg.Subtitles, cfg.Thumbnails)

	go func() {
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

	s.rescanLibraries()
	log.Println("Auto-scan: complete")
}

func (s *Server) rescanLibraries() {
	added := media.PopulateIDStore(s.config.Libraries)
	log.Printf("Library rescan: ID store refreshed for %d libraries (%d new items)",
		len(s.config.Libraries), len(added))
	if s.config.App.KodiSyncQueue {
		s.syncQueue.RecordAdded(added)
	}
}

func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	s.rescanLibraries()
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func runCrawler(name, configPath string, extraArgs []string) {
	bin, err := exec.LookPath(name)
	if err != nil {
		bin = "./" + name
	}

	args := []string{"-config", configPath}
	args = append(args, extraArgs...)

	log.Printf("Auto-scan: running %s %v", name, args)
	cmd := niceCommand(bin, args...)
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
