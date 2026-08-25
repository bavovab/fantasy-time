package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"salfetka-hub/dota-public/internal/publicdata"
)

func TestPublicServerAllowsOnlyPublishedReadEndpoints(t *testing.T) {
	root := t.TempDir()
	web := filepath.Join(root, "web")
	snapshot := filepath.Join(root, "snapshot")
	live := filepath.Join(root, "live")
	release := "20260718T110000Z-abcdef123456"
	releaseRoot := filepath.Join(snapshot, "releases", release)
	mustWrite(t, filepath.Join(web, "index.html"), []byte("public index"))
	mustWrite(t, filepath.Join(releaseRoot, "api", "health.json"), []byte(`{"status":"Healthy"}`))
	mustWrite(t, filepath.Join(releaseRoot, "api", "teams.json"), []byte(`[{"slug":"aurora"}]`))
	mustWrite(t, filepath.Join(releaseRoot, "api", "player-filter-data.json"), []byte(`{"mira":{"matches":[]}}`))
	mustWrite(t, filepath.Join(releaseRoot, "api", "teams", publicdata.SegmentKey("aurora")+".json"), []byte(`{"team":{"slug":"aurora"}}`))
	mustWrite(t, filepath.Join(snapshot, "current.json"), []byte(`{"release":"`+release+`"}`))
	mustWrite(t, filepath.Join(live, "overview.json"), []byte(`{"matches":[]}`))
	mustWrite(t, filepath.Join(live, "matches", "gotf-2026-test-a.json"), []byte(`{"id":"gotf-2026-test-a"}`))
	server := &publicServer{
		webRoot: web, snapshotRoot: snapshot, liveRoot: live,
		startedAt: time.Date(2026, 7, 18, 10, 59, 0, 0, time.UTC),
		now:       func() time.Time { return time.Date(2026, 7, 18, 11, 0, 0, 0, time.UTC) },
	}

	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/", http.StatusOK},
		{http.MethodGet, "/api/teams", http.StatusOK},
		{http.MethodGet, "/api/teams.json", http.StatusOK},
		{http.MethodGet, "/api/player-filter-data", http.StatusOK},
		{http.MethodHead, "/api/teams/aurora", http.StatusOK},
		{http.MethodHead, "/api/teams/aurora.json", http.StatusOK},
		{http.MethodHead, "/api/teams/" + publicdata.SegmentKey("aurora") + ".json", http.StatusOK},
		{http.MethodGet, "/api/live/overview", http.StatusOK},
		{http.MethodGet, "/api/live/matches/gotf-2026-test-a", http.StatusOK},
		{http.MethodGet, "/api/live/matches/../../health", http.StatusNotFound},
		{http.MethodPost, "/api/teams", http.StatusMethodNotAllowed},
		{http.MethodPut, "/api/teams/aurora", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/selection/global", http.StatusNotFound},
		{http.MethodGet, "/api/jobs/active", http.StatusNotFound},
		{http.MethodGet, "/.env", http.StatusNotFound},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.status, response.Body.String())
			}
			if response.Header().Get("Set-Cookie") != "" {
				t.Fatal("public server must not set cookies")
			}
			if !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Fatal("security policy is missing")
			}
		})
	}
}

func mustWrite(t *testing.T, filename string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
