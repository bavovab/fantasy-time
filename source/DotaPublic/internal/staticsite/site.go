package staticsite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"salfetka-hub/dota-public/internal/publicdata"
)

var domainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)

const indexNowKey = "8f4c91d27e6a43b5a0c2d9e71f8536ab"

type Config struct {
	BaseWebRoot   string
	PublicWebRoot string
	SnapshotRoot  string
	LiveRoot      string
	OutputRoot    string
	Domain        string
	Now           func() time.Time
}

type Result struct {
	Release   string `json:"release"`
	BuiltAt   string `json:"builtAt"`
	FileCount int    `json:"fileCount"`
	SizeBytes int64  `json:"sizeBytes"`
}

func Build(config Config) (Result, error) {
	if err := validateConfig(config); err != nil {
		return Result{}, err
	}
	releaseRoot, err := publicdata.ReadReleaseRoot(config.SnapshotRoot)
	if err != nil {
		return Result{}, fmt.Errorf("read current snapshot: %w", err)
	}
	release := filepath.Base(releaseRoot)
	if !publicdata.ValidRelease(release) {
		return Result{}, errors.New("invalid current snapshot release")
	}

	outputRoot, err := filepath.Abs(config.OutputRoot)
	if err != nil {
		return Result{}, err
	}
	parent := filepath.Dir(outputRoot)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return Result{}, err
	}
	staging, err := os.MkdirTemp(parent, ".fantasy-time-staging-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(staging)

	if err := copyTree(config.BaseWebRoot, staging, allowWebFile); err != nil {
		return Result{}, fmt.Errorf("copy base web files: %w", err)
	}
	if err := copyTree(config.PublicWebRoot, staging, allowWebFile); err != nil {
		return Result{}, fmt.Errorf("copy public web files: %w", err)
	}
	if err := installGitHubPrivacyPage(staging); err != nil {
		return Result{}, err
	}
	if err := copyTree(filepath.Join(releaseRoot, "api"), filepath.Join(staging, "api"), allowJSONFile); err != nil {
		return Result{}, fmt.Errorf("copy public API: %w", err)
	}
	if err := copyTree(filepath.Join(releaseRoot, "media"), filepath.Join(staging, "media"), allowMediaFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Result{}, fmt.Errorf("copy public media: %w", err)
	}
	if err := copyLiveSnapshot(config.LiveRoot, filepath.Join(staging, "api", "live")); err != nil {
		return Result{}, fmt.Errorf("copy public live snapshot: %w", err)
	}
	if err := enableStaticMode(staging, release); err != nil {
		return Result{}, err
	}
	if err := rewriteRootRelativeReferences(staging); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, ".nojekyll"), nil, 0o644); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, "CNAME"), []byte(strings.ToLower(config.Domain)+"\n"), 0o644); err != nil {
		return Result{}, err
	}

	now := time.Now().UTC()
	if config.Now != nil {
		now = config.Now().UTC()
	}
	if err := writeSEOFiles(staging, config.Domain, now); err != nil {
		return Result{}, err
	}
	result := Result{Release: release, BuiltAt: now.Format(time.RFC3339)}
	if err := validateSite(staging); err != nil {
		return Result{}, err
	}
	result.FileCount, result.SizeBytes, err = siteSize(staging)
	if err != nil {
		return Result{}, err
	}
	baseSize := result.SizeBytes
	result.FileCount++
	var manifest []byte
	for range 3 {
		manifest, err = json.MarshalIndent(result, "", "  ")
		if err != nil {
			return Result{}, err
		}
		manifest = append(manifest, '\n')
		updatedSize := baseSize + int64(len(manifest))
		if updatedSize == result.SizeBytes {
			break
		}
		result.SizeBytes = updatedSize
	}
	if err := os.WriteFile(filepath.Join(staging, "site-manifest.json"), manifest, 0o644); err != nil {
		return Result{}, err
	}

	if err := replaceDirectory(staging, outputRoot); err != nil {
		return Result{}, err
	}
	return result, nil
}

func validateConfig(config Config) error {
	for label, value := range map[string]string{
		"base web root": config.BaseWebRoot, "public web root": config.PublicWebRoot,
		"snapshot root": config.SnapshotRoot, "output root": config.OutputRoot,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
	}
	domain := strings.ToLower(strings.TrimSpace(config.Domain))
	if !domainPattern.MatchString(domain) || !strings.Contains(domain, ".") {
		return errors.New("valid publication domain is required")
	}
	outputRoot, err := filepath.Abs(config.OutputRoot)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(outputRoot)
	if outputRoot == filepath.Clean(volume+string(os.PathSeparator)) || filepath.Dir(outputRoot) == outputRoot {
		return errors.New("refusing to use a filesystem root as output")
	}
	return nil
}

func allowWebFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".js", ".css", ".png", ".jpg", ".jpeg", ".webp", ".gif", ".ico", ".svg", ".webmanifest", ".xml", ".txt", ".md":
		return true
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if !strings.Contains(clean, "/source/") {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".mod", ".sum", ".py", ".cs", ".dockerignore":
		return true
	}
	name := filepath.Base(path)
	return name == "Dockerfile" || name == "github-known-hosts"
}

func allowJSONFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}

func allowMediaFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".avif":
		return true
	default:
		return false
	}
}

func copyTree(source, destination string, allowed func(string) bool) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not allowed: %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(destination, 0o755)
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() || !allowed(path) {
			return nil
		}
		return copyFile(path, target)
	})
}

func copyFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func installGitHubPrivacyPage(root string) error {
	source := filepath.Join(root, "privacy-github.html")
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("GitHub privacy page is missing: %w", err)
	}
	if err := copyFile(source, filepath.Join(root, "privacy.html")); err != nil {
		return err
	}
	return os.Remove(source)
}

func copyLiveSnapshot(source, destination string) error {
	if strings.TrimSpace(source) != "" {
		if _, err := os.Stat(source); err == nil {
			if err := copyTree(source, destination, allowJSONFile); err != nil {
				return err
			}
		}
	}
	overview := filepath.Join(destination, "overview.json")
	if _, err := os.Stat(overview); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return err
		}
		return os.WriteFile(overview, []byte("{\"matches\":[],\"status\":\"not_available\"}\n"), 0o644)
	}
	return nil
}

func enableStaticMode(root, release string) error {
	filename := filepath.Join(root, "runtime-config.js")
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open runtime config: %w", err)
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "\nwindow.DOTA_HUB_STATIC_API = true;\nwindow.DOTA_HUB_STATIC_RELEASE = %q;\n", release)
	return err
}

func writeSEOFiles(root, domain string, now time.Time) error {
	host := strings.ToLower(strings.TrimSpace(domain))
	robots := fmt.Sprintf("User-agent: *\nAllow: /\nDisallow: /api/\n\nSitemap: https://%s/sitemap.xml\nHost: %s\n", host, host)
	if err := os.WriteFile(filepath.Join(root, "robots.txt"), []byte(robots), 0o644); err != nil {
		return fmt.Errorf("write robots.txt: %w", err)
	}
	sitemap := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://%s/</loc>
    <lastmod>%s</lastmod>
    <changefreq>daily</changefreq>
    <priority>1.0</priority>
  </url>
</urlset>
`, host, now.Format("2006-01-02"))
	if err := os.WriteFile(filepath.Join(root, "sitemap.xml"), []byte(sitemap), 0o644); err != nil {
		return fmt.Errorf("write sitemap.xml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, indexNowKey+".txt"), []byte(indexNowKey+"\n"), 0o644); err != nil {
		return fmt.Errorf("write IndexNow key: %w", err)
	}
	return nil
}

func rewriteRootRelativeReferences(root string) error {
	replacements := []struct{ old, new []byte }{
		{[]byte(`"/assets/`), []byte(`"assets/`)},
		{[]byte(`'/assets/`), []byte(`'assets/`)},
		{[]byte(`url(/assets/`), []byte(`url(assets/`)},
		{[]byte(`"/media/`), []byte(`"media/`)},
		{[]byte(`'/media/`), []byte(`'media/`)},
		{[]byte(`href="/privacy.html"`), []byte(`href="privacy.html"`)},
		{[]byte(`href="/rights.html`), []byte(`href="rights.html`)},
		{[]byte(`href="/#`), []byte(`href="./#`)},
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".html", ".js", ".css", ".json":
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := data
		for _, replacement := range replacements {
			updated = bytes.ReplaceAll(updated, replacement.old, replacement.new)
		}
		if bytes.Equal(data, updated) {
			return nil
		}
		return os.WriteFile(path, updated, 0o644)
	})
}

func validateSite(root string) error {
	for _, name := range []string{
		"index.html", "runtime-config.js", "privacy.html", "rights.html", "robots.txt", "sitemap.xml",
		"site.webmanifest", indexNowKey + ".txt", "api/health.json",
		"api/heroes.json", "api/teams.json", "api/tournament-players.json",
		"api/player-filter-data.json", "api/live/overview.json",
	} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("required public file is missing: %s", name)
		}
	}
	return filepath.WalkDir(filepath.Join(root, "api"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if !allowJSONFile(path) {
			return fmt.Errorf("unexpected public API file: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("invalid public JSON %s: %w", path, err)
		}
		return nil
	})
}

func siteSize(root string) (int, int64, error) {
	count := 0
	var size int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		count++
		size += info.Size()
		return nil
	})
	return count, size, err
}

func replaceDirectory(staging, output string) error {
	previous := output + ".previous"
	if err := os.RemoveAll(previous); err != nil {
		return err
	}
	hadOutput := false
	if _, err := os.Stat(output); err == nil {
		hadOutput = true
		if err := os.Rename(output, previous); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, output); err != nil {
		if hadOutput {
			_ = os.Rename(previous, output)
		}
		return err
	}
	return os.RemoveAll(previous)
}
