package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxImageBytes = 8 << 20

var imageFields = map[string]bool{
	"imageUrl": true, "logoUrl": true, "portraitUrl": true,
	"teamLogoUrl": true, "opponentLogo": true,
}

type imageMirror struct {
	cacheDir string
	mediaDir string
	client   *http.Client
	logger   *log.Logger
}

func newImageMirror(cacheDir, mediaDir string, logger *log.Logger) *imageMirror {
	client := &http.Client{Timeout: 25 * time.Second}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("too many image redirects")
		}
		if !allowedImageURL(request.URL) {
			return errors.New("image redirect host is not allowed")
		}
		return nil
	}
	return &imageMirror{cacheDir: cacheDir, mediaDir: mediaDir, client: client, logger: logger}
}

func (mirror *imageMirror) mirrorAll(ctx context.Context, roots ...any) (int, int) {
	urls := map[string]struct{}{}
	for _, root := range roots {
		collectImageURLs(root, urls)
	}
	if err := os.MkdirAll(mirror.cacheDir, 0o750); err != nil {
		mirror.logger.Printf("image cache unavailable: %v", err)
		return 0, len(urls)
	}
	if err := os.MkdirAll(mirror.mediaDir, 0o750); err != nil {
		mirror.logger.Printf("snapshot media unavailable: %v", err)
		return 0, len(urls)
	}

	type result struct{ source, target string }
	jobs := make(chan string)
	results := make(chan result, len(urls))
	workers := 8
	if len(urls) < workers {
		workers = len(urls)
	}
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for source := range jobs {
				target, err := mirror.mirrorOne(ctx, source)
				if err != nil {
					mirror.logger.Printf("image skipped host=%s: %v", imageHost(source), err)
					results <- result{source: source}
					continue
				}
				results <- result{source: source, target: target}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for source := range urls {
			select {
			case jobs <- source:
			case <-ctx.Done():
				return
			}
		}
	}()
	wait.Wait()
	close(results)

	replacements := map[string]string{}
	mirrored := 0
	for item := range results {
		replacements[item.source] = item.target
		if item.target != "" {
			mirrored++
		}
	}
	for _, root := range roots {
		rewriteImageURLs(root, replacements)
	}
	return mirrored, len(urls) - mirrored
}

func collectImageURLs(value any, result map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if imageFields[key] {
				if source, ok := child.(string); ok && strings.HasPrefix(source, "https://") {
					result[source] = struct{}{}
				}
				continue
			}
			collectImageURLs(child, result)
		}
	case []any:
		for _, child := range typed {
			collectImageURLs(child, result)
		}
	}
}

func rewriteImageURLs(value any, replacements map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if imageFields[key] {
				source, _ := child.(string)
				if strings.HasPrefix(source, "/media/") || strings.HasPrefix(source, "/assets/") {
					continue
				}
				typed[key] = replacements[source]
				continue
			}
			rewriteImageURLs(child, replacements)
		}
	case []any:
		for _, child := range typed {
			rewriteImageURLs(child, replacements)
		}
	}
}

func (mirror *imageMirror) mirrorOne(ctx context.Context, source string) (string, error) {
	parsed, err := url.Parse(source)
	if err != nil || !allowedImageURL(parsed) {
		return "", errors.New("image URL is not allowed")
	}
	sum := sha256.Sum256([]byte(source))
	key := hex.EncodeToString(sum[:])
	cacheFile, err := mirror.findCached(key)
	if err != nil {
		return "", err
	}
	if cacheFile == "" {
		cacheFile, err = mirror.download(ctx, source, key)
		if err != nil {
			return "", err
		}
	}
	name := filepath.Base(cacheFile)
	if err := copyFile(cacheFile, filepath.Join(mirror.mediaDir, name)); err != nil {
		return "", err
	}
	return "/media/" + name, nil
}

func (mirror *imageMirror) findCached(key string) (string, error) {
	for _, extension := range []string{".png", ".jpg", ".webp", ".gif", ".avif"} {
		filename := filepath.Join(mirror.cacheDir, key+extension)
		info, err := os.Stat(filename)
		if err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= maxImageBytes {
			return filename, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", nil
}

func (mirror *imageMirror) download(ctx context.Context, source, key string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Fantasy-Time-Snapshot/1.0")
	request.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif;q=0.8")
	response, err := mirror.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("image HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxImageBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > maxImageBytes {
		return "", errors.New("image size is invalid")
	}
	extension, err := imageExtension(data)
	if err != nil {
		return "", err
	}
	target := filepath.Join(mirror.cacheDir, key+extension)
	temporary := target + ".tmp"
	if err := os.WriteFile(temporary, data, 0o640); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	return target, nil
}

func allowedImageURL(value *url.URL) bool {
	if value == nil || value.Scheme != "https" || value.User != nil || value.Port() != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(value.Hostname(), "."))
	return host == "liquipedia.net" || strings.HasSuffix(host, ".liquipedia.net") ||
		host == "steamcdn-a.akamaihd.net" || strings.HasSuffix(host, ".steamstatic.com") ||
		strings.HasSuffix(host, ".steamusercontent.com")
}

func imageExtension(data []byte) (string, error) {
	switch http.DetectContentType(data) {
	case "image/png":
		return ".png", nil
	case "image/jpeg":
		return ".jpg", nil
	case "image/gif":
		return ".gif", nil
	case "image/webp":
		return ".webp", nil
	}
	if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return ".webp", nil
	}
	if len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")) && bytes.Contains(data[8:min(len(data), 32)], []byte("avif")) {
		return ".avif", nil
	}
	return "", errors.New("unsupported image format")
}

func copyFile(source, target string) error {
	if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary := target + ".tmp"
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return closeErr
	}
	return os.Rename(temporary, target)
}

func imageHost(source string) string {
	parsed, _ := url.Parse(source)
	if parsed == nil {
		return "invalid"
	}
	return parsed.Hostname()
}
