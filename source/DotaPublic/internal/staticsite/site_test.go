package staticsite

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildCreatesSelfContainedGitHubPagesSite(t *testing.T) {
	root := t.TempDir()
	baseWeb := filepath.Join(root, "base-web")
	publicWeb := filepath.Join(root, "public-web")
	snapshot := filepath.Join(root, "snapshot")
	live := filepath.Join(root, "live")
	output := filepath.Join(root, "site")
	release := "20260731T200000Z-abcdef123456"
	releaseRoot := filepath.Join(snapshot, "releases", release)

	writeTestFile(t, filepath.Join(baseWeb, "app.js"), `const logo = "/assets/logo.png";`)
	writeTestFile(t, filepath.Join(baseWeb, "runtime-config.js"), `window.DOTA_HUB_MODE = "public";`)
	writeTestFile(t, filepath.Join(publicWeb, "index.html"), `<img src="/assets/logo.png"><a href="/privacy.html">Privacy</a><a href="/rights.html#asset-registry">Rights</a>`)
	writeTestFile(t, filepath.Join(publicWeb, "privacy-github.html"), `<a href="/#my-team">Back</a>`)
	writeTestFile(t, filepath.Join(publicWeb, "rights.html"), `<a href="./#my-team">Back</a><p id="asset-registry">Assets</p>`)
	writeTestFile(t, filepath.Join(publicWeb, "site.webmanifest"), `{"name":"Fantasy Time"}`)
	writeTestFile(t, filepath.Join(publicWeb, "assets", "logo.png"), "image")
	writeTestFile(t, filepath.Join(publicWeb, "source", "DotaPublic", "main.go"), "package main")
	writeTestFile(t, filepath.Join(publicWeb, "source", "DotaPublic", "Dockerfile"), "FROM scratch")
	writeTestFile(t, filepath.Join(publicWeb, "not-public.go"), "package private")
	writeTestFile(t, filepath.Join(snapshot, "current.json"), `{"release":"`+release+`"}`)
	for _, name := range []string{"health", "heroes", "teams", "tournament-players", "player-filter-data"} {
		writeTestFile(t, filepath.Join(releaseRoot, "api", name+".json"), `{}`)
	}
	writeTestFile(t, filepath.Join(releaseRoot, "api", "teams", "aurora.json"), `{"logoUrl":"/media/abc.webp"}`)
	writeTestFile(t, filepath.Join(releaseRoot, "media", "abc.webp"), "media")
	writeTestFile(t, filepath.Join(live, "overview.json"), `{"matches":[]}`)

	result, err := Build(Config{
		BaseWebRoot: baseWeb, PublicWebRoot: publicWeb, SnapshotRoot: snapshot,
		LiveRoot: live, OutputRoot: output, Domain: "fantasy-time.online",
		Now: func() time.Time { return time.Date(2026, 7, 31, 20, 5, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Release != release || result.FileCount == 0 || result.SizeBytes == 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	assertContains(t, filepath.Join(output, "runtime-config.js"), "DOTA_HUB_STATIC_API = true")
	assertContains(t, filepath.Join(output, "app.js"), `"assets/logo.png"`)
	assertContains(t, filepath.Join(output, "index.html"), `href="privacy.html"`)
	assertContains(t, filepath.Join(output, "index.html"), `href="rights.html#asset-registry"`)
	assertContains(t, filepath.Join(output, "api", "teams", "aurora.json"), `"media/abc.webp"`)
	assertContains(t, filepath.Join(output, "privacy.html"), `href="./#my-team"`)
	assertContains(t, filepath.Join(output, "rights.html"), `id="asset-registry"`)
	assertContains(t, filepath.Join(output, "CNAME"), "fantasy-time.online")
	assertContains(t, filepath.Join(output, "robots.txt"), "Sitemap: https://fantasy-time.online/sitemap.xml")
	assertContains(t, filepath.Join(output, "sitemap.xml"), "<lastmod>2026-07-31</lastmod>")
	assertContains(t, filepath.Join(output, indexNowKey+".txt"), indexNowKey)
	assertContains(t, filepath.Join(output, "source", "DotaPublic", "main.go"), "package main")
	assertContains(t, filepath.Join(output, "source", "DotaPublic", "Dockerfile"), "FROM scratch")
	if _, err := os.Stat(filepath.Join(output, "privacy-github.html")); !os.IsNotExist(err) {
		t.Fatal("builder-only privacy template must not be published")
	}
	if _, err := os.Stat(filepath.Join(output, "current.json")); !os.IsNotExist(err) {
		t.Fatal("snapshot control files must not be published")
	}
	if _, err := os.Stat(filepath.Join(output, "not-public.go")); !os.IsNotExist(err) {
		t.Fatal("source files outside the source directory must not be published")
	}
	manifestRaw, err := os.ReadFile(filepath.Join(output, "site-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Result
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil || manifest.Release != release {
		t.Fatalf("invalid manifest: %s", manifestRaw)
	}
}

func TestBuildRejectsInvalidDomain(t *testing.T) {
	_, err := Build(Config{OutputRoot: filepath.Join(t.TempDir(), "site"), Domain: "bad domain"})
	if err == nil {
		t.Fatal("invalid domain must be rejected")
	}
}

func writeTestFile(t *testing.T, filename, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContains(t *testing.T, filename, expected string) {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), expected) {
		t.Fatalf("%s does not contain %q: %s", filename, expected, data)
	}
}
