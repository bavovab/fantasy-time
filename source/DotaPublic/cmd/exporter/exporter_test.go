package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"salfetka-hub/dota-public/internal/publicdata"
)

func TestExporterPublishesOnlySanitizedTournamentSnapshot(t *testing.T) {
	responses := map[string]string{
		"/api/health":                  `{"status":"Healthy","version":"1.6.1","details":{"gcMonitoring":{"secret":"no"}}}`,
		"/api/heroes":                  `{"1":{"name":"Hero","imageUrl":""}}`,
		"/api/teams":                   `[{"slug":"aurora","name":"Aurora","tag":"AUR","status":"invited","slotOrder":1,"logoUrl":"","matchCount":1,"parsedCount":1,"historyTeamId":77,"openDotaId":88}]`,
		"/api/tournament-players":      `[{"alias":"mira","accountId":10,"name":"Mira","position":4,"teamSlug":"aurora","teamName":"Aurora","stats":{"matches":1,"metrics":[],"totalPoints":10,"bestMatch":{"points":10,"matchIds":[123]},"bestSeries":{"points":10,"matchIds":[123]}}}]`,
		"/api/player-filter-data":      `{"mira":{"player":{"alias":"mira","accountId":10,"name":"Mira","position":4,"teamSlug":"aurora","teamName":"Aurora","stats":{"matches":1,"metrics":[],"totalPoints":10,"bestMatch":{"points":10,"matchIds":[123]},"bestSeries":{"points":10,"matchIds":[123]}}},"matches":[{"matchId":123,"seriesId":123,"seriesType":1,"startTime":1,"duration":1200,"heroId":1,"included":true,"won":true,"fantasyPoints":10,"metrics":[]}]}}`,
		"/api/teams/aurora":            `{"team":{"slug":"aurora","name":"Aurora","tag":"AUR","status":"invited","slotOrder":1,"logoUrl":"","matchCount":1,"parsedCount":1,"historyTeamId":77,"openDotaId":88,"roster":[{"accountId":10,"name":"Mira","position":4,"stats":{"matches":1,"metrics":[],"totalPoints":10,"bestMatch":{"points":10,"matchIds":[123]},"bestSeries":{"points":10,"matchIds":[123]}}}]},"matches":[{"matchId":123,"parseStatus":"done","parseError":"private path /data/replay.dem","included":true,"teamSlug":"aurora","seriesId":123,"seriesType":1,"startTime":1,"teamScore":1,"opponentScore":0,"teamWon":true}]}`,
		"/api/tournament-players/mira": `{"player":{"alias":"mira","accountId":10,"name":"Mira","position":4,"teamSlug":"aurora","teamName":"Aurora","stats":{"matches":1,"metrics":[],"totalPoints":10,"bestMatch":{"points":10,"matchIds":[123]},"bestSeries":{"points":10,"matchIds":[123]}}},"matches":[{"matchId":123,"seriesId":123,"seriesType":1,"startTime":1,"duration":1200,"heroId":1,"included":true,"won":true,"fantasyPoints":10,"metrics":[]}]}`,
		"/api/matches/123":             `{"matchId":123,"startTime":1,"duration":1200,"radiantWin":true,"radiantKills":10,"direKills":5,"replayPath":"/data/replays/private.dem","replaySalt":999,"parsedAt":"secret","players":[{"accountId":10,"steamId":7656119,"name":"Mira","proName":"Mira","team":"radiant","teamSlot":0,"heroId":1,"kills":1,"fantasyPoints":10}]}`,
	}
	var mutex sync.Mutex
	requested := []string{}
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		requested = append(requested, request.Method+" "+request.URL.Path)
		mutex.Unlock()
		body, ok := responses[request.URL.Path]
		if !ok {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, body)
	}))
	defer source.Close()

	snapshot := filepath.Join(t.TempDir(), "snapshot")
	worker, err := newExporter(config{
		sourceURL: source.URL, snapshotDir: snapshot, cacheDir: filepath.Join(t.TempDir(), "cache"),
		version: "test", checkInterval: time.Minute, forceInterval: 5 * time.Minute, maxStale: time.Hour,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return time.Date(2026, 7, 18, 11, 0, 0, 0, time.UTC) }
	changed, err := worker.sync(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected a new snapshot")
	}
	releaseRoot, err := publicdata.ReadReleaseRoot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	match := readObject(t, filepath.Join(releaseRoot, "api", "matches", "123.json"))
	if _, ok := match["replayPath"]; ok {
		t.Fatal("replayPath leaked into public snapshot")
	}
	if _, ok := match["replaySalt"]; ok {
		t.Fatal("replaySalt leaked into public snapshot")
	}
	player := asMap(asSlice(match["players"])[0])
	if _, ok := player["steamId"]; ok {
		t.Fatal("steamId leaked into public snapshot")
	}
	team := readObject(t, filepath.Join(releaseRoot, "api", "teams", publicdata.SegmentKey("aurora")+".json"))
	if _, ok := asMap(team["team"])["openDotaId"]; ok {
		t.Fatal("internal team ID leaked into public snapshot")
	}
	teamMatch := asMap(asSlice(team["matches"])[0])
	if _, ok := teamMatch["parseError"]; ok {
		t.Fatal("internal parse error leaked into public snapshot")
	}
	filterData := readObject(t, filepath.Join(releaseRoot, "api", "player-filter-data.json"))
	miraFilter, ok := filterData["mira"]
	if !ok {
		t.Fatal("read-only player filter data was not published")
	}
	if duration := asMap(asSlice(asMap(miraFilter)["matches"])[0])["duration"]; duration != float64(1200) {
		t.Fatalf("public player duration = %v, want 1200", duration)
	}
	mutex.Lock()
	defer mutex.Unlock()
	for _, entry := range requested {
		if !strings.HasPrefix(entry, "GET ") {
			t.Fatalf("exporter made a write request: %s", entry)
		}
		if strings.Contains(entry, "/selection/") || strings.Contains(entry, "/jobs/") {
			t.Fatalf("exporter requested an administrative endpoint: %s", entry)
		}
	}
}

func TestPublicFingerprintIncludesExporterVersion(t *testing.T) {
	base := &sourceBase{
		heroesRaw:        []byte(`{"heroes":[]}`),
		teamsRaw:         []byte(`[]`),
		playersRaw:       []byte(`[]`),
		playerDetailsRaw: []byte(`{}`),
		health:           map[string]any{"version": "source-1"},
	}
	if fingerprintBase(base, "1.2.2") == fingerprintBase(base, "1.2.3") {
		t.Fatal("exporter version must invalidate the public snapshot fingerprint")
	}
}

func TestReconcileMissingMatchesKeepsSnapshotUsable(t *testing.T) {
	teamMatch := map[string]any{"matchId": json.Number("999"), "parseStatus": "done", "included": true}
	playerMatch := map[string]any{"matchId": json.Number("999")}
	teams := map[string]any{"aurora": map[string]any{"matches": []any{teamMatch}}}
	players := map[string]any{"mira": map[string]any{"matches": []any{playerMatch}}}
	missing := reconcileMissingMatches(teams, players, map[string]any{})
	if missing != 1 {
		t.Fatalf("missing = %d, want 1", missing)
	}
	if teamMatch["parseStatus"] != "unavailable" || teamMatch["included"] != false {
		t.Fatalf("team match was not marked unavailable: %#v", teamMatch)
	}
	if len(asSlice(asMap(players["mira"])["matches"])) != 0 {
		t.Fatal("missing match remained clickable in player details")
	}
}

func TestPublicPlayerPortraitsReplaceSteamAvatars(t *testing.T) {
	source := map[string]any{
		"accountId":   float64(124801257),
		"alias":       "nightfall",
		"avatarUrl":   "https://avatars.steamstatic.com/private.jpg",
		"portraitUrl": "https://liquipedia.net/stale-portrait.png",
		"stats":       map[string]any{},
	}
	player := filterPlayer(source)
	if player["portraitUrl"] != "/assets/players/124801257.webp" {
		t.Fatalf("curated portrait was not applied: %#v", player["portraitUrl"])
	}
	if _, ok := player["avatarUrl"]; ok {
		t.Fatal("Steam avatar leaked into a public player")
	}

	team := filterTeam(map[string]any{
		"slug":   "aurora",
		"roster": []any{source},
	}, true)
	rosterPlayer := asMap(asSlice(team["roster"])[0])
	if rosterPlayer["portraitUrl"] != "/assets/players/124801257.webp" {
		t.Fatalf("curated roster portrait was not applied: %#v", rosterPlayer["portraitUrl"])
	}
	if _, ok := rosterPlayer["avatarUrl"]; ok {
		t.Fatal("Steam avatar leaked into a public roster")
	}
}

func TestIronWingPublicBrandingKeepsLegacySlug(t *testing.T) {
	team := filterTeam(map[string]any{
		"slug": "1w", "name": "1w Team", "tag": "1W", "logoUrl": "/old-logo.png",
	}, false)
	if team["slug"] != "1w" || team["name"] != "Iron Wing" || team["tag"] != "IW" || team["logoUrl"] != publicIronWingLogo {
		t.Fatalf("team branding = %#v", team)
	}

	player := filterPlayer(map[string]any{
		"alias": "player", "teamSlug": "1w", "teamName": "1w Team", "teamLogoUrl": "/old-logo.png", "stats": map[string]any{},
	})
	if player["teamSlug"] != "1w" || player["teamName"] != "Iron Wing" || player["teamLogoUrl"] != publicIronWingLogo {
		t.Fatalf("player branding = %#v", player)
	}

	matches := filterTeamMatches([]any{map[string]any{
		"matchId": float64(1), "opponentTeamId": float64(10150413), "opponentName": "1w Team", "opponentLogo": "/old-logo.png",
	}})
	match := asMap(matches[0])
	if match["opponentName"] != "Iron Wing" || match["opponentLogo"] != publicIronWingLogo {
		t.Fatalf("opponent branding = %#v", match)
	}
}

func TestOfficialSeason13PortraitSetIsComplete(t *testing.T) {
	if len(curatedPlayerPortraits) != 80 {
		t.Fatalf("official portrait count = %d, want 80", len(curatedPlayerPortraits))
	}
	for _, accountID := range []string{"331855530", "106573901", "1044002267"} {
		want := "/assets/players/" + accountID + ".webp"
		if curatedPlayerPortraits[accountID] != want {
			t.Fatalf("portrait %s = %q, want %q", accountID, curatedPlayerPortraits[accountID], want)
		}
	}
}

func TestCuratedPortraitPathsSurviveImageRewriting(t *testing.T) {
	value := map[string]any{"portraitUrl": "/assets/players/1044002267.webp"}
	rewriteImageURLs(value, map[string]string{})
	if value["portraitUrl"] != "/assets/players/1044002267.webp" {
		t.Fatalf("local portrait path was rewritten: %#v", value["portraitUrl"])
	}
}

func TestBundledTeamLogoPathsSurviveImageRewriting(t *testing.T) {
	value := map[string]any{"logoUrl": publicIronWingLogo}
	rewriteImageURLs(value, map[string]string{})
	if value["logoUrl"] != publicIronWingLogo {
		t.Fatalf("bundled team logo path was rewritten: %#v", value["logoUrl"])
	}
}

func readObject(t *testing.T, filename string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
