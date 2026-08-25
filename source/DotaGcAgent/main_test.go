package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPersistedSessionRoundTripUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	if err := saveSessionToken(path, "test-token"); err != nil {
		t.Fatal(err)
	}
	if got := loadSessionToken(path); got != "test-token" {
		t.Fatalf("token = %q, want test-token", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestCredentialsValidation(t *testing.T) {
	tests := []struct {
		name    string
		value   credentials
		wantErr bool
	}{
		{name: "valid", value: credentials{Username: "gc-user", Password: "secret"}},
		{name: "missing username", value: credentials{Password: "secret"}, wantErr: true},
		{name: "missing password", value: credentials{Username: "gc-user"}, wantErr: true},
		{name: "two guard codes", value: credentials{Username: "gc-user", Password: "secret", AuthCode: "A", TwoFactorCode: "B"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.value.validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestHealthBecomesHealthyOnlyAfterGCWelcome(t *testing.T) {
	state := newAgentState()
	state.setConfigured(true)
	state.setSteamConnected(true)
	state.setSteamLoggedOn(true)
	if state.snapshot().OK {
		t.Fatal("health must stay degraded before GC welcome")
	}
	state.setGCConnected(true)
	if snapshot := state.snapshot(); !snapshot.OK || snapshot.Status != "Healthy" {
		t.Fatalf("unexpected healthy snapshot: %+v", snapshot)
	}
}

func TestGCSessionWatchdogExpiresAndResets(t *testing.T) {
	timeout := 90 * time.Second
	startedAt := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	watchdog := newGCSessionWatchdog(timeout)

	watchdog.start(startedAt)
	if watchdog.expired(startedAt.Add(timeout - time.Second)) {
		t.Fatal("watchdog expired before the timeout")
	}
	if !watchdog.expired(startedAt.Add(timeout)) {
		t.Fatal("watchdog did not expire at the timeout")
	}

	watchdog.stop()
	if watchdog.expired(startedAt.Add(2 * timeout)) {
		t.Fatal("stopped watchdog must not remain expired")
	}
}

func TestSanitizeErrorLimitsLength(t *testing.T) {
	long := make([]byte, 600)
	for index := range long {
		long[index] = 'x'
	}
	if got := sanitizeError(errors.New(string(long))); len(got) != 500 {
		t.Fatalf("sanitizeError length = %d, want 500", len(got))
	}
}
