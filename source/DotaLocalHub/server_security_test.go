package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestServiceStatusEndpointsDoNotCreateUserSessions(t *testing.T) {
	for _, path := range []string{"/api/health", "/api/gc-monitor"} {
		if !isServiceStatusEndpoint(path) {
			t.Fatalf("%s must be treated as a service status endpoint", path)
		}
	}
	if isServiceStatusEndpoint("/api/matches") {
		t.Fatal("user API must not be treated as a service status endpoint")
	}
}

func TestReplayUploadExtension(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
		ext  string
	}{
		{name: "8871520485.dem", ok: true, ext: ".dem"},
		{name: "8871520485.dem.bz2", ok: true, ext: ".bz2"},
		{name: "match.bz2", ok: false},
		{name: "../8871520485.dem", ok: true, ext: ".dem"},
		{name: "match.zip", ok: false},
	}
	for _, test := range tests {
		ext, ok := replayUploadExtension(test.name)
		if ok != test.ok || ext != test.ext {
			t.Fatalf("replayUploadExtension(%q) = %q, %v; want %q, %v", test.name, ext, ok, test.ext, test.ok)
		}
	}
}

func TestValidateReplayUploadFilename(t *testing.T) {
	name, extension, matchID, err := validateReplayUploadFilename("8871520485.dem.bz2")
	if err != nil {
		t.Fatal(err)
	}
	if name != "8871520485.dem.bz2" || extension != ".bz2" || matchID != 8871520485 {
		t.Fatalf("unexpected validation result: %q %q %d", name, extension, matchID)
	}

	blocked := []string{
		`..\folder\8871520485.dem`,
		"replay.dem",
		"8871520485-copy.dem",
		"8871520485.bz2",
		"123.dem",
	}
	for _, name := range blocked {
		if _, _, _, err := validateReplayUploadFilename(name); err == nil {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
}

func TestAllowedOrigin(t *testing.T) {
	server := &Server{config: Config{AllowedOrigins: []string{"https://trusted.example"}}}
	allowed := []string{
		"http://127.0.0.1:8787",
		"http://localhost:8787",
		"http://[::1]:8787",
		"https://trusted.example",
	}
	for _, origin := range allowed {
		if !server.allowedOrigin(origin) {
			t.Fatalf("expected %s to be allowed", origin)
		}
	}
	blocked := []string{
		"https://evil.github.io",
		"https://127.0.0.1.evil.example",
		"file://local",
		"null",
	}
	for _, origin := range blocked {
		if server.allowedOrigin(origin) {
			t.Fatalf("expected %s to be blocked", origin)
		}
	}
}

func TestLimitedWriterRejectsOversizeReplay(t *testing.T) {
	var buffer bytes.Buffer
	writer := &limitedWriter{writer: &buffer, limit: 4}
	if _, err := writer.Write([]byte("test")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("x")); !errors.Is(err, errReplayTooLarge) {
		t.Fatalf("expected errReplayTooLarge, got %v", err)
	}
	if buffer.String() != "test" {
		t.Fatalf("limited writer wrote unexpected data %q", buffer.String())
	}
}
