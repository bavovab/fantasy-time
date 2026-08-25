package main

import "testing"

func TestOpponentRatingUsesResultsAndTeamAliases(t *testing.T) {
	identities := []teamRatingIdentity{
		{PrimaryID: 10, HistoryID: 11},
		{PrimaryID: 20, HistoryID: 21},
	}
	var matches []teamRatingMatch
	for index := 0; index < 20; index++ {
		matches = append(matches, teamRatingMatch{
			MatchID:    uint64(index + 1),
			StartTime:  int64(index + 1),
			TeamID:     10,
			OpponentID: 20,
			TeamWon:    true,
		})
	}
	model := buildOpponentRatingModel(identities, matches)
	strong, strongConfidence := model.strength(10)
	strongAlias, _ := model.strength(11)
	weak, weakConfidence := model.strength(20)

	if strong <= 1 || weak >= 1 {
		t.Fatalf("results must separate strong and weak teams, got %.2f and %.2f", strong, weak)
	}
	if strongAlias != strong {
		t.Fatalf("history id must resolve to the same rating: %.2f != %.2f", strongAlias, strong)
	}
	if strongConfidence < 80 || weakConfidence < 80 {
		t.Fatalf("twenty maps must produce high confidence, got %.2f and %.2f", strongConfidence, weakConfidence)
	}
}

func TestOpponentRatingDeduplicatesMirroredTeamRows(t *testing.T) {
	model := buildOpponentRatingModel(
		[]teamRatingIdentity{{PrimaryID: 10}, {PrimaryID: 20}},
		[]teamRatingMatch{
			{MatchID: 1, StartTime: 1, TeamID: 10, OpponentID: 20, TeamWon: true},
			{MatchID: 1, StartTime: 1, TeamID: 20, OpponentID: 10, TeamWon: false},
		},
	)
	if model.ratings[10].Games != 1 || model.ratings[20].Games != 1 {
		t.Fatalf("one map must be rated once, got %d and %d games", model.ratings[10].Games, model.ratings[20].Games)
	}
}

func TestPlayerStabilityIsRobustAndReportsConfidence(t *testing.T) {
	stable := reliabilityMatches(98, 100, 101, 99, 102)
	volatile := reliabilityMatches(50, 150, 50, 150, 50)
	outlier := reliabilityMatches(99, 100, 100, 101, 500)

	stableResult := playerReliability(stable)
	volatileResult := playerReliability(volatile)
	outlierResult := playerReliability(outlier)
	if stableResult.Stability <= 90 {
		t.Fatalf("tight results must be highly stable, got %.2f", stableResult.Stability)
	}
	if volatileResult.Stability >= stableResult.Stability {
		t.Fatalf("volatile results must rank below stable results: %.2f >= %.2f", volatileResult.Stability, stableResult.Stability)
	}
	if outlierResult.Stability <= volatileResult.Stability {
		t.Fatalf("one outlier must not outweigh repeated volatility: %.2f <= %.2f", outlierResult.Stability, volatileResult.Stability)
	}
	if stableResult.StabilityConfidence < 50 || stableResult.StabilityConfidence > 60 {
		t.Fatalf("five maps must report moderate confidence, got %.2f", stableResult.StabilityConfidence)
	}
}

func TestPlayerStabilityDoesNotPretendOneMapIsReliable(t *testing.T) {
	result := playerReliability(reliabilityMatches(100))
	if result.Stability != 0 {
		t.Fatalf("one map must not receive a stability score, got %.2f", result.Stability)
	}
	if result.StabilityConfidence <= 0 || result.StabilityConfidence >= 30 {
		t.Fatalf("one map must have explicitly low confidence, got %.2f", result.StabilityConfidence)
	}
}

func reliabilityMatches(points ...float64) []TournamentPlayerMatch {
	matches := make([]TournamentPlayerMatch, 0, len(points))
	for _, value := range points {
		matches = append(matches, TournamentPlayerMatch{
			Included:                   true,
			FantasyPoints:              value,
			OpponentStrength:           1,
			OpponentStrengthConfidence: 80,
		})
	}
	return matches
}
