package main

import (
	"errors"
	"fmt"
	"strings"
)

type rosterPlayer struct {
	Name     string `json:"name"`
	Position int    `json:"position,omitempty"`
	Team     string `json:"team,omitempty"`
}

type tournamentFormat struct {
	Groups            []groupConfig        `json:"groups"`
	TiebreakerRules   []string             `json:"tiebreakerRules"`
	TiebreakerMatches []scheduledMatch     `json:"tiebreakerMatches,omitempty"`
	PlayoffRounds     []bracketRoundConfig `json:"playoffRounds"`
}

type groupConfig struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Teams   []string         `json:"teams"`
	Matches []scheduledMatch `json:"matches"`
}

type scheduledMatch struct {
	ID          string `json:"id"`
	TeamA       string `json:"teamA"`
	TeamB       string `json:"teamB"`
	ScheduledAt string `json:"scheduledAt,omitempty"`
	BestOf      int    `json:"bestOf"`
	Label       string `json:"label,omitempty"`
}

type bracketRoundConfig struct {
	Name    string           `json:"name"`
	Matches []scheduledMatch `json:"matches"`
}

func validateTournamentFormat(format tournamentFormat) error {
	seen := make(map[string]bool)
	for _, group := range format.Groups {
		if strings.TrimSpace(group.ID) == "" || strings.TrimSpace(group.Name) == "" {
			return errors.New("live config group id and name are required")
		}
		if len(group.Teams) < 2 {
			return fmt.Errorf("live config group %s must contain at least two teams", group.ID)
		}
		for _, match := range group.Matches {
			if err := validateScheduledMatch(match, seen); err != nil {
				return err
			}
		}
	}
	for _, round := range format.PlayoffRounds {
		if strings.TrimSpace(round.Name) == "" {
			return errors.New("live config playoff round name is required")
		}
		for _, match := range round.Matches {
			if err := validateScheduledMatch(match, seen); err != nil {
				return err
			}
		}
	}
	for _, match := range format.TiebreakerMatches {
		if err := validateScheduledMatch(match, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateScheduledMatch(match scheduledMatch, seen map[string]bool) error {
	if strings.TrimSpace(match.ID) == "" || strings.TrimSpace(match.TeamA) == "" || strings.TrimSpace(match.TeamB) == "" {
		return errors.New("live config scheduled match id and teams are required")
	}
	if seen[match.ID] {
		return fmt.Errorf("live config scheduled match id %s is duplicated", match.ID)
	}
	seen[match.ID] = true
	if match.BestOf < 1 || match.BestOf > 7 {
		return fmt.Errorf("live config scheduled match %s has invalid bestOf", match.ID)
	}
	return nil
}

func buildPublicGroups(groups []groupConfig, live []publicMatch) []publicGroup {
	result := make([]publicGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, publicGroup{
			ID:      group.ID,
			Name:    group.Name,
			Teams:   append([]string(nil), group.Teams...),
			Matches: buildPublicMatches(group.Matches, live),
		})
	}
	return result
}

func buildPublicBracket(rounds []bracketRoundConfig) []bracketRound {
	result := make([]bracketRound, 0, len(rounds))
	for _, round := range rounds {
		result = append(result, bracketRound{
			Name:    round.Name,
			Matches: buildPublicMatches(round.Matches, nil),
		})
	}
	return result
}

func buildPublicMatches(configured []scheduledMatch, live []publicMatch) []publicMatch {
	result := make([]publicMatch, 0, len(configured))
	for _, item := range configured {
		current, ok := findLiveMatch(item, live)
		if !ok {
			current = publicMatch{
				ID:      item.ID,
				Radiant: team{Name: item.TeamA},
				Dire:    team{Name: item.TeamB},
				State:   "scheduled",
				Score:   publicScore{},
			}
		}
		current.Stage = "series"
		current.Label = item.Label
		current.ScheduledAt = item.ScheduledAt
		current.BestOf = item.BestOf
		result = append(result, current)
	}
	return result
}

func findLiveMatch(item scheduledMatch, live []publicMatch) (publicMatch, bool) {
	left, right := teamKey(item.TeamA), teamKey(item.TeamB)
	for _, current := range live {
		radiant, dire := teamKey(current.Radiant.Name), teamKey(current.Dire.Name)
		if (radiant == left && dire == right) || (radiant == right && dire == left) {
			return current, true
		}
	}
	return publicMatch{}, false
}
