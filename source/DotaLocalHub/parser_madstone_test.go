package main

import (
	"os"
	"testing"
)

func TestMadstoneUsesBundlePicker(t *testing.T) {
	path := os.Getenv("DOTA_TEST_REPLAY")
	if path == "" {
		t.Skip("DOTA_TEST_REPLAY is not set")
	}
	match, err := parseReplay(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(match.Players) != 10 {
		t.Fatalf("expected 10 players, got %d", len(match.Players))
	}

	unique := map[int32]struct{}{}
	var total int32
	for _, player := range match.Players {
		unique[player.Madstone] = struct{}{}
		total += player.Madstone
	}
	if total == 0 {
		t.Fatal("expected at least one picked Madstone Bundle")
	}
	if len(unique) == 1 {
		t.Fatalf("all players have the same pickup count: %v", unique)
	}
}
