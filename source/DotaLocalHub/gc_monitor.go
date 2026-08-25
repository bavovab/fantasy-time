package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"
)

type GCMonitorSnapshot struct {
	Enabled                bool       `json:"enabled"`
	State                  string     `json:"state"`
	Priority               string     `json:"priority"`
	DiscoverySource        string     `json:"discoverySource"`
	CycleIntervalSeconds   int        `json:"cycleIntervalSeconds"`
	PlayersPerTeamPerCycle int        `json:"playersPerTeamPerCycle"`
	LastCycleAt            *time.Time `json:"lastCycleAt"`
	LastSuccessfulCycleAt  *time.Time `json:"lastSuccessfulCycleAt"`
	LastErrorAt            *time.Time `json:"lastErrorAt"`
	LastError              string     `json:"lastError,omitempty"`
	NextCycleAt            *time.Time `json:"nextCycleAt"`
	ConsecutiveFailures    int        `json:"consecutiveFailures"`
	DiscoveryRequestsTotal int64      `json:"discoveryRequestsTotal"`
	HistoryRequestsTotal   int64      `json:"historyRequestsTotal"`
	DetailsRequestsTotal   int64      `json:"detailsRequestsTotal"`
	MatchesDiscoveredTotal int64      `json:"matchesDiscoveredTotal"`
	MatchesQueuedTotal     int64      `json:"matchesQueuedTotal"`
	WaitingForReplayTotal  int64      `json:"waitingForReplayTotal"`
	LastCycleCandidates    int        `json:"lastCycleCandidates"`
	LastCycleQueued        int        `json:"lastCycleQueued"`
	LastCycleWaitingReplay int        `json:"lastCycleWaitingReplay"`
}

type GCMonitor struct {
	mu         sync.RWMutex
	store      *Store
	jobs       *JobManager
	client     *GCClient
	config     Config
	rotation   int
	replayWait map[uint64]time.Time
	snapshot   GCMonitorSnapshot
}

type gcMonitorTeam struct {
	TournamentTeam
	RosterIDs []uint32
}

type gcCandidate struct {
	MatchID      uint64
	StartTime    int64
	TournamentID uint32
	LeagueName   string
	HintedTeams  map[string]bool
}

type gcCycleResult struct {
	HistoryRequests   int
	DiscoveryRequests int
	DetailsRequests   int
	Candidates        int
	Queued            int
	WaitingReplay     int
}

func newGCMonitor(store *Store, jobs *JobManager, client *GCClient, config Config) *GCMonitor {
	enabled := config.GCMonitorEnabled && client != nil
	state := "disabled"
	if enabled {
		state = "starting"
	}
	return &GCMonitor{
		store:      store,
		jobs:       jobs,
		client:     client,
		config:     config,
		replayWait: make(map[uint64]time.Time),
		snapshot: GCMonitorSnapshot{
			Enabled:                enabled,
			State:                  state,
			Priority:               "gc-first-metadata",
			DiscoverySource:        "opendota-index",
			CycleIntervalSeconds:   config.GCMonitorIntervalSeconds,
			PlayersPerTeamPerCycle: 0,
		},
	}
}

func (m *GCMonitor) Start(ctx context.Context) {
	if !m.Snapshot().Enabled {
		return
	}
	go m.loop(ctx)
}

func (m *GCMonitor) Snapshot() GCMonitorSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

func (m *GCMonitor) loop(ctx context.Context) {
	delay := 15 * time.Second
	for {
		next := time.Now().UTC().Add(delay)
		m.mu.Lock()
		m.snapshot.NextCycleAt = &next
		m.mu.Unlock()

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		m.mu.Lock()
		m.snapshot.State = "running"
		m.snapshot.NextCycleAt = nil
		m.mu.Unlock()

		result, err := m.runCycle(ctx)
		now := time.Now().UTC()
		m.mu.Lock()
		m.snapshot.LastCycleAt = &now
		m.snapshot.DiscoveryRequestsTotal += int64(result.DiscoveryRequests)
		m.snapshot.HistoryRequestsTotal += int64(result.HistoryRequests)
		m.snapshot.DetailsRequestsTotal += int64(result.DetailsRequests)
		m.snapshot.MatchesDiscoveredTotal += int64(result.Candidates)
		m.snapshot.MatchesQueuedTotal += int64(result.Queued)
		m.snapshot.WaitingForReplayTotal += int64(result.WaitingReplay)
		m.snapshot.LastCycleCandidates = result.Candidates
		m.snapshot.LastCycleQueued = result.Queued
		m.snapshot.LastCycleWaitingReplay = result.WaitingReplay
		if err == nil {
			m.snapshot.State = "idle"
			m.snapshot.LastSuccessfulCycleAt = &now
			m.snapshot.LastError = ""
			m.snapshot.ConsecutiveFailures = 0
			delay = time.Duration(m.config.GCMonitorIntervalSeconds) * time.Second
		} else {
			m.snapshot.State = "backoff"
			m.snapshot.LastErrorAt = &now
			m.snapshot.LastError = sanitizeMonitorError(err)
			m.snapshot.ConsecutiveFailures++
			delay = monitorBackoff(m.snapshot.ConsecutiveFailures)
		}
		m.mu.Unlock()
		if err != nil {
			log.Printf("GC monitor: %v; next attempt in %s", err, delay)
		}
	}
}

func monitorBackoff(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	delay := time.Minute << min(failures-1, 4)
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}

func sanitizeMonitorError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}

func (m *GCMonitor) runCycle(parent context.Context) (gcCycleResult, error) {
	var result gcCycleResult
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	if err := m.client.Healthy(ctx); err != nil {
		return result, err
	}

	teams, err := m.monitorTeams(ctx)
	if err != nil {
		return result, err
	}
	if len(teams) == 0 {
		return result, nil
	}

	lookback := time.Now().Add(-time.Duration(m.config.GCInitialLookbackHours) * time.Hour).Unix()
	teamIDs := make([]uint64, 0, len(teams)*2)
	for _, team := range teams {
		teamIDs = append(teamIDs, team.OpenDotaID, team.HistoryTeamID)
	}
	rows, discoveryErr := m.jobs.downloader.OpenDotaExplorerTournamentMatches(ctx, teamIDs, 200)
	result.DiscoveryRequests++
	if discoveryErr != nil {
		return result, discoveryErr
	}
	candidates := gcCandidatesFromIndex(rows, teams, lookback)

	ordered := make([]*gcCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ordered = append(ordered, candidate)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].StartTime > ordered[j].StartTime })
	result.Candidates = len(ordered)

	for _, candidate := range ordered {
		if result.Queued >= m.config.GCMaxNewMatchesPerCycle || result.DetailsRequests >= m.config.GCMaxNewMatchesPerCycle {
			break
		}
		if !m.candidateNeedsWork(ctx, candidate) {
			continue
		}
		if nextCheck := m.replayWait[candidate.MatchID]; !nextCheck.IsZero() && time.Now().Before(nextCheck) {
			continue
		}
		metadata, detailsErr := m.client.MatchDetails(ctx, candidate.MatchID)
		result.DetailsRequests++
		if detailsErr != nil {
			if gcServiceUnavailable(detailsErr) {
				return result, detailsErr
			}
			continue
		}
		if metadata.LeagueID == 0 && candidate.TournamentID == 0 {
			continue
		}
		completeCandidateLeague(&metadata, candidate)

		teamSlugs := make([]string, 0, 2)
		for _, team := range teams {
			isRadiant, matched := rosterSide(metadata.Players, team.RosterIDs)
			if !matched {
				continue
			}
			summary := teamSummaryFromGC(metadata, isRadiant)
			record := teamMatchFrom(metadata, summary, team.Slug, isRadiant)
			exists, existsErr := m.store.MatchExists(ctx, metadata.MatchID)
			if existsErr != nil {
				return result, existsErr
			}
			switch {
			case exists:
				record.ParseStatus = "done"
			case metadata.ReplaySalt == 0:
				record.ParseStatus = "pending"
				record.ParseError = "Valve has not published the replay yet; GC monitor will check again"
			default:
				record.ParseStatus = "pending"
			}
			if err := m.store.UpsertTeamMatch(ctx, record); err != nil {
				return result, err
			}
			teamSlugs = append(teamSlugs, team.Slug)
		}
		if len(teamSlugs) == 0 {
			continue
		}
		exists, existsErr := m.store.MatchExists(ctx, metadata.MatchID)
		if existsErr != nil {
			return result, existsErr
		}
		if exists {
			continue
		}
		if metadata.ReplaySalt == 0 {
			m.replayWait[candidate.MatchID] = time.Now().Add(10 * time.Minute)
			result.WaitingReplay++
			continue
		}
		delete(m.replayWait, candidate.MatchID)
		m.jobs.StartGCMatch(metadata, teamSlugs)
		result.Queued++
	}
	return result, nil
}

func gcCandidatesFromIndex(rows []teamExplorerRow, teams []gcMonitorTeam, lookback int64) map[uint64]*gcCandidate {
	teamSlugs := make(map[uint64][]string, len(teams)*2)
	for _, team := range teams {
		for _, teamID := range []uint64{team.OpenDotaID, team.HistoryTeamID} {
			if teamID != 0 {
				teamSlugs[teamID] = append(teamSlugs[teamID], team.Slug)
			}
		}
	}
	candidates := make(map[uint64]*gcCandidate)
	for _, row := range rows {
		if row.MatchID == 0 || row.Duration == 0 || row.StartTime < lookback || row.LeagueID == 0 {
			continue
		}
		hints := append([]string{}, teamSlugs[row.RadiantTeamID]...)
		hints = append(hints, teamSlugs[row.DireTeamID]...)
		if len(hints) == 0 {
			continue
		}
		candidate := &gcCandidate{
			MatchID: row.MatchID, StartTime: row.StartTime, TournamentID: uint32(row.LeagueID),
			LeagueName: row.LeagueName, HintedTeams: make(map[string]bool),
		}
		for _, slug := range hints {
			candidate.HintedTeams[slug] = true
		}
		candidates[row.MatchID] = candidate
	}
	return candidates
}

func completeCandidateLeague(metadata *MatchMetadata, candidate *gcCandidate) {
	if metadata.LeagueID == 0 {
		metadata.LeagueID = int(candidate.TournamentID)
	}
	if metadata.LeagueName == "" {
		metadata.LeagueName = candidate.LeagueName
	}
}

func (m *GCMonitor) monitorTeams(ctx context.Context) ([]gcMonitorTeam, error) {
	teams, err := m.store.ListTeams(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]gcMonitorTeam, 0, len(teams))
	for _, team := range teams {
		if team.Status == "tbd" {
			continue
		}
		roster, rosterErr := m.store.TeamRosterAccountIDs(ctx, team.Slug)
		if rosterErr != nil {
			return nil, rosterErr
		}
		if len(roster) != 5 {
			continue
		}
		result = append(result, gcMonitorTeam{TournamentTeam: team, RosterIDs: roster})
	}
	return result, nil
}

func (m *GCMonitor) candidateNeedsWork(ctx context.Context, candidate *gcCandidate) bool {
	for slug := range candidate.HintedTeams {
		status, exists, err := m.store.TeamMatchStatus(ctx, slug, candidate.MatchID)
		if err != nil || !exists || status == "pending" {
			return true
		}
	}
	return false
}

func gcServiceUnavailable(err error) bool {
	var clientErr *GCClientError
	if !errors.As(err, &clientErr) {
		return true
	}
	return clientErr.Status == 0 || clientErr.Status == http.StatusServiceUnavailable || clientErr.Status == http.StatusGatewayTimeout
}

func teamSummaryFromGC(metadata MatchMetadata, isRadiant bool) TeamMatchSummary {
	summary := TeamMatchSummary{
		MatchID:      metadata.MatchID,
		RadiantWin:   metadata.RadiantWin,
		RadiantScore: metadata.RadiantScore,
		DireScore:    metadata.DireScore,
		Radiant:      isRadiant,
		Duration:     metadata.Duration,
		StartTime:    metadata.StartTime,
		LeagueID:     metadata.LeagueID,
		LeagueName:   metadata.LeagueName,
		SeriesID:     metadata.SeriesID,
		SeriesType:   metadata.SeriesType,
		Cluster:      metadata.Cluster,
	}
	if isRadiant {
		summary.OpposingTeamID = metadata.DireTeamID
		summary.OpposingTeamName = metadata.DireName
		summary.OpposingTeamLogo = metadata.DireLogo
	} else {
		summary.OpposingTeamID = metadata.RadiantTeamID
		summary.OpposingTeamName = metadata.RadiantName
		summary.OpposingTeamLogo = metadata.RadiantLogo
	}
	return summary
}

func (m *GCMonitor) String() string {
	snapshot := m.Snapshot()
	return fmt.Sprintf("GC monitor state=%s queued=%d failures=%d", snapshot.State, snapshot.MatchesQueuedTotal, snapshot.ConsecutiveFailures)
}
