package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dota2 "github.com/paralin/go-dota2"
	"github.com/paralin/go-dota2/protocol"
)

var (
	errGCUnavailable = errors.New("Dota Game Coordinator is not connected")
	errGCBusy        = errors.New("another Game Coordinator request is already in progress")
)

type gcGateway struct {
	clientMu    sync.RWMutex
	client      *dota2.Dota2
	requestSlot chan struct{}
	minInterval time.Duration
	lastRequest time.Time
	requestID   atomic.Uint32
	state       *agentState
}

func newGCGateway(state *agentState) *gcGateway {
	interval := 4 * time.Second
	if value := strings.TrimSpace(os.Getenv("DOTA_GC_REQUEST_INTERVAL")); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed >= time.Second {
			interval = parsed
		}
	}
	return &gcGateway{
		requestSlot: make(chan struct{}, 1),
		minInterval: interval,
		state:       state,
	}
}

func (g *gcGateway) setClient(client *dota2.Dota2) {
	g.clientMu.Lock()
	g.client = client
	g.clientMu.Unlock()
}

func (g *gcGateway) clearClient(client *dota2.Dota2) {
	g.clientMu.Lock()
	if g.client == client {
		g.client = nil
	}
	g.clientMu.Unlock()
}

func (g *gcGateway) execute(ctx context.Context, kind string, call func(*dota2.Dota2) error) error {
	select {
	case g.requestSlot <- struct{}{}:
		defer func() { <-g.requestSlot }()
	default:
		return errGCBusy
	}

	if delay := time.Until(g.lastRequest.Add(g.minInterval)); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	g.clientMu.RLock()
	client := g.client
	if client == nil {
		g.clientMu.RUnlock()
		return errGCUnavailable
	}
	g.lastRequest = time.Now()
	err := call(client)
	g.clientMu.RUnlock()
	g.state.recordGCRequest(kind, err)
	return err
}

type gcHistoryMatch struct {
	MatchID        uint64 `json:"matchId"`
	StartTime      uint32 `json:"startTime"`
	Duration       uint32 `json:"duration"`
	TeamID         uint32 `json:"teamId"`
	TeamName       string `json:"teamName"`
	LobbyType      uint32 `json:"lobbyType"`
	GameMode       uint32 `json:"gameMode"`
	TournamentID   uint32 `json:"tournamentId"`
	TournamentTier uint32 `json:"tournamentTier"`
}

type gcHistoryResponse struct {
	AccountID uint32           `json:"accountId"`
	Matches   []gcHistoryMatch `json:"matches"`
}

func (g *gcGateway) playerHistoryHandler(w http.ResponseWriter, r *http.Request) {
	accountID64, err := strconv.ParseUint(r.PathValue("accountID"), 10, 32)
	if err != nil || accountID64 == 0 {
		writeAPIError(w, http.StatusBadRequest, errors.New("invalid account ID"))
		return
	}
	limit := uint64(10)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, parseErr := strconv.ParseUint(raw, 10, 8); parseErr == nil {
			limit = parsed
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	accountID := uint32(accountID64)
	requested := uint32(limit)
	requestID := g.requestID.Add(1)
	includePractice := false
	includeCustom := false
	includeEvents := true
	result := gcHistoryResponse{AccountID: accountID, Matches: make([]gcHistoryMatch, 0, limit)}
	err = g.execute(ctx, "history", func(client *dota2.Dota2) error {
		response, requestErr := client.GetPlayerMatchHistory(ctx, &protocol.CMsgDOTAGetPlayerMatchHistory{
			AccountId:              &accountID,
			MatchesRequested:       &requested,
			RequestId:              &requestID,
			IncludePracticeMatches: &includePractice,
			IncludeCustomGames:     &includeCustom,
			IncludeEventGames:      &includeEvents,
		})
		if requestErr != nil {
			return requestErr
		}
		for _, match := range response.GetMatches() {
			if match.GetMatchId() == 0 || match.GetDuration() == 0 {
				continue
			}
			result.Matches = append(result.Matches, gcHistoryMatch{
				MatchID:        match.GetMatchId(),
				StartTime:      match.GetStartTime(),
				Duration:       match.GetDuration(),
				TeamID:         match.GetTeamId(),
				TeamName:       match.GetTeamName(),
				LobbyType:      match.GetLobbyType(),
				GameMode:       match.GetGameMode(),
				TournamentID:   match.GetTourneyId(),
				TournamentTier: match.GetTourneyTier(),
			})
		}
		return nil
	})
	if err != nil {
		writeGatewayError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, result)
}

type gcMatchPlayer struct {
	AccountID  uint32 `json:"account_id"`
	PlayerSlot int    `json:"player_slot"`
	Name       string `json:"name"`
	Persona    string `json:"personaname"`
}

type gcMatchDetails struct {
	MatchID       uint64          `json:"match_id"`
	Cluster       int             `json:"cluster"`
	ReplaySalt    uint64          `json:"replay_salt"`
	Duration      int             `json:"duration"`
	StartTime     int64           `json:"start_time"`
	RadiantWin    bool            `json:"radiant_win"`
	GameMode      int             `json:"game_mode"`
	LobbyType     int             `json:"lobby_type"`
	LeagueID      int             `json:"league_id"`
	SeriesID      uint64          `json:"series_id"`
	SeriesType    int             `json:"series_type"`
	RadiantTeamID uint64          `json:"radiant_team_id"`
	DireTeamID    uint64          `json:"dire_team_id"`
	RadiantName   string          `json:"radiant_name"`
	DireName      string          `json:"dire_name"`
	RadiantLogo   string          `json:"radiant_logo"`
	DireLogo      string          `json:"dire_logo"`
	RadiantScore  int             `json:"radiant_score"`
	DireScore     int             `json:"dire_score"`
	Players       []gcMatchPlayer `json:"players"`
}

// gcLiveGame is intentionally limited to SourceTV's published lobby metadata.
// Relay addresses, watch secrets and Steam session data must never leave the observer.
type gcLiveGame struct {
	MatchID        uint64         `json:"matchId"`
	LobbyID        uint64         `json:"lobbyId"`
	LeagueID       uint32         `json:"leagueId"`
	SeriesID       uint32         `json:"seriesId"`
	GameTime       int32          `json:"gameTimeSeconds"`
	Delay          uint32         `json:"delaySeconds"`
	RadiantName    string         `json:"radiantName"`
	DireName       string         `json:"direName"`
	RadiantScore   uint32         `json:"radiantScore"`
	DireScore      uint32         `json:"direScore"`
	RadiantLead    int32          `json:"radiantGoldLead"`
	BuildingState  uint32         `json:"buildingState"`
	WatchEligible  bool           `json:"watchEligible"`
	SpectatorCount uint32         `json:"spectatorCount"`
	Players        []gcLivePlayer `json:"players"`
}

type gcLivePlayer struct {
	AccountID uint32 `json:"accountId"`
	HeroID    int32  `json:"heroId"`
	Team      uint32 `json:"team"`
	TeamSlot  uint32 `json:"teamSlot"`
}

func (g *gcGateway) liveGamesHandler(w http.ResponseWriter, r *http.Request) {
	leagueID := uint64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("leagueId")); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, errors.New("invalid league ID"))
			return
		}
		leagueID = parsed
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	if len(search) > 96 {
		writeAPIError(w, http.StatusBadRequest, errors.New("search is too long"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result := make([]gcLiveGame, 0)
	err := g.execute(ctx, "live_games", func(client *dota2.Dota2) error {
		response, requestErr := client.FindTopSourceTVGames(ctx, search, uint32(leagueID), 0, 0, 0, nil)
		if requestErr != nil {
			return requestErr
		}
		for _, game := range response.GetGameList() {
			if game.GetLobbyId() == 0 || game.GetMatchId() == 0 {
				continue
			}
			item := gcLiveGame{
				MatchID:        game.GetMatchId(),
				LobbyID:        game.GetLobbyId(),
				LeagueID:       game.GetLeagueId(),
				SeriesID:       game.GetSeriesId(),
				GameTime:       game.GetGameTime(),
				Delay:          game.GetDelay(),
				RadiantName:    game.GetTeamNameRadiant(),
				DireName:       game.GetTeamNameDire(),
				RadiantScore:   game.GetRadiantScore(),
				DireScore:      game.GetDireScore(),
				RadiantLead:    game.GetRadiantLead(),
				BuildingState:  game.GetBuildingState(),
				WatchEligible:  game.GetIsWatchEligible(),
				SpectatorCount: game.GetSpectators(),
				Players:        make([]gcLivePlayer, 0, len(game.GetPlayers())),
			}
			for _, player := range game.GetPlayers() {
				item.Players = append(item.Players, gcLivePlayer{
					AccountID: player.GetAccountId(), HeroID: player.GetHeroId(), Team: player.GetTeam(), TeamSlot: player.GetTeamSlot(),
				})
			}
			result = append(result, item)
		}
		return nil
	})
	if err != nil {
		writeGatewayError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"games": result})
}

func (g *gcGateway) matchDetailsHandler(w http.ResponseWriter, r *http.Request) {
	matchID, err := strconv.ParseUint(r.PathValue("matchID"), 10, 64)
	if err != nil || matchID == 0 {
		writeAPIError(w, http.StatusBadRequest, errors.New("invalid match ID"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var result gcMatchDetails
	err = g.execute(ctx, "match_details", func(client *dota2.Dota2) error {
		response, requestErr := client.RequestMatchDetails(ctx, matchID)
		if requestErr != nil {
			return requestErr
		}
		if response.GetResult() != 1 || response.GetMatch() == nil {
			return fmt.Errorf("match details result %d", response.GetResult())
		}
		match := response.GetMatch()
		result = gcMatchDetails{
			MatchID:       match.GetMatchId(),
			Cluster:       int(match.GetCluster()),
			ReplaySalt:    uint64(match.GetReplaySalt()),
			Duration:      int(match.GetDuration()),
			StartTime:     int64(match.GetStarttime()),
			RadiantWin:    match.GetMatchOutcome() == protocol.EMatchOutcome_k_EMatchOutcome_RadVictory,
			GameMode:      int(match.GetGameMode()),
			LobbyType:     int(match.GetLobbyType()),
			LeagueID:      int(match.GetLeagueid()),
			SeriesID:      uint64(match.GetSeriesId()),
			SeriesType:    int(match.GetSeriesType()),
			RadiantTeamID: uint64(match.GetRadiantTeamId()),
			DireTeamID:    uint64(match.GetDireTeamId()),
			RadiantName:   match.GetRadiantTeamName(),
			DireName:      match.GetDireTeamName(),
			RadiantLogo:   match.GetRadiantTeamLogoUrl(),
			DireLogo:      match.GetDireTeamLogoUrl(),
			RadiantScore:  int(match.GetRadiantTeamScore()),
			DireScore:     int(match.GetDireTeamScore()),
			Players:       make([]gcMatchPlayer, 0, len(match.GetPlayers())),
		}
		for _, player := range match.GetPlayers() {
			result.Players = append(result.Players, gcMatchPlayer{
				AccountID:  player.GetAccountId(),
				PlayerSlot: int(player.GetPlayerSlot()),
				Name:       player.GetProName(),
				Persona:    player.GetPlayerName(),
			})
		}
		return nil
	})
	if err != nil {
		writeGatewayError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, result)
}

func writeGatewayError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, errGCUnavailable) {
		status = http.StatusServiceUnavailable
	} else if errors.Is(err, errGCBusy) {
		status = http.StatusTooManyRequests
	} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		status = http.StatusGatewayTimeout
	}
	writeAPIError(w, status, err)
}

func writeAPIError(w http.ResponseWriter, status int, err error) {
	writeAPIJSON(w, status, map[string]any{"ok": false, "error": sanitizeError(err)})
}

func writeAPIJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
