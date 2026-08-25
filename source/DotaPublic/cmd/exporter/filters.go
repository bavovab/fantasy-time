package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	publicIronWingSlug = "1w"
	publicIronWingName = "Iron Wing"
	publicIronWingLogo = "/assets/ti2026/teams/10150413-iron-wing.webp"
)

func filterHeroes(value any) map[string]any {
	result := map[string]any{}
	for id, hero := range asMap(value) {
		result[id] = copyFields(asMap(hero), "name", "imageUrl")
	}
	return result
}

func filterTeamSummaries(value any) []any {
	result := make([]any, 0, len(asSlice(value)))
	for _, item := range asSlice(value) {
		result = append(result, filterTeam(asMap(item), false))
	}
	return result
}

func filterTeamDetail(value any) map[string]any {
	source := asMap(value)
	result := map[string]any{
		"team":    filterTeam(asMap(source["team"]), true),
		"matches": filterTeamMatches(source["matches"]),
	}
	return result
}

func filterTeam(source map[string]any, includeRoster bool) map[string]any {
	result := copyFields(source, "slug", "name", "tag", "status", "slotOrder", "logoUrl", "matchCount", "parsedCount")
	applyPublicTeamBranding(result)
	if includeRoster {
		roster := make([]any, 0, len(asSlice(source["roster"])))
		for _, item := range asSlice(source["roster"]) {
			player := copyFields(asMap(item), "accountId", "name", "personaName", "portraitUrl", "position")
			applyPublicPlayerPortrait(player)
			player["stats"] = filterStats(asMap(asMap(item)["stats"]))
			roster = append(roster, player)
		}
		result["roster"] = roster
	}
	return result
}

func filterTeamMatches(value any) []any {
	allowed := []string{
		"duration", "included", "leagueId", "leagueName", "matchId", "opponentLogo", "opponentName",
		"opponentScore", "opponentTeamId", "parseStatus", "rosterMatched", "seriesId",
		"seriesType", "startTime", "teamScore", "teamSlug", "teamWon",
	}
	result := make([]any, 0, len(asSlice(value)))
	for _, item := range asSlice(value) {
		match := copyFields(asMap(item), allowed...)
		applyPublicOpponentBranding(match)
		result = append(result, match)
	}
	return result
}

func filterPlayerSummaries(value any) []any {
	result := make([]any, 0, len(asSlice(value)))
	for _, item := range asSlice(value) {
		result = append(result, filterPlayer(asMap(item)))
	}
	return result
}

func filterPlayerDetail(value any) map[string]any {
	source := asMap(value)
	matches := make([]any, 0, len(asSlice(source["matches"])))
	for _, item := range asSlice(source["matches"]) {
		match := copyFields(asMap(item),
			"duration", "fantasyPoints", "heroId", "included", "leagueId", "leagueName", "matchId", "opponentName",
			"opponentStrength", "opponentStrengthConfidence", "opponentTeamId", "seriesId",
			"seriesType", "startTime", "won")
		match["metrics"] = filterMetrics(asMap(item)["metrics"])
		applyPublicOpponentBranding(match)
		matches = append(matches, match)
	}
	return map[string]any{"player": filterPlayer(asMap(source["player"])), "matches": matches}
}

func filterPlayer(source map[string]any) map[string]any {
	result := copyFields(source,
		"accountId", "alias", "name", "opponentStrength", "opponentStrengthConfidence",
		"personaName", "portraitUrl", "position", "stability", "stabilityConfidence",
		"strengthAdjustedPoints", "teamLogoUrl", "teamName", "teamSlug")
	applyPublicPlayerPortrait(result)
	applyPublicPlayerBranding(result)
	result["stats"] = filterStats(asMap(source["stats"]))
	return result
}

func applyPublicTeamBranding(team map[string]any) {
	if !strings.EqualFold(stringField(team, "slug"), publicIronWingSlug) {
		return
	}
	team["name"] = publicIronWingName
	team["tag"] = "IW"
	team["logoUrl"] = publicIronWingLogo
}

func applyPublicPlayerBranding(player map[string]any) {
	if !strings.EqualFold(stringField(player, "teamSlug"), publicIronWingSlug) {
		return
	}
	player["teamName"] = publicIronWingName
	player["teamLogoUrl"] = publicIronWingLogo
}

func applyPublicOpponentBranding(match map[string]any) {
	name := strings.ToLower(strings.TrimSpace(stringField(match, "opponentName")))
	compact := strings.NewReplacer(" ", "", "-", "", "_", "").Replace(name)
	teamID := numberString(match["opponentTeamId"])
	if teamID != "10150413" && compact != "1w" && compact != "1wteam" && compact != "ironwing" {
		return
	}
	match["opponentName"] = publicIronWingName
	match["opponentLogo"] = publicIronWingLogo
}

func filterStats(source map[string]any) map[string]any {
	result := copyFields(source, "matches", "totalPoints")
	result["metrics"] = filterMetrics(source["metrics"])
	result["bestMatch"] = filterRecord(asMap(source["bestMatch"]))
	result["bestSeries"] = filterRecord(asMap(source["bestSeries"]))
	return result
}

func filterMetrics(value any) []any {
	result := make([]any, 0, len(asSlice(value)))
	for _, item := range asSlice(value) {
		result = append(result, copyFields(asMap(item), "average", "averagePoints", "key", "label"))
	}
	return result
}

func filterRecord(source map[string]any) map[string]any {
	return copyFields(source, "points", "matchIds")
}

func filterMatch(value any) map[string]any {
	source := asMap(value)
	result := copyFields(source,
		"cluster", "direKills", "duration", "gameMode", "lobbyType", "matchId",
		"radiantKills", "radiantWin", "startTime")
	players := make([]any, 0, len(asSlice(source["players"])))
	allowed := []string{
		"accountId", "assists", "campsStacked", "courierKills", "cs", "deaths",
		"denies", "fantasyPoints", "firstBlood", "gpm", "heroId", "kills", "lastHits",
		"lotuses", "madstone", "name", "playerIndex", "proName", "roshanKills", "runesGrabbed",
		"smokes", "stuns", "team", "teamSlot", "teamfightParticipation", "tormentors",
		"totalEarnedGold", "towerKills", "wardsPlaced", "watchers",
	}
	for _, item := range asSlice(source["players"]) {
		players = append(players, copyFields(asMap(item), allowed...))
	}
	result["players"] = players
	return result
}

func copyFields(source map[string]any, names ...string) map[string]any {
	result := make(map[string]any, len(names))
	for _, name := range names {
		if value, ok := source[name]; ok {
			result[name] = value
		}
	}
	return result
}

func asMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func asSlice(value any) []any {
	if result, ok := value.([]any); ok {
		return result
	}
	return []any{}
}

func stringField(source map[string]any, name string) string {
	value, _ := source[name].(string)
	return value
}

func numberString(value any) string {
	switch typed := value.(type) {
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%.0f", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	default:
		return fmt.Sprint(value)
	}
}
