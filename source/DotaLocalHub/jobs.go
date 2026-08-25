package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type JobManager struct {
	mu                sync.RWMutex
	jobs              map[string]*Job
	byKey             map[string]string
	pendingMatchUsers map[uint64]map[string]bool
	root              string
	store             *Store
	downloader        *Downloader
	gc                *GCClient
	config            Config
	parseQueue        chan parseQueueItem
	parseQueueMu      sync.Mutex
	parseRunningID    string
	parseQueueOrder   []string
}

type parseQueueItem struct {
	ID           string
	Kind         string
	UserID       string
	MatchID      uint64
	Path         string
	OriginalName string
	ReservedSlot bool
	Metadata     *MatchMetadata
	TeamSlugs    []string
}

const maxUserPendingParseJobs = 3

func newJobManager(root string, store *Store, downloader *Downloader, gc *GCClient, config Config) *JobManager {
	manager := &JobManager{
		jobs:              make(map[string]*Job),
		byKey:             make(map[string]string),
		pendingMatchUsers: make(map[uint64]map[string]bool),
		root:              root,
		store:             store,
		downloader:        downloader,
		gc:                gc,
		config:            config,
		parseQueue:        make(chan parseQueueItem, 128),
	}
	go manager.runParseQueue()
	go manager.runParseRetryLoop()
	return manager
}

func (manager *JobManager) StartGCMatch(metadata MatchMetadata, teamSlugs []string) Job {
	key := fmt.Sprintf("match:%d", metadata.MatchID)
	job, created := manager.createJob(key, Job{Kind: "gc-match", MatchID: metadata.MatchID})
	if created {
		copyMetadata := metadata
		manager.enqueueParseJob(parseQueueItem{
			ID:        job.ID,
			Kind:      "gc-match",
			MatchID:   metadata.MatchID,
			Metadata:  &copyMetadata,
			TeamSlugs: append([]string(nil), teamSlugs...),
		})
	}
	return job
}

func (manager *JobManager) Start(matchID uint64) Job {
	return manager.StartMatch(matchID)
}

func (manager *JobManager) StartMatch(matchID uint64) Job {
	key := fmt.Sprintf("match:%d", matchID)
	job, created := manager.createJob(key, Job{Kind: "match", MatchID: matchID})
	if created {
		manager.enqueueParseJob(parseQueueItem{ID: job.ID, Kind: "match", MatchID: matchID})
	}
	return job
}

func (manager *JobManager) StartUserMatch(userID string, matchID uint64, reservedSlot bool) Job {
	key := fmt.Sprintf("match:%d", matchID)
	job, created := manager.createJob(key, Job{Kind: "match", UserID: userID, MatchID: matchID, ReservedSlot: reservedSlot})
	manager.addPendingMatchUser(matchID, userID)
	if created {
		manager.enqueueParseJob(parseQueueItem{ID: job.ID, Kind: "match", UserID: userID, MatchID: matchID, ReservedSlot: reservedSlot})
	}
	return job
}

func (manager *JobManager) StartTeam(slug string) Job {
	key := "team:" + slug
	job, created := manager.createJob(key, Job{Kind: "team", TeamSlug: slug})
	if created {
		go manager.runTeam(job.ID, slug)
	}
	return job
}

func (manager *JobManager) StartAllTeams(slugs []string) Job {
	job, created := manager.createJob("teams:all", Job{Kind: "all-teams"})
	if created {
		go manager.runAllTeams(job.ID, slugs)
	}
	return job
}

func (manager *JobManager) StartLocalReplay(path, originalName string, matchID uint64) Job {
	return manager.StartUserLocalReplay("", path, originalName, matchID, false)
}

func (manager *JobManager) StartUserLocalReplay(userID, path, originalName string, matchID uint64, reservedSlot bool) Job {
	key := fmt.Sprintf("local:%d", time.Now().UnixNano())
	job, created := manager.createJob(key, Job{Kind: "local-replay", UserID: userID, MatchID: matchID, OriginalName: originalName, ReservedSlot: reservedSlot})
	if created {
		manager.enqueueParseJob(parseQueueItem{
			ID:           job.ID,
			Kind:         "local-replay",
			UserID:       userID,
			MatchID:      matchID,
			Path:         path,
			OriginalName: originalName,
			ReservedSlot: reservedSlot,
		})
	}
	return job
}

func (manager *JobManager) CompletedUserMatch(userID string, matchID uint64, kind, message string) Job {
	now := time.Now()
	if kind == "" {
		kind = "match"
	}
	job := Job{
		ID:        fmt.Sprintf("done:%s:%d:%d", userID, matchID, now.UnixNano()),
		Kind:      kind,
		UserID:    userID,
		MatchID:   matchID,
		State:     "done",
		Message:   message,
		Progress:  100,
		CreatedAt: now,
		UpdatedAt: now,
	}
	manager.mu.Lock()
	manager.jobs[job.ID] = &job
	manager.mu.Unlock()
	return job
}

func (manager *JobManager) enqueueParseJob(item parseQueueItem) {
	position := manager.registerParseQueueItem(item.ID)
	if position <= 1 {
		manager.update(item.ID, "queued", "В очереди на обработку: следующая задача", 1)
	} else {
		manager.update(item.ID, "queued", fmt.Sprintf("В очереди на обработку: место %d", position), 1)
	}
	manager.parseQueue <- item
}

func (manager *JobManager) addPendingMatchUser(matchID uint64, userID string) {
	if matchID == 0 || userID == "" {
		return
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	users := manager.pendingMatchUsers[matchID]
	if users == nil {
		users = make(map[string]bool)
		manager.pendingMatchUsers[matchID] = users
	}
	users[userID] = true
}

func (manager *JobManager) pendingUsersForMatch(matchID uint64) []string {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	users := manager.pendingMatchUsers[matchID]
	result := make([]string, 0, len(users))
	for userID := range users {
		result = append(result, userID)
	}
	return result
}

func (manager *JobManager) clearPendingMatchUsers(matchID uint64) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	delete(manager.pendingMatchUsers, matchID)
}

func (manager *JobManager) ActiveUserMatch(userID string, matchID uint64) (Job, bool) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	id, exists := manager.byKey[fmt.Sprintf("match:%d", matchID)]
	if !exists {
		return Job{}, false
	}
	job := manager.jobs[id]
	if job == nil || job.State == "done" || job.State == "error" {
		return Job{}, false
	}
	if manager.jobVisibleToUserLocked(job, userID) {
		return *job, true
	}
	return Job{}, false
}

func (manager *JobManager) UserPendingParseJobs(userID string) int {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	count := 0
	for _, job := range manager.jobs {
		if job == nil || job.State == "done" || job.State == "error" {
			continue
		}
		if job.Kind != "match" && job.Kind != "local-replay" {
			continue
		}
		if manager.jobVisibleToUserLocked(job, userID) {
			count++
		}
	}
	return count
}

func (manager *JobManager) UserCanAccessJob(jobID, userID string) bool {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.jobVisibleToUserLocked(manager.jobs[jobID], userID)
}

func (manager *JobManager) jobVisibleToUserLocked(job *Job, userID string) bool {
	if job == nil {
		return false
	}
	if job.Kind == "match" && job.MatchID != 0 {
		if manager.pendingMatchUsers[job.MatchID][userID] {
			return true
		}
		if job.UserID != "" {
			return job.UserID == userID
		}
		return true
	}
	if job.UserID != "" {
		return job.UserID == userID
	}
	return true
}

func (manager *JobManager) registerParseQueueItem(id string) int {
	manager.parseQueueMu.Lock()
	defer manager.parseQueueMu.Unlock()
	manager.parseQueueOrder = append(manager.parseQueueOrder, id)
	return len(manager.parseQueueOrder)
}

func (manager *JobManager) runParseQueue() {
	for item := range manager.parseQueue {
		manager.startQueuedParseItem(item.ID)
		switch item.Kind {
		case "local-replay":
			manager.runLocalReplay(item.ID, item.UserID, item.Path, item.OriginalName, item.MatchID)
		case "gc-match":
			manager.runGCMatch(item.ID, item.Metadata, item.TeamSlugs)
		default:
			manager.runMatch(item.ID, item.MatchID)
		}
		manager.finishQueuedParseItem(item.ID)
	}
}

func (manager *JobManager) startQueuedParseItem(id string) {
	manager.parseQueueMu.Lock()
	manager.parseRunningID = id
	for index, queuedID := range manager.parseQueueOrder {
		if queuedID == id {
			manager.parseQueueOrder = append(manager.parseQueueOrder[:index], manager.parseQueueOrder[index+1:]...)
			break
		}
	}
	remaining := append([]string(nil), manager.parseQueueOrder...)
	manager.parseQueueMu.Unlock()

	for index, queuedID := range remaining {
		position := index + 1
		if position == 1 {
			manager.update(queuedID, "queued", "В очереди на обработку: следующая задача", 1)
		} else {
			manager.update(queuedID, "queued", fmt.Sprintf("В очереди на обработку: место %d", position), 1)
		}
	}
	manager.update(id, "queued", "Очередь подошла, начинаю обработку", 2)
}

func (manager *JobManager) finishQueuedParseItem(id string) {
	manager.parseQueueMu.Lock()
	if manager.parseRunningID == id {
		manager.parseRunningID = ""
	}
	manager.parseQueueMu.Unlock()
}

func (manager *JobManager) parseQueueIdle() bool {
	manager.parseQueueMu.Lock()
	defer manager.parseQueueMu.Unlock()
	return manager.parseRunningID == "" && len(manager.parseQueueOrder) == 0 && len(manager.parseQueue) == 0
}

func (manager *JobManager) ParseQueue(userID string) ParseQueueSnapshot {
	manager.parseQueueMu.Lock()
	runningID := manager.parseRunningID
	queuedIDs := append([]string(nil), manager.parseQueueOrder...)
	manager.parseQueueMu.Unlock()

	manager.mu.RLock()
	defer manager.mu.RUnlock()

	snapshot := ParseQueueSnapshot{Queued: make([]Job, 0, len(queuedIDs))}
	if runningID != "" {
		if job := manager.jobs[runningID]; job != nil && job.State != "done" && job.State != "error" && manager.jobVisibleToUserLocked(job, userID) {
			copy := *job
			snapshot.Running = &copy
		}
	}
	for _, id := range queuedIDs {
		if job := manager.jobs[id]; job != nil && job.State != "done" && job.State != "error" && manager.jobVisibleToUserLocked(job, userID) {
			snapshot.Queued = append(snapshot.Queued, *job)
		}
	}
	return snapshot
}

func (manager *JobManager) ActiveTeam(slug string) (Job, bool) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	id, exists := manager.byKey["team:"+slug]
	if !exists {
		return Job{}, false
	}
	job := manager.jobs[id]
	if job == nil || job.State == "done" || job.State == "error" {
		return Job{}, false
	}
	return *job, true
}

func (manager *JobManager) ActiveAllTeams() (Job, bool) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	id, exists := manager.byKey["teams:all"]
	if !exists {
		return Job{}, false
	}
	job := manager.jobs[id]
	if job == nil || job.State == "done" || job.State == "error" {
		return Job{}, false
	}
	return *job, true
}

func (manager *JobManager) runAllTeams(id string, slugs []string) {
	manager.updateBatchCount(id, 0, len(slugs), 0)
	failed := 0
	for index, slug := range slugs {
		child := manager.StartTeam(slug)
		childFailed := false
		for {
			time.Sleep(time.Second)
			current, exists := manager.Get(child.ID)
			if !exists {
				break
			}
			totalProgress := int((float64(index) + float64(current.Progress)/100) / float64(max(1, len(slugs))) * 100)
			manager.update(id, "team_parsing",
				fmt.Sprintf("Команда %d из %d: %s", index+1, len(slugs), current.Message),
				totalProgress)
			if current.State == "error" {
				childFailed = true
				manager.update(id, "team_parsing",
					fmt.Sprintf("Команда %d из %d завершилась с ошибкой: %s", index+1, len(slugs), current.Error),
					totalProgress)
				break
			}
			if current.State == "done" {
				break
			}
		}
		if childFailed {
			failed++
		}
		manager.updateBatchCount(id, index+1, len(slugs), failed)
	}
	manager.finishBatch(id, len(slugs), failed)
}

func (manager *JobManager) createJob(key string, seed Job) (Job, bool) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if id, exists := manager.byKey[key]; exists {
		if job := manager.jobs[id]; job != nil && job.State != "error" && job.State != "done" {
			return *job, false
		}
	}

	now := time.Now()
	seed.ID = fmt.Sprintf("%s-%d", key, now.UnixNano())
	seed.State = "queued"
	seed.Message = "Задача поставлена в очередь"
	seed.Progress = 1
	seed.CreatedAt = now
	seed.UpdatedAt = now
	manager.jobs[seed.ID] = &seed
	manager.byKey[key] = seed.ID
	return seed, true
}

func (manager *JobManager) Get(id string) (Job, bool) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	job, ok := manager.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *job, true
}

func (manager *JobManager) update(id, state, message string, progress int) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if job := manager.jobs[id]; job != nil {
		job.State = state
		job.Message = message
		job.Progress = progress
		job.UpdatedAt = time.Now()
	}
}

func (manager *JobManager) updateCount(id string, completed, total int) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if job := manager.jobs[id]; job != nil {
		job.Completed = completed
		job.Total = total
		job.UpdatedAt = time.Now()
	}
}

func (manager *JobManager) updateBatchCount(id string, completed, total, failed int) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if job := manager.jobs[id]; job != nil {
		job.Completed = completed
		job.Total = total
		job.Failed = failed
		job.UpdatedAt = time.Now()
	}
}

func (manager *JobManager) finishBatch(id string, total, failed int) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if job := manager.jobs[id]; job != nil {
		job.State = "done"
		job.Progress = 100
		job.Completed = total
		job.Total = total
		job.Failed = failed
		job.Message = fmt.Sprintf("Обновление завершено: %d успешно, %d с ошибкой", total-failed, failed)
		if failed > 0 {
			job.Error = fmt.Sprintf("%d команд завершились с ошибкой; откройте их по отдельности для подробностей", failed)
		}
		job.UpdatedAt = time.Now()
	}
}

func (manager *JobManager) fail(id string, err error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if job := manager.jobs[id]; job != nil {
		job.State = "error"
		job.Message = "Не удалось выполнить задачу"
		job.Error = err.Error()
		job.UpdatedAt = time.Now()
	}
}

func (manager *JobManager) runMatch(id string, matchID uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	exists, err := manager.store.MatchExists(ctx, matchID)
	if err != nil {
		manager.releasePendingMatchSlots(ctx, matchID)
		manager.fail(id, err)
		return
	}
	if exists {
		_ = manager.store.SetAllTeamMatchStatuses(ctx, matchID, "done", "")
		manager.completePendingMatchUsers(ctx, matchID, "match-id", true)
		manager.update(id, "done", "Матч уже есть в базе", 100)
		return
	}

	manager.update(id, "metadata", "Получаю данные матча через Steam", 5)
	metadata, err := manager.preferredMatchMetadata(ctx, matchID)
	if err != nil {
		handled, success := manager.trySavedReplayFallback(ctx, id, matchID, err)
		if handled {
			if success {
				manager.completePendingMatchUsers(ctx, matchID, "match-id", true)
			} else {
				manager.releasePendingMatchSlots(ctx, matchID)
			}
			return
		}
		manager.registerParseRetry(ctx, matchID, "manual", err)
		manager.releasePendingMatchSlots(ctx, matchID)
		manager.fail(id, friendlyReplayError(err))
		return
	}

	err = manager.processMatch(ctx, metadata, func(state, message string, progress int) {
		manager.update(id, state, message, progress)
	})
	if err != nil {
		_ = manager.store.SetAllTeamMatchStatuses(ctx, matchID, "error", friendlyReplayError(err).Error())
		manager.registerParseRetry(ctx, matchID, "manual", err)
		manager.releasePendingMatchSlots(ctx, matchID)
		manager.fail(id, friendlyReplayError(err))
		return
	}
	_ = manager.store.SetAllTeamMatchStatuses(ctx, matchID, "done", "")
	manager.completePendingMatchUsers(ctx, matchID, "match-id", true)
	manager.update(id, "done", "Матч скачан, распарсен и сохранён", 100)
}

func (manager *JobManager) runGCMatch(id string, metadata *MatchMetadata, teamSlugs []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if metadata == nil || metadata.MatchID == 0 {
		manager.fail(id, fmt.Errorf("GC match metadata is missing"))
		return
	}

	exists, err := manager.store.MatchExists(ctx, metadata.MatchID)
	if err != nil {
		manager.fail(id, err)
		return
	}
	if exists {
		_ = manager.store.SetAllTeamMatchStatuses(ctx, metadata.MatchID, "done", "")
		manager.update(id, "done", "Match is already present in the database", 100)
		return
	}
	if metadata.Cluster == 0 || metadata.ReplaySalt == 0 {
		err := fmt.Errorf("GC metadata has no replay_salt yet")
		_ = manager.store.SetAllTeamMatchStatuses(ctx, metadata.MatchID, "pending", err.Error())
		manager.registerParseRetry(ctx, metadata.MatchID, "gc-monitor", err)
		manager.update(id, "done", "Replay is not published yet; retry scheduled", 100)
		return
	}

	for _, slug := range teamSlugs {
		_ = manager.store.SetTeamMatchStatus(ctx, slug, metadata.MatchID, "parsing", "")
	}
	err = manager.processMatch(ctx, *metadata, func(state, message string, progress int) {
		manager.update(id, state, message, progress)
	})
	if err != nil {
		friendly := friendlyReplayError(err)
		_ = manager.store.SetAllTeamMatchStatuses(ctx, metadata.MatchID, "error", friendly.Error())
		manager.registerParseRetry(ctx, metadata.MatchID, "gc-monitor", err)
		manager.fail(id, friendly)
		return
	}
	_ = manager.store.SetAllTeamMatchStatuses(ctx, metadata.MatchID, "done", "")
	manager.update(id, "done", "GC match downloaded, parsed and saved", 100)
}

func (manager *JobManager) preferredMatchDetails(ctx context.Context, matchID uint64) (MatchMetadata, error) {
	if manager.gc != nil {
		metadata, err := manager.gc.MatchDetails(ctx, matchID)
		if err == nil && metadata.MatchID == matchID && metadata.Cluster != 0 && len(metadata.Players) > 0 {
			return metadata, nil
		}
	}
	return manager.downloader.MatchDetails(ctx, matchID)
}

func (manager *JobManager) preferredMatchMetadata(ctx context.Context, matchID uint64) (MatchMetadata, error) {
	if manager.gc != nil {
		metadata, err := manager.gc.MatchDetails(ctx, matchID)
		if err == nil && metadata.MatchID == matchID && metadata.Cluster != 0 && metadata.ReplaySalt != 0 {
			return metadata, nil
		}
	}
	return manager.downloader.MatchMetadata(ctx, matchID)
}

func (manager *JobManager) registerParseRetry(ctx context.Context, matchID uint64, source string, err error) {
	if matchID == 0 || !retryableParseError(err) {
		return
	}
	users := manager.pendingUsersForMatch(matchID)
	if len(users) == 0 {
		_ = manager.store.UpsertParseRetry(ctx, "", matchID, source, err)
		return
	}
	for _, userID := range users {
		_ = manager.store.UpsertParseRetry(ctx, userID, matchID, source, err)
	}
}

func (manager *JobManager) completePendingMatchUsers(ctx context.Context, matchID uint64, source string, countQuota bool) {
	users := manager.pendingUsersForMatch(matchID)
	for _, userID := range users {
		_ = manager.store.LinkUserMatch(ctx, userID, matchID, source)
		if countQuota {
			_ = manager.store.FinalizeDailyMatchSlot(ctx, userID)
		}
	}
}

func (manager *JobManager) releasePendingMatchSlots(ctx context.Context, matchID uint64) {
	users := manager.pendingUsersForMatch(matchID)
	for _, userID := range users {
		_ = manager.store.ReleaseDailyMatchSlot(ctx, userID)
	}
	manager.clearPendingMatchUsers(matchID)
}

func retryableParseError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errReplayTooLarge) ||
		errors.Is(err, errUnsupportedReplayArchive) ||
		errors.Is(err, errInvalidReplayArchive) {
		return false
	}
	var httpErr *replayHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == http.StatusNotFound ||
			httpErr.StatusCode == http.StatusTooManyRequests ||
			httpErr.StatusCode >= http.StatusInternalServerError
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"steam web api",
		"stratz",
		"opendota",
		"replay_salt",
		"http 429",
		"http 500",
		"http 502",
		"http 503",
		"http 504",
		"не удалось скачать реплей valve",
		"connection reset",
		"connection refused",
		"temporary failure",
		"timeout",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func (manager *JobManager) runParseRetryLoop() {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		<-timer.C
		manager.processDueParseRetries()
		timer.Reset(time.Minute)
	}
}

func (manager *JobManager) processDueParseRetries() {
	if !manager.parseQueueIdle() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	records, err := manager.store.DueParseRetries(ctx, 1)
	if err != nil || len(records) == 0 {
		return
	}
	record := records[0]
	exists, err := manager.store.MatchExists(ctx, record.MatchID)
	if err == nil && exists {
		if record.UserID != "" {
			_ = manager.store.LinkUserMatch(ctx, record.UserID, record.MatchID, "retry")
		}
		_ = manager.store.DeleteParseRetry(ctx, record.UserID, record.MatchID)
		return
	}

	metadata, err := manager.preferredMatchMetadata(ctx, record.MatchID)
	if err == nil {
		err = manager.processMatch(ctx, metadata, func(state, message string, progress int) {})
	}
	if err != nil {
		_ = manager.store.SetAllTeamMatchStatuses(ctx, record.MatchID, "error", friendlyReplayError(err).Error())
		_ = manager.store.UpsertParseRetry(ctx, record.UserID, record.MatchID, "auto", err)
		return
	}
	_ = manager.store.SetAllTeamMatchStatuses(ctx, record.MatchID, "done", "")
	if record.UserID != "" {
		_ = manager.store.LinkUserMatch(ctx, record.UserID, record.MatchID, "retry")
	}
	_ = manager.store.DeleteParseRetry(ctx, record.UserID, record.MatchID)
}

func (manager *JobManager) trySavedReplayFallback(ctx context.Context, id string, matchID uint64, metadataErr error) (bool, bool) {
	message := strings.ToLower(metadataErr.Error())
	if !strings.Contains(message, "replay_salt") {
		return false, false
	}

	savedDirectory := filepath.Join(manager.root, "data", "saved-replays")
	candidates := []string{
		filepath.Join(savedDirectory, fmt.Sprintf("%d.dem", matchID)),
		filepath.Join(savedDirectory, fmt.Sprintf("%d.dem.bz2", matchID)),
	}

	for _, savedPath := range candidates {
		info, statErr := os.Stat(savedPath)
		if statErr != nil || info.IsDir() {
			continue
		}

		uploadDirectory := filepath.Join(manager.root, "data", "uploads")
		if err := os.MkdirAll(uploadDirectory, 0755); err != nil {
			manager.fail(id, err)
			return true, false
		}

		extension := strings.ToLower(filepath.Ext(savedPath))
		tempPath := filepath.Join(uploadDirectory, fmt.Sprintf("saved-fallback-%d-%d%s", matchID, time.Now().UnixNano(), extension))
		if err := copyFile(savedPath, tempPath); err != nil {
			manager.fail(id, err)
			return true, false
		}

		manager.update(id, "parsing", "Valve не отдала replay по HTTP, беру сохранённую локальную копию", 12)
		if _, err := manager.processLocalReplay(ctx, tempPath, filepath.Base(savedPath), matchID, func(state, message string, progress int) {
			manager.update(id, state, message, progress)
		}); err != nil {
			manager.fail(id, friendlyReplayError(err))
			return true, false
		}
		manager.update(id, "done", "Матч распарсен из сохранённого локального replay", 100)
		return true, true
	}

	return false, false
}

func (manager *JobManager) runLocalReplay(id, userID, uploadPath, originalName string, requestedMatchID uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	matchID, err := manager.processLocalReplay(ctx, uploadPath, originalName, requestedMatchID, func(state, message string, progress int) {
		manager.update(id, state, message, progress)
	})
	if err != nil {
		_ = manager.store.ReleaseDailyMatchSlot(ctx, userID)
		manager.fail(id, friendlyReplayError(err))
		return
	}
	_ = manager.store.LinkUserMatch(ctx, userID, matchID, "local-upload")
	_ = manager.store.FinalizeDailyMatchSlot(ctx, userID)
	manager.update(id, "done", "Локальный replay сохранён, распарсен и привязан к найденным TI-командам", 100)
}

type teamCandidate struct {
	metadata MatchMetadata
	record   TeamMatchRecord
}

func shouldRefreshPlayerProfile(player TeamPlayer, now int64) bool {
	return (player.AvatarURL == "" || player.PersonaName == "") &&
		(player.ProfileCheckedAt == 0 || now-player.ProfileCheckedAt >= int64(24*time.Hour/time.Second))
}

func shouldRefreshPlayerPortrait(player TeamPlayer, now int64) bool {
	return player.PortraitURL == "" &&
		(player.PortraitCheckedAt == 0 || now-player.PortraitCheckedAt >= int64(7*24*time.Hour/time.Second))
}

func missingRecentTeamMatches(summaries []TeamMatchSummary, existing map[uint64]bool, stopAfterKnown, maxMissing int) []TeamMatchSummary {
	missing := make([]TeamMatchSummary, 0)
	knownStreak := 0
	for _, summary := range summaries {
		if existing[summary.MatchID] {
			knownStreak++
			if stopAfterKnown > 0 && knownStreak >= stopAfterKnown {
				break
			}
			continue
		}
		knownStreak = 0
		missing = append(missing, summary)
		if maxMissing > 0 && len(missing) >= maxMissing {
			break
		}
	}
	return missing
}

func (manager *JobManager) runTeam(id, slug string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()

	team, err := manager.store.GetTeam(ctx, slug)
	if err != nil {
		manager.fail(id, err)
		return
	}
	if team.Status == "tbd" || len(team.Roster) != 5 {
		manager.fail(id, fmt.Errorf("для %s пока не определён турнирный состав", team.Name))
		return
	}

	roster := make([]uint32, 0, 5)
	manager.update(id, "profiles", fmt.Sprintf("Обновляю профили игроков: 0 из %d", len(team.Roster)), 1)
	now := time.Now().Unix()
	for index, player := range team.Roster {
		roster = append(roster, player.AccountID)
		manager.update(id, "profiles",
			fmt.Sprintf("Обновляю профили игроков: %d из %d", index+1, len(team.Roster)),
			1+int(float64(index+1)/float64(len(team.Roster))*2))
		if shouldRefreshPlayerProfile(player, now) {
			profileCtx, profileCancel := context.WithTimeout(ctx, 2*time.Second)
			profile, _ := manager.downloader.PlayerProfile(profileCtx, player.AccountID)
			profileCancel()
			profile.AccountID = player.AccountID
			_ = manager.store.UpdateTeamPlayerProfile(ctx, slug, profile)
		}
		if shouldRefreshPlayerPortrait(player, now) {
			portraitCtx, portraitCancel := context.WithTimeout(ctx, 6*time.Second)
			portraitURL, _ := manager.downloader.PlayerPortrait(portraitCtx, player.Name)
			portraitCancel()
			_ = manager.store.UpdateTeamPlayerPortrait(ctx, slug, player.AccountID, portraitURL)
		}
	}

	manager.update(id, "team_history", "Обновляю историю матчей команды", 4)
	summaries, err := manager.loadTeamHistory(ctx, team)
	if err != nil {
		manager.fail(id, err)
		return
	}
	summaryByID := make(map[uint64]TeamMatchSummary, len(summaries))
	for _, summary := range summaries {
		summaryByID[summary.MatchID] = summary
	}
	var candidates []teamCandidate
	candidateIDs := make(map[uint64]bool)
	existingCount, err := manager.store.TeamMatchCount(ctx, slug)
	if err != nil {
		manager.fail(id, err)
		return
	}
	existingMatches, err := manager.store.ListTeamMatches(ctx, slug)
	if err != nil {
		manager.fail(id, err)
		return
	}
	existingIDs := make(map[uint64]bool, len(existingMatches))
	for _, existing := range existingMatches {
		existingIDs[existing.MatchID] = true
		if summary, ok := summaryByID[existing.MatchID]; ok {
			changed := false
			if summary.LeagueID != 0 && existing.LeagueID != summary.LeagueID {
				existing.LeagueID = summary.LeagueID
				changed = true
			}
			if summary.LeagueName != "" && existing.LeagueName != summary.LeagueName {
				existing.LeagueName = summary.LeagueName
				changed = true
			}
			if summary.SeriesID != 0 && (existing.SeriesID != summary.SeriesID || existing.SeriesType != summary.SeriesType) {
				existing.SeriesID = summary.SeriesID
				existing.SeriesType = summary.SeriesType
				changed = true
			}
			if summary.OpposingTeamName != "" && existing.OpponentName != summary.OpposingTeamName {
				existing.OpponentName = summary.OpposingTeamName
				changed = true
			}
			if summary.OpposingTeamLogo != "" && existing.OpponentLogo != summary.OpposingTeamLogo {
				existing.OpponentLogo = summary.OpposingTeamLogo
				changed = true
			}
			if changed {
				_ = manager.store.UpsertTeamMatch(ctx, existing)
			}
		}
		if existing.ParseStatus != "pending" && existing.ParseStatus != "parsing" {
			continue
		}
		metadata, metadataErr := manager.preferredMatchDetails(ctx, existing.MatchID)
		if metadataErr != nil {
			continue
		}
		existing.SeriesID = metadata.SeriesID
		existing.SeriesType = metadata.SeriesType
		existing.ParseStatus = "pending"
		existing.ParseError = ""
		candidates = append(candidates, teamCandidate{metadata: metadata, record: existing})
		candidateIDs[existing.MatchID] = true
		time.Sleep(100 * time.Millisecond)
	}
	maxScan := len(summaries)
	if maxScan > 100 {
		maxScan = 100
	}
	newerSummaries := 0
	metadataFailures := 0
	rosterMismatches := 0
	var lastMetadataErr error
	maxMissing := 0
	if existingCount == 0 {
		maxMissing = 20
	}
	missingSummaries := missingRecentTeamMatches(summaries[:maxScan], existingIDs, 20, maxMissing)
	for index, summary := range missingSummaries {
		newerSummaries++
		manager.update(
			id,
			"roster_check",
			fmt.Sprintf("Проверяю составы: найдено %d матчей", len(candidates)),
			5+int(float64(index+1)/float64(max(1, len(missingSummaries)))*20),
		)

		metadata, err := manager.preferredMatchDetails(ctx, summary.MatchID)
		time.Sleep(180 * time.Millisecond)
		if err != nil {
			metadataFailures++
			lastMetadataErr = err
			manager.update(id, "roster_check",
				fmt.Sprintf("Матч #%d: metadata недоступна (%d ошибок)", summary.MatchID, metadataFailures),
				5+int(float64(index+1)/float64(max(1, len(missingSummaries)))*20))
			continue
		}
		isRadiant, matched := rosterSide(metadata.Players, roster)
		if !matched {
			rosterMismatches++
			continue
		}

		record := teamMatchFrom(metadata, summary, slug, isRadiant)
		candidates = append(candidates, teamCandidate{metadata: metadata, record: record})
		candidateIDs[summary.MatchID] = true
		existingIDs[summary.MatchID] = true
		if existingCount == 0 && len(candidates) == 20 {
			break
		}
	}

	if len(candidates) == 0 {
		if newerSummaries > 0 && metadataFailures == newerSummaries {
			manager.fail(id, fmt.Errorf("не удалось получить metadata ни для одного из %d новых матчей; последняя ошибка: %v", newerSummaries, lastMetadataErr))
			return
		}
		if existingCount > 0 {
			manager.update(id, "done",
				fmt.Sprintf("Новых матчей текущего состава не найдено: проверено %d, несовпадений состава %d, ошибок metadata %d", newerSummaries, rosterMismatches, metadataFailures),
				100)
			return
		}
		manager.fail(id, fmt.Errorf("не нашлось матчей, сыгранных текущей пятёркой %s", team.Name))
		return
	}

	for index := range candidates {
		exists, err := manager.store.MatchExists(ctx, candidates[index].metadata.MatchID)
		if err != nil {
			manager.fail(id, err)
			return
		}
		switch {
		case exists:
			candidates[index].record.ParseStatus = "done"
		case candidates[index].metadata.ReplaySalt == 0:
			candidates[index].record.ParseStatus = "unavailable"
			candidates[index].record.ParseError = "Dota 2 не сохранила доступный реплей или срок его хранения закончился"
		default:
			candidates[index].record.ParseStatus = "pending"
		}
		if err := manager.store.UpsertTeamMatch(ctx, candidates[index].record); err != nil {
			manager.fail(id, err)
			return
		}
	}

	manager.updateCount(id, 0, len(candidates))
	var completed, unavailable, failed int
	for index, candidate := range candidates {
		manager.updateCount(id, index, len(candidates))

		exists, err := manager.store.MatchExists(ctx, candidate.metadata.MatchID)
		if err != nil {
			failed++
			continue
		}
		if exists {
			completed++
			_ = manager.store.SetAllTeamMatchStatuses(ctx, candidate.metadata.MatchID, "done", "")
			continue
		}
		if candidate.metadata.ReplaySalt == 0 {
			unavailable++
			continue
		}

		baseProgress := 28 + int(float64(index)/float64(len(candidates))*68)
		manager.update(
			id,
			"team_parsing",
			fmt.Sprintf("Матч %d из %d — #%d", index+1, len(candidates), candidate.metadata.MatchID),
			baseProgress,
		)
		_ = manager.store.SetTeamMatchStatus(ctx, slug, candidate.metadata.MatchID, "parsing", "")

		err = manager.processMatch(ctx, candidate.metadata, func(state, message string, progress int) {
			scaled := baseProgress + int(float64(progress)/100*(68/float64(len(candidates))))
			manager.update(id, "team_parsing", fmt.Sprintf("Матч %d/%d: %s", index+1, len(candidates), message), scaled)
		})
		if err != nil {
			failed++
			_ = manager.store.SetAllTeamMatchStatuses(ctx, candidate.metadata.MatchID, "error", friendlyReplayError(err).Error())
			manager.registerParseRetry(ctx, candidate.metadata.MatchID, "team-sync", err)
			continue
		}
		completed++
		_ = manager.store.SetAllTeamMatchStatuses(ctx, candidate.metadata.MatchID, "done", "")
	}

	manager.updateCount(id, len(candidates), len(candidates))
	manager.update(
		id,
		"done",
		fmt.Sprintf("Готово: %d матчей в статистике, %d без реплея, %d с ошибкой", completed, unavailable, failed),
		100,
	)
}

func friendlyReplayError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "manta"), strings.Contains(message, "decompress"), strings.Contains(message, "распаков"):
		return fmt.Errorf("Dota 2 некорректно сохранила реплей, поэтому его не удалось разобрать: %v", err)
	case strings.Contains(message, "replay_salt"), strings.Contains(message, "http 404"):
		return fmt.Errorf("Dota 2 не сохранила доступный реплей или срок его хранения закончился")
	case strings.Contains(message, "download"), strings.Contains(message, "скач"):
		return fmt.Errorf("сервер реплеев Dota 2 не отдал файл: %v", err)
	default:
		return fmt.Errorf("внешние данные Dota 2 оказались неполными или повреждёнными: %v", err)
	}
}

func (manager *JobManager) loadTeamHistory(ctx context.Context, team TournamentTeam) ([]TeamMatchSummary, error) {
	ids := []uint64{team.OpenDotaID}
	if team.HistoryTeamID != 0 && team.HistoryTeamID != team.OpenDotaID {
		ids = append(ids, team.HistoryTeamID)
	}

	unique := make(map[uint64]TeamMatchSummary)
	for _, teamID := range ids {
		if teamID == 0 {
			continue
		}
		matches, err := manager.downloader.TeamMatches(ctx, teamID)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if _, exists := unique[match.MatchID]; !exists {
				unique[match.MatchID] = match
			}
		}
	}

	matches := make([]TeamMatchSummary, 0, len(unique))
	for _, match := range unique {
		matches = append(matches, match)
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].StartTime > matches[j].StartTime
	})
	return matches, nil
}

func rosterSide(players []OpenDotaPlayer, roster []uint32) (bool, bool) {
	expected := make(map[uint32]struct{}, len(roster))
	for _, id := range roster {
		expected[id] = struct{}{}
	}

	radiant := make(map[uint32]struct{})
	dire := make(map[uint32]struct{})
	for _, player := range players {
		if player.AccountID == 0 {
			continue
		}
		if player.PlayerSlot < 128 {
			radiant[player.AccountID] = struct{}{}
		} else {
			dire[player.AccountID] = struct{}{}
		}
	}
	if sameAccountSet(expected, radiant) {
		return true, true
	}
	if sameAccountSet(expected, dire) {
		return false, true
	}
	return false, false
}

func sameAccountSet(expected, actual map[uint32]struct{}) bool {
	if len(expected) != 5 || len(actual) != 5 {
		return false
	}
	for id := range expected {
		if _, exists := actual[id]; !exists {
			return false
		}
	}
	return true
}

func teamMatchFrom(metadata MatchMetadata, summary TeamMatchSummary, slug string, isRadiant bool) TeamMatchRecord {
	leagueName := metadata.LeagueName
	if leagueName == "" {
		leagueName = summary.LeagueName
	}
	seriesID := metadata.SeriesID
	seriesType := metadata.SeriesType
	if seriesID == 0 {
		seriesID = summary.SeriesID
		seriesType = summary.SeriesType
	}
	record := TeamMatchRecord{
		TeamSlug:      slug,
		MatchID:       metadata.MatchID,
		LeagueID:      metadata.LeagueID,
		StartTime:     metadata.StartTime,
		Duration:      metadata.Duration,
		LeagueName:    leagueName,
		SeriesID:      seriesID,
		SeriesType:    seriesType,
		RosterMatched: true,
		ParseStatus:   "pending",
		Included:      true,
	}
	if isRadiant {
		record.OpponentTeamID = metadata.DireTeamID
		record.OpponentName = metadata.DireName
		record.TeamScore = metadata.RadiantScore
		record.OpponentScore = metadata.DireScore
		record.TeamWon = metadata.RadiantWin
	} else {
		record.OpponentTeamID = metadata.RadiantTeamID
		record.OpponentName = metadata.RadiantName
		record.TeamScore = metadata.DireScore
		record.OpponentScore = metadata.RadiantScore
		record.TeamWon = !metadata.RadiantWin
	}
	if record.OpponentName == "" {
		record.OpponentName = summary.OpposingTeamName
	}
	record.OpponentLogo = summary.OpposingTeamLogo
	return record
}

func (manager *JobManager) processMatch(
	ctx context.Context,
	metadata MatchMetadata,
	progress func(state, message string, progress int),
) error {
	replayDirectory := filepath.Join(manager.root, "data", "replays")
	compressedPath := filepath.Join(replayDirectory, fmt.Sprintf("%d.dem.bz2", metadata.MatchID))
	replayPath := filepath.Join(replayDirectory, fmt.Sprintf("%d.dem", metadata.MatchID))

	if _, err := os.Stat(replayPath); err != nil {
		progress("downloading", "Скачиваю реплей с Valve", 10)
		if _, err := os.Stat(compressedPath); err != nil {
			err = manager.downloader.DownloadReplay(ctx, metadata, compressedPath, func(downloaded, total int64) {
				value := 25
				message := fmt.Sprintf("Скачано %.1f МБ", float64(downloaded)/(1024*1024))
				if total > 0 {
					fraction := float64(downloaded) / float64(total)
					value = 10 + int(fraction*42)
					message = fmt.Sprintf(
						"Скачано %.1f из %.1f МБ",
						float64(downloaded)/(1024*1024),
						float64(total)/(1024*1024),
					)
				}
				progress("downloading", message, value)
			})
			if err != nil {
				return err
			}
		}

		progress("decompressing", "Распаковываю replay", 56)
		if err := decompressReplay(compressedPath, replayPath, manager.maxReplayBytes()); err != nil {
			return fmt.Errorf("ошибка распаковки: %w", err)
		}
		if !manager.config.KeepCompressed {
			_ = os.Remove(compressedPath)
		}
	}

	if err := ensureFileWithinLimit(replayPath, manager.maxReplayBytes()); err != nil {
		return err
	}

	progress("parsing", "Manta разбирает replay", 65)
	parsed, err := parseReplay(replayPath)
	if err != nil {
		return fmt.Errorf("ошибка Manta: %w", err)
	}
	names := make(map[uint32]string, len(metadata.Players))
	for _, player := range metadata.Players {
		if player.Persona != "" {
			names[player.AccountID] = repairPlayerName(player.Persona)
		}
	}
	for index := range parsed.Players {
		if playerNameLooksBroken(parsed.Players[index].Name) {
			parsed.Players[index].Name = names[parsed.Players[index].AccountID]
			if parsed.Players[index].Name == "" {
				parsed.Players[index].Name = "Без имени"
			}
		}
	}

	duration := metadata.Duration
	if duration == 0 {
		duration = int(parsed.GameDurationSeconds + 0.5)
	}
	if duration > 0 {
		for index := range parsed.Players {
			parsed.Players[index].GPM = float64(parsed.Players[index].TotalEarnedGold) * 60 / float64(duration)
		}
	}
	record := MatchRecord{
		MatchID:    metadata.MatchID,
		Cluster:    metadata.Cluster,
		ReplaySalt: metadata.ReplaySalt,
		StartTime:  metadata.StartTime,
		Duration:   duration,
		RadiantWin: metadata.RadiantWin,
		GameMode:   metadata.GameMode,
		LobbyType:  metadata.LobbyType,
		ReplayPath: relativePath(manager.root, replayPath),
		ParsedAt:   time.Now(),
		Players:    parsed.Players,
	}

	progress("saving", "Сохраняю статистику в SQLite", 92)
	if err := manager.store.SaveMatch(ctx, record); err != nil {
		return err
	}
	record.ReplayPath = ""
	_ = manager.store.ClearReplayPath(ctx, metadata.MatchID)
	_ = os.Remove(replayPath)
	_ = os.Remove(compressedPath)

	jsonDirectory := filepath.Join(manager.root, "data", "json")
	if err := os.MkdirAll(jsonDirectory, 0755); err == nil {
		if data, err := json.MarshalIndent(record, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(jsonDirectory, fmt.Sprintf("%d.json", metadata.MatchID)), data, 0644)
		}
	}
	return nil
}

func (manager *JobManager) processLocalReplay(
	ctx context.Context,
	uploadPath string,
	originalName string,
	requestedMatchID uint64,
	progress func(state, message string, progress int),
) (uint64, error) {
	progress("saving", "Сохраняю локальный replay", 8)
	savedDirectory := filepath.Join(manager.root, "data", "saved-replays")
	if err := os.MkdirAll(savedDirectory, 0755); err != nil {
		return 0, err
	}

	extension, ok := replayUploadExtension(originalName)
	if !ok {
		extension, ok = replayUploadExtension(uploadPath)
	}
	if !ok {
		return 0, fmt.Errorf("поддерживаются только .dem и .dem.bz2")
	}

	temporaryReplayPath := uploadPath
	if extension == ".bz2" {
		progress("decompressing", "Распаковываю локальный .dem.bz2", 18)
		temporaryReplayPath = strings.TrimSuffix(uploadPath, filepath.Ext(uploadPath)) + ".dem"
		if err := decompressReplay(uploadPath, temporaryReplayPath, manager.maxReplayBytes()); err != nil {
			return 0, fmt.Errorf("ошибка распаковки локального replay: %w", err)
		}
	} else if err := ensureFileWithinLimit(uploadPath, manager.maxReplayBytes()); err != nil {
		return 0, err
	}

	progress("parsing", "Manta читает локальный replay", 35)
	parsed, err := parseReplay(temporaryReplayPath)
	if err != nil {
		return 0, fmt.Errorf("ошибка Manta: %w", err)
	}

	matchID := parsed.MatchID
	if requestedMatchID != 0 {
		matchID = requestedMatchID
	}
	if matchID == 0 {
		if value := matchIDFromFilename(originalName); value != 0 {
			matchID = value
		}
	}
	if matchID == 0 {
		return 0, fmt.Errorf("не удалось определить match ID из replay; укажи match ID рядом с файлом")
	}

	finalReplayPath := filepath.Join(savedDirectory, fmt.Sprintf("%d.dem", matchID))
	if temporaryReplayPath != finalReplayPath {
		if err := copyFile(temporaryReplayPath, finalReplayPath); err != nil {
			return 0, err
		}
	}
	if extension == ".bz2" {
		finalCompressedPath := filepath.Join(savedDirectory, fmt.Sprintf("%d.dem.bz2", matchID))
		_ = copyFile(uploadPath, finalCompressedPath)
		_ = os.Remove(temporaryReplayPath)
	}
	_ = os.Remove(uploadPath)

	metadata := metadataFromParsedReplay(parsed, matchID)
	if remote, remoteErr := manager.preferredMatchDetails(ctx, matchID); remoteErr == nil {
		metadata = mergeMatchMetadata(metadata, remote)
	}

	progress("saving", "Сохраняю локальный матч в SQLite", 74)
	record := matchRecordFromParsed(manager.root, metadata, parsed, finalReplayPath)
	if err := manager.store.SaveMatch(ctx, record); err != nil {
		return 0, err
	}

	progress("saving", "Проверяю совпадение с TI-составами", 88)
	linked, err := manager.linkReplayToTournamentTeams(ctx, metadata, parsed)
	if err != nil {
		return 0, err
	}
	if linked == 0 {
		progress("done", "Матч сохранён. Полная пятёрка TI-команды в replay не найдена", 98)
	}

	jsonDirectory := filepath.Join(manager.root, "data", "json")
	if err := os.MkdirAll(jsonDirectory, 0755); err == nil {
		record.ReplayPath = relativePath(manager.root, finalReplayPath)
		if data, err := json.MarshalIndent(record, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(jsonDirectory, fmt.Sprintf("%d.json", matchID)), data, 0644)
		}
	}
	return matchID, nil
}

func metadataFromParsedReplay(parsed ParsedMatch, matchID uint64) MatchMetadata {
	duration := int(parsed.GameDurationSeconds + 0.5)
	startTime := int64(0)
	if parsed.EndTime > 0 {
		startTime = int64(parsed.EndTime) - int64(duration)
	}
	return MatchMetadata{
		MatchID:       matchID,
		Duration:      duration,
		StartTime:     startTime,
		RadiantWin:    parsed.RadiantWin,
		GameMode:      parsed.GameMode,
		RadiantTeamID: parsed.RadiantTeamID,
		DireTeamID:    parsed.DireTeamID,
		RadiantName:   parsed.RadiantTeamTag,
		DireName:      parsed.DireTeamTag,
	}
}

func mergeMatchMetadata(base, remote MatchMetadata) MatchMetadata {
	if remote.MatchID != 0 {
		base.MatchID = remote.MatchID
	}
	if remote.Cluster != 0 {
		base.Cluster = remote.Cluster
	}
	if remote.ReplaySalt != 0 {
		base.ReplaySalt = remote.ReplaySalt
	}
	if remote.Duration != 0 {
		base.Duration = remote.Duration
	}
	if remote.StartTime != 0 {
		base.StartTime = remote.StartTime
	}
	base.RadiantWin = remote.RadiantWin
	if remote.GameMode != 0 {
		base.GameMode = remote.GameMode
	}
	if remote.LobbyType != 0 {
		base.LobbyType = remote.LobbyType
	}
	if remote.SeriesID != 0 {
		base.SeriesID = remote.SeriesID
	}
	if remote.SeriesType != 0 {
		base.SeriesType = remote.SeriesType
	}
	if remote.RadiantTeamID != 0 {
		base.RadiantTeamID = remote.RadiantTeamID
	}
	if remote.DireTeamID != 0 {
		base.DireTeamID = remote.DireTeamID
	}
	if remote.RadiantName != "" {
		base.RadiantName = remote.RadiantName
	}
	if remote.DireName != "" {
		base.DireName = remote.DireName
	}
	if remote.RadiantScore != 0 || remote.DireScore != 0 {
		base.RadiantScore = remote.RadiantScore
		base.DireScore = remote.DireScore
	}
	if len(remote.Players) > 0 {
		base.Players = remote.Players
	}
	return base
}

func matchRecordFromParsed(root string, metadata MatchMetadata, parsed ParsedMatch, replayPath string) MatchRecord {
	duration := metadata.Duration
	if duration == 0 {
		duration = int(parsed.GameDurationSeconds + 0.5)
	}
	players := append([]PlayerRecord(nil), parsed.Players...)
	if duration > 0 {
		for index := range players {
			players[index].GPM = float64(players[index].TotalEarnedGold) * 60 / float64(duration)
		}
	}
	return MatchRecord{
		MatchID:    metadata.MatchID,
		Cluster:    metadata.Cluster,
		ReplaySalt: metadata.ReplaySalt,
		StartTime:  metadata.StartTime,
		Duration:   duration,
		RadiantWin: metadata.RadiantWin,
		GameMode:   metadata.GameMode,
		LobbyType:  metadata.LobbyType,
		ReplayPath: relativePath(root, replayPath),
		ParsedAt:   time.Now(),
		Players:    players,
	}
}

func (manager *JobManager) linkReplayToTournamentTeams(ctx context.Context, metadata MatchMetadata, parsed ParsedMatch) (int, error) {
	teams, err := manager.store.ListTeams(ctx)
	if err != nil {
		return 0, err
	}
	linked := 0
	for _, team := range teams {
		if team.Status == "tbd" {
			continue
		}
		roster, err := manager.store.TeamRosterAccountIDs(ctx, team.Slug)
		if err != nil || len(roster) != 5 {
			continue
		}
		side, ok := rosterReplaySide(parsed.Players, roster)
		if !ok {
			continue
		}
		record := localTeamMatchRecord(metadata, team.Slug, side)
		if err := manager.store.UpsertTeamMatch(ctx, record); err != nil {
			return linked, err
		}
		linked++
	}
	return linked, nil
}

func rosterReplaySide(players []PlayerRecord, roster []uint32) (string, bool) {
	expected := make(map[uint32]struct{}, len(roster))
	for _, accountID := range roster {
		expected[accountID] = struct{}{}
	}
	for _, side := range []string{"radiant", "dire"} {
		actual := make(map[uint32]struct{})
		for _, player := range players {
			if player.Team == side && player.AccountID != 0 {
				actual[player.AccountID] = struct{}{}
			}
		}
		if sameAccountSet(expected, actual) {
			return side, true
		}
	}
	return "", false
}

func localTeamMatchRecord(metadata MatchMetadata, slug, side string) TeamMatchRecord {
	record := TeamMatchRecord{
		TeamSlug:      slug,
		MatchID:       metadata.MatchID,
		LeagueID:      metadata.LeagueID,
		StartTime:     metadata.StartTime,
		Duration:      metadata.Duration,
		LeagueName:    "Локальные реплеи",
		SeriesID:      metadata.SeriesID,
		SeriesType:    metadata.SeriesType,
		RosterMatched: true,
		ParseStatus:   "done",
		Included:      true,
	}
	if record.SeriesID == 0 {
		record.SeriesID = metadata.MatchID
	}
	if side == "radiant" {
		record.OpponentTeamID = metadata.DireTeamID
		record.OpponentName = metadata.DireName
		record.TeamWon = metadata.RadiantWin
		record.TeamScore = metadata.RadiantScore
		record.OpponentScore = metadata.DireScore
	} else {
		record.OpponentTeamID = metadata.RadiantTeamID
		record.OpponentName = metadata.RadiantName
		record.TeamWon = !metadata.RadiantWin
		record.TeamScore = metadata.DireScore
		record.OpponentScore = metadata.RadiantScore
	}
	if record.OpponentName == "" {
		record.OpponentName = "Unknown"
	}
	return record
}

func matchIDFromFilename(name string) uint64 {
	base := filepath.Base(name)
	var digits strings.Builder
	for _, char := range base {
		if char >= '0' && char <= '9' {
			digits.WriteRune(char)
			continue
		}
		if digits.Len() >= 8 {
			break
		}
		digits.Reset()
	}
	if digits.Len() < 8 {
		return 0
	}
	var value uint64
	_, _ = fmt.Sscanf(digits.String(), "%d", &value)
	return value
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	output, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func ensureFileWithinLimit(path string, limit int64) error {
	if limit <= 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > limit {
		return fmt.Errorf("%w: %.1f MiB > %.1f MiB", errReplayTooLarge, bytesToMiB(info.Size()), bytesToMiB(limit))
	}
	return nil
}

func (manager *JobManager) maxReplayBytes() int64 {
	if manager.config.MaxReplayBytes > 0 {
		return manager.config.MaxReplayBytes
	}
	return defaultMaxReplayBytes
}

func relativePath(root, path string) string {
	value, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(value)
}
