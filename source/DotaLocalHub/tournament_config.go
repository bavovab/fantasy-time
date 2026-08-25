package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type TournamentConfig struct {
	Slug                string           `json:"slug"`
	Name                string           `json:"name"`
	ReplaceableTBDSlots []int            `json:"replaceableTbdSlots"`
	Teams               []TournamentTeam `json:"teams"`
}

func loadTournamentConfig(root string) (TournamentConfig, error) {
	path := tournamentConfigPath(root)
	file, err := os.Open(path)
	if err != nil {
		return TournamentConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	defer file.Close()

	var config TournamentConfig
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return TournamentConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateTournamentConfig(config); err != nil {
		return TournamentConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	return config, nil
}

func tournamentConfigPath(root string) string {
	return filepath.Join(root, "data", "tournaments", "ti2026.json")
}

func validateTournamentConfig(config TournamentConfig) error {
	if config.Slug == "" {
		return fmt.Errorf("tournament slug is empty")
	}
	if config.Name == "" {
		return fmt.Errorf("tournament name is empty")
	}
	if len(config.Teams) != 16 {
		return fmt.Errorf("tournament must have exactly 16 slots, got %d", len(config.Teams))
	}

	replaceable := make(map[int]bool, len(config.ReplaceableTBDSlots))
	for _, slot := range config.ReplaceableTBDSlots {
		if slot < 1 || slot > 16 {
			return fmt.Errorf("replaceable TBD slot %d is outside 1..16", slot)
		}
		replaceable[slot] = true
	}

	seenSlugs := make(map[string]struct{}, len(config.Teams))
	seenSlots := make(map[int]string, len(config.Teams))
	seenGlobalPlayers := make(map[uint32]string)
	for _, team := range config.Teams {
		if team.Slug == "" {
			return fmt.Errorf("team with empty slug at slot %d", team.SlotOrder)
		}
		if team.Name == "" {
			return fmt.Errorf("team %s has empty name", team.Slug)
		}
		if _, exists := seenSlugs[team.Slug]; exists {
			return fmt.Errorf("duplicate team slug %q", team.Slug)
		}
		seenSlugs[team.Slug] = struct{}{}
		if team.SlotOrder < 1 || team.SlotOrder > 16 {
			return fmt.Errorf("team %s has slot %d outside 1..16", team.Slug, team.SlotOrder)
		}
		if previous, exists := seenSlots[team.SlotOrder]; exists {
			return fmt.Errorf("duplicate slot %d: %s and %s", team.SlotOrder, previous, team.Slug)
		}
		seenSlots[team.SlotOrder] = team.Slug
		if team.Status != "invited" && team.Status != "qualifier" && team.Status != "tbd" {
			return fmt.Errorf("team %s has unsupported status %q", team.Slug, team.Status)
		}
		if team.Status == "tbd" {
			if len(team.Roster) != 0 {
				return fmt.Errorf("TBD team %s must not have roster", team.Slug)
			}
			if !replaceable[team.SlotOrder] {
				return fmt.Errorf("TBD team %s at slot %d is not listed in replaceableTbdSlots", team.Slug, team.SlotOrder)
			}
			continue
		}
		if len(team.Roster) != 5 {
			return fmt.Errorf("team %s must have exactly 5 players, got %d", team.Slug, len(team.Roster))
		}
		seenPositions := make(map[int]uint32, 5)
		seenTeamPlayers := make(map[uint32]struct{}, 5)
		for _, player := range team.Roster {
			if player.AccountID == 0 {
				return fmt.Errorf("team %s has player with empty accountId", team.Slug)
			}
			if player.Name == "" {
				return fmt.Errorf("team %s has player %d with empty name", team.Slug, player.AccountID)
			}
			if player.Position < 1 || player.Position > 5 {
				return fmt.Errorf("team %s player %s has position %d outside 1..5", team.Slug, player.Name, player.Position)
			}
			if previous, exists := seenPositions[player.Position]; exists {
				return fmt.Errorf("team %s has duplicate position %d for %d and %d", team.Slug, player.Position, previous, player.AccountID)
			}
			seenPositions[player.Position] = player.AccountID
			if _, exists := seenTeamPlayers[player.AccountID]; exists {
				return fmt.Errorf("team %s has duplicate player accountId %d", team.Slug, player.AccountID)
			}
			seenTeamPlayers[player.AccountID] = struct{}{}
			if previousTeam, exists := seenGlobalPlayers[player.AccountID]; exists {
				return fmt.Errorf("player accountId %d is listed in both %s and %s", player.AccountID, previousTeam, team.Slug)
			}
			seenGlobalPlayers[player.AccountID] = team.Slug
		}
	}

	for slot := 1; slot <= 16; slot++ {
		if _, exists := seenSlots[slot]; !exists {
			return fmt.Errorf("slot %d is missing", slot)
		}
	}
	return nil
}

func replaceableTBDSlotSet(config TournamentConfig) map[int]bool {
	result := make(map[int]bool, len(config.ReplaceableTBDSlots))
	for _, slot := range config.ReplaceableTBDSlots {
		result[slot] = true
	}
	return result
}
