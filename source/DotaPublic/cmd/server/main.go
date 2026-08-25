package main

import (
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"salfetka-hub/dota-public/internal/publicdata"
)

var (
	mediaNamePattern     = regexp.MustCompile(`^[a-f0-9]{64}\.(png|jpg|webp|gif|avif)$`)
	apiSegmentKeyPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type publicServer struct {
	webRoot      string
	snapshotRoot string
	liveRoot     string
	startedAt    time.Time
	now          func() time.Time
}

func main() {
	listen := envOr("DOTA_PUBLIC_LISTEN", "0.0.0.0:8080")
	server := &http.Server{
		Addr: listen,
		Handler: &publicServer{
			webRoot: envOr("DOTA_PUBLIC_WEB_ROOT", "/app/web"), snapshotRoot: envOr("DOTA_PUBLIC_SNAPSHOT_DIR", "/snapshot"),
			liveRoot:  envOr("DOTA_PUBLIC_LIVE_DIR", "/live"),
			startedAt: time.Now(), now: time.Now,
		},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	log.Printf("dota public server listening on %s", listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func (server *publicServer) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	setSecurityHeaders(response)
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		writeJSONError(response, http.StatusMethodNotAllowed, "read-only public service")
		return
	}
	if request.URL.Path != path.Clean(request.URL.Path) && request.URL.Path != "/" {
		writeJSONError(response, http.StatusNotFound, "not found")
		return
	}

	switch {
	case request.URL.Path == "/healthz":
		server.serveHealth(response, request)
	case strings.HasPrefix(request.URL.Path, "/api/"):
		server.serveAPI(response, request)
	case strings.HasPrefix(request.URL.Path, "/media/"):
		server.serveMedia(response, request)
	default:
		server.serveStatic(response, request)
	}
}

func (server *publicServer) serveHealth(response http.ResponseWriter, request *http.Request) {
	releaseRoot, err := publicdata.ReadReleaseRoot(server.snapshotRoot)
	if err != nil {
		writeJSONError(response, http.StatusServiceUnavailable, "snapshot unavailable")
		return
	}
	if _, err := os.Stat(filepath.Join(releaseRoot, "api", "health.json")); err != nil {
		writeJSONError(response, http.StatusServiceUnavailable, "snapshot unavailable")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodGet {
		_, _ = response.Write([]byte(`{"status":"Healthy"}`))
	}
}

func (server *publicServer) serveAPI(response http.ResponseWriter, request *http.Request) {
	if relative, ok := liveAPIFile(request.URL.Path); ok {
		serveFile(response, request, filepath.Join(server.liveRoot, relative), "application/json; charset=utf-8", "no-store")
		return
	}
	relative, ok := apiFile(request.URL.Path)
	if !ok {
		writeJSONError(response, http.StatusNotFound, "public endpoint not found")
		return
	}
	releaseRoot, err := publicdata.ReadReleaseRoot(server.snapshotRoot)
	if err != nil {
		writeJSONError(response, http.StatusServiceUnavailable, "snapshot unavailable")
		return
	}
	if relative == filepath.Join("api", "health.json") {
		server.servePublicHealth(response, request, filepath.Join(releaseRoot, relative))
		return
	}
	cache := "public, max-age=30, stale-if-error=300"
	serveFile(response, request, filepath.Join(releaseRoot, relative), "application/json; charset=utf-8", cache)
}

func liveAPIFile(requestPath string) (string, bool) {
	if requestPath == "/api/live/overview" || requestPath == "/api/live/overview.json" {
		return "overview.json", true
	}
	parts := strings.Split(strings.Trim(requestPath, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "live" || parts[2] != "matches" {
		return "", false
	}
	matchID := strings.TrimSuffix(parts[3], ".json")
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`).MatchString(matchID) {
		return "", false
	}
	return filepath.Join("matches", matchID+".json"), true
}

func (server *publicServer) servePublicHealth(response http.ResponseWriter, request *http.Request, filename string) {
	raw, err := os.ReadFile(filename)
	if err != nil || len(raw) > 1<<20 {
		writeJSONError(response, http.StatusServiceUnavailable, "snapshot unavailable")
		return
	}
	var health map[string]any
	if err := json.Unmarshal(raw, &health); err != nil {
		writeJSONError(response, http.StatusServiceUnavailable, "snapshot unavailable")
		return
	}
	now := time.Now()
	if server.now != nil {
		now = server.now()
	}
	startedAt := server.startedAt
	if startedAt.IsZero() {
		startedAt = now
	}
	health["uptimeSeconds"] = max(0, int(now.Sub(startedAt).Seconds()))
	generatedAt, _ := time.Parse(time.RFC3339, fmt.Sprint(health["generatedAt"]))
	age := max(0, int(now.Sub(generatedAt).Seconds()))
	details, _ := health["details"].(map[string]any)
	if details == nil {
		details = map[string]any{}
	}
	details["snapshotAgeSeconds"] = age
	health["details"] = details
	if !generatedAt.IsZero() && now.Sub(generatedAt) > 10*time.Minute {
		health["status"] = "Degraded"
		health["lastError"] = "Публичный снимок обновляется с задержкой"
		health["lastErrorAt"] = generatedAt.Format(time.RFC3339)
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	if request.Method == http.MethodHead {
		response.WriteHeader(http.StatusOK)
		return
	}
	_ = json.NewEncoder(response).Encode(health)
}

func apiFile(requestPath string) (string, bool) {
	parts := strings.Split(strings.Trim(requestPath, "/"), "/")
	if len(parts) == 2 && parts[0] == "api" {
		name := strings.TrimSuffix(parts[1], ".json")
		switch name {
		case "health", "heroes", "teams", "tournament-players", "player-filter-data":
			return filepath.Join("api", name+".json"), true
		}
	}
	if len(parts) != 3 || parts[0] != "api" || parts[2] == "" {
		return "", false
	}
	segment := strings.TrimSuffix(parts[2], ".json")
	if segment == "" || len(segment) > 160 {
		return "", false
	}
	switch parts[1] {
	case "teams", "tournament-players":
		key := segment
		if !apiSegmentKeyPattern.MatchString(key) {
			key = publicdata.SegmentKey(segment)
		}
		return filepath.Join("api", parts[1], key+".json"), true
	case "matches":
		if _, err := strconv.ParseUint(segment, 10, 64); err != nil {
			return "", false
		}
		return filepath.Join("api", "matches", segment+".json"), true
	default:
		return "", false
	}
}

func (server *publicServer) serveMedia(response http.ResponseWriter, request *http.Request) {
	name := strings.TrimPrefix(request.URL.Path, "/media/")
	if !mediaNamePattern.MatchString(name) {
		writeJSONError(response, http.StatusNotFound, "not found")
		return
	}
	releaseRoot, err := publicdata.ReadReleaseRoot(server.snapshotRoot)
	if err != nil {
		writeJSONError(response, http.StatusServiceUnavailable, "snapshot unavailable")
		return
	}
	serveFile(response, request, filepath.Join(releaseRoot, "media", name), mime.TypeByExtension(filepath.Ext(name)), "public, max-age=31536000, immutable")
}

func (server *publicServer) serveStatic(response http.ResponseWriter, request *http.Request) {
	name := strings.TrimPrefix(request.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		writeJSONError(response, http.StatusNotFound, "not found")
		return
	}
	for _, segment := range strings.Split(filepath.ToSlash(clean), "/") {
		if strings.HasPrefix(segment, ".") {
			writeJSONError(response, http.StatusNotFound, "not found")
			return
		}
	}
	extension := strings.ToLower(filepath.Ext(clean))
	allowed := map[string]bool{
		".html": true, ".js": true, ".css": true, ".png": true, ".jpg": true,
		".jpeg": true, ".webp": true, ".gif": true, ".ico": true, ".svg": true,
	}
	if !allowed[extension] {
		writeJSONError(response, http.StatusNotFound, "not found")
		return
	}
	cache := "public, max-age=86400"
	if clean == "index.html" || clean == "runtime-config.js" || extension == ".html" {
		cache = "no-store"
	} else if extension != ".js" && extension != ".css" {
		cache = "public, max-age=2592000, stale-while-revalidate=86400"
	}
	contentType := mime.TypeByExtension(extension)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	serveFile(response, request, filepath.Join(server.webRoot, clean), contentType, cache)
}

func serveFile(response http.ResponseWriter, request *http.Request, filename, contentType, cacheControl string) {
	file, err := os.Open(filename)
	if err != nil {
		writeJSONError(response, http.StatusNotFound, "not found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeJSONError(response, http.StatusNotFound, "not found")
		return
	}
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Cache-Control", cacheControl)
	http.ServeContent(response, request, info.Name(), info.ModTime(), file)
}

func setSecurityHeaders(response http.ResponseWriter) {
	response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'none'")
	response.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	response.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("X-Frame-Options", "DENY")
}

func writeJSONError(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"error": message})
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func (server *publicServer) String() string {
	return fmt.Sprintf("web=%s snapshot=%s live=%s", server.webRoot, server.snapshotRoot, server.liveRoot)
}
