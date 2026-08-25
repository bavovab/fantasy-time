package main

import (
	"context"
	"os"
	"testing"
)

func TestPlayerPortrait(t *testing.T) {
	if os.Getenv("DOTA_TEST_PORTRAIT") == "" {
		t.Skip("set DOTA_TEST_PORTRAIT=1 for the network portrait test")
	}
	value, err := newDownloader(Config{}).PlayerPortrait(context.Background(), "Pure")
	if err != nil {
		t.Fatal(err)
	}
	if value == "" {
		t.Fatal("empty portrait URL")
	}
	t.Log(value)
}
