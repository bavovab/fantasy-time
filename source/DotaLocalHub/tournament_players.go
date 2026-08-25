package main

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"sort"
	"strings"
	"unicode"
)

type TournamentPlayerSummary struct {
	Alias                      string             `json:"alias"`
	AccountID                  uint32             `json:"accountId"`
	Name                       string             `json:"name"`
	PersonaName                string             `json:"personaName,omitempty"`
	AvatarURL                  string             `json:"avatarUrl,omitempty"`
	PortraitURL                string             `json:"portraitUrl,omitempty"`
	Position                   int                `json:"position"`
	TeamSlug                   string             `json:"teamSlug"`
	TeamName                   string             `json:"teamName"`
	TeamLogoURL                string             `json:"teamLogoUrl,omitempty"`
	Stats                      PlayerFantasyStats `json:"stats"`
	Stability                  float64            `json:"stability"`
	StabilityConfidence        float64            `json:"stabilityConfidence"`
	OpponentStrength           float64            `json:"opponentStrength"`
	OpponentStrengthConfidence float64            `json:"opponentStrengthConfidence"`
	StrengthAdjustedPoints     float64            `json:"strengthAdjustedPoints"`
}

type TournamentPlayerMatch struct {
	MatchID                    uint64          `json:"matchId"`
	LeagueID                   int             `json:"leagueId,omitempty"`
	SeriesID                   uint64          `json:"seriesId"`
	SeriesType                 int             `json:"seriesType"`
	StartTime                  int64           `json:"startTime"`
	Duration                   int             `json:"duration"`
	HeroID                     int32           `json:"heroId"`
	OpponentName               string          `json:"opponentName"`
	OpponentTeamID             uint64          `json:"opponentTeamId"`
	LeagueName                 string          `json:"leagueName"`
	Won                        bool            `json:"won"`
	Included                   bool            `json:"included"`
	FantasyPoints              float64         `json:"fantasyPoints"`
	OpponentStrength           float64         `json:"opponentStrength"`
	OpponentStrengthConfidence float64         `json:"opponentStrengthConfidence"`
	Metrics                    []FantasyMetric `json:"metrics"`
}

const (
	opponentRatingBaseline = 1500.0
	opponentRatingK        = 24.0
)

type teamRatingIdentity struct {
	PrimaryID uint64
	HistoryID uint64
}

type teamRatingMatch struct {
	MatchID    uint64
	StartTime  int64
	TeamID     uint64
	OpponentID uint64
	TeamWon    bool
}

type teamRating struct {
	Value float64
	Games int
}

type opponentRatingModel struct {
	aliases map[uint64]uint64
	ratings map[uint64]teamRating
}

type playerReliabilitySummary struct {
	Stability                  float64
	StabilityConfidence        float64
	OpponentStrength           float64
	OpponentStrengthConfidence float64
	StrengthAdjustedPoints     float64
}

type TournamentPlayerDetail struct {
	Player  TournamentPlayerSummary `json:"player"`
	Matches []TournamentPlayerMatch `json:"matches"`
}

func playerAlias(name string) string {
	var result []rune
	dash := false
	for _, char := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			result = append(result, char)
			dash = false
		} else if len(result) > 0 && !dash {
			result = append(result, '-')
			dash = true
		}
	}
	return strings.Trim(string(result), "-")
}

func (s *Store) tournamentPlayerRoster(ctx context.Context) ([]TournamentPlayerSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT tr.account_id, tr.name, tr.persona_name, tr.avatar_url, tr.portrait_url,
       tr.position, tr.team_slug, tt.name, tt.logo_url
FROM team_rosters tr
JOIN tournament_teams tt ON tt.slug = tr.team_slug
WHERE tt.status <> 'tbd'
ORDER BY tt.slot_order, tr.position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []TournamentPlayerSummary
	for rows.Next() {
		var player TournamentPlayerSummary
		if err := rows.Scan(
			&player.AccountID, &player.Name, &player.PersonaName, &player.AvatarURL,
			&player.PortraitURL, &player.Position, &player.TeamSlug,
			&player.TeamName, &player.TeamLogoURL,
		); err != nil {
			return nil, err
		}
		player.Alias = playerAlias(player.Name)
		players = append(players, player)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return players, nil
}

func (s *Store) hydrateTournamentPlayer(ctx context.Context, player *TournamentPlayerSummary, ratingModel opponentRatingModel) ([]TournamentPlayerMatch, error) {
	stats, err := s.PlayerFantasyStats(ctx, player.TeamSlug, player.AccountID)
	if err != nil {
		return nil, err
	}
	player.Stats = stats
	matches, err := s.tournamentPlayerMatches(ctx, player.TeamSlug, player.AccountID, ratingModel)
	if err != nil {
		return nil, err
	}
	reliability := playerReliability(matches)
	player.Stability = reliability.Stability
	player.StabilityConfidence = reliability.StabilityConfidence
	player.OpponentStrength = reliability.OpponentStrength
	player.OpponentStrengthConfidence = reliability.OpponentStrengthConfidence
	player.StrengthAdjustedPoints = reliability.StrengthAdjustedPoints
	return matches, nil
}

func (s *Store) ListTournamentPlayers(ctx context.Context) ([]TournamentPlayerSummary, error) {
	players, err := s.tournamentPlayerRoster(ctx)
	if err != nil {
		return nil, err
	}
	ratingModel, err := s.tournamentOpponentRatingModel(ctx)
	if err != nil {
		return nil, err
	}

	for index := range players {
		if _, err := s.hydrateTournamentPlayer(ctx, &players[index], ratingModel); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(players, func(i, j int) bool {
		return players[i].Stats.TotalPoints > players[j].Stats.TotalPoints
	})
	return players, nil
}

func (s *Store) TournamentPlayer(ctx context.Context, alias string) (TournamentPlayerDetail, error) {
	players, err := s.tournamentPlayerRoster(ctx)
	if err != nil {
		return TournamentPlayerDetail{}, err
	}
	for index := range players {
		if strings.EqualFold(players[index].Alias, playerAlias(alias)) {
			ratingModel, err := s.tournamentOpponentRatingModel(ctx)
			if err != nil {
				return TournamentPlayerDetail{}, err
			}
			matches, err := s.hydrateTournamentPlayer(ctx, &players[index], ratingModel)
			if err != nil {
				return TournamentPlayerDetail{}, err
			}
			return TournamentPlayerDetail{Player: players[index], Matches: matches}, nil
		}
	}
	return TournamentPlayerDetail{}, sql.ErrNoRows
}

func (s *Store) TournamentPlayerFilterData(ctx context.Context) (map[string]TournamentPlayerDetail, error) {
	players, err := s.tournamentPlayerRoster(ctx)
	if err != nil {
		return nil, err
	}
	ratingModel, err := s.tournamentOpponentRatingModel(ctx)
	if err != nil {
		return nil, err
	}
	details := make(map[string]TournamentPlayerDetail, len(players))
	for index := range players {
		matches, err := s.hydrateTournamentPlayer(ctx, &players[index], ratingModel)
		if err != nil {
			return nil, err
		}
		details[players[index].Alias] = TournamentPlayerDetail{Player: players[index], Matches: matches}
	}
	return details, nil
}

func (s *Store) tournamentPlayerMatches(ctx context.Context, teamSlug string, accountID uint32, ratingModel opponentRatingModel) ([]TournamentPlayerMatch, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT tm.match_id, tm.series_id, tm.series_type, tm.start_time, tm.duration, p.hero_id,
       tm.opponent_name, tm.opponent_team_id, tm.league_id, tm.league_name, tm.team_won, tm.included,
       p.kills, p.deaths, p.cs, p.gpm, p.madstone, p.tower_kills,
       p.wards_placed, p.camps_stacked, p.runes_grabbed, p.watchers, p.lotuses,
       p.roshan_kills, p.teamfight_participation, p.stuns, p.tormentors,
       p.courier_kills, p.first_blood, p.smokes
FROM team_matches tm
JOIN players p ON p.match_id = tm.match_id
WHERE tm.team_slug = ? AND tm.roster_matched = 1
  AND tm.parse_status = 'done' AND p.account_id = ?
ORDER BY tm.start_time ASC`, teamSlug, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []TournamentPlayerMatch
	for rows.Next() {
		var match TournamentPlayerMatch
		var won, included int
		var avg fantasyAverages
		avg.Matches = 1
		if err := rows.Scan(
			&match.MatchID, &match.SeriesID, &match.SeriesType, &match.StartTime, &match.Duration,
			&match.HeroID, &match.OpponentName, &match.OpponentTeamID,
			&match.LeagueID, &match.LeagueName, &won, &included,
			&avg.Kills, &avg.Deaths, &avg.CS, &avg.GPM, &avg.Madstone,
			&avg.TowerKills, &avg.WardsPlaced, &avg.CampsStacked,
			&avg.RunesGrabbed, &avg.Watchers, &avg.Lotuses, &avg.RoshanKills,
			&avg.TeamfightParticipation, &avg.Stuns, &avg.Tormentors,
			&avg.CourierKills, &avg.FirstBlood, &avg.Smokes,
		); err != nil {
			return nil, err
		}
		match.Won = won != 0
		match.Included = included != 0
		matchStats := calculateFantasyStats(avg)
		match.FantasyPoints = matchStats.TotalPoints
		match.Metrics = matchStats.Metrics
		match.OpponentStrength, match.OpponentStrengthConfidence = ratingModel.strength(match.OpponentTeamID)
		if match.SeriesID == 0 {
			match.SeriesID = match.MatchID
		}
		matches = append(matches, match)
	}
	return matches, rows.Err()
}

func (s *Store) tournamentOpponentRatingModel(ctx context.Context) (opponentRatingModel, error) {
	identityRows, err := s.db.QueryContext(ctx, `
SELECT opendota_id, history_team_id
FROM tournament_teams
WHERE status <> 'tbd'`)
	if err != nil {
		return opponentRatingModel{}, err
	}
	defer identityRows.Close()

	var identities []teamRatingIdentity
	for identityRows.Next() {
		var identity teamRatingIdentity
		if err := identityRows.Scan(&identity.PrimaryID, &identity.HistoryID); err != nil {
			return opponentRatingModel{}, err
		}
		identities = append(identities, identity)
	}
	if err := identityRows.Err(); err != nil {
		return opponentRatingModel{}, err
	}
	if err := identityRows.Close(); err != nil {
		return opponentRatingModel{}, err
	}

	matchRows, err := s.db.QueryContext(ctx, `
SELECT tm.match_id, tm.start_time, tm.team_won, tm.opponent_team_id,
       tt.opendota_id, tt.history_team_id
FROM team_matches tm
JOIN tournament_teams tt ON tt.slug = tm.team_slug
WHERE tm.roster_matched = 1 AND tm.parse_status = 'done'
  AND tm.opponent_team_id <> 0
ORDER BY tm.start_time ASC, tm.match_id ASC, tm.team_slug ASC`)
	if err != nil {
		return opponentRatingModel{}, err
	}
	defer matchRows.Close()

	var matches []teamRatingMatch
	for matchRows.Next() {
		var match teamRatingMatch
		var won int
		var primaryID, historyID uint64
		if err := matchRows.Scan(
			&match.MatchID, &match.StartTime, &won, &match.OpponentID,
			&primaryID, &historyID,
		); err != nil {
			return opponentRatingModel{}, err
		}
		match.TeamID = primaryID
		if match.TeamID == 0 {
			match.TeamID = historyID
		}
		match.TeamWon = won != 0
		matches = append(matches, match)
	}
	if err := matchRows.Err(); err != nil {
		return opponentRatingModel{}, err
	}
	return buildOpponentRatingModel(identities, matches), nil
}

func buildOpponentRatingModel(identities []teamRatingIdentity, matches []teamRatingMatch) opponentRatingModel {
	// One chronological Elo update per map keeps the score data-driven while
	// naturally shrinking teams with only a few observed games toward neutral.
	model := opponentRatingModel{
		aliases: make(map[uint64]uint64),
		ratings: make(map[uint64]teamRating),
	}
	for _, identity := range identities {
		canonical := identity.PrimaryID
		if canonical == 0 {
			canonical = identity.HistoryID
		}
		if canonical == 0 {
			continue
		}
		model.ratings[canonical] = teamRating{Value: opponentRatingBaseline}
		if identity.PrimaryID != 0 {
			model.aliases[identity.PrimaryID] = canonical
		}
		if identity.HistoryID != 0 {
			model.aliases[identity.HistoryID] = canonical
		}
	}

	ordered := append([]teamRatingMatch(nil), matches...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].StartTime != ordered[j].StartTime {
			return ordered[i].StartTime < ordered[j].StartTime
		}
		return ordered[i].MatchID < ordered[j].MatchID
	})
	seenMatches := make(map[uint64]struct{}, len(ordered))
	for _, match := range ordered {
		if match.MatchID != 0 {
			if _, exists := seenMatches[match.MatchID]; exists {
				continue
			}
			seenMatches[match.MatchID] = struct{}{}
		}
		teamID := model.canonicalID(match.TeamID)
		opponentID := model.canonicalID(match.OpponentID)
		if teamID == 0 || opponentID == 0 || teamID == opponentID {
			continue
		}
		team := model.rating(teamID)
		opponent := model.rating(opponentID)
		expected := 1 / (1 + math.Pow(10, (opponent.Value-team.Value)/400))
		actual := 0.0
		if match.TeamWon {
			actual = 1
		}
		delta := opponentRatingK * (actual - expected)
		team.Value += delta
		team.Games++
		opponent.Value -= delta
		opponent.Games++
		model.ratings[teamID] = team
		model.ratings[opponentID] = opponent
	}
	return model
}

func (model opponentRatingModel) canonicalID(teamID uint64) uint64 {
	if canonical, exists := model.aliases[teamID]; exists {
		return canonical
	}
	return teamID
}

func (model opponentRatingModel) rating(teamID uint64) teamRating {
	if rating, exists := model.ratings[teamID]; exists {
		return rating
	}
	return teamRating{Value: opponentRatingBaseline}
}

func (model opponentRatingModel) strength(teamID uint64) (strength, confidence float64) {
	teamID = model.canonicalID(teamID)
	if teamID == 0 {
		return 1, 0
	}
	rating, exists := model.ratings[teamID]
	if !exists || rating.Games == 0 {
		return 1, 0
	}
	strength = 1 + (rating.Value-opponentRatingBaseline)/1000
	strength = math.Max(0.75, math.Min(1.25, strength))
	confidence = 100 * (1 - math.Exp(-float64(rating.Games)/8))
	return round2(strength), round2(confidence)
}

func playerReliability(matches []TournamentPlayerMatch) playerReliabilitySummary {
	var selected []TournamentPlayerMatch
	for _, match := range matches {
		if match.Included {
			selected = append(selected, match)
		}
	}
	if len(selected) == 0 {
		return playerReliabilitySummary{}
	}
	var strengthSum, strengthConfidenceSum, adjustedSum float64
	points := make([]float64, 0, len(selected))
	for _, match := range selected {
		strengthSum += match.OpponentStrength
		strengthConfidenceSum += match.OpponentStrengthConfidence
		adjustedSum += match.FantasyPoints * match.OpponentStrength
		points = append(points, match.FantasyPoints)
	}
	result := playerReliabilitySummary{
		StabilityConfidence:        round2(100 * (1 - math.Exp(-float64(len(selected))/6))),
		OpponentStrength:           round2(strengthSum / float64(len(selected))),
		OpponentStrengthConfidence: round2(strengthConfidenceSum / float64(len(selected))),
		StrengthAdjustedPoints:     round2(adjustedSum / float64(len(selected))),
	}
	if len(points) < 2 {
		return result
	}

	// Schedule difficulty is reported separately. Stability therefore measures
	// raw player output and blends robust MAD/IQR spread with a small tail term.
	sort.Float64s(points)
	center := quantile(points, 0.5)
	deviations := make([]float64, len(points))
	mean := 0.0
	for index, value := range points {
		mean += value
		deviations[index] = math.Abs(value - center)
	}
	mean /= float64(len(points))
	sort.Float64s(deviations)
	madSigma := 1.4826 * quantile(deviations, 0.5)
	iqrSigma := (quantile(points, 0.75) - quantile(points, 0.25)) / 1.349
	robustSigma := math.Max(madSigma, iqrSigma)
	variance := 0.0
	for _, value := range points {
		variance += math.Pow(value-mean, 2)
	}
	standardDeviation := math.Sqrt(variance / float64(len(points)))
	dispersion := 0.8*robustSigma + 0.2*standardDeviation
	coefficient := dispersion / math.Max(math.Abs(center), 1)
	result.Stability = round2(100 / (1 + 2*coefficient))
	return result
}

func quantile(sortedValues []float64, probability float64) float64 {
	if len(sortedValues) == 0 {
		return 0
	}
	position := float64(len(sortedValues)-1) * probability
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return sortedValues[lower]
	}
	weight := position - float64(lower)
	return sortedValues[lower]*(1-weight) + sortedValues[upper]*weight
}

func (server *Server) listTournamentPlayers(response http.ResponseWriter, request *http.Request) {
	players, err := server.store.ListTournamentPlayers(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	writeJSON(response, http.StatusOK, players)
}

func (server *Server) tournamentPlayerFilterData(response http.ResponseWriter, request *http.Request) {
	details, err := server.store.TournamentPlayerFilterData(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	writeJSON(response, http.StatusOK, details)
}

func (server *Server) getTournamentPlayer(response http.ResponseWriter, request *http.Request) {
	player, err := server.store.TournamentPlayer(request.Context(), request.PathValue("alias"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(response, http.StatusNotFound, errors.New("игрок не найден"))
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	writeJSON(response, http.StatusOK, player)
}
