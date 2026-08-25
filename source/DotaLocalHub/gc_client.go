package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type GCClient struct {
	baseURL string
	client  *http.Client
}

type GCClientError struct {
	Status int
	Text   string
}

func (e *GCClientError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("Dota GC HTTP %d: %s", e.Status, e.Text)
	}
	return "Dota GC: " + e.Text
}

type GCHistoryMatch struct {
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

type gcHistoryPayload struct {
	AccountID uint32           `json:"accountId"`
	Matches   []GCHistoryMatch `json:"matches"`
}

func newGCClient(baseURL string) *GCClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &GCClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *GCClient) Healthy(ctx context.Context) error {
	var payload struct {
		OK bool `json:"ok"`
	}
	if err := c.getJSON(ctx, c.baseURL+"/api/health", &payload); err != nil {
		return err
	}
	if !payload.OK {
		return &GCClientError{Status: http.StatusServiceUnavailable, Text: "agent is not connected"}
	}
	return nil
}

func (c *GCClient) PlayerHistory(ctx context.Context, accountID uint32, limit int) ([]GCHistoryMatch, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}
	endpoint := fmt.Sprintf("%s/api/players/%d/matches?limit=%d", c.baseURL, accountID, limit)
	var payload gcHistoryPayload
	if err := c.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}
	return payload.Matches, nil
}

func (c *GCClient) MatchDetails(ctx context.Context, matchID uint64) (MatchMetadata, error) {
	endpoint := c.baseURL + "/api/matches/" + strconv.FormatUint(matchID, 10)
	var payload MatchMetadata
	if err := c.getJSON(ctx, endpoint, &payload); err != nil {
		return MatchMetadata{}, err
	}
	if payload.MatchID == 0 {
		return MatchMetadata{}, &GCClientError{Text: "match details response has no match ID"}
	}
	return payload, nil
}

func (c *GCClient) getJSON(ctx context.Context, endpoint string, target any) error {
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return &GCClientError{Text: err.Error()}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return &GCClientError{Text: err.Error()}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(response.Body).Decode(&payload)
		if payload.Error == "" {
			payload.Error = http.StatusText(response.StatusCode)
		}
		return &GCClientError{Status: response.StatusCode, Text: payload.Error}
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return &GCClientError{Text: "invalid JSON response: " + err.Error()}
	}
	return nil
}
