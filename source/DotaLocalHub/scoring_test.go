package main

import (
	"testing"
	"time"
)

func TestFantasyFormula(t *testing.T) {
	stats := calculateFantasyStats(fantasyAverages{
		Matches:                10,
		Kills:                  5,
		Deaths:                 3,
		CS:                     100,
		GPM:                    500,
		Madstone:               2,
		TowerKills:             1,
		WardsPlaced:            2,
		CampsStacked:           1,
		RunesGrabbed:           2,
		Watchers:               1,
		Lotuses:                1,
		RoshanKills:            0.1,
		TeamfightParticipation: 0.5,
		Stuns:                  10,
		Tormentors:             0.1,
		CourierKills:           0.1,
		FirstBlood:             0.1,
		Smokes:                 1,
	})
	if len(stats.Metrics) != 18 {
		t.Fatalf("expected 18 metrics, got %d", len(stats.Metrics))
	}
	if stats.TotalPoints != 6574.8 {
		t.Fatalf("expected fantasy total 6574.8, got %f", stats.TotalPoints)
	}
}

func TestRosterSide(t *testing.T) {
	roster := []uint32{1, 2, 3, 4, 5}
	players := []OpenDotaPlayer{
		{AccountID: 10, PlayerSlot: 0},
		{AccountID: 11, PlayerSlot: 1},
		{AccountID: 12, PlayerSlot: 2},
		{AccountID: 13, PlayerSlot: 3},
		{AccountID: 14, PlayerSlot: 4},
		{AccountID: 1, PlayerSlot: 128},
		{AccountID: 2, PlayerSlot: 129},
		{AccountID: 3, PlayerSlot: 130},
		{AccountID: 4, PlayerSlot: 131},
		{AccountID: 5, PlayerSlot: 132},
	}
	isRadiant, matched := rosterSide(players, roster)
	if !matched || isRadiant {
		t.Fatalf("expected exact roster on Dire")
	}
}

func TestNormalizeStratzPlayerSlot(t *testing.T) {
	cases := map[int]int{0: 0, 4: 4, 5: 128, 9: 132, 128: 128, 132: 132}
	for input, want := range cases {
		if got := normalizeStratzPlayerSlot(input); got != want {
			t.Fatalf("normalizeStratzPlayerSlot(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestRosterSideWithNormalizedStratzSlots(t *testing.T) {
	roster := []uint32{1, 2, 3, 4, 5}
	players := make([]OpenDotaPlayer, 0, 10)
	for slot := 0; slot < 5; slot++ {
		players = append(players, OpenDotaPlayer{AccountID: uint32(10 + slot), PlayerSlot: normalizeStratzPlayerSlot(slot)})
	}
	for slot := 5; slot < 10; slot++ {
		players = append(players, OpenDotaPlayer{AccountID: uint32(slot - 4), PlayerSlot: normalizeStratzPlayerSlot(slot)})
	}
	isRadiant, matched := rosterSide(players, roster)
	if !matched || isRadiant {
		t.Fatalf("expected normalized STRATZ roster on Dire")
	}
}

func TestTeamSummaryFromExplorer(t *testing.T) {
	row := teamExplorerRow{MatchID: 99, RadiantTeamID: 10, DireTeamID: 20, RadiantScore: 30, DireScore: 12,
		LeagueName: "Test League", SeriesID: 7, SeriesType: 1, DireName: "Opponent", DireLogo: "logo.png"}
	radiant := teamSummaryFromExplorer(row, 10)
	if !radiant.Radiant || radiant.OpposingTeamID != 20 || radiant.LeagueName != "Test League" ||
		radiant.SeriesID != 7 || radiant.OpposingTeamName != "Opponent" || radiant.OpposingTeamLogo != "logo.png" {
		t.Fatalf("expected team 10 on Radiant against 20: %+v", radiant)
	}
	dire := teamSummaryFromExplorer(row, 20)
	if dire.Radiant || dire.OpposingTeamID != 10 {
		t.Fatalf("expected team 20 on Dire against 10: %+v", dire)
	}
}

func TestMissingRecentTeamMatchesFindsHoleBeforeKnownStreak(t *testing.T) {
	summaries := make([]TeamMatchSummary, 0, 25)
	existing := make(map[uint64]bool)
	for id := uint64(25); id >= 1; id-- {
		summaries = append(summaries, TeamMatchSummary{MatchID: id})
		existing[id] = true
	}
	delete(existing, 17)
	missing := missingRecentTeamMatches(summaries, existing, 20, 0)
	if len(missing) != 1 || missing[0].MatchID != 17 {
		t.Fatalf("expected missing match 17, got %+v", missing)
	}
}

func TestPlayerImagesAreNotRefetchedWhenCached(t *testing.T) {
	now := time.Now().Unix()
	cached := TeamPlayer{PersonaName: "name", AvatarURL: "avatar", PortraitURL: "portrait"}
	if shouldRefreshPlayerProfile(cached, now) || shouldRefreshPlayerPortrait(cached, now) {
		t.Fatal("cached profile and portrait must not be fetched again")
	}
	recentMiss := TeamPlayer{ProfileCheckedAt: now, PortraitCheckedAt: now}
	if shouldRefreshPlayerProfile(recentMiss, now) || shouldRefreshPlayerPortrait(recentMiss, now) {
		t.Fatal("recent failed lookups must be cached")
	}
}
