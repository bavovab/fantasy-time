package main

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

var (
	errReplayTooLarge           = errors.New("replay exceeds configured size limit")
	errUnsupportedReplayArchive = errors.New("unsupported replay archive compression")
	errInvalidReplayArchive     = errors.New("invalid or truncated replay archive")
	zstdReplayMagic             = []byte{0x28, 0xb5, 0x2f, 0xfd}
)

type replayHTTPError struct {
	URL        string
	StatusCode int
}

func (err *replayHTTPError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d", err.URL, err.StatusCode)
}

type Downloader struct {
	client             *http.Client
	steamClient        *http.Client
	config             Config
	steamLimiter       *requestLimiter
	steamStateMu       sync.Mutex
	steamFailures      int
	steamDisabledUntil time.Time
	heroMu             sync.Mutex
	heroes             map[int]HeroInfo
}

func newDownloader(config Config) *Downloader {
	directTransport := directIPv4Transport()
	return &Downloader{
		client: &http.Client{
			Timeout:   45 * time.Second,
			Transport: retryTransport{base: directTransport},
		},
		steamClient:  &http.Client{Timeout: 45 * time.Second, Transport: directTransport},
		config:       config,
		steamLimiter: newRequestLimiter(steamRequestInterval(config)),
	}
}

func directIPv4Transport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 12 * time.Second
	transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", address)
	}
	return transport
}

func steamRequestInterval(config Config) time.Duration {
	milliseconds := config.SteamAPIRequestIntervalMs
	if milliseconds <= 0 {
		milliseconds = 1200
	}
	if milliseconds < 1000 {
		milliseconds = 1000
	}
	return time.Duration(milliseconds) * time.Millisecond
}

type requestLimiter struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func newRequestLimiter(interval time.Duration) *requestLimiter {
	return &requestLimiter{interval: interval}
}

func (limiter *requestLimiter) Wait(ctx context.Context) error {
	if limiter == nil || limiter.interval <= 0 {
		return nil
	}

	limiter.mu.Lock()
	now := time.Now()
	wait := time.Duration(0)
	if limiter.next.After(now) {
		wait = limiter.next.Sub(now)
		limiter.next = limiter.next.Add(limiter.interval)
	} else {
		limiter.next = now.Add(limiter.interval)
	}
	limiter.mu.Unlock()

	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type retryTransport struct {
	base http.RoundTripper
}

func (transport retryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := transport.base
	if base == nil {
		base = http.DefaultTransport
	}
	delays := []time.Duration{0, 700 * time.Millisecond, 1800 * time.Millisecond, 3500 * time.Millisecond}
	var lastErr error
	for attempt, delay := range delays {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-request.Context().Done():
				timer.Stop()
				return nil, request.Context().Err()
			case <-timer.C:
			}
		}

		cloned := request.Clone(request.Context())
		response, err := base.RoundTrip(cloned)
		if err != nil {
			lastErr = err
			continue
		}
		if response.StatusCode != http.StatusTooManyRequests && response.StatusCode < 500 {
			return response, nil
		}
		if attempt == len(delays)-1 {
			return response, nil
		}
		io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		response.Body.Close()
		lastErr = fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil, lastErr
}

func (d *Downloader) HeroNames(ctx context.Context) (map[int]HeroInfo, error) {
	d.heroMu.Lock()
	defer d.heroMu.Unlock()
	if len(d.heroes) > 0 {
		return cloneHeroNames(d.heroes), nil
	}

	endpoint := "https://api.opendota.com/api/constants/heroes"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "DotaLocalHub/0.2")
	response, err := d.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("не удалось загрузить имена героев: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenDota heroes вернула HTTP %d", response.StatusCode)
	}

	var raw map[string]struct {
		ID            int    `json:"id"`
		LocalizedName string `json:"localized_name"`
		Image         string `json:"img"`
	}
	if err := json.NewDecoder(response.Body).Decode(&raw); err != nil {
		return nil, err
	}
	d.heroes = make(map[int]HeroInfo, len(raw))
	for key, hero := range raw {
		id := hero.ID
		if id == 0 {
			id, _ = strconv.Atoi(key)
		}
		if id > 0 && hero.LocalizedName != "" {
			imageURL := hero.Image
			if strings.HasPrefix(imageURL, "/") {
				imageURL = "https://cdn.cloudflare.steamstatic.com" + imageURL
			}
			d.heroes[id] = HeroInfo{Name: hero.LocalizedName, ImageURL: imageURL}
		}
	}
	return cloneHeroNames(d.heroes), nil
}

func cloneHeroNames(source map[int]HeroInfo) map[int]HeroInfo {
	result := make(map[int]HeroInfo, len(source))
	for id, hero := range source {
		result[id] = hero
	}
	return result
}

func (d *Downloader) MatchMetadata(ctx context.Context, matchID uint64) (MatchMetadata, error) {
	metadata, err := d.MatchDetails(ctx, matchID)
	if err != nil {
		return metadata, err
	}
	if metadata.Cluster == 0 {
		return metadata, errors.New("Steam Web API не вернул cluster матча")
	}
	if metadata.ReplaySalt == 0 {
		return metadata, errors.New("у матча пока нет replay_salt; реплей мог ещё не появиться или уже устарел")
	}
	return metadata, nil
}

func (d *Downloader) PlayerProfile(ctx context.Context, accountID uint32) (PlayerProfile, error) {
	endpoint := fmt.Sprintf("https://api.opendota.com/api/players/%d", accountID)
	if d.config.OpenDotaAPIKey != "" {
		endpoint += "?api_key=" + url.QueryEscape(d.config.OpenDotaAPIKey)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return PlayerProfile{}, err
	}
	request.Header.Set("User-Agent", "DotaLocalHub/0.3")
	response, err := d.client.Do(request)
	if err != nil {
		return PlayerProfile{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return PlayerProfile{}, fmt.Errorf("OpenDota player %d вернула HTTP %d", accountID, response.StatusCode)
	}
	var payload struct {
		Profile struct {
			PersonaName string `json:"personaname"`
			Name        string `json:"name"`
			AvatarFull  string `json:"avatarfull"`
		} `json:"profile"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return PlayerProfile{}, err
	}
	return PlayerProfile{
		AccountID: accountID, PersonaName: payload.Profile.PersonaName,
		ProName: payload.Profile.Name, AvatarURL: payload.Profile.AvatarFull,
	}, nil
}

func (d *Downloader) PlayerPortrait(ctx context.Context, proName string) (string, error) {
	pageURL := "https://liquipedia.net/dota2/" + url.PathEscape(strings.ReplaceAll(proName, " ", "_"))
	content, err := d.liquipediaPage(ctx, pageURL)
	if err != nil {
		return "", err
	}
	if strings.Contains(content, "(Disambiguation)") {
		pattern := regexp.MustCompile(`href="(/dota2/[^"]+_(?:[^"]*player[^"]*))"`)
		if match := pattern.FindStringSubmatch(content); len(match) == 2 {
			content, err = d.liquipediaPage(ctx, "https://liquipedia.net"+html.UnescapeString(match[1]))
			if err != nil {
				return "", err
			}
		}
	}
	imagePattern := regexp.MustCompile(`/commons/images/[a-f0-9]/[a-f0-9]{2}/[^"'<> ]+\.(?:png|webp|jpg|jpeg)`)
	candidates := imagePattern.FindAllString(content, -1)
	nameToken := strings.ToLower(strings.NewReplacer("`", "", "-", "", " ", "_").Replace(proName))
	var best string
	for _, candidate := range candidates {
		lower := strings.ToLower(candidate)
		if !looksLikePlayerPortrait(candidate) {
			continue
		}
		fileName := lower[strings.LastIndex(lower, "/")+1:]
		if !strings.Contains(strings.NewReplacer("`", "", "-", "").Replace(fileName), nameToken) {
			continue
		}
		if strings.Contains(lower, "_2026_") {
			return "https://liquipedia.net" + html.UnescapeString(candidate), nil
		}
		if best == "" || candidate > best {
			best = candidate
		}
	}
	if best == "" {
		return "", fmt.Errorf("турнирный портрет %s не найден", proName)
	}
	return "https://liquipedia.net" + html.UnescapeString(best), nil
}

func looksLikePlayerPortrait(candidate string) bool {
	lower := strings.ToLower(candidate)
	for _, blocked := range []string{"itemicon", "gameasset", "minimap_icon", "spell", "ability"} {
		if strings.Contains(lower, blocked) {
			return false
		}
	}
	return true
}

func (d *Downloader) liquipediaPage(ctx context.Context, endpoint string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 DotaLocalHub/0.4")
	response, err := d.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Liquipedia вернула HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	return string(body), err
}

func (d *Downloader) MatchDetails(ctx context.Context, matchID uint64) (MatchMetadata, error) {
	explorerCtx, explorerCancel := context.WithTimeout(ctx, 15*time.Second)
	explorerMetadata, explorerErr := d.OpenDotaExplorerMatchDetails(explorerCtx, matchID)
	explorerCancel()
	if explorerErr == nil {
		return explorerMetadata, nil
	}

	steamCtx, steamCancel := context.WithTimeout(ctx, 15*time.Second)
	metadata, steamErr := d.SteamMatchDetails(steamCtx, matchID)
	steamCancel()
	if steamErr == nil {
		d.noteSteamSuccess()
		return metadata, nil
	}
	d.noteSteamFailure(steamErr)

	stratzCtx, stratzCancel := context.WithTimeout(ctx, 20*time.Second)
	stratzMetadata, stratzErr := d.StratzMatchDetails(stratzCtx, matchID)
	stratzCancel()
	if stratzErr == nil {
		return stratzMetadata, nil
	}

	openDotaCtx, openDotaCancel := context.WithTimeout(ctx, 20*time.Second)
	openDotaMetadata, openDotaErr := d.OpenDotaMatchDetails(openDotaCtx, matchID)
	openDotaCancel()
	if openDotaErr == nil {
		return openDotaMetadata, nil
	}
	if d.config.SteamAPIKey == "" {
		return MatchMetadata{}, fmt.Errorf("Steam API ключ не настроен, STRATZ fallback не сработал: %v; OpenDota fallback: %w", stratzErr, openDotaErr)
	}
	return MatchMetadata{}, fmt.Errorf("OpenDota Explorer: %v; Steam Web API: %v; STRATZ fallback: %v; OpenDota fallback: %w", explorerErr, steamErr, stratzErr, openDotaErr)
}

type matchExplorerRow struct {
	MatchID       uint64 `json:"match_id"`
	Cluster       int    `json:"cluster"`
	ReplaySalt    uint64 `json:"replay_salt"`
	Duration      int    `json:"duration"`
	StartTime     int64  `json:"start_time"`
	RadiantWin    bool   `json:"radiant_win"`
	GameMode      int    `json:"game_mode"`
	LobbyType     int    `json:"lobby_type"`
	SeriesID      uint64 `json:"series_id"`
	SeriesType    int    `json:"series_type"`
	RadiantTeamID uint64 `json:"radiant_team_id"`
	DireTeamID    uint64 `json:"dire_team_id"`
	RadiantName   string `json:"radiant_name"`
	DireName      string `json:"dire_name"`
	RadiantScore  int    `json:"radiant_score"`
	DireScore     int    `json:"dire_score"`
	LeagueName    string `json:"league_name"`
	AccountID     uint32 `json:"account_id"`
	PlayerSlot    int    `json:"player_slot"`
}

func (d *Downloader) OpenDotaExplorerMatchDetails(ctx context.Context, matchID uint64) (MatchMetadata, error) {
	query := fmt.Sprintf("select m.match_id,m.cluster,m.replay_salt,m.duration,m.start_time,m.radiant_win,m.game_mode,m.lobby_type,m.series_id,m.series_type,m.radiant_team_id,m.dire_team_id,rt.name as radiant_name,dt.name as dire_name,m.radiant_score,m.dire_score,l.name as league_name,p.account_id,p.player_slot from matches m join player_matches p on p.match_id=m.match_id left join teams rt on rt.team_id=m.radiant_team_id left join teams dt on dt.team_id=m.dire_team_id left join leagues l on l.leagueid=m.leagueid where m.match_id=%d", matchID)
	var payload struct {
		Rows []matchExplorerRow `json:"rows"`
	}
	if err := d.openDotaExplorer(ctx, query, &payload); err != nil {
		return MatchMetadata{}, err
	}
	if len(payload.Rows) == 0 {
		return MatchMetadata{}, fmt.Errorf("OpenDota Explorer не вернул матч %d", matchID)
	}
	first := payload.Rows[0]
	metadata := MatchMetadata{
		MatchID: first.MatchID, Cluster: first.Cluster, ReplaySalt: first.ReplaySalt,
		Duration: first.Duration, StartTime: first.StartTime, RadiantWin: first.RadiantWin,
		GameMode: first.GameMode, LobbyType: first.LobbyType, SeriesID: first.SeriesID,
		SeriesType: first.SeriesType, RadiantTeamID: first.RadiantTeamID, DireTeamID: first.DireTeamID,
		RadiantName: first.RadiantName, DireName: first.DireName,
		RadiantScore: first.RadiantScore, DireScore: first.DireScore,
		LeagueName: first.LeagueName,
		Players:    make([]OpenDotaPlayer, 0, len(payload.Rows)),
	}
	for _, row := range payload.Rows {
		metadata.Players = append(metadata.Players, OpenDotaPlayer{AccountID: row.AccountID, PlayerSlot: row.PlayerSlot})
	}
	return metadata, nil
}

func (d *Downloader) SteamMatchDetails(ctx context.Context, matchID uint64) (MatchMetadata, error) {
	if d.config.SteamAPIKey == "" {
		return MatchMetadata{}, errors.New("Steam API ключ не настроен")
	}
	if err := d.steamBackoffError(); err != nil {
		return MatchMetadata{}, err
	}
	if err := d.steamLimiter.Wait(ctx); err != nil {
		return MatchMetadata{}, err
	}

	endpoint := fmt.Sprintf("https://api.steampowered.com/IDOTA2Match_570/GetMatchDetails/v1/?key=%s&match_id=%d",
		url.QueryEscape(d.config.SteamAPIKey), matchID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return MatchMetadata{}, err
	}
	request.Header.Set("User-Agent", "DotaLocalHub/0.5")

	response, err := d.steamClient.Do(request)
	if err != nil {
		return MatchMetadata{}, fmt.Errorf("Steam Web API недоступен: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusTooManyRequests {
		return MatchMetadata{}, errors.New("Steam Web API вернул лимит обращений HTTP 429")
	}
	if response.StatusCode != http.StatusOK {
		return MatchMetadata{}, fmt.Errorf("Steam Web API вернул HTTP %d", response.StatusCode)
	}

	var payload struct {
		Result struct {
			Status        int              `json:"status"`
			StatusDetail  string           `json:"statusDetail"`
			StatusDetail2 string           `json:"status_detail"`
			MatchID       uint64           `json:"match_id"`
			Cluster       int              `json:"cluster"`
			ReplaySalt    uint64           `json:"replay_salt"`
			Duration      int              `json:"duration"`
			StartTime     int64            `json:"start_time"`
			RadiantWin    bool             `json:"radiant_win"`
			GameMode      int              `json:"game_mode"`
			LobbyType     int              `json:"lobby_type"`
			SeriesID      uint64           `json:"series_id"`
			SeriesType    int              `json:"series_type"`
			RadiantTeamID uint64           `json:"radiant_team_id"`
			DireTeamID    uint64           `json:"dire_team_id"`
			RadiantName   string           `json:"radiant_name"`
			DireName      string           `json:"dire_name"`
			RadiantScore  int              `json:"radiant_score"`
			DireScore     int              `json:"dire_score"`
			Players       []OpenDotaPlayer `json:"players"`
		} `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return MatchMetadata{}, err
	}
	result := payload.Result
	if result.MatchID == 0 {
		if result.Status != 0 {
			detail := strings.TrimSpace(result.StatusDetail)
			if detail == "" {
				detail = strings.TrimSpace(result.StatusDetail2)
			}
			if detail == "" {
				detail = fmt.Sprintf("status %d", result.Status)
			}
			return MatchMetadata{}, fmt.Errorf("Steam Web API не нашёл матч %d: %s", matchID, detail)
		}
		return MatchMetadata{}, fmt.Errorf("Steam Web API вернул пустой матч %d", matchID)
	}

	return MatchMetadata{
		MatchID:       result.MatchID,
		Cluster:       result.Cluster,
		ReplaySalt:    result.ReplaySalt,
		Duration:      result.Duration,
		StartTime:     result.StartTime,
		RadiantWin:    result.RadiantWin,
		GameMode:      result.GameMode,
		LobbyType:     result.LobbyType,
		SeriesID:      result.SeriesID,
		SeriesType:    result.SeriesType,
		RadiantTeamID: result.RadiantTeamID,
		DireTeamID:    result.DireTeamID,
		RadiantName:   result.RadiantName,
		DireName:      result.DireName,
		RadiantScore:  result.RadiantScore,
		DireScore:     result.DireScore,
		Players:       result.Players,
	}, nil
}

func (d *Downloader) steamBackoffError() error {
	d.steamStateMu.Lock()
	defer d.steamStateMu.Unlock()
	if time.Now().Before(d.steamDisabledUntil) {
		return fmt.Errorf("Steam Web API временно на паузе до %s после предыдущих ошибок", d.steamDisabledUntil.Format("15:04:05"))
	}
	return nil
}

func (d *Downloader) noteSteamSuccess() {
	d.steamStateMu.Lock()
	defer d.steamStateMu.Unlock()
	d.steamFailures = 0
	d.steamDisabledUntil = time.Time{}
}

func (d *Downloader) noteSteamFailure(err error) {
	d.steamStateMu.Lock()
	defer d.steamStateMu.Unlock()
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "429"), strings.Contains(message, "лимит"):
		d.steamFailures = 0
		d.steamDisabledUntil = time.Now().Add(2 * time.Minute)
	case strings.Contains(message, "временно на паузе"):
		return
	default:
		d.steamFailures++
		if d.steamFailures >= 2 {
			d.steamFailures = 0
			d.steamDisabledUntil = time.Now().Add(5 * time.Minute)
		}
	}
}

func (d *Downloader) OpenDotaMatchDetails(ctx context.Context, matchID uint64) (MatchMetadata, error) {
	endpoint := fmt.Sprintf("https://api.opendota.com/api/matches/%d", matchID)
	if d.config.OpenDotaAPIKey != "" {
		endpoint += "?api_key=" + url.QueryEscape(d.config.OpenDotaAPIKey)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return MatchMetadata{}, err
	}
	request.Header.Set("User-Agent", "DotaLocalHub/0.1")

	response, err := d.client.Do(request)
	if err != nil {
		return MatchMetadata{}, fmt.Errorf("OpenDota недоступна: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return MatchMetadata{}, fmt.Errorf("матч %d не найден в OpenDota", matchID)
	}
	if response.StatusCode != http.StatusOK {
		return MatchMetadata{}, fmt.Errorf("OpenDota вернула HTTP %d", response.StatusCode)
	}

	var metadata MatchMetadata
	if err := json.NewDecoder(response.Body).Decode(&metadata); err != nil {
		return metadata, err
	}
	if metadata.MatchID == 0 {
		metadata.MatchID = matchID
	}
	return metadata, nil
}

func (d *Downloader) StratzMatchDetails(ctx context.Context, matchID uint64) (MatchMetadata, error) {
	if d.config.StratzToken == "" {
		return MatchMetadata{}, errors.New("STRATZ token не настроен")
	}

	const query = `query MatchReplay($id: Long!) {
  match(id: $id) {
    id
    clusterId
    replaySalt
    durationSeconds
    startDateTime
    didRadiantWin
    gameMode
    lobbyType
    seriesId
    seriesType
    radiantTeamId
    direTeamId
    radiantTeam { name }
    direTeam { name }
    radiantKills
    direKills
    players {
      steamAccountId
      playerSlot
      steamAccount {
        name
        proSteamAccount { name }
      }
    }
  }
}`
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": map[string]any{"id": matchID},
	})
	if err != nil {
		return MatchMetadata{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.stratz.com/graphql", strings.NewReader(string(body)))
	if err != nil {
		return MatchMetadata{}, err
	}
	request.Header.Set("Authorization", "Bearer "+d.config.StratzToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "DotaLocalHub/0.6")

	response, err := d.client.Do(request)
	if err != nil {
		return MatchMetadata{}, fmt.Errorf("STRATZ недоступен: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return MatchMetadata{}, fmt.Errorf("STRATZ вернул HTTP %d", response.StatusCode)
	}

	var payload struct {
		Data struct {
			Match struct {
				ID              uint64 `json:"id"`
				ClusterID       int    `json:"clusterId"`
				ReplaySalt      uint64 `json:"replaySalt"`
				DurationSeconds int    `json:"durationSeconds"`
				StartDateTime   int64  `json:"startDateTime"`
				DidRadiantWin   bool   `json:"didRadiantWin"`
				GameMode        int    `json:"gameMode"`
				LobbyType       int    `json:"lobbyType"`
				SeriesID        uint64 `json:"seriesId"`
				SeriesType      int    `json:"seriesType"`
				RadiantTeamID   uint64 `json:"radiantTeamId"`
				DireTeamID      uint64 `json:"direTeamId"`
				RadiantTeam     struct {
					Name string `json:"name"`
				} `json:"radiantTeam"`
				DireTeam struct {
					Name string `json:"name"`
				} `json:"direTeam"`
				RadiantKills int `json:"radiantKills"`
				DireKills    int `json:"direKills"`
				Players      []struct {
					SteamAccountID uint32 `json:"steamAccountId"`
					PlayerSlot     int    `json:"playerSlot"`
					SteamAccount   struct {
						Name            string `json:"name"`
						ProSteamAccount struct {
							Name string `json:"name"`
						} `json:"proSteamAccount"`
					} `json:"steamAccount"`
				} `json:"players"`
			} `json:"match"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return MatchMetadata{}, err
	}
	if len(payload.Errors) > 0 {
		return MatchMetadata{}, fmt.Errorf("STRATZ GraphQL: %s", payload.Errors[0].Message)
	}
	result := payload.Data.Match
	if result.ID == 0 {
		return MatchMetadata{}, fmt.Errorf("STRATZ не нашёл матч %d", matchID)
	}
	players := make([]OpenDotaPlayer, 0, len(result.Players))
	for _, player := range result.Players {
		name := player.SteamAccount.ProSteamAccount.Name
		if name == "" {
			name = player.SteamAccount.Name
		}
		players = append(players, OpenDotaPlayer{
			AccountID:  player.SteamAccountID,
			PlayerSlot: normalizeStratzPlayerSlot(player.PlayerSlot),
			Name:       name,
			Persona:    player.SteamAccount.Name,
		})
	}
	return MatchMetadata{
		MatchID:       result.ID,
		Cluster:       result.ClusterID,
		ReplaySalt:    result.ReplaySalt,
		Duration:      result.DurationSeconds,
		StartTime:     result.StartDateTime,
		RadiantWin:    result.DidRadiantWin,
		GameMode:      result.GameMode,
		LobbyType:     result.LobbyType,
		SeriesID:      result.SeriesID,
		SeriesType:    result.SeriesType,
		RadiantTeamID: result.RadiantTeamID,
		DireTeamID:    result.DireTeamID,
		RadiantName:   result.RadiantTeam.Name,
		DireName:      result.DireTeam.Name,
		RadiantScore:  result.RadiantKills,
		DireScore:     result.DireKills,
		Players:       players,
	}, nil
}

func normalizeStratzPlayerSlot(slot int) int {
	if slot >= 5 && slot <= 9 {
		return slot + 123
	}
	return slot
}

func (d *Downloader) TeamMatches(ctx context.Context, teamID uint64) ([]TeamMatchSummary, error) {
	if teamID == 0 {
		return nil, errors.New("у команды не указан OpenDota team ID")
	}
	matches, explorerErr := d.OpenDotaExplorerTeamMatches(ctx, teamID)
	if explorerErr == nil {
		return matches, nil
	}

	endpoint := fmt.Sprintf("https://api.opendota.com/api/teams/%d/matches", teamID)
	if d.config.OpenDotaAPIKey != "" {
		endpoint += "?api_key=" + url.QueryEscape(d.config.OpenDotaAPIKey)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "DotaLocalHub/0.2")

	response, err := d.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("OpenDota Explorer: %v; OpenDota teams: %w", explorerErr, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenDota вернула HTTP %d", response.StatusCode)
	}

	matches = nil
	if err := json.NewDecoder(response.Body).Decode(&matches); err != nil {
		return nil, err
	}
	return matches, nil
}

type teamExplorerRow struct {
	MatchID       uint64 `json:"match_id"`
	RadiantWin    bool   `json:"radiant_win"`
	RadiantScore  int    `json:"radiant_score"`
	DireScore     int    `json:"dire_score"`
	Duration      int    `json:"duration"`
	StartTime     int64  `json:"start_time"`
	LeagueID      int    `json:"leagueid"`
	Cluster       int    `json:"cluster"`
	RadiantTeamID uint64 `json:"radiant_team_id"`
	DireTeamID    uint64 `json:"dire_team_id"`
	LeagueName    string `json:"league_name"`
	SeriesID      uint64 `json:"series_id"`
	SeriesType    int    `json:"series_type"`
	RadiantName   string `json:"radiant_name"`
	DireName      string `json:"dire_name"`
	RadiantLogo   string `json:"radiant_logo"`
	DireLogo      string `json:"dire_logo"`
}

func (d *Downloader) OpenDotaExplorerTeamMatches(ctx context.Context, teamID uint64) ([]TeamMatchSummary, error) {
	radiantRows, err := d.openDotaExplorerTeamRows(ctx, teamID, "radiant_team_id")
	if err != nil {
		return nil, err
	}
	direRows, err := d.openDotaExplorerTeamRows(ctx, teamID, "dire_team_id")
	if err != nil {
		return nil, err
	}
	rows := append(radiantRows, direRows...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].StartTime > rows[j].StartTime })

	matches := make([]TeamMatchSummary, 0, min(100, len(rows)))
	seen := make(map[uint64]bool, len(rows))
	for _, row := range rows {
		if len(matches) == 100 {
			break
		}
		if row.MatchID == 0 || seen[row.MatchID] || (row.RadiantTeamID != teamID && row.DireTeamID != teamID) {
			continue
		}
		seen[row.MatchID] = true
		matches = append(matches, teamSummaryFromExplorer(row, teamID))
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("OpenDota Explorer не вернул матчи команды %d", teamID)
	}
	return matches, nil
}

func (d *Downloader) OpenDotaExplorerTournamentMatches(ctx context.Context, teamIDs []uint64, limit int) ([]teamExplorerRow, error) {
	unique := make(map[uint64]struct{}, len(teamIDs))
	for _, teamID := range teamIDs {
		if teamID != 0 {
			unique[teamID] = struct{}{}
		}
	}
	ids := make([]uint64, 0, len(unique))
	for teamID := range unique {
		ids = append(ids, teamID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) == 0 {
		return nil, errors.New("tournament has no team IDs for match discovery")
	}
	if limit < 1 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}

	values := make([]string, 0, len(ids))
	for _, teamID := range ids {
		values = append(values, strconv.FormatUint(teamID, 10))
	}
	idList := strings.Join(values, ",")
	query := fmt.Sprintf("select m.match_id,m.radiant_win,m.radiant_score,m.dire_score,m.duration,m.start_time,m.leagueid,m.cluster,m.radiant_team_id,m.dire_team_id,m.series_id,m.series_type,l.name as league_name,rt.name as radiant_name,dt.name as dire_name,rt.logo_url as radiant_logo,dt.logo_url as dire_logo from matches m left join leagues l on l.leagueid=m.leagueid left join teams rt on rt.team_id=m.radiant_team_id left join teams dt on dt.team_id=m.dire_team_id where m.radiant_team_id in (%s) or m.dire_team_id in (%s) order by m.start_time desc limit %d", idList, idList, limit)
	var payload struct {
		Rows []teamExplorerRow `json:"rows"`
	}
	if err := d.openDotaExplorer(ctx, query, &payload); err != nil {
		return nil, err
	}
	return payload.Rows, nil
}

func (d *Downloader) openDotaExplorerTeamRows(ctx context.Context, teamID uint64, teamColumn string) ([]teamExplorerRow, error) {
	query := fmt.Sprintf("select m.match_id,m.radiant_win,m.radiant_score,m.dire_score,m.duration,m.start_time,m.leagueid,m.cluster,m.radiant_team_id,m.dire_team_id,m.series_id,m.series_type,l.name as league_name,rt.name as radiant_name,dt.name as dire_name,rt.logo_url as radiant_logo,dt.logo_url as dire_logo from matches m left join leagues l on l.leagueid=m.leagueid left join teams rt on rt.team_id=m.radiant_team_id left join teams dt on dt.team_id=m.dire_team_id where m.%s=%d order by m.match_id desc limit 100", teamColumn, teamID)
	var payload struct {
		Rows []teamExplorerRow `json:"rows"`
	}
	if err := d.openDotaExplorer(ctx, query, &payload); err != nil {
		return nil, err
	}
	return payload.Rows, nil
}

func (d *Downloader) openDotaExplorer(ctx context.Context, query string, destination any) error {
	values := url.Values{"sql": []string{query}}
	endpoint := "https://api.opendota.com/api/explorer?" + values.Encode()
	curlCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(curlCtx, "curl", "--fail", "--silent", "--show-error", "--compressed", "--ipv4", "--max-time", "12", "--user-agent", "DotaLocalHub/0.7", endpoint)
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("OpenDota Explorer curl: %w", err)
	}
	if err := json.Unmarshal(output, destination); err != nil {
		return fmt.Errorf("OpenDota Explorer вернул некорректный JSON: %w", err)
	}
	return nil
}

func teamSummaryFromExplorer(row teamExplorerRow, teamID uint64) TeamMatchSummary {
	isRadiant := row.RadiantTeamID == teamID
	opposingTeamID := row.RadiantTeamID
	opposingTeamName := row.RadiantName
	opposingTeamLogo := row.RadiantLogo
	if isRadiant {
		opposingTeamID = row.DireTeamID
		opposingTeamName = row.DireName
		opposingTeamLogo = row.DireLogo
	}
	return TeamMatchSummary{
		MatchID: row.MatchID, RadiantWin: row.RadiantWin,
		RadiantScore: row.RadiantScore, DireScore: row.DireScore,
		Radiant: isRadiant, Duration: row.Duration, StartTime: row.StartTime,
		LeagueID: row.LeagueID, LeagueName: row.LeagueName,
		SeriesID: row.SeriesID, SeriesType: row.SeriesType,
		Cluster: row.Cluster, OpposingTeamID: opposingTeamID,
		OpposingTeamName: opposingTeamName, OpposingTeamLogo: opposingTeamLogo,
	}
}

func (d *Downloader) DownloadReplay(
	ctx context.Context,
	metadata MatchMetadata,
	destination string,
	progress func(downloaded, total int64),
) error {
	urls := []string{
		fmt.Sprintf("https://replay%d.valve.net/570/%d_%d.dem.bz2", metadata.Cluster, metadata.MatchID, metadata.ReplaySalt),
		fmt.Sprintf("http://replay%d.valve.net/570/%d_%d.dem.bz2", metadata.Cluster, metadata.MatchID, metadata.ReplaySalt),
	}

	var lastError error
	for _, replayURL := range urls {
		if err := d.download(ctx, replayURL, destination, progress); err == nil {
			return nil
		} else {
			lastError = err
		}
	}
	return fmt.Errorf("не удалось скачать реплей Valve: %w", lastError)
}

func (d *Downloader) download(
	ctx context.Context,
	replayURL string,
	destination string,
	progress func(downloaded, total int64),
) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, replayURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "DotaLocalHub/0.1")

	client := &http.Client{Timeout: 15 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &replayHTTPError{URL: replayURL, StatusCode: response.StatusCode}
	}
	if limit := d.maxReplayBytes(); limit > 0 && response.ContentLength > limit {
		return fmt.Errorf("%w: %.1f MiB > %.1f MiB", errReplayTooLarge, bytesToMiB(response.ContentLength), bytesToMiB(limit))
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	temporary := destination + ".part"
	file, err := os.Create(temporary)
	if err != nil {
		return err
	}

	writer := &progressWriter{
		writer: file,
		total:  response.ContentLength,
		limit:  d.maxReplayBytes(),
		update: progress,
	}
	_, copyErr := io.Copy(writer, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(temporary)
		return closeErr
	}
	return os.Rename(temporary, destination)
}

func decompressReplay(source, destination string, maxBytes int64) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	temporary := destination + ".part"
	output, err := os.Create(temporary)
	if err != nil {
		return err
	}

	reader, closeReader, err := replayArchiveReader(input)
	if err != nil {
		output.Close()
		os.Remove(temporary)
		return err
	}

	_, copyErr := io.Copy(&limitedWriter{writer: output, limit: maxBytes}, reader)
	closeReader()
	closeErr := output.Close()
	if copyErr != nil {
		os.Remove(temporary)
		if errors.Is(copyErr, errReplayTooLarge) {
			return copyErr
		}
		return fmt.Errorf("%w: %v", errInvalidReplayArchive, copyErr)
	}
	if closeErr != nil {
		os.Remove(temporary)
		return closeErr
	}
	return os.Rename(temporary, destination)
}

func replayArchiveReader(input io.Reader) (io.Reader, func(), error) {
	buffered := bufio.NewReader(input)
	header, err := buffered.Peek(4)
	if err != nil {
		return nil, func() {}, fmt.Errorf("%w: cannot read header: %v", errInvalidReplayArchive, err)
	}

	switch {
	case bytes.Equal(header, zstdReplayMagic):
		decoder, err := zstd.NewReader(
			buffered,
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderLowmem(true),
		)
		if err != nil {
			return nil, func() {}, fmt.Errorf("%w: zstd decoder: %v", errInvalidReplayArchive, err)
		}
		return decoder, decoder.Close, nil
	case header[0] == 'B' && header[1] == 'Z' && header[2] == 'h':
		return bzip2.NewReader(buffered), func() {}, nil
	default:
		return nil, func() {}, fmt.Errorf("%w: magic % x", errUnsupportedReplayArchive, header)
	}
}

type progressWriter struct {
	writer     io.Writer
	downloaded int64
	total      int64
	limit      int64
	update     func(downloaded, total int64)
	lastUpdate time.Time
}

func (w *progressWriter) Write(data []byte) (int, error) {
	if w.limit > 0 && w.downloaded+int64(len(data)) > w.limit {
		return 0, fmt.Errorf("%w: %.1f MiB > %.1f MiB", errReplayTooLarge, bytesToMiB(w.downloaded+int64(len(data))), bytesToMiB(w.limit))
	}
	n, err := w.writer.Write(data)
	w.downloaded += int64(n)
	if w.update != nil && (time.Since(w.lastUpdate) > 300*time.Millisecond || err != nil) {
		w.update(w.downloaded, w.total)
		w.lastUpdate = time.Now()
	}
	return n, err
}

type limitedWriter struct {
	writer  io.Writer
	written int64
	limit   int64
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if w.limit > 0 && w.written+int64(len(data)) > w.limit {
		return 0, fmt.Errorf("%w: %.1f MiB > %.1f MiB", errReplayTooLarge, bytesToMiB(w.written+int64(len(data))), bytesToMiB(w.limit))
	}
	n, err := w.writer.Write(data)
	w.written += int64(n)
	return n, err
}

func (d *Downloader) maxReplayBytes() int64 {
	if d.config.MaxReplayBytes > 0 {
		return d.config.MaxReplayBytes
	}
	return defaultMaxReplayBytes
}

func bytesToMiB(value int64) float64 {
	return float64(value) / (1024 * 1024)
}
