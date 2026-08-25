package main

import "time"

const steamID64Base uint64 = 76561197960265728
const currentParserVersion = 2

type Config struct {
	Listen                    string   `json:"listen"`
	AllowedOrigins            []string `json:"allowedOrigins"`
	OpenDotaAPIKey            string   `json:"openDotaApiKey"`
	SteamAPIKey               string   `json:"steamApiKey"`
	SteamAPIKeyFile           string   `json:"steamApiKeyFile"`
	SteamAPIRequestIntervalMs int      `json:"steamApiRequestIntervalMs"`
	StratzToken               string   `json:"stratzToken"`
	StratzTokenFile           string   `json:"stratzTokenFile"`
	KeepCompressed            bool     `json:"keepCompressed"`
	MaxUploadBytes            int64    `json:"maxUploadBytes"`
	MaxReplayBytes            int64    `json:"maxReplayBytes"`
	GCBaseURL                 string   `json:"gcBaseUrl"`
	GCMonitorEnabled          bool     `json:"gcMonitorEnabled"`
	GCMonitorIntervalSeconds  int      `json:"gcMonitorIntervalSeconds"`
	GCHistoryMatches          int      `json:"gcHistoryMatches"`
	GCInitialLookbackHours    int      `json:"gcInitialLookbackHours"`
	GCMaxNewMatchesPerCycle   int      `json:"gcMaxNewMatchesPerCycle"`
}

type OpenDotaPlayer struct {
	AccountID  uint32 `json:"account_id"`
	PlayerSlot int    `json:"player_slot"`
	Name       string `json:"name"`
	Persona    string `json:"personaname"`
}

type MatchMetadata struct {
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
	LeagueName    string           `json:"league_name"`
	LeagueID      int              `json:"league_id,omitempty"`
	RadiantLogo   string           `json:"radiant_logo,omitempty"`
	DireLogo      string           `json:"dire_logo,omitempty"`
	Players       []OpenDotaPlayer `json:"players"`
}

type TeamMatchSummary struct {
	MatchID          uint64 `json:"match_id"`
	RadiantWin       bool   `json:"radiant_win"`
	RadiantScore     int    `json:"radiant_score"`
	DireScore        int    `json:"dire_score"`
	Radiant          bool   `json:"radiant"`
	Duration         int    `json:"duration"`
	StartTime        int64  `json:"start_time"`
	LeagueID         int    `json:"leagueid"`
	LeagueName       string `json:"league_name"`
	SeriesID         uint64 `json:"series_id"`
	SeriesType       int    `json:"series_type"`
	Cluster          int    `json:"cluster"`
	OpposingTeamID   uint64 `json:"opposing_team_id"`
	OpposingTeamName string `json:"opposing_team_name"`
	OpposingTeamLogo string `json:"opposing_team_logo"`
}

type ParsedMatch struct {
	PlaybackSeconds     float32        `json:"playbackSeconds"`
	GameDurationSeconds float64        `json:"gameDurationSeconds"`
	GameDurationSource  string         `json:"gameDurationSource"`
	MatchID             uint64         `json:"matchId,omitempty"`
	GameMode            int            `json:"gameMode,omitempty"`
	RadiantWin          bool           `json:"radiantWin"`
	LeagueID            uint32         `json:"leagueId,omitempty"`
	RadiantTeamID       uint64         `json:"radiantTeamId,omitempty"`
	DireTeamID          uint64         `json:"direTeamId,omitempty"`
	RadiantTeamTag      string         `json:"radiantTeamTag,omitempty"`
	DireTeamTag         string         `json:"direTeamTag,omitempty"`
	EndTime             uint32         `json:"endTime,omitempty"`
	Players             []PlayerRecord `json:"players"`
}

type MatchRecord struct {
	MatchID      uint64         `json:"matchId"`
	Cluster      int            `json:"cluster"`
	ReplaySalt   uint64         `json:"replaySalt"`
	StartTime    int64          `json:"startTime"`
	Duration     int            `json:"duration"`
	RadiantWin   bool           `json:"radiantWin"`
	GameMode     int            `json:"gameMode"`
	LobbyType    int            `json:"lobbyType"`
	ReplayPath   string         `json:"replayPath"`
	ParsedAt     time.Time      `json:"parsedAt"`
	RadiantKills int32          `json:"radiantKills"`
	DireKills    int32          `json:"direKills"`
	Players      []PlayerRecord `json:"players,omitempty"`
}

type ParseRetryRecord struct {
	UserID         string `json:"-"`
	MatchID        uint64 `json:"matchId"`
	Status         string `json:"status"`
	Error          string `json:"error"`
	Attempts       int    `json:"attempts"`
	Source         string `json:"source"`
	FirstAttemptAt int64  `json:"firstAttemptAt"`
	LastAttemptAt  int64  `json:"lastAttemptAt"`
	NextAttemptAt  int64  `json:"nextAttemptAt"`
	UpdatedAt      int64  `json:"updatedAt"`
}

type PlayerRecord struct {
	PlayerIndex            int     `json:"playerIndex"`
	Name                   string  `json:"name"`
	SteamID                uint64  `json:"steamId"`
	AccountID              uint32  `json:"accountId"`
	HeroID                 int32   `json:"heroId"`
	Team                   string  `json:"team"`
	TeamSlot               int     `json:"teamSlot"`
	Kills                  int32   `json:"kills"`
	Deaths                 int32   `json:"deaths"`
	Assists                int32   `json:"assists"`
	CS                     int32   `json:"cs"`
	LastHits               int32   `json:"lastHits"`
	Denies                 int32   `json:"denies"`
	GPM                    float64 `json:"gpm"`
	TotalEarnedGold        int32   `json:"totalEarnedGold"`
	Madstone               int32   `json:"madstone"`
	TowerKills             int32   `json:"towerKills"`
	WardsPlaced            int32   `json:"wardsPlaced"`
	CampsStacked           int32   `json:"campsStacked"`
	RunesGrabbed           int32   `json:"runesGrabbed"`
	Watchers               int32   `json:"watchers"`
	Lotuses                int32   `json:"lotuses"`
	RoshanKills            int32   `json:"roshanKills"`
	TeamfightParticipation float32 `json:"teamfightParticipation"`
	Stuns                  float32 `json:"stuns"`
	Tormentors             int32   `json:"tormentors"`
	CourierKills           int32   `json:"courierKills"`
	FirstBlood             int32   `json:"firstBlood"`
	Smokes                 int32   `json:"smokes"`
	ProName                string  `json:"proName,omitempty"`
	AvatarURL              string  `json:"avatarUrl,omitempty"`
	FantasyPoints          float64 `json:"fantasyPoints"`
}

type TournamentTeam struct {
	Slug          string       `json:"slug"`
	Name          string       `json:"name"`
	Tag           string       `json:"tag"`
	Status        string       `json:"status"`
	SlotOrder     int          `json:"slotOrder"`
	OpenDotaID    uint64       `json:"openDotaId"`
	HistoryTeamID uint64       `json:"historyTeamId"`
	LogoURL       string       `json:"logoUrl"`
	Roster        []TeamPlayer `json:"roster,omitempty"`
	MatchCount    int          `json:"matchCount"`
	ParsedCount   int          `json:"parsedCount"`
}

type TeamPlayer struct {
	AccountID         uint32              `json:"accountId"`
	Name              string              `json:"name"`
	PersonaName       string              `json:"personaName,omitempty"`
	AvatarURL         string              `json:"avatarUrl,omitempty"`
	PortraitURL       string              `json:"portraitUrl,omitempty"`
	Position          int                 `json:"position"`
	ProfileCheckedAt  int64               `json:"-"`
	PortraitCheckedAt int64               `json:"-"`
	Stats             *PlayerFantasyStats `json:"stats,omitempty"`
}

type TeamMatchRecord struct {
	TeamSlug       string `json:"teamSlug"`
	MatchID        uint64 `json:"matchId"`
	LeagueID       int    `json:"leagueId,omitempty"`
	StartTime      int64  `json:"startTime"`
	Duration       int    `json:"duration"`
	OpponentTeamID uint64 `json:"opponentTeamId"`
	OpponentName   string `json:"opponentName"`
	OpponentLogo   string `json:"opponentLogo"`
	TeamWon        bool   `json:"teamWon"`
	TeamScore      int    `json:"teamScore"`
	OpponentScore  int    `json:"opponentScore"`
	LeagueName     string `json:"leagueName"`
	SeriesID       uint64 `json:"seriesId"`
	SeriesType     int    `json:"seriesType"`
	RosterMatched  bool   `json:"rosterMatched"`
	ParseStatus    string `json:"parseStatus"`
	ParseError     string `json:"parseError,omitempty"`
	Included       bool   `json:"included"`
}

type TeamDetail struct {
	Team    TournamentTeam    `json:"team"`
	Matches []TeamMatchRecord `json:"matches"`
}

type FantasyMetric struct {
	Key           string  `json:"key"`
	Label         string  `json:"label"`
	Average       float64 `json:"average"`
	AveragePoints float64 `json:"averagePoints"`
}

type PlayerFantasyStats struct {
	Matches     int             `json:"matches"`
	Metrics     []FantasyMetric `json:"metrics"`
	TotalPoints float64         `json:"totalPoints"`
	BestMatch   FantasyRecord   `json:"bestMatch"`
	BestSeries  FantasyRecord   `json:"bestSeries"`
}

type FantasyRecord struct {
	Points   float64  `json:"points"`
	MatchIDs []uint64 `json:"matchIds"`
}

type PlayerProfile struct {
	AccountID   uint32
	PersonaName string
	ProName     string
	AvatarURL   string
}

type HeroInfo struct {
	Name     string `json:"name"`
	ImageURL string `json:"imageUrl"`
}

type Job struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	UserID       string    `json:"-"`
	MatchID      uint64    `json:"matchId,omitempty"`
	TeamSlug     string    `json:"teamSlug,omitempty"`
	OriginalName string    `json:"originalName,omitempty"`
	ReservedSlot bool      `json:"-"`
	State        string    `json:"state"`
	Message      string    `json:"message"`
	Progress     int       `json:"progress"`
	Completed    int       `json:"completed,omitempty"`
	Failed       int       `json:"failed,omitempty"`
	Total        int       `json:"total,omitempty"`
	Error        string    `json:"error,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type ParseQueueSnapshot struct {
	Running *Job  `json:"running,omitempty"`
	Queued  []Job `json:"queued"`
}

type ParseRequest struct {
	MatchID string `json:"matchId"`
}

type LocalReplayRequest struct {
	MatchID string `json:"matchId,omitempty"`
}

type SelectionRequest struct {
	Mode        string           `json:"mode,omitempty"`
	MatchID     uint64           `json:"matchId,omitempty"`
	LeagueName  string           `json:"leagueName,omitempty"`
	LeagueNames []string         `json:"leagueNames,omitempty"`
	Limit       int              `json:"limit,omitempty"`
	All         bool             `json:"all,omitempty"`
	Included    bool             `json:"included"`
	Matches     []MatchSelection `json:"matches,omitempty"`
}

type MatchSelection struct {
	MatchID  uint64 `json:"matchId"`
	Included bool   `json:"included"`
}

type SelectionLeague struct {
	Name          string `json:"name"`
	MatchCount    int    `json:"matchCount"`
	IncludedCount int    `json:"includedCount"`
}

type SelectionOverview struct {
	Leagues         []SelectionLeague `json:"leagues"`
	MaxMatches      int               `json:"maxMatches"`
	SelectedMatches int               `json:"selectedMatches"`
	AllMatches      int               `json:"allMatches"`
	SelectedPerTeam int               `json:"selectedPerTeam"`
	AllSelected     bool              `json:"allSelected"`
}
