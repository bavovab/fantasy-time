package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDirectoryKeepsGitMetadataAndReplacesPublishedFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "site")
	destination := filepath.Join(root, "repository")
	mustWritePublisherTest(t, filepath.Join(source, "index.html"), "new")
	mustWritePublisherTest(t, filepath.Join(source, "api", "teams.json"), "[]")
	mustWritePublisherTest(t, filepath.Join(destination, ".git", "config"), "git")
	mustWritePublisherTest(t, filepath.Join(destination, "obsolete.txt"), "old")

	if err := syncDirectory(source, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, ".git", "config")); err != nil {
		t.Fatal("git metadata was removed")
	}
	if _, err := os.Stat(filepath.Join(destination, "obsolete.txt")); !os.IsNotExist(err) {
		t.Fatal("obsolete published file was not removed")
	}
	data, err := os.ReadFile(filepath.Join(destination, "index.html"))
	if err != nil || string(data) != "new" {
		t.Fatalf("published file was not copied: %q %v", data, err)
	}
}

func TestSyncDirectoryRejectsNonRepository(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "site")
	destination := filepath.Join(root, "plain-directory")
	mustWritePublisherTest(t, filepath.Join(source, "index.html"), "site")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syncDirectory(source, destination); err == nil {
		t.Fatal("sync into a non-repository must be rejected")
	}
}

func mustWritePublisherTest(t *testing.T, filename, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}
