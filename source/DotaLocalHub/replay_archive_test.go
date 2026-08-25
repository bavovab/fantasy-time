package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestDecompressReplaySupportsZstandard(t *testing.T) {
	payload := []byte("PBDEMS2 test replay payload")
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	archive := encoder.EncodeAll(payload, nil)
	encoder.Close()

	assertReplayDecompresses(t, archive, payload, 1<<20)
}

func TestDecompressReplaySupportsLegacyBZip2(t *testing.T) {
	archive := mustDecodeBase64(t, "QlpoOTFBWSZTWdmS0PYAABN9/oQCAxAJHB4oDhAOBCgBCZIQCUgGwBEAAucIBkAgAFRgNAAANAAAAPKDAAADIADTQDJkBJJw63qSgNMIygatKPaYG+4b+BYHJ8c2RRDXOh4SMINCG2PwMfY5k6D0AFH78XckU4UJDZktD2A=")
	payload := mustDecodeBase64(t, "ktVlJhasREpKBK8aijlkrKBFDUPWzyM70DIz9LqS+HGebCor1PX4jbB+zQ2jozsmNIPbmywVh4atY2O+NdFzNbo=")

	assertReplayDecompresses(t, archive, payload, 1<<20)
}

func TestDecompressReplayRejectsUnknownCompression(t *testing.T) {
	err := decompressReplayFixture(t, []byte("not a replay archive"), 1<<20)
	if !errors.Is(err, errUnsupportedReplayArchive) {
		t.Fatalf("got %v, want unsupported archive error", err)
	}
}

func TestDecompressReplayRejectsTruncatedZstandard(t *testing.T) {
	err := decompressReplayFixture(t, append([]byte{}, zstdReplayMagic...), 1<<20)
	if !errors.Is(err, errInvalidReplayArchive) {
		t.Fatalf("got %v, want invalid archive error", err)
	}
}

func TestDecompressReplayKeepsOutputLimit(t *testing.T) {
	payload := make([]byte, 1024)
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	archive := encoder.EncodeAll(payload, nil)
	encoder.Close()

	err = decompressReplayFixture(t, archive, 32)
	if !errors.Is(err, errReplayTooLarge) {
		t.Fatalf("got %v, want replay size error", err)
	}
}

func TestRetryableParseErrorSeparatesTransportAndArchiveFailures(t *testing.T) {
	if !retryableParseError(&replayHTTPError{URL: "https://replay", StatusCode: http.StatusBadGateway}) {
		t.Fatal("Valve HTTP 502 must be retryable")
	}
	if retryableParseError(errInvalidReplayArchive) {
		t.Fatal("invalid replay archive must not be retried")
	}
	if retryableParseError(errUnsupportedReplayArchive) {
		t.Fatal("unknown replay compression must not be retried")
	}
}

func TestParseRetryBackoffStopsAtBound(t *testing.T) {
	now := time.Now().Unix()
	if next := nextParseRetryAt(1, now, now); next-now != int64((5 * time.Minute).Seconds()) {
		t.Fatalf("first retry delay = %d seconds", next-now)
	}
	if next := nextParseRetryAt(maxParseRetryAttempts, now, now); next != 0 {
		t.Fatalf("retry %d must stop, got %d", maxParseRetryAttempts, next)
	}
	if next := nextParseRetryAt(2, now-int64((25*time.Hour).Seconds()), now); next != 0 {
		t.Fatalf("retry older than 24 hours must stop, got %d", next)
	}
}

func TestValveZstdMigrationRequeuesOnlyAffectedMatchesOnce(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := &Store{db: database}
	if _, err := database.Exec(`
CREATE TABLE maintenance_migrations (name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL);
CREATE TABLE team_matches (
    team_slug TEXT NOT NULL,
    match_id INTEGER NOT NULL,
    league_id INTEGER NOT NULL,
    parse_status TEXT NOT NULL,
    parse_error TEXT NOT NULL,
    PRIMARY KEY (team_slug, match_id)
);
CREATE TABLE parse_retries (
    user_id TEXT NOT NULL,
    match_id INTEGER NOT NULL,
    PRIMARY KEY (user_id, match_id)
);
INSERT INTO team_matches VALUES
    ('one', 11, 20009, 'error', 'bzip2 data invalid: bad magic value'),
    ('two', 12, 19917, 'error', 'Valve returned HTTP 502'),
    ('other', 13, 12345, 'error', 'bzip2 data invalid: bad magic value');
INSERT INTO parse_retries VALUES ('', 11), ('', 12), ('', 13);`); err != nil {
		t.Fatal(err)
	}

	if err := store.requeueValveZstdFailures(); err != nil {
		t.Fatal(err)
	}
	assertTeamMatchStatus(t, database, 11, "pending")
	assertTeamMatchStatus(t, database, 12, "pending")
	assertTeamMatchStatus(t, database, 13, "error")
	var retryCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM parse_retries`).Scan(&retryCount); err != nil {
		t.Fatal(err)
	}
	if retryCount != 1 {
		t.Fatalf("remaining retries = %d, want 1", retryCount)
	}

	if _, err := database.Exec(`UPDATE team_matches SET parse_status = 'error' WHERE match_id = 11`); err != nil {
		t.Fatal(err)
	}
	if err := store.requeueValveZstdFailures(); err != nil {
		t.Fatal(err)
	}
	assertTeamMatchStatus(t, database, 11, "error")
}

func assertTeamMatchStatus(t *testing.T, database *sql.DB, matchID int64, want string) {
	t.Helper()
	var got string
	if err := database.QueryRowContext(context.Background(), `SELECT parse_status FROM team_matches WHERE match_id = ?`, matchID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("match %d status = %q, want %q", matchID, got, want)
	}
}

func assertReplayDecompresses(t *testing.T, archive, payload []byte, maxBytes int64) {
	t.Helper()
	directory := t.TempDir()
	source := filepath.Join(directory, "replay.dem.bz2")
	destination := filepath.Join(directory, "replay.dem")
	if err := os.WriteFile(source, archive, 0600); err != nil {
		t.Fatal(err)
	}
	if err := decompressReplay(source, destination, maxBytes); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(payload) {
		t.Fatalf("decompressed payload differs: got %d bytes, want %d", len(actual), len(payload))
	}
}

func decompressReplayFixture(t *testing.T, archive []byte, maxBytes int64) error {
	t.Helper()
	directory := t.TempDir()
	source := filepath.Join(directory, "replay.dem.bz2")
	if err := os.WriteFile(source, archive, 0600); err != nil {
		t.Fatal(err)
	}
	return decompressReplay(source, filepath.Join(directory, "replay.dem"), maxBytes)
}

func mustDecodeBase64(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
