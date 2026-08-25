package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	dota2 "github.com/paralin/go-dota2"
	devents "github.com/paralin/go-dota2/events"
	"github.com/paralin/go-steam"
	"github.com/sirupsen/logrus"
)

const (
	serviceName         = "dota-gc-agent"
	displayName         = "Dota Game Coordinator Agent"
	gcHelloInterval     = 5 * time.Second
	gcConnectionTimeout = 90 * time.Second
)

var (
	version   = "dev"
	buildDate = "unknown"
	commit    = "unknown"
)

type credentials struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	AuthCode      string `json:"authCode,omitempty"`
	TwoFactorCode string `json:"twoFactorCode,omitempty"`
}

type persistedSession struct {
	AccessToken string `json:"accessToken"`
}

type gcSessionWatchdog struct {
	waitingSince time.Time
	timeout      time.Duration
}

func newGCSessionWatchdog(timeout time.Duration) *gcSessionWatchdog {
	return &gcSessionWatchdog{timeout: timeout}
}

func (w *gcSessionWatchdog) start(now time.Time) {
	if w.waitingSince.IsZero() {
		w.waitingSince = now
	}
}

func (w *gcSessionWatchdog) stop() {
	w.waitingSince = time.Time{}
}

func (w *gcSessionWatchdog) expired(now time.Time) bool {
	return !w.waitingSince.IsZero() && now.Sub(w.waitingSince) >= w.timeout
}

func (c credentials) validate() error {
	if strings.TrimSpace(c.Username) == "" {
		return errors.New("Steam username is missing")
	}
	if c.Password == "" {
		return errors.New("Steam password is missing")
	}
	if c.AuthCode != "" && c.TwoFactorCode != "" {
		return errors.New("only one Steam Guard code type may be supplied")
	}
	return nil
}

func loadCredentials(path string) (credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	var result credentials
	if err := json.Unmarshal(data, &result); err != nil {
		return credentials{}, fmt.Errorf("decode credentials: %w", err)
	}
	if err := result.validate(); err != nil {
		return credentials{}, err
	}
	return result, nil
}

func loadSessionToken(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var session persistedSession
	if err := json.Unmarshal(data, &session); err != nil {
		return ""
	}
	return strings.TrimSpace(session.AccessToken)
}

func saveSessionToken(path, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("Steam session token is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(persistedSession{AccessToken: token})
	if err != nil {
		return err
	}
	temporary := path + ".new"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Chmod(path, 0600)
}

type agentState struct {
	mu                        sync.RWMutex
	startedAt                 time.Time
	configured                bool
	steamConnected            bool
	steamLoggedOn             bool
	gcConnected               bool
	authState                 string
	lastSuccessAt             *time.Time
	lastErrorAt               *time.Time
	lastError                 string
	lastGCMessageAt           *time.Time
	reconnectCount            int
	gcRequestsTotal           int64
	gcRequestsFailed          int64
	lastHistoryRequestAt      *time.Time
	lastMatchDetailsRequestAt *time.Time
}

func (s *agentState) recordGCRequest(kind string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.gcRequestsTotal++
	if err != nil {
		s.gcRequestsFailed++
	} else {
		s.lastSuccessAt = &now
		s.lastGCMessageAt = &now
	}
	if kind == "history" {
		s.lastHistoryRequestAt = &now
	} else if kind == "match_details" {
		s.lastMatchDetailsRequestAt = &now
	}
}

func newAgentState() *agentState {
	return &agentState{startedAt: time.Now().UTC(), authState: "not_configured"}
}

func (s *agentState) setConfigured(configured bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configured = configured
	if configured {
		s.authState = "connecting"
	}
}

func (s *agentState) setSteamConnected(value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steamConnected = value
	if !value {
		s.steamLoggedOn = false
		s.gcConnected = false
	}
}

func (s *agentState) setSteamLoggedOn(value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steamLoggedOn = value
	if value {
		s.authState = "authenticated"
	}
}

func (s *agentState) setGCConnected(value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcConnected = value
	now := time.Now().UTC()
	if value {
		s.lastSuccessAt = &now
		s.lastGCMessageAt = &now
		s.lastError = ""
	}
}

func (s *agentState) markFailure(authState string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.lastErrorAt = &now
	s.lastError = sanitizeError(err)
	if authState != "" {
		s.authState = authState
	}
}

func (s *agentState) incrementReconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconnectCount++
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}

type versionInfo struct {
	Version        string `json:"version"`
	BuildDate      string `json:"buildDate"`
	APIVersion     string `json:"apiVersion"`
	SchemaVersion  string `json:"schemaVersion"`
	CommitHash     string `json:"commitHash"`
	DockerImageTag string `json:"dockerImageTag"`
}

type healthDetails struct {
	Configured                bool       `json:"configured"`
	SteamConnected            bool       `json:"steamConnected"`
	SteamLoggedOn             bool       `json:"steamLoggedOn"`
	GCConnected               bool       `json:"gcConnected"`
	AuthState                 string     `json:"authState"`
	ReconnectCount            int        `json:"reconnectCount"`
	LastGCMessageAt           *time.Time `json:"lastGcMessageAt"`
	OnlineTracking            bool       `json:"onlineTracking"`
	GCRequestsTotal           int64      `json:"gcRequestsTotal"`
	GCRequestsFailed          int64      `json:"gcRequestsFailed"`
	LastHistoryRequestAt      *time.Time `json:"lastHistoryRequestAt"`
	LastMatchDetailsRequestAt *time.Time `json:"lastMatchDetailsRequestAt"`
}

type healthResponse struct {
	OK                        bool          `json:"ok"`
	Service                   string        `json:"service"`
	DisplayName               string        `json:"displayName"`
	Status                    string        `json:"status"`
	ProcessRunning            bool          `json:"processRunning"`
	StartedAt                 time.Time     `json:"startedAt"`
	UptimeSeconds             int64         `json:"uptimeSeconds"`
	LastSuccessfulOperationAt *time.Time    `json:"lastSuccessfulOperationAt"`
	LastErrorAt               *time.Time    `json:"lastErrorAt"`
	LastError                 *string       `json:"lastError"`
	Version                   versionInfo   `json:"version"`
	Details                   healthDetails `json:"details"`
	At                        time.Time     `json:"at"`
	Time                      time.Time     `json:"time"`
}

func (s *agentState) snapshot() healthResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().UTC()
	status := "Degraded"
	if s.gcConnected {
		status = "Healthy"
	}
	var lastError *string
	if s.lastError != "" {
		value := s.lastError
		lastError = &value
	}
	return healthResponse{
		OK:                        s.gcConnected,
		Service:                   serviceName,
		DisplayName:               displayName,
		Status:                    status,
		ProcessRunning:            true,
		StartedAt:                 s.startedAt,
		UptimeSeconds:             int64(now.Sub(s.startedAt).Seconds()),
		LastSuccessfulOperationAt: s.lastSuccessAt,
		LastErrorAt:               s.lastErrorAt,
		LastError:                 lastError,
		Version: versionInfo{
			Version:        version,
			BuildDate:      buildDate,
			APIVersion:     "1",
			SchemaVersion:  "1",
			CommitHash:     commit,
			DockerImageTag: envOrDefault("DOTA_GC_IMAGE_TAG", "latest"),
		},
		Details: healthDetails{
			Configured:                s.configured,
			SteamConnected:            s.steamConnected,
			SteamLoggedOn:             s.steamLoggedOn,
			GCConnected:               s.gcConnected,
			AuthState:                 s.authState,
			ReconnectCount:            s.reconnectCount,
			LastGCMessageAt:           s.lastGCMessageAt,
			OnlineTracking:            false,
			GCRequestsTotal:           s.gcRequestsTotal,
			GCRequestsFailed:          s.gcRequestsFailed,
			LastHistoryRequestAt:      s.lastHistoryRequestAt,
			LastMatchDetailsRequestAt: s.lastMatchDetailsRequestAt,
		},
		At:   now,
		Time: now,
	}
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func healthHandler(state *agentState) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		response := state.snapshot()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if !response.OK {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(response)
	}
}

// statusHandler is private diagnostic output for the local setup helper.
// Unlike /api/health it deliberately stays 200 while Steam Guard is pending,
// so the helper can read authState without weakening the container healthcheck.
func statusHandler(state *agentState) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(state.snapshot())
	}
}

var errPermanentAuthentication = errors.New("authentication requires manual action")
var errSessionTokenRejected = errors.New("persisted Steam session was rejected")

func runSession(ctx context.Context, creds credentials, state *agentState, gateway *gcGateway, sessionPath string) error {
	client := steam.NewClient()
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	dota := dota2.New(client, logger)
	defer gateway.clearClient(dota)
	defer dota.Close()
	defer client.Disconnect()

	client.Connect()
	state.setSteamConnected(false)
	sessionToken := loadSessionToken(sessionPath)
	usingSessionToken := sessionToken != ""

	helloTicker := time.NewTicker(gcHelloInterval)
	defer helloTicker.Stop()
	gcWatchdog := newGCSessionWatchdog(gcConnectionTimeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-helloTicker.C:
			snapshot := state.snapshot()
			if snapshot.Details.SteamLoggedOn && !snapshot.Details.GCConnected {
				gcWatchdog.start(now)
				if gcWatchdog.expired(now) {
					state.setSteamConnected(false)
					return fmt.Errorf("Dota GC handshake timed out after %s", gcConnectionTimeout)
				}
				dota.SayHello()
			} else {
				gcWatchdog.stop()
			}
		case event, ok := <-client.Events():
			if !ok {
				state.setSteamConnected(false)
				return errors.New("Steam event stream closed")
			}
			switch value := event.(type) {
			case *steam.ConnectedEvent:
				state.setSteamConnected(true)
				go func() {
					authCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
					defer cancel()
					details := &steam.LogOnDetails{
						Username:               creds.Username,
						Password:               creds.Password,
						AuthCode:               creds.AuthCode,
						TwoFactorCode:          creds.TwoFactorCode,
						DeviceFriendlyName:     "Salfetka Dota GC Agent",
						ShouldRememberPassword: true,
					}
					if usingSessionToken {
						details.Password = ""
						details.AuthCode = ""
						details.TwoFactorCode = ""
						details.AccessToken = sessionToken
					}
					if err := client.Auth.LogOn(authCtx, details); err == nil && details.AccessToken != "" && !usingSessionToken {
						if saveSessionToken(sessionPath, details.AccessToken) != nil {
							log.Printf("Steam session could not be persisted")
						}
					}
				}()
			case *steam.LoggedOnEvent:
				state.setSteamLoggedOn(true)
				gcWatchdog.start(time.Now())
				dota.SetPlaying(true)
				dota.SayHello()
			case *steam.LogOnFailedEvent:
				authState := string(value.AuthSessionState)
				if authState == "" {
					authState = "denied"
				}
				err := value.Err
				if err == nil {
					err = fmt.Errorf("Steam logon failed: %s", value.Result.String())
				}
				state.markFailure(authState, err)
				if usingSessionToken {
					_ = os.Remove(sessionPath)
					return fmt.Errorf("%w: %v", errSessionTokenRejected, err)
				}
				return fmt.Errorf("%w: %v", errPermanentAuthentication, err)
			case *steam.LoggedOffEvent:
				state.setSteamConnected(false)
				return fmt.Errorf("Steam logged off: %s", value.Result.String())
			case *steam.DisconnectedEvent:
				state.setSteamConnected(false)
				return errors.New("Steam connection closed")
			case steam.FatalErrorEvent:
				state.setSteamConnected(false)
				return fmt.Errorf("Steam connection failed: %v", error(value))
			case *devents.ClientWelcomed:
				state.setGCConnected(true)
				gcWatchdog.stop()
				gateway.setClient(dota)
				log.Printf("Dota GC session established")
			case *devents.GCConnectionStatusChanged:
				if strings.Contains(value.NewState.String(), "HAVE_SESSION") {
					state.setGCConnected(true)
					gcWatchdog.stop()
					gateway.setClient(dota)
				} else {
					state.setGCConnected(false)
					gcWatchdog.start(time.Now())
					gateway.clearClient(dota)
				}
			case error:
				state.markFailure("", value)
			}
		}
	}
}

func runAgent(ctx context.Context, creds credentials, state *agentState, gateway *gcGateway, sessionPath string) {
	backoff := 5 * time.Second
	for {
		err := runSession(ctx, creds, state, gateway, sessionPath)
		if errors.Is(err, context.Canceled) {
			return
		}
		if errors.Is(err, errPermanentAuthentication) {
			log.Printf("Steam authentication requires updated credentials or confirmation")
			return
		}
		if errors.Is(err, errSessionTokenRejected) {
			state.markFailure("reconnecting", err)
			log.Printf("Stored Steam session expired; retrying with credentials")
			continue
		}
		state.markFailure("reconnecting", err)
		state.incrementReconnect()
		log.Printf("Steam connection interrupted; retrying in %s", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 2*time.Minute {
			backoff *= 2
		}
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	state := newAgentState()
	gateway := newGCGateway(state)
	secretPath := envOrDefault("DOTA_GC_CREDENTIALS_FILE", "/run/secrets/dota_gc_credentials")
	sessionPath := envOrDefault("DOTA_GC_SESSION_FILE", "/var/lib/dota-gc/session.json")
	creds, err := loadCredentials(secretPath)
	if err != nil {
		state.markFailure("not_configured", err)
		log.Printf("Dota GC credentials are not configured")
	} else {
		state.setConfigured(true)
		go runAgent(ctx, creds, state, gateway, sessionPath)
		go runLiveReporter(ctx)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", healthHandler(state))
	mux.HandleFunc("GET /api/status", statusHandler(state))
	mux.HandleFunc("GET /api/players/{accountID}/matches", gateway.playerHistoryHandler)
	mux.HandleFunc("GET /api/matches/{matchID}", gateway.matchDetailsHandler)
	mux.HandleFunc("GET /api/live/games", gateway.liveGamesHandler)
	server := &http.Server{
		Addr:              envOrDefault("DOTA_GC_LISTEN", "0.0.0.0:8788"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("Dota GC agent listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
