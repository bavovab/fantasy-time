package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// runLiveReporter is enabled only for the separate dota-live-observer containers.
// It submits SourceTV's public lobby metadata to the private coordinator; it never
// sends Steam credentials, session data, relay addresses, or watch secrets.
func runLiveReporter(ctx context.Context) {
	observerID := strings.TrimSpace(os.Getenv("DOTA_LIVE_OBSERVER_ID"))
	coordinatorURL := strings.TrimRight(strings.TrimSpace(os.Getenv("DOTA_LIVE_COORDINATOR_URL")), "/")
	leagueID, err := strconv.ParseUint(strings.TrimSpace(os.Getenv("DOTA_LIVE_LEAGUE_ID")), 10, 32)
	if observerID == "" || coordinatorURL == "" || err != nil || leagueID == 0 {
		return
	}
	tokenPath := strings.TrimSpace(os.Getenv("DOTA_LIVE_TOKEN_FILE"))
	token, err := os.ReadFile(tokenPath)
	if err != nil || len(strings.TrimSpace(string(token))) < 24 {
		log.Printf("live reporter disabled: coordinator token is unavailable")
		return
	}
	token = []byte(strings.TrimSpace(string(token)))
	client := &http.Client{Timeout: 20 * time.Second}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		if err := submitLiveCandidates(ctx, client, coordinatorURL, observerID, uint32(leagueID), token); err != nil {
			log.Printf("live reporter observer %s: %v", observerID, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func submitLiveCandidates(ctx context.Context, client *http.Client, coordinatorURL, observerID string, leagueID uint32, token []byte) error {
	// Steam GC may leave a league-filtered SourceTV request unanswered while
	// the event is between maps. The bounded top-games response is immediate;
	// filter it here and enforce the same league again in the coordinator.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8788/api/live/games", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("list SourceTV games: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("list SourceTV games: HTTP %d", response.StatusCode)
	}
	var games struct {
		Games []gcLiveGame `json:"games"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&games); err != nil {
		return fmt.Errorf("decode SourceTV games: %w", err)
	}
	candidates := make([]map[string]any, 0, len(games.Games))
	for _, game := range games.Games {
		if !game.WatchEligible || game.MatchID == 0 || game.LobbyID == 0 || game.LeagueID != leagueID {
			continue
		}
		players := make([]map[string]any, 0, len(game.Players))
		for _, player := range game.Players {
			players = append(players, map[string]any{
				"accountId": player.AccountID, "heroId": player.HeroID, "team": player.Team, "teamSlot": player.TeamSlot,
			})
		}
		candidates = append(candidates, map[string]any{
			"matchId": game.MatchID, "lobbyId": game.LobbyID, "leagueId": game.LeagueID,
			"seriesId":    game.SeriesID,
			"teamRadiant": game.RadiantName, "teamDire": game.DireName,
			"gameTimeSeconds": game.GameTime, "delaySeconds": game.Delay, "spectators": game.SpectatorCount,
			"radiantScore": game.RadiantScore, "direScore": game.DireScore, "radiantGoldLead": game.RadiantLead,
			"players": players,
		})
	}
	body, err := json.Marshal(map[string]any{"candidates": candidates})
	if err != nil {
		return err
	}
	request, err = http.NewRequestWithContext(ctx, http.MethodPost, coordinatorURL+"/api/observers/"+observerID+"/candidates", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+string(token))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		return fmt.Errorf("submit candidates: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("submit candidates: HTTP %d", response.StatusCode)
	}
	return nil
}
