package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"salfetka-hub/dota-public/internal/publicdata"
	"salfetka-hub/dota-public/internal/staticsite"
)

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type config struct {
	baseWebRoot   string
	publicWebRoot string
	snapshotRoot  string
	liveRoot      string
	workRoot      string
	repository    string
	branch        string
	domain        string
	keyFile       string
	knownHosts    string
	checkInterval time.Duration
	minInterval   time.Duration
	version       string
}

type publisherState struct {
	Fingerprint          string    `json:"fingerprint"`
	Release              string    `json:"release"`
	Commit               string    `json:"commit,omitempty"`
	LastSuccessfulPushAt time.Time `json:"lastSuccessfulPushAt"`
}

type healthStatus struct {
	Status                    string         `json:"status"`
	LastCheckedAt             string         `json:"lastCheckedAt"`
	LastSuccessfulOperationAt string         `json:"lastSuccessfulOperationAt,omitempty"`
	LastErrorAt               string         `json:"lastErrorAt,omitempty"`
	LastError                 string         `json:"lastError,omitempty"`
	Version                   string         `json:"version"`
	Details                   map[string]any `json:"details"`
}

type publisher struct {
	config config
	logger *log.Logger
	now    func() time.Time
	gitEnv []string
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	worker := &publisher{config: cfg, logger: log.New(os.Stdout, "github-publisher: ", log.LstdFlags|log.LUTC), now: time.Now}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(cfg); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "build-only" {
		result, err := worker.buildSite()
		if err != nil {
			log.Fatal(err)
		}
		worker.logger.Printf("static site validation complete release=%s files=%d bytes=%d", result.Release, result.FileCount, result.SizeBytes)
		return
	}
	if err := worker.prepareSSH(); err != nil {
		log.Fatal(err)
	}
	if len(os.Args) > 1 && os.Args[1] == "publish-once" {
		if err := worker.process(true); err != nil {
			log.Fatal(err)
		}
		return
	}

	for {
		if err := worker.process(false); err != nil {
			worker.logger.Printf("publication failed: %v", err)
		}
		time.Sleep(cfg.checkInterval)
	}
}

func loadConfig() (config, error) {
	checkInterval, err := time.ParseDuration(envOr("DOTA_PUBLIC_GITHUB_CHECK_INTERVAL", "1m"))
	if err != nil || checkInterval < 30*time.Second {
		return config{}, errors.New("DOTA_PUBLIC_GITHUB_CHECK_INTERVAL must be at least 30s")
	}
	minInterval, err := time.ParseDuration(envOr("DOTA_PUBLIC_GITHUB_MIN_INTERVAL", "10m"))
	if err != nil || minInterval < 6*time.Minute {
		return config{}, errors.New("DOTA_PUBLIC_GITHUB_MIN_INTERVAL must be at least 6m")
	}
	cfg := config{
		baseWebRoot:   envOr("DOTA_PUBLIC_BASE_WEB_ROOT", "/app/web-base"),
		publicWebRoot: envOr("DOTA_PUBLIC_WEB_ROOT", "/app/web-public"),
		snapshotRoot:  envOr("DOTA_PUBLIC_SNAPSHOT_DIR", "/snapshot"),
		liveRoot:      envOr("DOTA_PUBLIC_LIVE_DIR", "/live"),
		workRoot:      envOr("DOTA_PUBLIC_GITHUB_WORK_DIR", "/work"),
		repository:    strings.TrimSpace(envOr("DOTA_PUBLIC_GITHUB_REPOSITORY", "bavovab/fantasy-time")),
		branch:        strings.TrimSpace(envOr("DOTA_PUBLIC_GITHUB_BRANCH", "main")),
		domain:        strings.TrimSpace(envOr("DOTA_PUBLIC_GITHUB_DOMAIN", "fantasy-time.online")),
		keyFile:       envOr("DOTA_PUBLIC_GITHUB_KEY_FILE", "/run/secrets/dota_public_github_deploy_key"),
		knownHosts:    envOr("DOTA_PUBLIC_GITHUB_KNOWN_HOSTS", "/app/github-known-hosts"),
		checkInterval: checkInterval, minInterval: minInterval,
		version: envOr("DOTA_PUBLIC_GITHUB_VERSION", "1.0.0"),
	}
	if !repositoryPattern.MatchString(cfg.repository) {
		return config{}, errors.New("DOTA_PUBLIC_GITHUB_REPOSITORY must be owner/name")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._/-]+$`).MatchString(cfg.branch) || strings.Contains(cfg.branch, "..") {
		return config{}, errors.New("invalid GitHub branch")
	}
	return cfg, nil
}

func (worker *publisher) process(force bool) error {
	now := worker.now().UTC()
	fingerprint, release, err := worker.sourceFingerprint()
	if err != nil {
		worker.writeHealth("Degraded", publisherState{}, release, false, err)
		return err
	}
	state, _ := readState(worker.statePath())
	repositoryReady := directoryExists(filepath.Join(worker.repoPath(), ".git"))
	if !force && repositoryReady && state.Fingerprint == fingerprint {
		worker.writeHealth("Healthy", state, release, false, nil)
		return nil
	}
	if !force && !state.LastSuccessfulPushAt.IsZero() && now.Sub(state.LastSuccessfulPushAt) < worker.config.minInterval {
		worker.writeHealth("Healthy", state, release, true, nil)
		return nil
	}

	result, err := worker.buildSite()
	if err != nil {
		worker.writeHealth("Degraded", state, release, true, err)
		return err
	}
	commit, changed, err := worker.publish(result.Release)
	if err != nil {
		worker.writeHealth("Degraded", state, release, true, err)
		return err
	}
	state = publisherState{
		Fingerprint: fingerprint, Release: result.Release, Commit: commit,
		LastSuccessfulPushAt: now,
	}
	if err := writeJSONAtomic(worker.statePath(), state); err != nil {
		return err
	}
	worker.writeHealth("Healthy", state, result.Release, false, nil)
	worker.logger.Printf("publication complete release=%s changed=%t commit=%s files=%d bytes=%d", result.Release, changed, commit, result.FileCount, result.SizeBytes)
	return nil
}

func (worker *publisher) buildSite() (staticsite.Result, error) {
	return staticsite.Build(staticsite.Config{
		BaseWebRoot: worker.config.baseWebRoot, PublicWebRoot: worker.config.publicWebRoot,
		SnapshotRoot: worker.config.snapshotRoot, LiveRoot: worker.config.liveRoot,
		OutputRoot: worker.sitePath(), Domain: worker.config.domain, Now: worker.now,
	})
}

func (worker *publisher) sourceFingerprint() (string, string, error) {
	releaseRoot, err := publicdata.ReadReleaseRoot(worker.config.snapshotRoot)
	if err != nil {
		return "", "", err
	}
	release := filepath.Base(releaseRoot)
	var exporterState struct {
		Fingerprint string `json:"fingerprint"`
	}
	data, err := os.ReadFile(filepath.Join(worker.config.snapshotRoot, "exporter-state.json"))
	if err != nil {
		return "", release, err
	}
	if err := json.Unmarshal(data, &exporterState); err != nil || strings.TrimSpace(exporterState.Fingerprint) == "" {
		return "", release, errors.New("exporter fingerprint is unavailable")
	}
	hash := sha256.New()
	_, _ = io.WriteString(hash, exporterState.Fingerprint+"\n"+worker.config.domain+"\n"+worker.config.version+"\n")
	for _, root := range []string{worker.config.baseWebRoot, worker.config.publicWebRoot} {
		if err := hashTree(hash, root); err != nil {
			return "", release, err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), release, nil
}

func hashTree(target io.Writer, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link in public web root: %s", path)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(target, filepath.ToSlash(relative)+"\x00")
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(target, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func (worker *publisher) prepareSSH() error {
	privateKey, err := os.ReadFile(worker.config.keyFile)
	if err != nil || len(privateKey) < 64 {
		return errors.New("GitHub deploy key is unavailable")
	}
	if _, err := os.Stat(worker.config.knownHosts); err != nil {
		return errors.New("GitHub known_hosts file is unavailable")
	}
	keyPath := filepath.Join(os.TempDir(), "fantasy-time-github-key")
	if err := os.WriteFile(keyPath, privateKey, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		return err
	}
	sshCommand := fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=%s -o ConnectTimeout=15", shellQuote(keyPath), shellQuote(worker.config.knownHosts))
	worker.gitEnv = append(os.Environ(), "GIT_SSH_COMMAND="+sshCommand, "GIT_TERMINAL_PROMPT=0")
	return nil
}

func (worker *publisher) publish(release string) (string, bool, error) {
	if err := worker.prepareRepository(); err != nil {
		return "", false, err
	}
	if err := syncDirectory(worker.sitePath(), worker.repoPath()); err != nil {
		return "", false, err
	}
	if _, err := worker.git("add", "-A"); err != nil {
		return "", false, err
	}
	status, err := worker.git("status", "--porcelain")
	if err != nil {
		return "", false, err
	}
	changed := strings.TrimSpace(status) != ""
	if changed {
		message := fmt.Sprintf("Publish Fantasy Time snapshot %s", release)
		if _, err := worker.git("commit", "-m", message); err != nil {
			return "", false, err
		}
		if _, err := worker.git("push", "--set-upstream", "origin", "HEAD:refs/heads/"+worker.config.branch); err != nil {
			return "", false, err
		}
	}
	commit, err := worker.git("rev-parse", "HEAD")
	return strings.TrimSpace(commit), changed, err
}

func (worker *publisher) prepareRepository() error {
	repo := worker.repoPath()
	if err := os.MkdirAll(repo, 0o750); err != nil {
		return err
	}
	if !directoryExists(filepath.Join(repo, ".git")) {
		if _, err := worker.git("init", "-b", worker.config.branch); err != nil {
			return err
		}
	}
	remote := "ssh://git@ssh.github.com:443/" + worker.config.repository + ".git"
	if _, err := worker.git("remote", "get-url", "origin"); err == nil {
		if _, err := worker.git("remote", "set-url", "origin", remote); err != nil {
			return err
		}
	} else if _, err := worker.git("remote", "add", "origin", remote); err != nil {
		return err
	}
	for _, pair := range [][2]string{{"user.name", "Fantasy Time Publisher"}, {"user.email", "publisher@fantasy-time.online"}, {"commit.gpgsign", "false"}} {
		if _, err := worker.git("config", pair[0], pair[1]); err != nil {
			return err
		}
	}

	if _, err := worker.git("fetch", "--depth=1", "origin", worker.config.branch); err == nil {
		if _, err := worker.git("checkout", "-B", worker.config.branch, "FETCH_HEAD"); err != nil {
			return err
		}
		return nil
	}
	remoteRefs, err := worker.git("ls-remote", "origin")
	if err != nil {
		return fmt.Errorf("GitHub repository is not reachable: %w", err)
	}
	if strings.TrimSpace(remoteRefs) != "" {
		return fmt.Errorf("branch %s is unavailable in non-empty repository", worker.config.branch)
	}
	if _, err := worker.git("checkout", "-B", worker.config.branch); err != nil {
		return err
	}
	return nil
}

func (worker *publisher) git(arguments ...string) (string, error) {
	commandArgs := append([]string{"-C", worker.repoPath()}, arguments...)
	command := exec.Command("git", commandArgs...)
	command.Env = worker.gitEnv
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 500 {
			message = message[:500]
		}
		return "", fmt.Errorf("git %s failed: %s", arguments[0], message)
	}
	return string(output), nil
}

func syncDirectory(source, destination string) error {
	if !directoryExists(filepath.Join(destination, ".git")) {
		return errors.New("refusing to sync into a directory without .git")
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(destination, entry.Name())); err != nil {
			return err
		}
	}
	return copyAll(source, destination)
}

func copyAll(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link in generated site: %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		input.Close()
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func (worker *publisher) writeHealth(status string, state publisherState, release string, pending bool, failure error) {
	now := worker.now().UTC()
	health := healthStatus{
		Status: status, LastCheckedAt: now.Format(time.RFC3339), Version: worker.config.version,
		Details: map[string]any{"repository": worker.config.repository, "branch": worker.config.branch, "release": release, "pending": pending},
	}
	if !state.LastSuccessfulPushAt.IsZero() {
		health.LastSuccessfulOperationAt = state.LastSuccessfulPushAt.UTC().Format(time.RFC3339)
		health.Details["commit"] = state.Commit
	}
	if failure != nil {
		health.LastErrorAt = now.Format(time.RFC3339)
		health.LastError = failure.Error()
	}
	if err := writeJSONAtomic(worker.healthPath(), health); err != nil {
		worker.logger.Printf("write health failed: %v", err)
	}
}

func healthcheck(cfg config) error {
	data, err := os.ReadFile(filepath.Join(cfg.workRoot, "publisher-health.json"))
	if err != nil {
		return err
	}
	var health healthStatus
	if err := json.Unmarshal(data, &health); err != nil {
		return err
	}
	checked, err := time.Parse(time.RFC3339, health.LastCheckedAt)
	if err != nil || time.Since(checked) > 3*cfg.checkInterval+30*time.Second {
		return errors.New("publisher check loop is stale")
	}
	if health.Status != "Healthy" {
		return errors.New("publisher reports a failed publication")
	}
	return nil
}

func readState(path string) (publisherState, error) {
	var state publisherState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o640); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func (worker *publisher) repoPath() string {
	return filepath.Join(worker.config.workRoot, "repository")
}
func (worker *publisher) sitePath() string { return filepath.Join(worker.config.workRoot, "site") }
func (worker *publisher) statePath() string {
	return filepath.Join(worker.config.workRoot, "publisher-state.json")
}
func (worker *publisher) healthPath() string {
	return filepath.Join(worker.config.workRoot, "publisher-health.json")
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
