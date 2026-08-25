package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"salfetka-hub/dota-public/internal/publicdata"
)

const maxJSONBytes = 32 << 20

type config struct {
	sourceURL     string
	snapshotDir   string
	cacheDir      string
	version       string
	checkInterval time.Duration
	forceInterval time.Duration
	maxStale      time.Duration
}

type exporter struct {
	config config
	client *http.Client
	logger *log.Logger
	now    func() time.Time
}

type exporterState struct {
	Fingerprint  string    `json:"fingerprint"`
	Release      string    `json:"release"`
	LastExportAt time.Time `json:"lastExportAt"`
}

type sourceBase struct {
	healthRaw        []byte
	heroesRaw        []byte
	teamsRaw         []byte
	playersRaw       []byte
	playerDetailsRaw []byte
	health           any
	heroes           any
	teams            any
	players          any
	playerDetails    any
}

type endpoint struct {
	key  string
	path string
}

type httpStatusError struct {
	status int
}

func (failure *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d", failure.status)
}

func newExporter(configuration config, logger *log.Logger) (*exporter, error) {
	base, err := url.Parse(configuration.sourceURL)
	if err != nil || base.Scheme != "http" || base.Host == "" || base.Path != "" {
		return nil, errors.New("DOTA_PUBLIC_SOURCE_URL must be a plain internal HTTP origin")
	}
	configuration.sourceURL = strings.TrimRight(configuration.sourceURL, "/")
	return &exporter{
		config: configuration,
		client: &http.Client{Timeout: 45 * time.Second},
		logger: logger,
		now:    time.Now,
	}, nil
}

func (worker *exporter) sync(ctx context.Context, force bool) (bool, error) {
	base, err := worker.fetchBase(ctx)
	if err != nil {
		return false, err
	}
	fingerprint := fingerprintBase(base, worker.config.version)
	state, _ := worker.readState()
	if !force && state.Fingerprint == fingerprint {
		if _, err := publicdata.ReadReleaseRoot(worker.config.snapshotDir); err == nil {
			worker.logger.Printf("snapshot unchanged fingerprint=%s", fingerprint[:12])
			return false, nil
		}
	}
	if err := worker.export(ctx, base, fingerprint); err != nil {
		return false, err
	}
	return true, nil
}

func (worker *exporter) fetchBase(ctx context.Context) (*sourceBase, error) {
	healthRaw, health, err := worker.fetch(ctx, "/api/health")
	if err != nil {
		return nil, fmt.Errorf("source health: %w", err)
	}
	heroesRaw, heroes, err := worker.fetch(ctx, "/api/heroes")
	if err != nil {
		return nil, fmt.Errorf("heroes: %w", err)
	}
	teamsRaw, teams, err := worker.fetch(ctx, "/api/teams")
	if err != nil {
		return nil, fmt.Errorf("teams: %w", err)
	}
	playersRaw, players, err := worker.fetch(ctx, "/api/tournament-players")
	if err != nil {
		return nil, fmt.Errorf("players: %w", err)
	}
	playerDetailsRaw, playerDetails, err := worker.fetch(ctx, "/api/player-filter-data")
	if err != nil {
		return nil, fmt.Errorf("player filter data: %w", err)
	}
	if len(asSlice(teams)) == 0 || len(asSlice(players)) == 0 {
		return nil, errors.New("source returned an empty tournament")
	}
	return &sourceBase{
		healthRaw: healthRaw, heroesRaw: heroesRaw, teamsRaw: teamsRaw, playersRaw: playersRaw,
		playerDetailsRaw: playerDetailsRaw,
		health:           health, heroes: heroes, teams: teams, players: players, playerDetails: playerDetails,
	}, nil
}

func fingerprintBase(base *sourceBase, exporterVersion string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("public-schema-v5"))
	_, _ = hash.Write([]byte(exporterVersion))
	for _, raw := range [][]byte{base.heroesRaw, base.teamsRaw, base.playersRaw, base.playerDetailsRaw} {
		_, _ = hash.Write(raw)
	}
	_, _ = hash.Write([]byte(stringField(asMap(base.health), "version")))
	return hex.EncodeToString(hash.Sum(nil))
}

func (worker *exporter) export(ctx context.Context, base *sourceBase, fingerprint string) error {
	generatedAt := worker.now().UTC()
	release := generatedAt.Format("20060102T150405Z") + "-" + fingerprint[:12]
	releasesDir := filepath.Join(worker.config.snapshotDir, "releases")
	staging := filepath.Join(releasesDir, ".tmp-"+release)
	final := filepath.Join(releasesDir, release)
	if err := os.MkdirAll(filepath.Join(staging, "api", "teams"), 0o750); err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := os.MkdirAll(filepath.Join(staging, "api", "tournament-players"), 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(staging, "api", "matches"), 0o750); err != nil {
		return err
	}

	heroes := filterHeroes(base.heroes)
	teams := filterTeamSummaries(base.teams)
	players := filterPlayerSummaries(base.players)

	teamEndpoints := make([]endpoint, 0, len(teams))
	for _, item := range teams {
		slug := stringField(asMap(item), "slug")
		if slug == "" {
			return errors.New("team without slug")
		}
		teamEndpoints = append(teamEndpoints, endpoint{key: slug, path: "/api/teams/" + url.PathEscape(slug)})
	}
	teamRaw, err := worker.fetchMany(ctx, teamEndpoints, 8, false)
	if err != nil {
		return fmt.Errorf("team details: %w", err)
	}
	teamDetails := make(map[string]any, len(teamRaw))
	playerDetails := make(map[string]any, len(players))
	matchIDs := map[string]struct{}{}
	for key, value := range teamRaw {
		filtered := filterTeamDetail(value)
		teamDetails[key] = filtered
		collectMatchIDs(asMap(filtered)["matches"], matchIDs)
	}
	playerDetailSource := asMap(base.playerDetails)
	for _, item := range players {
		alias := stringField(asMap(item), "alias")
		if alias == "" {
			return errors.New("player without alias")
		}
		value, exists := playerDetailSource[alias]
		if !exists {
			return fmt.Errorf("missing player detail: %s", alias)
		}
		filtered := filterPlayerDetail(value)
		playerDetails[alias] = filtered
		collectMatchIDs(asMap(filtered)["matches"], matchIDs)
	}

	matchEndpoints := make([]endpoint, 0, len(matchIDs))
	for matchID := range matchIDs {
		matchEndpoints = append(matchEndpoints, endpoint{key: matchID, path: "/api/matches/" + matchID})
	}
	sort.Slice(matchEndpoints, func(i, j int) bool { return matchEndpoints[i].key < matchEndpoints[j].key })
	matchRaw, err := worker.fetchMany(ctx, matchEndpoints, 12, true)
	if err != nil {
		return fmt.Errorf("match details: %w", err)
	}
	matches := make(map[string]any, len(matchRaw))
	for key, value := range matchRaw {
		matches[key] = filterMatch(value)
	}
	missingMatchDetails := reconcileMissingMatches(teamDetails, playerDetails, matches)

	imageRoots := []any{heroes, teams, players}
	for _, value := range teamDetails {
		imageRoots = append(imageRoots, value)
	}
	for _, value := range playerDetails {
		imageRoots = append(imageRoots, value)
	}
	for _, value := range matches {
		imageRoots = append(imageRoots, value)
	}
	mirroredImages, missingImages := newImageMirror(worker.config.cacheDir, filepath.Join(staging, "media"), worker.logger).mirrorAll(ctx, imageRoots...)

	health := map[string]any{
		"status":                    "Healthy",
		"uptimeSeconds":             0,
		"lastSuccessfulOperationAt": generatedAt.Format(time.RFC3339),
		"lastErrorAt":               nil,
		"lastError":                 "",
		"version":                   worker.config.version,
		"buildDate":                 generatedAt.Format("2006-01-02"),
		"apiVersion":                "1.0",
		"schemaVersion":             "1",
		"dockerImageTag":            worker.config.version,
		"generatedAt":               generatedAt.Format(time.RFC3339),
		"snapshotVersion":           release,
		"sourceVersion":             stringField(asMap(base.health), "version"),
		"details": map[string]any{
			"teams": len(teams), "players": len(players), "matches": len(matches),
			"missingMatchDetails": missingMatchDetails,
			"mirroredImages":      mirroredImages, "missingImages": missingImages,
		},
	}

	if err := writeJSON(filepath.Join(staging, "api", "health.json"), health); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(staging, "api", "heroes.json"), heroes); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(staging, "api", "teams.json"), teams); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(staging, "api", "tournament-players.json"), players); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(staging, "api", "player-filter-data.json"), playerDetails); err != nil {
		return err
	}
	for slug, detail := range teamDetails {
		if err := writeJSON(filepath.Join(staging, "api", "teams", publicdata.SegmentKey(slug)+".json"), detail); err != nil {
			return err
		}
	}
	for alias, detail := range playerDetails {
		if err := writeJSON(filepath.Join(staging, "api", "tournament-players", publicdata.SegmentKey(alias)+".json"), detail); err != nil {
			return err
		}
	}
	for matchID, match := range matches {
		if err := writeJSON(filepath.Join(staging, "api", "matches", matchID+".json"), match); err != nil {
			return err
		}
	}
	if _, err := os.Stat(final); err == nil {
		return errors.New("snapshot release already exists")
	}
	if err := os.Rename(staging, final); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(worker.config.snapshotDir, "current.json"), publicdata.Pointer{Release: release}); err != nil {
		return err
	}
	state := exporterState{Fingerprint: fingerprint, Release: release, LastExportAt: generatedAt}
	if err := writeJSONAtomic(filepath.Join(worker.config.snapshotDir, "exporter-state.json"), state); err != nil {
		return err
	}
	worker.pruneReleases(release, 3)
	worker.logger.Printf("snapshot published release=%s teams=%d players=%d matches=%d missing_match_details=%d images=%d missing_images=%d", release, len(teams), len(players), len(matches), missingMatchDetails, mirroredImages, missingImages)
	return nil
}

func (worker *exporter) fetch(ctx context.Context, requestPath string) ([]byte, any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, worker.config.sourceURL+requestPath, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := worker.client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, &httpStatusError{status: response.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxJSONBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(raw) == 0 || len(raw) > maxJSONBytes {
		return nil, nil, errors.New("JSON response size is invalid")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, nil, err
	}
	return raw, value, nil
}

func (worker *exporter) fetchMany(ctx context.Context, endpoints []endpoint, concurrency int, allowNotFound bool) (map[string]any, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(map[string]any, len(endpoints))
	jobs := make(chan endpoint)
	var resultMutex sync.Mutex
	var firstError error
	var errorMutex sync.Mutex
	var wait sync.WaitGroup
	if concurrency > len(endpoints) {
		concurrency = len(endpoints)
	}
	for range concurrency {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for item := range jobs {
				_, value, err := worker.fetch(ctx, item.path)
				if err != nil {
					var statusFailure *httpStatusError
					if allowNotFound && errors.As(err, &statusFailure) && statusFailure.status == http.StatusNotFound {
						worker.logger.Printf("match detail unavailable match_id=%s", item.key)
						continue
					}
					errorMutex.Lock()
					if firstError == nil {
						firstError = fmt.Errorf("%s: %w", item.path, err)
						cancel()
					}
					errorMutex.Unlock()
					continue
				}
				resultMutex.Lock()
				result[item.key] = value
				resultMutex.Unlock()
			}
		}()
	}
	func() {
		defer close(jobs)
		for _, item := range endpoints {
			select {
			case jobs <- item:
			case <-ctx.Done():
				return
			}
		}
	}()
	wait.Wait()
	if firstError != nil {
		return nil, firstError
	}
	return result, nil
}

func reconcileMissingMatches(teamDetails, playerDetails, matches map[string]any) int {
	missing := map[string]bool{}
	for _, detail := range teamDetails {
		for _, item := range asSlice(asMap(detail)["matches"]) {
			match := asMap(item)
			matchID := numberString(match["matchId"])
			if _, ok := matches[matchID]; ok {
				continue
			}
			missing[matchID] = true
			match["parseStatus"] = "unavailable"
			match["included"] = false
		}
	}
	for _, detail := range playerDetails {
		object := asMap(detail)
		filtered := make([]any, 0, len(asSlice(object["matches"])))
		for _, item := range asSlice(object["matches"]) {
			matchID := numberString(asMap(item)["matchId"])
			if _, ok := matches[matchID]; ok {
				filtered = append(filtered, item)
			} else {
				missing[matchID] = true
			}
		}
		object["matches"] = filtered
	}
	delete(missing, "")
	delete(missing, "<nil>")
	return len(missing)
}

func collectMatchIDs(value any, result map[string]struct{}) {
	for _, item := range asSlice(value) {
		matchID := numberString(asMap(item)["matchId"])
		if matchID != "" && matchID != "<nil>" && matchID != "0" {
			result[matchID] = struct{}{}
		}
	}
}

func (worker *exporter) readState() (exporterState, error) {
	var state exporterState
	raw, err := os.ReadFile(filepath.Join(worker.config.snapshotDir, "exporter-state.json"))
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(raw, &state)
	return state, err
}

func (worker *exporter) forceRequired() bool {
	state, err := worker.readState()
	return err != nil || state.LastExportAt.IsZero() || worker.now().Sub(state.LastExportAt) >= worker.config.forceInterval
}

func (worker *exporter) pruneReleases(current string, keep int) {
	directory := filepath.Join(worker.config.snapshotDir, "releases")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && publicdata.ValidRelease(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	retained := map[string]bool{current: true}
	for _, name := range names {
		if len(retained) >= keep {
			break
		}
		retained[name] = true
	}
	for _, name := range names {
		if retained[name] {
			continue
		}
		_ = os.RemoveAll(filepath.Join(directory, name))
	}
}

func writeJSON(filename string, value any) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0o640)
}

func writeJSONAtomic(filename string, value any) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temporary := filename + ".tmp"
	if err := os.WriteFile(temporary, data, 0o640); err != nil {
		return err
	}
	if err := os.Rename(temporary, filename); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
