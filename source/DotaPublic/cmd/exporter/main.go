package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"salfetka-hub/dota-public/internal/publicdata"
)

func main() {
	configuration := config{
		sourceURL:     envOr("DOTA_PUBLIC_SOURCE_URL", "http://dota-local-hub:8787"),
		snapshotDir:   envOr("DOTA_PUBLIC_SNAPSHOT_DIR", "/snapshot"),
		cacheDir:      envOr("DOTA_PUBLIC_IMAGE_CACHE_DIR", "/cache"),
		version:       envOr("DOTA_PUBLIC_VERSION", "1.0.0"),
		checkInterval: durationOr("DOTA_PUBLIC_CHECK_INTERVAL", time.Minute),
		forceInterval: durationOr("DOTA_PUBLIC_FORCE_INTERVAL", 5*time.Minute),
		maxStale:      durationOr("DOTA_PUBLIC_MAX_STALE", 30*time.Minute),
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(configuration); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	logger := log.New(os.Stdout, "dota-public-exporter ", log.LstdFlags|log.LUTC)
	worker, err := newExporter(configuration, logger)
	if err != nil {
		logger.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	run := func() {
		cycle, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()
		changed, err := worker.sync(cycle, worker.forceRequired())
		if err != nil {
			logger.Printf("snapshot update failed: %v; keeping the last valid release", err)
			return
		}
		if changed {
			logger.Print("snapshot update completed")
		}
	}
	run()
	ticker := time.NewTicker(configuration.checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func healthcheck(configuration config) error {
	releaseRoot, err := publicdata.ReadReleaseRoot(configuration.snapshotDir)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(releaseRoot, "api", "health.json"))
	if err != nil {
		return err
	}
	var health struct {
		GeneratedAt time.Time `json:"generatedAt"`
	}
	if err := json.Unmarshal(raw, &health); err != nil {
		return err
	}
	if health.GeneratedAt.IsZero() {
		return errors.New("snapshot has no generation time")
	}
	if time.Since(health.GeneratedAt) > configuration.maxStale {
		return errors.New("snapshot is stale")
	}
	return nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationOr(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
