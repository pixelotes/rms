// Command rms is the media server entrypoint: it loads the config and runs the
// HTTP server (Jellyfin-compatible API + web UI) until interrupted.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/server"
)

func main() {
	configPath := flag.String("config", "config/config.yml", "path to the config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	applyGCTuning(cfg)

	srv := server.New(cfg)

	go func() {
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// applyGCTuning applies the optional GC settings from config. The standard Go
// env vars take precedence: GOGC and GOMEMLIMIT are already honored by the
// runtime at startup, so we only apply a config value when its env var is
// absent. This lets operators tune memory on constrained devices (e.g. a
// Raspberry Pi) from config.yml while keeping per-process env overrides.
func applyGCTuning(cfg *config.Config) {
	if os.Getenv("GOGC") == "" && cfg.App.GOGC > 0 {
		debug.SetGCPercent(cfg.App.GOGC)
		log.Printf("GC: GOGC set to %d from config", cfg.App.GOGC)
	}
	if os.Getenv("GOMEMLIMIT") == "" && cfg.App.MemoryLimitMB > 0 {
		limit := int64(cfg.App.MemoryLimitMB) << 20 // MiB -> bytes
		debug.SetMemoryLimit(limit)
		log.Printf("GC: soft memory limit set to %d MiB from config", cfg.App.MemoryLimitMB)
	}
}
