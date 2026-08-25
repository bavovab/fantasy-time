package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxRequestBytes = 1 << 20

var observerIDPattern = regexp.MustCompile(`^[1-9][0-9]?$`)
var matchIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var teamKeyPattern = regexp.MustCompile(`[^a-z0-9]+`)

type config struct {
	TournamentID   string                  `json:"tournamentId"`
	TournamentName string                  `json:"tournamentName"`
	LeagueID       uint64                  `json:"leagueId"`
	MaxObservers   int                     `json:"maxObservers"`
	AllowedTeams   []string                `json:"allowedTeams,omitempty"`
	Roster         map[string]rosterPlayer `json:"roster,omitempty"`
	Format         tournamentFormat        `json:"format"`
}

type candidatePlayer struct {
	AccountID uint32 `json:"accountId"`
	HeroID    int32  `json:"heroId"`
	Team      uint32 `json:"team"`
	TeamSlot  uint32 `json:"teamSlot"`
}

type candidate struct {
	MatchID         uint64            `json:"matchId"`
	LobbyID         uint64            `json:"lobbyId"`
	LeagueID        uint64            `json:"leagueId"`
	SeriesID        uint32            `json:"seriesId,omitempty"`
	TeamRadiant     string            `json:"teamRadiant"`
	TeamDire        string            `json:"teamDire"`
	GameTime        int64             `json:"gameTimeSeconds"`
	Delay           int64             `json:"delaySeconds"`
	Spectators      uint32            `json:"spectators"`
	RadiantScore    uint32            `json:"radiantScore"`
	DireScore       uint32            `json:"direScore"`
	RadiantGoldLead int32             `json:"radiantGoldLead"`
	Players         []candidatePlayer `json:"players,omitempty"`
}

type candidatesRequest struct {
	Candidates []candidate `json:"candidates"`
}

type assignment struct {
	MatchID  uint64 `json:"matchId"`
	LobbyID  uint64 `json:"lobbyId"`
	PublicID string `json:"publicId"`
}

type assignmentResponse struct {
	Enabled    bool        `json:"enabled"`
	Reason     string      `json:"reason,omitempty"`
	Assignment *assignment `json:"assignment,omitempty"`
}

type liveSnapshot struct {
	MatchID   uint64          `json:"matchId"`
	Status    string          `json:"status"`
	UpdatedAt string          `json:"updatedAt"`
	GameTime  int64           `json:"gameTimeSeconds,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type publicMatch struct {
	ID              string      `json:"id"`
	MatchID         uint64      `json:"matchId,omitempty"`
	SeriesID        uint32      `json:"seriesId,omitempty"`
	Radiant         team        `json:"radiant"`
	Dire            team        `json:"dire"`
	State           string      `json:"state"`
	Stage           string      `json:"stage,omitempty"`
	Label           string      `json:"label,omitempty"`
	ScheduledAt     string      `json:"scheduledAt,omitempty"`
	BestOf          int         `json:"bestOf,omitempty"`
	Interesting     bool        `json:"interesting,omitempty"`
	InterestReason  string      `json:"interestReason,omitempty"`
	GameTimeSeconds int64       `json:"gameTimeSeconds,omitempty"`
	DelaySeconds    int64       `json:"delaySeconds,omitempty"`
	Spectators      uint32      `json:"spectators,omitempty"`
	Score           publicScore `json:"score"`
	ObserverID      string      `json:"-"`
}

type team struct {
	Name string `json:"name"`
}

type publicOverview struct {
	SchemaVersion int `json:"schemaVersion"`
	Tournament    struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		TestMode bool   `json:"testMode"`
		Notice   string `json:"notice"`
	} `json:"tournament"`
	UpdatedAt string        `json:"updatedAt"`
	Matches   []publicMatch `json:"matches"`
	Groups    []publicGroup `json:"groups"`
	Bracket   struct {
		Rounds []bracketRound `json:"rounds"`
	} `json:"bracket"`
	Tiebreakers publicTiebreakers `json:"tiebreakers"`
}

type bracketRound struct {
	Name    string        `json:"name"`
	Matches []publicMatch `json:"matches"`
}

type publicGroup struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Teams   []string      `json:"teams"`
	Matches []publicMatch `json:"matches"`
}

type publicTiebreakers struct {
	Rules   []string      `json:"rules"`
	Matches []publicMatch `json:"matches"`
}

type coordinator struct {
	mu                   sync.Mutex
	config               config
	token                []byte
	publicDir            string
	candidatesByObserver map[string][]candidate
	assignments          map[string]assignment
	matchOwners          map[uint64]string
	stateByMatch         map[uint64]string
	telemetryByMatch     map[uint64]*telemetryState
}

func main() {
	check := flag.Bool("healthcheck", false, "validate runtime configuration")
	addr := flag.String("listen", envOr("DOTA_LIVE_LISTEN", ":8791"), "HTTP listen address")
	configPath := flag.String("config", envOr("DOTA_LIVE_CONFIG", "/config/essence2-test.json"), "live tournament config")
	tokenPath := flag.String("token-file", envOr("DOTA_LIVE_TOKEN_FILE", "/run/secrets/dota_live_token"), "internal API token")
	publicDir := flag.String("public-dir", envOr("DOTA_LIVE_PUBLIC_DIR", "/public"), "public snapshot directory")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	token, err := os.ReadFile(*tokenPath)
	if err != nil {
		log.Fatalf("read live coordinator token: %v", err)
	}
	token = []byte(strings.TrimSpace(string(token)))
	if len(token) < 24 {
		log.Fatal("live coordinator token is missing or too short")
	}
	if *check {
		return
	}
	if err := os.MkdirAll(filepath.Join(*publicDir, "matches"), 0o755); err != nil {
		log.Fatalf("create public snapshot directory: %v", err)
	}
	c := &coordinator{
		config: cfg, token: token, publicDir: *publicDir,
		candidatesByObserver: make(map[string][]candidate), assignments: make(map[string]assignment), matchOwners: make(map[uint64]string), stateByMatch: make(map[uint64]string),
		telemetryByMatch: make(map[uint64]*telemetryState),
	}
	if err := c.publishOverviewLocked(); err != nil {
		log.Fatalf("write initial live overview: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", c.health)
	mux.HandleFunc("POST /api/observers/", c.observerAPI)
	mux.HandleFunc("POST /api/gsi/", c.gsiAPI)
	server := &http.Server{Addr: *addr, Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 45 * time.Second}
	log.Printf("live coordinator listening on %s; league=%d enabled=%t", *addr, cfg.LeagueID, cfg.LeagueID != 0)
	log.Fatal(server.ListenAndServe())
}

func (c *coordinator) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "enabled": c.config.LeagueID != 0, "leagueId": c.config.LeagueID,
		"tournamentId": c.config.TournamentID, "trackedTeams": len(c.config.AllowedTeams),
	})
}

func (c *coordinator) observerAPI(w http.ResponseWriter, r *http.Request) {
	if !c.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/observers/"), "/")
	if len(parts) != 2 || !observerIDPattern.MatchString(parts[0]) || (parts[1] != "candidates" && parts[1] != "snapshot") {
		http.NotFound(w, r)
		return
	}
	if parts[1] == "candidates" {
		c.candidates(w, r, parts[0])
		return
	}
	c.snapshot(w, r, parts[0])
}

func (c *coordinator) candidates(w http.ResponseWriter, r *http.Request, observerID string) {
	var input candidatesRequest
	if err := decodeJSON(r, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(input.Candidates) > 100 {
		http.Error(w, "too many candidates", http.StatusBadRequest)
		return
	}
	filtered := make([]candidate, 0, len(input.Candidates))
	seen := make(map[uint64]bool)
	for _, item := range input.Candidates {
		if item.MatchID == 0 || item.LeagueID != c.config.LeagueID || seen[item.MatchID] {
			continue
		}
		seen[item.MatchID] = true
		item.TeamRadiant = limitedText(item.TeamRadiant, 80)
		item.TeamDire = limitedText(item.TeamDire, 80)
		if !c.candidateAllowed(item) {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].MatchID < filtered[j].MatchID })

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config.LeagueID == 0 {
		writeJSON(w, assignmentResponse{Enabled: false, Reason: "league_id_required"})
		return
	}
	c.candidatesByObserver[observerID] = filtered
	c.reconcileLocked()
	response := assignmentResponse{Enabled: true}
	if item, ok := c.assignments[observerID]; ok {
		copy := item
		response.Assignment = &copy
	}
	if err := c.publishOverviewLocked(); err != nil {
		http.Error(w, "publish overview failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func (c *coordinator) snapshot(w http.ResponseWriter, r *http.Request, observerID string) {
	var input liveSnapshot
	if err := decodeJSON(r, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if input.MatchID == 0 || !json.Valid(input.Payload) || len(input.Payload) == 0 {
		http.Error(w, "invalid snapshot", http.StatusBadRequest)
		return
	}
	if input.Status != "live" && input.Status != "not_started" && input.Status != "dota_tv_unavailable" && input.Status != "finished" {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	assigned, ok := c.assignments[observerID]
	if !ok || assigned.MatchID != input.MatchID {
		http.Error(w, "match is not assigned to observer", http.StatusConflict)
		return
	}
	if input.UpdatedAt == "" {
		input.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	var public map[string]any
	if err := json.Unmarshal(input.Payload, &public); err != nil {
		http.Error(w, "invalid snapshot payload", http.StatusBadRequest)
		return
	}
	public["id"] = assigned.PublicID
	public["matchId"] = input.MatchID
	public["state"] = input.Status
	public["updatedAt"] = input.UpdatedAt
	if input.GameTime != 0 {
		public["gameTimeSeconds"] = input.GameTime
	}
	data, err := json.Marshal(public)
	if err != nil {
		http.Error(w, "encode snapshot", http.StatusBadRequest)
		return
	}
	if err := writeAtomic(filepath.Join(c.publicDir, "matches", assigned.PublicID+".json"), data); err != nil {
		http.Error(w, "write snapshot", http.StatusInternalServerError)
		return
	}
	c.stateByMatch[input.MatchID] = input.Status
	if err := c.publishOverviewLocked(); err != nil {
		http.Error(w, "publish overview failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *coordinator) reconcileLocked() {
	// Keep a valid assignment first. This avoids moving a live stream when a second observer refreshes.
	for observerID, item := range c.assignments {
		if containsCandidate(c.candidatesByObserver[observerID], item.MatchID) {
			continue
		}
		delete(c.assignments, observerID)
		delete(c.matchOwners, item.MatchID)
	}
	limit := c.config.MaxObservers
	if limit <= 0 {
		limit = 3
	}
	active := 0
	for _, item := range c.assignments {
		if item.MatchID != 0 {
			active++
		}
	}
	if active >= limit {
		return
	}
	observerIDs := make([]string, 0, len(c.candidatesByObserver))
	for id := range c.candidatesByObserver {
		observerIDs = append(observerIDs, id)
	}
	sort.Strings(observerIDs)
	for _, observerID := range observerIDs {
		if active >= limit {
			break
		}
		if _, assigned := c.assignments[observerID]; assigned {
			continue
		}
		for _, item := range c.candidatesByObserver[observerID] {
			if _, owned := c.matchOwners[item.MatchID]; owned {
				continue
			}
			publicID := fmt.Sprintf("%s-%d", safeSlug(c.config.TournamentID), item.MatchID)
			c.assignments[observerID] = assignment{MatchID: item.MatchID, LobbyID: item.LobbyID, PublicID: publicID}
			c.matchOwners[item.MatchID] = observerID
			active++
			break
		}
	}
}

func (c *coordinator) publishOverviewLocked() error {
	overview := publicOverview{SchemaVersion: 3, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	overview.Tournament.ID = safeSlug(c.config.TournamentID)
	overview.Tournament.Name = c.config.TournamentName
	overview.Tournament.TestMode = true
	overview.Tournament.Notice = "Тестовое наблюдение за 1win Essence II. Показываются только текущие матчи команд из нашей выборки TI 2026; результаты турнира не меняют статистику TI."
	matches := make([]publicMatch, 0, len(c.assignments))
	ids := make([]string, 0, len(c.assignments))
	for id := range c.assignments {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, observerID := range ids {
		assigned := c.assignments[observerID]
		var matched candidate
		for _, item := range c.candidatesByObserver[observerID] {
			if item.MatchID == assigned.MatchID {
				matched = item
				break
			}
		}
		state := c.stateByMatch[assigned.MatchID]
		if state == "" {
			state = "live"
		}
		interesting, reason := interestingMatch(matched)
		liveMatch := publicMatch{
			ID: assigned.PublicID, MatchID: assigned.MatchID,
			Radiant: team{Name: matched.TeamRadiant}, Dire: team{Name: matched.TeamDire},
			State: state, Interesting: interesting, InterestReason: reason, ObserverID: observerID,
			GameTimeSeconds: matched.GameTime, DelaySeconds: matched.Delay, Spectators: matched.Spectators,
			Score: publicScore{Radiant: int(matched.RadiantScore), Dire: int(matched.DireScore), RadiantGoldLead: int(matched.RadiantGoldLead)},
		}
		matches = append(matches, liveMatch)
		if c.stateByMatch[assigned.MatchID] == "" {
			if err := c.writeSourceTVSnapshotLocked(liveMatch, matched); err != nil {
				return err
			}
		} else if err := c.ensureMatchPlaceholderLocked(liveMatch); err != nil {
			return err
		}
	}
	overview.Matches = matches
	overview.Groups = buildPublicGroups(c.config.Format.Groups, matches)
	overview.Bracket.Rounds = buildPublicBracket(c.config.Format.PlayoffRounds)
	overview.Tiebreakers = publicTiebreakers{
		Rules:   append([]string(nil), c.config.Format.TiebreakerRules...),
		Matches: buildPublicMatches(c.config.Format.TiebreakerMatches, matches),
	}
	data, err := json.MarshalIndent(overview, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(c.publicDir, "overview.json"), data)
}

func (c *coordinator) writeSourceTVSnapshotLocked(item publicMatch, source candidate) error {
	players := make([]publicLivePlayer, 0, len(source.Players))
	sourcePlayers := append([]candidatePlayer(nil), source.Players...)
	sort.SliceStable(sourcePlayers, func(i, j int) bool {
		leftTeam, rightTeam := sourcePlayers[i].Team != 0, sourcePlayers[j].Team != 0
		if leftTeam != rightTeam {
			return !leftTeam
		}
		return sourcePlayers[i].TeamSlot < sourcePlayers[j].TeamSlot
	})
	teamIndexes := map[string]int{"radiant": 0, "dire": 0}
	for _, candidatePlayer := range sourcePlayers {
		teamName := "radiant"
		if candidatePlayer.Team != 0 {
			teamName = "dire"
		}
		teamIndexes[teamName]++
		position := int(candidatePlayer.TeamSlot)
		if position < 1 || position > 5 {
			position = teamIndexes[teamName]
		}
		playerName := fmt.Sprintf("Игрок %d", position)
		if roster, ok := c.config.Roster[strconv.FormatUint(uint64(candidatePlayer.AccountID), 10)]; ok {
			playerName = roster.Name
			if roster.Position >= 1 && roster.Position <= 5 && (candidatePlayer.TeamSlot < 1 || candidatePlayer.TeamSlot > 5) {
				position = roster.Position
			}
		}
		players = append(players, publicLivePlayer{
			ID:         fmt.Sprintf("%s-player-%d", teamName, teamIndexes[teamName]),
			PlayerName: playerName, Position: position,
			Team: teamName, HeroID: int(candidatePlayer.HeroID), Alive: true,
		})
	}
	message := "Данные SourceTV обновляются каждые 20 секунд"
	if source.Delay > 0 {
		message += fmt.Sprintf(" · задержка трансляции около %d мин", max(1, source.Delay/60))
	}
	message += " · SourceTV не передаёт KDA и координаты героев"
	payload := publicLiveSnapshot{
		ID: item.ID, MatchID: item.MatchID, State: "live", Message: message,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339), GameTimeSeconds: source.GameTime,
		Radiant: item.Radiant, Dire: item.Dire, Score: item.Score,
		Map: publicMap{ImageURL: "/dota-map-7.41.png"}, Players: players,
		Spectators: source.Spectators, DelaySeconds: source.Delay, Source: "SourceTV",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(c.publicDir, "matches", item.ID+".json"), data)
}

func (c *coordinator) ensureMatchPlaceholderLocked(item publicMatch) error {
	filename := filepath.Join(c.publicDir, "matches", item.ID+".json")
	if _, err := os.Stat(filename); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	payload := publicLiveSnapshot{
		ID: item.ID, MatchID: item.MatchID, State: item.State,
		Message:   "Ожидаю начало матча и данные внутриигровой трансляции",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Radiant:   item.Radiant, Dire: item.Dire,
		Map: publicMap{ImageURL: "/dota-map-7.41.png"},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeAtomic(filename, data)
}

func interestingMatch(item candidate) (bool, string) {
	if item.GameTime >= 20*60 && item.RadiantGoldLead >= -3000 && item.RadiantGoldLead <= 3000 {
		return true, "Равная игра после 20-й минуты: разница по золоту не превышает 3 000"
	}
	if item.GameTime >= 35*60 {
		return true, "Поздняя стадия матча: результат может решить одна драка"
	}
	if item.Spectators >= 10000 {
		return true, "Матч привлёк особенно много зрителей"
	}
	return false, ""
}

func (c *coordinator) authorized(r *http.Request) bool {
	value := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if value == r.Header.Get("Authorization") || value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), c.token) == 1
}

func loadConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, fmt.Errorf("read live config: %w", err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("decode live config: %w", err)
	}
	cfg.TournamentID = safeSlug(cfg.TournamentID)
	if cfg.TournamentID == "" {
		return config{}, errors.New("live config tournamentId is required")
	}
	if cfg.TournamentName == "" {
		cfg.TournamentName = cfg.TournamentID
	}
	for index, name := range cfg.AllowedTeams {
		cfg.AllowedTeams[index] = teamKey(name)
	}
	if cfg.Roster == nil {
		cfg.Roster = map[string]rosterPlayer{}
	}
	if err := validateTournamentFormat(cfg.Format); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func (c *coordinator) candidateAllowed(item candidate) bool {
	if len(c.config.AllowedTeams) == 0 {
		return true
	}
	radiant := teamKey(item.TeamRadiant)
	dire := teamKey(item.TeamDire)
	for _, allowed := range c.config.AllowedTeams {
		if allowed != "" && (radiant == allowed || dire == allowed) {
			return true
		}
	}
	return false
}

func teamKey(value string) string {
	return strings.Trim(teamKeyPattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "-"), "-")
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid JSON: multiple values")
	}
	return nil
}

func containsCandidate(items []candidate, matchID uint64) bool {
	for _, item := range items {
		if item.MatchID == matchID {
			return true
		}
	}
	return false
}
func limitedText(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}
func safeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}
func writeAtomic(path string, data []byte) error {
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
