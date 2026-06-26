package server

import (
	"crypto/subtle"
	"log"
	"net/http"
	"os/exec"
	"time"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/media"
	"raspberry-media-server/internal/tv"
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

// startIndexRefresh starts an independent goroutine that refreshes the ID store
// at a fine-grained interval (minutes) without running any crawlers.
// Controlled by auto_scan.rescan_interval_minutes; 0 = disabled.
// Runs independently of auto_scan.enabled so users can have fast index refresh
// without the overhead of metacrawler/subcrawler.
func (s *Server) startIndexRefresh() {
	m := s.config.Crawlers.AutoScan.RescanIntervalMinutes
	if m <= 0 {
		return
	}
	interval := time.Duration(m) * time.Minute
	log.Printf("Index refresh enabled: every %dm (rescan-only, no crawlers)", m)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			s.rescanLibraries()
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

// rescanLibraries refreshes the in-memory ID store and records deltas in the
// Kodi sync queue. This is intentionally cheap: just a filesystem walk with
// no external processes. It is the single point called by runAutoScan,
// handleRescan, handleRescanHook, and startIndexRefresh.
func (s *Server) rescanLibraries() {
	added, removed := media.PopulateIDStore(s.config.Libraries)
	log.Printf("Library rescan: ID store refreshed for %d libraries (+%d / -%d items)",
		len(s.config.Libraries), len(added), len(removed))
	if s.config.App.KodiSyncQueue {
		s.syncQueue.RecordAdded(added)
		s.syncQueue.RecordRemoved(removed)
	}
	s.refreshTVChannels()
}

// refreshTVChannels re-parses every content_type: "tv" library into the
// in-memory channel store. Cheap and side-effect free (no disk writes).
func (s *Server) refreshTVChannels() {
	total, errs := tv.Populate(s.config.Libraries)
	for _, err := range errs {
		log.Printf("TV: %v", err)
	}
	if total > 0 || len(errs) > 0 {
		log.Printf("TV: %d channel(s) loaded", total)
	}
}

// handleRescan is the authenticated endpoint for manually triggering a rescan
// from the web UI or any client with a valid session.
func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	s.rescanLibraries()
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRescanHook is the webhook endpoint. It authenticates via a static
// bearer token (X-Webhook-Token header or ?token= query param) and debounces
// rapid successive calls (e.g. multiple files arriving at once from Sonarr).
func (s *Server) handleRescanHook(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("X-Webhook-Token")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	expected := s.config.App.WebhookToken
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		respondError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	// Debounce: reset a 5-second timer on every incoming hook call.
	// Only one rescan fires even when multiple files arrive in quick succession.
	s.rescanMu.Lock()
	if s.rescanTimer != nil {
		s.rescanTimer.Stop()
	}
	s.rescanTimer = time.AfterFunc(5*time.Second, s.rescanLibraries)
	s.rescanMu.Unlock()

	respondJSON(w, http.StatusOK, map[string]string{"status": "queued"})
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
