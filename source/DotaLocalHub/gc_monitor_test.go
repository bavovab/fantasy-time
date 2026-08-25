package main

import (
	"net/http"
	"testing"
	"time"
)

func TestMonitorBackoffIsBounded(t *testing.T) {
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{1, time.Minute},
		{2, 2 * time.Minute},
		{5, 15 * time.Minute},
		{20, 15 * time.Minute},
	}
	for _, test := range cases {
		if got := monitorBackoff(test.failures); got != test.want {
			t.Fatalf("monitorBackoff(%d) = %s, want %s", test.failures, got, test.want)
		}
	}
}

func TestGCServiceUnavailableDistinguishesBusyFromOutage(t *testing.T) {
	if gcServiceUnavailable(&GCClientError{Status: http.StatusTooManyRequests, Text: "busy"}) {
		t.Fatal("HTTP 429 must skip one target without backing off the whole monitor")
	}
	if !gcServiceUnavailable(&GCClientError{Status: http.StatusServiceUnavailable, Text: "offline"}) {
		t.Fatal("HTTP 503 must back off the monitor")
	}
}

func TestTeamSummaryFromGCKeepsOpponentAndSeries(t *testing.T) {
	metadata := MatchMetadata{
		MatchID: 42, RadiantWin: true, RadiantScore: 30, DireScore: 20,
		Duration: 2400, StartTime: 1234, LeagueID: 99, LeagueName: "Tournament",
		SeriesID: 77, SeriesType: 1, Cluster: 188,
		RadiantTeamID: 10, DireTeamID: 20,
		RadiantName: "Radiant", DireName: "Dire",
		RadiantLogo: "radiant.png", DireLogo: "dire.png",
	}
	summary := teamSummaryFromGC(metadata, true)
	if summary.OpposingTeamID != 20 || summary.OpposingTeamName != "Dire" || summary.OpposingTeamLogo != "dire.png" {
		t.Fatalf("unexpected opponent: %#v", summary)
	}
	if summary.SeriesID != 77 || summary.LeagueID != 99 || !summary.Radiant {
		t.Fatalf("series or side was lost: %#v", summary)
	}
}

func TestGCCandidatesFromIndexFiltersAndMapsTeams(t *testing.T) {
	teams := []gcMonitorTeam{
		{TournamentTeam: TournamentTeam{Slug: "radiant", OpenDotaID: 10}},
		{TournamentTeam: TournamentTeam{Slug: "dire", OpenDotaID: 20}},
	}
	rows := []teamExplorerRow{
		{MatchID: 42, StartTime: 2000, Duration: 2400, LeagueID: 99, LeagueName: "Tournament", RadiantTeamID: 10, DireTeamID: 20},
		{MatchID: 43, StartTime: 1000, Duration: 2400, LeagueID: 99, RadiantTeamID: 10, DireTeamID: 30},
		{MatchID: 44, StartTime: 2000, Duration: 0, LeagueID: 99, RadiantTeamID: 10, DireTeamID: 20},
	}

	candidates := gcCandidatesFromIndex(rows, teams, 1500)
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	candidate := candidates[42]
	if candidate == nil || !candidate.HintedTeams["radiant"] || !candidate.HintedTeams["dire"] || candidate.TournamentID != 99 || candidate.LeagueName != "Tournament" {
		t.Fatalf("unexpected candidate: %#v", candidate)
	}
}

func TestCompleteCandidateLeagueUsesDiscoveryMetadata(t *testing.T) {
	metadata := MatchMetadata{}
	candidate := &gcCandidate{TournamentID: 19785, LeagueName: "Esports World Cup 2026"}
	completeCandidateLeague(&metadata, candidate)
	if metadata.LeagueID != 19785 || metadata.LeagueName != "Esports World Cup 2026" {
		t.Fatalf("unexpected league metadata: %#v", metadata)
	}
}

func TestCompleteCandidateLeagueDoesNotInventTournamentName(t *testing.T) {
	metadata := MatchMetadata{}
	completeCandidateLeague(&metadata, &gcCandidate{TournamentID: 42})
	if metadata.LeagueID != 42 || metadata.LeagueName != "" {
		t.Fatalf("unknown league must stay unnamed: %#v", metadata)
	}
}
