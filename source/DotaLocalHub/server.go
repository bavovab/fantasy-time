package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	root       string
	store      *Store
	jobs       *JobManager
	downloader *Downloader
	config     Config
	startedAt  time.Time
	gcMonitor  *GCMonitor
}

const (
	dailyUserMatchLimit  = 3
	dailyUserLookupLimit = 20
)

func (server *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", server.health)
	mux.HandleFunc("GET /api/gc-monitor", server.gcMonitorStatus)
	mux.HandleFunc("GET /api/heroes", server.heroes)
	mux.HandleFunc("GET /api/matches", server.listMatches)
	mux.HandleFunc("GET /api/matches/retries", server.listMatchRetries)
	mux.HandleFunc("POST /api/matches/retries/{matchID}/stop", server.stopMatchRetry)
	mux.HandleFunc("GET /api/matches/{matchID}", server.getMatch)
	mux.HandleFunc("POST /api/matches/parse", server.startParse)
	mux.HandleFunc("POST /api/replays/upload", server.uploadReplay)
	mux.HandleFunc("GET /api/teams", server.listTeams)
	mux.HandleFunc("GET /api/teams/{slug}", server.getTeam)
	mux.HandleFunc("POST /api/tournament/reload", server.reloadTournament)
	mux.HandleFunc("GET /api/tournament-players", server.listTournamentPlayers)
	mux.HandleFunc("GET /api/player-filter-data", server.tournamentPlayerFilterData)
	mux.HandleFunc("GET /api/tournament-players/{alias}", server.getTournamentPlayer)
	mux.HandleFunc("POST /api/teams/{slug}/sync", server.syncTeam)
	mux.HandleFunc("POST /api/teams/sync-all", server.syncAllTeams)
	mux.HandleFunc("POST /api/teams/{slug}/selection", server.updateTeamSelection)
	mux.HandleFunc("GET /api/selection/global", server.globalSelectionOverview)
	mux.HandleFunc("POST /api/selection/global", server.updateGlobalSelection)
	mux.HandleFunc("GET /api/jobs/{jobID}", server.getJob)
	mux.HandleFunc("GET /api/jobs/active", server.activeJob)
	mux.HandleFunc("GET /api/jobs/parse-queue", server.parseQueue)

	webDirectory := filepath.Join(server.root, "web")
	fileServer := http.FileServer(http.Dir(webDirectory))
	mux.Handle("/", noCache(fileServer))
	return server.middleware(mux)
}

func (server *Server) heroes(response http.ResponseWriter, request *http.Request) {
	heroes, err := server.downloader.HeroNames(request.Context())
	if err != nil {
		writeError(response, http.StatusBadGateway, err)
		return
	}
	writeJSON(response, http.StatusOK, heroes)
}

func (server *Server) health(response http.ResponseWriter, request *http.Request) {
	now := time.Now().UTC()
	version := strings.TrimSpace(os.Getenv("DOTA_HUB_VERSION"))
	if version == "" {
		version = "1.0.0"
	}
	imageTag := strings.TrimSpace(os.Getenv("DOTA_HUB_IMAGE_TAG"))
	if imageTag == "" {
		imageTag = "latest"
	}
	buildDate := server.startedAt
	if value := strings.TrimSpace(os.Getenv("DOTA_HUB_BUILD_DATE")); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			buildDate = parsed
		}
	}
	details := map[string]any{
		"websiteAvailable": true,
	}
	if server.gcMonitor != nil {
		details["gcMonitoring"] = server.gcMonitor.Snapshot()
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"ok":                        true,
		"service":                   "dota-local-hub",
		"displayName":               "Dota Local Hub",
		"status":                    "Healthy",
		"processRunning":            true,
		"startedAt":                 server.startedAt,
		"uptimeSeconds":             int64(now.Sub(server.startedAt).Seconds()),
		"lastSuccessfulOperationAt": now,
		"lastErrorAt":               nil,
		"lastError":                 nil,
		"version": map[string]any{
			"version":        version,
			"buildDate":      buildDate,
			"apiVersion":     "1",
			"schemaVersion":  "1",
			"commitHash":     nil,
			"dockerImageTag": imageTag,
		},
		"details": details,
		"at":      now,
		"time":    now,
	})
}

func (server *Server) gcMonitorStatus(response http.ResponseWriter, request *http.Request) {
	if server.gcMonitor == nil {
		writeJSON(response, http.StatusOK, GCMonitorSnapshot{Enabled: false, State: "disabled", Priority: "gc-first"})
		return
	}
	writeJSON(response, http.StatusOK, server.gcMonitor.Snapshot())
}

func (server *Server) listMatches(response http.ResponseWriter, request *http.Request) {
	matches, err := server.store.ListMatches(request.Context(), userIDFromContext(request.Context()))
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	writeJSON(response, http.StatusOK, matches)
}

func (server *Server) listMatchRetries(response http.ResponseWriter, request *http.Request) {
	retries, err := server.store.ListParseRetries(request.Context(), userIDFromContext(request.Context()))
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	writeJSON(response, http.StatusOK, retries)
}

func (server *Server) stopMatchRetry(response http.ResponseWriter, request *http.Request) {
	matchID, err := strconv.ParseUint(request.PathValue("matchID"), 10, 64)
	if err != nil || matchID == 0 {
		writeError(response, http.StatusBadRequest, errors.New("некорректный match ID"))
		return
	}
	stopped, err := server.store.StopParseRetry(request.Context(), userIDFromContext(request.Context()), matchID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	if !stopped {
		writeError(response, http.StatusNotFound, errors.New("отложенный матч не найден"))
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"ok":      true,
		"matchId": matchID,
		"status":  "stopped",
	})
}

func (server *Server) getMatch(response http.ResponseWriter, request *http.Request) {
	matchID, err := strconv.ParseUint(request.PathValue("matchID"), 10, 64)
	if err != nil || matchID == 0 {
		writeError(response, http.StatusBadRequest, errors.New("некорректный match ID"))
		return
	}
	allowed, err := server.store.UserCanAccessMatch(request.Context(), userIDFromContext(request.Context()), matchID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	if !allowed {
		writeError(response, http.StatusNotFound, errors.New("матч не найден в вашей истории"))
		return
	}

	match, err := server.store.GetMatch(request.Context(), matchID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(response, http.StatusNotFound, errors.New("матч не найден в локальной базе"))
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	for index := range match.Players {
		player := &match.Players[index]
		player.Name = repairPlayerName(player.Name)
		if playerNameLooksBroken(player.Name) && player.AccountID != 0 {
			profileContext, cancel := context.WithTimeout(request.Context(), 5*time.Second)
			profile, profileErr := server.downloader.PlayerProfile(profileContext, player.AccountID)
			cancel()
			if profileErr == nil && profile.PersonaName != "" {
				player.Name = repairPlayerName(profile.PersonaName)
				_ = server.store.UpdatePlayerName(request.Context(), player.AccountID, player.Name)
			}
		}
		if playerNameLooksBroken(player.Name) {
			if player.ProName != "" {
				player.Name = player.ProName
			} else {
				player.Name = "Игрок"
			}
		}
		player.FantasyPoints = fantasyPointsForPlayer(*player)
	}
	writeJSON(response, http.StatusOK, match)
}

func (server *Server) startParse(response http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	userID := userIDFromContext(request.Context())
	var input ParseRequest
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, errors.New("ожидался JSON с matchId"))
		return
	}

	matchID, err := strconv.ParseUint(strings.TrimSpace(input.MatchID), 10, 64)
	if err != nil || matchID == 0 {
		writeError(response, http.StatusBadRequest, errors.New("match ID должен состоять из цифр"))
		return
	}

	hasMatch, err := server.store.UserHasMatch(request.Context(), userID, matchID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	if hasMatch {
		job := server.jobs.CompletedUserMatch(userID, matchID, "match", "Матч уже есть в вашей истории")
		writeJSON(response, http.StatusAccepted, job)
		return
	}

	exists, err := server.store.MatchExists(request.Context(), matchID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	if exists {
		if err := server.store.LinkUserMatch(request.Context(), userID, matchID, "existing-db"); err != nil {
			writeError(response, http.StatusInternalServerError, err)
			return
		}
		job := server.jobs.CompletedUserMatch(userID, matchID, "match", "Матч уже был в базе и добавлен в вашу историю")
		writeJSON(response, http.StatusAccepted, job)
		return
	}

	if active, ok := server.jobs.ActiveUserMatch(userID, matchID); ok {
		writeJSON(response, http.StatusAccepted, active)
		return
	}
	if server.jobs.UserPendingParseJobs(userID) >= maxUserPendingParseJobs {
		writeError(response, http.StatusTooManyRequests, fmt.Errorf("в вашей очереди уже %d задачи; дождитесь завершения одной из них", maxUserPendingParseJobs))
		return
	}
	if err := server.store.ReserveDailyMatchSlot(request.Context(), userID, dailyUserMatchLimit); err != nil {
		writeError(response, http.StatusTooManyRequests, err)
		return
	}
	if err := server.store.ConsumeDailyLookupAttempt(request.Context(), userID, dailyUserLookupLimit); err != nil {
		_ = server.store.ReleaseDailyMatchSlot(request.Context(), userID)
		writeError(response, http.StatusTooManyRequests, err)
		return
	}

	job := server.jobs.StartUserMatch(userID, matchID, true)
	writeJSON(response, http.StatusAccepted, job)
}

func (server *Server) uploadReplay(response http.ResponseWriter, request *http.Request) {
	userID := userIDFromContext(request.Context())
	request.Body = http.MaxBytesReader(response, request.Body, server.maxUploadBytes())
	if err := request.ParseMultipartForm(16 << 20); err != nil {
		writeError(response, http.StatusBadRequest, fmt.Errorf("не удалось прочитать replay: файл слишком большой или повреждён"))
		return
	}
	file, header, err := request.FormFile("replay")
	if err != nil {
		writeError(response, http.StatusBadRequest, errors.New("перетащи .dem или .dem.bz2 файл"))
		return
	}
	defer file.Close()

	originalName, extension, filenameMatchID, err := validateReplayUploadFilename(header.Filename)
	if err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}

	var matchID uint64
	if value := strings.TrimSpace(request.FormValue("matchId")); value != "" {
		parsed, parseErr := strconv.ParseUint(value, 10, 64)
		if parseErr != nil || parsed == 0 {
			writeError(response, http.StatusBadRequest, errors.New("match ID должен состоять из цифр"))
			return
		}
		matchID = parsed
	}
	if matchID != 0 && matchID != filenameMatchID {
		writeError(response, http.StatusBadRequest, errors.New("match ID в поле и в имени replay не совпадают"))
		return
	}
	matchID = filenameMatchID

	hasMatch, err := server.store.UserHasMatch(request.Context(), userID, matchID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	if hasMatch {
		job := server.jobs.CompletedUserMatch(userID, matchID, "local-replay", "Матч уже есть в вашей истории")
		writeJSON(response, http.StatusAccepted, job)
		return
	}
	exists, err := server.store.MatchExists(request.Context(), matchID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	if exists {
		if err := server.store.LinkUserMatch(request.Context(), userID, matchID, "existing-db"); err != nil {
			writeError(response, http.StatusInternalServerError, err)
			return
		}
		job := server.jobs.CompletedUserMatch(userID, matchID, "local-replay", "Матч уже был в базе и добавлен в вашу историю")
		writeJSON(response, http.StatusAccepted, job)
		return
	}
	if server.jobs.UserPendingParseJobs(userID) >= maxUserPendingParseJobs {
		writeError(response, http.StatusTooManyRequests, fmt.Errorf("в вашей очереди уже %d задачи; дождитесь завершения одной из них", maxUserPendingParseJobs))
		return
	}
	if err := server.store.ReserveDailyMatchSlot(request.Context(), userID, dailyUserMatchLimit); err != nil {
		writeError(response, http.StatusTooManyRequests, err)
		return
	}

	uploadDirectory := filepath.Join(server.root, "data", "uploads")
	if err := os.MkdirAll(uploadDirectory, 0755); err != nil {
		_ = server.store.ReleaseDailyMatchSlot(request.Context(), userID)
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	temporary, err := os.CreateTemp(uploadDirectory, "local-replay-*"+extension)
	if err != nil {
		_ = server.store.ReleaseDailyMatchSlot(request.Context(), userID)
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	temporaryPath := temporary.Name()
	if _, err := io.Copy(temporary, file); err != nil {
		temporary.Close()
		_ = os.Remove(temporaryPath)
		_ = server.store.ReleaseDailyMatchSlot(request.Context(), userID)
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		_ = server.store.ReleaseDailyMatchSlot(request.Context(), userID)
		writeError(response, http.StatusInternalServerError, err)
		return
	}

	job := server.jobs.StartUserLocalReplay(userID, temporaryPath, originalName, matchID, true)
	writeJSON(response, http.StatusAccepted, job)
}

func (server *Server) getJob(response http.ResponseWriter, request *http.Request) {
	job, exists := server.jobs.Get(request.PathValue("jobID"))
	if !exists {
		writeError(response, http.StatusNotFound, errors.New("задача не найдена"))
		return
	}
	if !server.jobs.UserCanAccessJob(job.ID, userIDFromContext(request.Context())) {
		writeError(response, http.StatusNotFound, errors.New("задача не найдена"))
		return
	}
	writeJSON(response, http.StatusOK, job)
}

func (server *Server) activeJob(response http.ResponseWriter, request *http.Request) {
	slug := strings.TrimSpace(request.URL.Query().Get("teamSlug"))
	var job Job
	var exists bool
	if slug != "" {
		job, exists = server.jobs.ActiveTeam(slug)
	} else {
		job, exists = server.jobs.ActiveAllTeams()
	}
	if !exists {
		writeJSON(response, http.StatusOK, map[string]any{"active": false})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"active": true, "job": job})
}

func (server *Server) parseQueue(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, server.jobs.ParseQueue(userIDFromContext(request.Context())))
}

func (server *Server) listTeams(response http.ResponseWriter, request *http.Request) {
	teams, err := server.store.ListTeams(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	writeJSON(response, http.StatusOK, teams)
}

func (server *Server) getTeam(response http.ResponseWriter, request *http.Request) {
	slug := strings.TrimSpace(request.PathValue("slug"))
	team, err := server.store.GetTeam(request.Context(), slug)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(response, http.StatusNotFound, errors.New("команда не найдена"))
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	matches, err := server.store.ListTeamMatches(request.Context(), slug)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	if matches == nil {
		matches = make([]TeamMatchRecord, 0)
	}
	team.MatchCount = len(matches)
	for _, match := range matches {
		if match.ParseStatus == "done" {
			team.ParsedCount++
		}
	}
	writeJSON(response, http.StatusOK, TeamDetail{Team: team, Matches: matches})
}

func (server *Server) syncTeam(response http.ResponseWriter, request *http.Request) {
	slug := strings.TrimSpace(request.PathValue("slug"))
	team, err := server.store.GetTeam(request.Context(), slug)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(response, http.StatusNotFound, errors.New("команда не найдена"))
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	if team.Status == "tbd" {
		writeError(response, http.StatusConflict, errors.New("слот команды пока не определён"))
		return
	}
	job := server.jobs.StartTeam(slug)
	writeJSON(response, http.StatusAccepted, job)
}

func (server *Server) syncAllTeams(response http.ResponseWriter, request *http.Request) {
	slugs, err := server.store.SyncableTeamSlugs(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	job := server.jobs.StartAllTeams(slugs)
	writeJSON(response, http.StatusAccepted, job)
}

func (server *Server) reloadTournament(response http.ResponseWriter, request *http.Request) {
	tournament, err := loadTournamentConfig(server.root)
	if err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	if err := server.store.seedTournament(tournament); err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	teams, err := server.store.ListTeams(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"ok":         true,
		"tournament": tournament.Name,
		"teams":      teams,
	})
}

func (server *Server) updateTeamSelection(response http.ResponseWriter, request *http.Request) {
	slug := strings.TrimSpace(request.PathValue("slug"))
	defer request.Body.Close()
	var input SelectionRequest
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, errors.New("некорректные параметры выбора матчей"))
		return
	}
	var err error
	switch {
	case len(input.LeagueNames) > 0:
		err = server.store.ApplyFilteredSelection(request.Context(), slug, input.LeagueNames, input.Limit, input.All)
	case len(input.Matches) > 0:
		err = server.store.SetMatchesIncluded(request.Context(), slug, input.Matches)
	case input.Mode != "":
		err = server.store.ApplySelectionMode(request.Context(), slug, input.Mode)
	case input.LeagueName != "":
		err = server.store.SetLeagueIncluded(request.Context(), slug, input.LeagueName, input.Included)
	case input.MatchID != 0:
		err = server.store.SetMatchIncluded(request.Context(), slug, input.MatchID, input.Included)
	default:
		err = errors.New("не указан матч, турнир или режим")
	}
	if err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (server *Server) globalSelectionOverview(response http.ResponseWriter, request *http.Request) {
	overview, err := server.store.GlobalSelectionOverview(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	writeJSON(response, http.StatusOK, overview)
}

func (server *Server) updateGlobalSelection(response http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var input SelectionRequest
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, errors.New("некорректные параметры глобального фильтра"))
		return
	}
	if len(input.LeagueNames) == 0 {
		writeError(response, http.StatusBadRequest, errors.New("выбери хотя бы один турнир"))
		return
	}
	if err := server.store.ApplyGlobalFilteredSelection(request.Context(), input.LeagueNames, input.Limit, input.All); err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (server *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: http: https:; connect-src 'self' http://127.0.0.1:* http://localhost:*; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")

		origin := request.Header.Get("Origin")
		if server.allowedOrigin(origin) {
			response.Header().Set("Access-Control-Allow-Origin", origin)
			response.Header().Set("Vary", "Origin")
			response.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			response.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			response.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}

		if strings.HasPrefix(request.URL.Path, "/api/") && !isServiceStatusEndpoint(request.URL.Path) {
			var err error
			request, err = server.ensureAnonymousSession(response, request)
			if err != nil {
				writeError(response, http.StatusInternalServerError, err)
				return
			}
		}

		started := time.Now()
		next.ServeHTTP(response, request)
		if strings.HasPrefix(request.URL.Path, "/api/") && !isServiceStatusEndpoint(request.URL.Path) {
			log.Printf("%s %s %s", request.Method, request.URL.Path, time.Since(started).Round(time.Millisecond))
		}
	})
}

func isServiceStatusEndpoint(path string) bool {
	return path == "/api/health" || path == "/api/gc-monitor"
}

func (server *Server) allowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range server.config.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := parsed.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(response, request)
	})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		log.Printf("write JSON: %v", err)
	}
}

func writeError(response http.ResponseWriter, status int, err error) {
	if status >= http.StatusInternalServerError {
		log.Printf("internal error: %v", err)
		writeJSON(response, status, map[string]string{"error": "Внутренняя ошибка приложения. Подробности записаны в локальный лог."})
		return
	}
	writeJSON(response, status, map[string]string{"error": err.Error()})
}

func listenURL(address string) string {
	host := address
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	return fmt.Sprintf("http://%s", host)
}

func (server *Server) maxUploadBytes() int64 {
	if server.config.MaxUploadBytes > 0 {
		return server.config.MaxUploadBytes
	}
	return defaultMaxUploadBytes
}

func replayUploadExtension(name string) (string, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".dem.bz2"):
		return ".bz2", true
	case strings.HasSuffix(lower, ".dem"):
		return ".dem", true
	default:
		return "", false
	}
}

func validateReplayUploadFilename(name string) (string, string, uint64, error) {
	original := strings.TrimSpace(name)
	if original == "" || original == "." {
		return "", "", 0, errors.New("имя replay-файла должно быть match ID: например 8871520485.dem")
	}
	if strings.ContainsAny(original, `/\`) || filepath.Base(original) != original {
		return "", "", 0, errors.New("имя replay-файла не должно содержать путь")
	}
	extension, ok := replayUploadExtension(original)
	if !ok {
		return "", "", 0, errors.New("поддерживаются только имена вида 8871520485.dem или 8871520485.dem.bz2")
	}
	stem := original
	lower := strings.ToLower(original)
	if strings.HasSuffix(lower, ".dem.bz2") {
		stem = original[:len(original)-len(".dem.bz2")]
	} else {
		stem = strings.TrimSuffix(original, filepath.Ext(original))
	}
	if len(stem) < 8 || len(stem) > 12 {
		return "", "", 0, errors.New("имя replay-файла должно быть match ID: например 8871520485.dem")
	}
	for _, char := range stem {
		if char < '0' || char > '9' {
			return "", "", 0, errors.New("имя replay-файла должно быть только match ID: например 8871520485.dem")
		}
	}
	matchID, err := strconv.ParseUint(stem, 10, 64)
	if err != nil || matchID == 0 {
		return "", "", 0, errors.New("имя replay-файла содержит некорректный match ID")
	}
	return original, extension, matchID, nil
}
