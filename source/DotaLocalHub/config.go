package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultMaxUploadBytes int64 = 1 << 30
const defaultMaxReplayBytes int64 = 1 << 30

func loadConfig(root string) (Config, error) {
	config := Config{
		Listen:                    "127.0.0.1:8787",
		SteamAPIRequestIntervalMs: 1200,
		MaxUploadBytes:            defaultMaxUploadBytes,
		MaxReplayBytes:            defaultMaxReplayBytes,
		GCMonitorIntervalSeconds:  180,
		GCHistoryMatches:          10,
		GCInitialLookbackHours:    12,
		GCMaxNewMatchesPerCycle:   8,
	}

	path := filepath.Join(root, "config.json")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return config, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return config, err
		}
	}

	if value := strings.TrimSpace(os.Getenv("DOTA_HUB_LISTEN")); value != "" {
		config.Listen = value
	}
	if value := strings.TrimSpace(os.Getenv("DOTA_HUB_ALLOWED_ORIGINS")); value != "" {
		config.AllowedOrigins = splitList(value)
	}
	if value := strings.TrimSpace(os.Getenv("DOTA_HUB_MAX_UPLOAD_BYTES")); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			config.MaxUploadBytes = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("DOTA_HUB_MAX_REPLAY_BYTES")); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			config.MaxReplayBytes = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("OPENDOTA_API_KEY")); value != "" {
		config.OpenDotaAPIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("STEAM_API_KEY")); value != "" {
		config.SteamAPIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("DOTA_HUB_STEAM_API_KEY_FILE")); value != "" {
		config.SteamAPIKeyFile = value
	}
	if value := strings.TrimSpace(os.Getenv("DOTA_HUB_STEAM_API_INTERVAL_MS")); value != "" {
		var parsed int
		if err := json.Unmarshal([]byte(value), &parsed); err == nil && parsed > 0 {
			config.SteamAPIRequestIntervalMs = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("STRATZ_TOKEN")); value != "" {
		config.StratzToken = value
	}
	if value := strings.TrimSpace(os.Getenv("DOTA_HUB_GC_URL")); value != "" {
		config.GCBaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("DOTA_HUB_GC_MONITOR_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			config.GCMonitorEnabled = parsed
		}
	}
	for name, target := range map[string]*int{
		"DOTA_HUB_GC_MONITOR_INTERVAL_SECONDS": &config.GCMonitorIntervalSeconds,
		"DOTA_HUB_GC_HISTORY_MATCHES":          &config.GCHistoryMatches,
		"DOTA_HUB_GC_INITIAL_LOOKBACK_HOURS":   &config.GCInitialLookbackHours,
		"DOTA_HUB_GC_MAX_NEW_PER_CYCLE":        &config.GCMaxNewMatchesPerCycle,
	} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
				*target = parsed
			}
		}
	}
	if value := strings.TrimSpace(os.Getenv("DOTA_HUB_STRATZ_TOKEN_FILE")); value != "" {
		config.StratzTokenFile = value
	}
	if config.SteamAPIKey == "" {
		config.SteamAPIKey = readFirstSecret(config.SteamAPIKeyFile,
			filepath.Join(root, "data", "secrets", "steam.token"),
			filepath.Join(root, "data", "secrets", "steam-api-key.txt"),
		)
	}
	if config.StratzToken == "" {
		config.StratzToken = readFirstSecret(config.StratzTokenFile,
			filepath.Join(root, "data", "secrets", "stratz.token"),
			filepath.Join(root, "data", "secrets", "stratz-api-token.txt"),
		)
	}
	if config.Listen == "" {
		config.Listen = "127.0.0.1:8787"
	}
	if config.SteamAPIRequestIntervalMs <= 0 {
		config.SteamAPIRequestIntervalMs = 1200
	}
	if config.MaxUploadBytes <= 0 {
		config.MaxUploadBytes = defaultMaxUploadBytes
	}
	if config.MaxReplayBytes <= 0 {
		config.MaxReplayBytes = defaultMaxReplayBytes
	}
	if config.GCMonitorIntervalSeconds < 120 {
		config.GCMonitorIntervalSeconds = 120
	}
	if config.GCHistoryMatches < 1 || config.GCHistoryMatches > 20 {
		config.GCHistoryMatches = 10
	}
	if config.GCInitialLookbackHours < 1 || config.GCInitialLookbackHours > 168 {
		config.GCInitialLookbackHours = 12
	}
	if config.GCMaxNewMatchesPerCycle < 1 || config.GCMaxNewMatchesPerCycle > 16 {
		config.GCMaxNewMatchesPerCycle = 8
	}
	return config, nil
}

func splitList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func readFirstSecret(paths ...string) string {
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if value := strings.TrimSpace(string(data)); value != "" {
			return value
		}
	}
	return ""
}

func applicationRoot() (string, error) {
	if value := strings.TrimSpace(os.Getenv("DOTA_HUB_DIR")); value != "" {
		return filepath.Abs(value)
	}

	executable, err := os.Executable()
	if err == nil && !strings.Contains(strings.ToLower(executable), "go-build") {
		return filepath.Dir(executable), nil
	}
	return os.Getwd()
}
