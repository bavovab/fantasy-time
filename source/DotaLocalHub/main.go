package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	rootFlag := flag.String("root", "", "application data directory")
	flag.Parse()

	root, err := applicationRoot()
	if err != nil {
		log.Fatal(err)
	}
	if *rootFlag != "" {
		root, err = filepath.Abs(*rootFlag)
		if err != nil {
			log.Fatal(err)
		}
	}

	for _, directory := range []string{
		filepath.Join(root, "data"),
		filepath.Join(root, "data", "replays"),
		filepath.Join(root, "data", "json"),
		filepath.Join(root, "data", "tournaments"),
	} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			log.Fatal(err)
		}
	}

	config, err := loadConfig(root)
	if err != nil {
		log.Fatalf("config.json: %v", err)
	}

	tournament, err := loadTournamentConfig(root)
	if err != nil {
		log.Fatalf("tournament config: %v", err)
	}

	store, err := openStore(filepath.Join(root, "data", "dota-hub.db"), tournament)
	if err != nil {
		log.Fatalf("SQLite: %v", err)
	}
	defer store.Close()
	cleanupReplayDirectory(filepath.Join(root, "data", "replays"))
	_ = store.ClearAllReplayPaths(context.Background())

	downloader := newDownloader(config)
	gcClient := newGCClient(config.GCBaseURL)
	jobs := newJobManager(root, store, downloader, gcClient, config)
	gcMonitor := newGCMonitor(store, jobs, gcClient, config)
	gcMonitor.Start(context.Background())
	server := &Server{
		root: root, store: store, jobs: jobs, downloader: downloader, config: config,
		startedAt: time.Now().UTC(),
		gcMonitor: gcMonitor,
	}

	httpServer := &http.Server{
		Addr:              config.Listen,
		Handler:           server.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Println()
	fmt.Println("Dota Local Hub запущен")
	fmt.Printf("Открой в браузере: %s\n", listenURL(config.Listen))
	fmt.Printf("Данные: %s\n", filepath.Join(root, "data"))
	fmt.Println("Для остановки нажми Ctrl+C")
	fmt.Println()

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func cleanupReplayDirectory(directory string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".dem") || strings.Contains(name, ".dem.bz2") {
			_ = os.Remove(filepath.Join(directory, entry.Name()))
		}
	}
}
