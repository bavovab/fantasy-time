package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxGSIRequestBytes = 4 << 20

type flexibleUint64 uint64

func (value *flexibleUint64) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		unquoted, err := strconv.Unquote(raw)
		if err != nil {
			return err
		}
		raw = unquoted
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid unsigned integer")
	}
	*value = flexibleUint64(parsed)
	return nil
}

type gsiEnvelope struct {
	Auth struct {
		Token string `json:"token"`
	} `json:"auth"`
	Provider struct {
		AppID int `json:"appid"`
	} `json:"provider"`
	Map       gsiMap                                   `json:"map"`
	Player    map[string]map[string]gsiPlayer          `json:"player"`
	Hero      map[string]map[string]gsiHero            `json:"hero"`
	Items     map[string]map[string]map[string]gsiItem `json:"items"`
	Buildings map[string]map[string]gsiBuilding        `json:"buildings"`
	Events    []map[string]json.RawMessage             `json:"events"`
	Minimap   map[string]gsiMinimapElement             `json:"minimap"`
	Roshan    *gsiRoshan                               `json:"roshan"`
	Teleports map[string]verifiedTeleportObservation   `json:"verified_teleports,omitempty"`
}

type gsiMap struct {
	MatchID              flexibleUint64 `json:"matchid"`
	GameTime             int64          `json:"game_time"`
	ClockTime            int64          `json:"clock_time"`
	RadiantScore         int            `json:"radiant_score"`
	DireScore            int            `json:"dire_score"`
	GameState            string         `json:"game_state"`
	Paused               bool           `json:"paused"`
	Daytime              bool           `json:"daytime"`
	RoshanState          string         `json:"roshan_state"`
	RoshanStateEndSecond int            `json:"roshan_state_end_seconds"`
}

type gsiPlayer struct {
	Name        string `json:"name"`
	Kills       int    `json:"kills"`
	Deaths      int    `json:"deaths"`
	Assists     int    `json:"assists"`
	LastHits    int    `json:"last_hits"`
	Denies      int    `json:"denies"`
	Gold        int    `json:"gold"`
	GPM         int    `json:"gpm"`
	XPM         int    `json:"xpm"`
	NetWorth    int    `json:"net_worth"`
	TeamName    string `json:"team_name"`
	PlayerSlot  int    `json:"player_slot"`
	TeamSlot    int    `json:"team_slot"`
	TowerDamage int    `json:"tower_damage"`
}

type gsiHero struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Level          int     `json:"level"`
	X              float64 `json:"xpos"`
	Y              float64 `json:"ypos"`
	Alive          bool    `json:"alive"`
	RespawnSeconds int     `json:"respawn_seconds"`
	Health         int     `json:"health"`
	MaxHealth      int     `json:"max_health"`
}

type gsiBuilding struct {
	Health    int `json:"health"`
	MaxHealth int `json:"max_health"`
}

type gsiItem struct {
	Name     string  `json:"name"`
	Cooldown float64 `json:"cooldown"`
}

type gsiMinimapElement struct {
	X             float64 `json:"xpos"`
	Y             float64 `json:"ypos"`
	RemainingTime float64 `json:"remainingtime"`
	EventDuration float64 `json:"eventduration"`
	Image         string  `json:"image"`
	Team          int     `json:"team"`
	Name          string  `json:"name"`
	UnitName      string  `json:"unitname"`
}

type gsiRoshan struct {
	Health             int     `json:"health"`
	MaxHealth          int     `json:"max_health"`
	Alive              bool    `json:"alive"`
	SpawnPhase         int     `json:"spawn_phase"`
	PhaseTimeRemaining float64 `json:"phase_time_remaining"`
	X                  float64 `json:"xpos"`
	Y                  float64 `json:"ypos"`
}

// Teleport targets are deliberately not inferred from position jumps. A native
// observer helper may add this extension only after it has decoded a verified
// DotaTV order/channel target.
type verifiedTeleportObservation struct {
	Channeling  bool    `json:"channeling"`
	TargetX     float64 `json:"target_x"`
	TargetY     float64 `json:"target_y"`
	RemainingMS int     `json:"remaining_ms"`
	Verified    bool    `json:"verified"`
}

type telemetryState struct {
	Players              map[string]*telemetryPlayerState
	DestroyedBy          map[string]string
	SeenEvents           map[string]struct{}
	BuildingAlive        map[string]bool
	HasBuildingSample    bool
	TeleportOwners       map[string]string
	RoshanKilledGameTime int64
}

type telemetryPlayerState struct {
	LastHealth      int
	DamageSequence  uint64
	WasAlive        bool
	HasSample       bool
	DeathX          float64
	DeathY          float64
	HasDeathPoint   bool
	LastTowerDamage int
	LastTPCooldown  float64
}

type publicLiveSnapshot struct {
	ID              string             `json:"id"`
	MatchID         uint64             `json:"matchId"`
	State           string             `json:"state"`
	Message         string             `json:"message"`
	UpdatedAt       string             `json:"updatedAt"`
	GameTimeSeconds int64              `json:"gameTimeSeconds"`
	Radiant         team               `json:"radiant"`
	Dire            team               `json:"dire"`
	Score           publicScore        `json:"score"`
	Map             publicMap          `json:"map"`
	Players         []publicLivePlayer `json:"players"`
	Paused          bool               `json:"paused,omitempty"`
	Spectators      uint32             `json:"spectators,omitempty"`
	DelaySeconds    int64              `json:"delaySeconds,omitempty"`
	Source          string             `json:"source,omitempty"`
}

type publicScore struct {
	Radiant         int `json:"radiant"`
	Dire            int `json:"dire"`
	RadiantGoldLead int `json:"radiantGoldLead"`
}

type publicMap struct {
	ImageURL  string             `json:"imageUrl"`
	Players   []publicLivePlayer `json:"players"`
	Buildings []publicBuilding   `json:"buildings,omitempty"`
	Roshan    *publicRoshan      `json:"roshan,omitempty"`
}

type publicLivePlayer struct {
	ID             string          `json:"id"`
	PlayerName     string          `json:"playerName"`
	Position       int             `json:"position,omitempty"`
	Team           string          `json:"team"`
	HeroID         int             `json:"heroId"`
	Level          int             `json:"level"`
	Kills          int             `json:"kills"`
	Deaths         int             `json:"deaths"`
	Assists        int             `json:"assists"`
	LastHits       int             `json:"lastHits"`
	Denies         int             `json:"denies"`
	NetWorth       int             `json:"netWorth"`
	Gold           int             `json:"gold"`
	GPM            int             `json:"gpm"`
	XPM            int             `json:"xpm"`
	Health         int             `json:"health"`
	MaxHealth      int             `json:"maxHealth"`
	Alive          bool            `json:"alive"`
	RespawnSeconds int             `json:"respawnSeconds"`
	X              float64         `json:"x"`
	Y              float64         `json:"y"`
	DamageSequence uint64          `json:"damageSequence"`
	StatsAvailable bool            `json:"statsAvailable"`
	Teleport       *publicTeleport `json:"teleport,omitempty"`
}

type publicTeleport struct {
	Channeling  bool    `json:"channeling"`
	TargetX     float64 `json:"targetX"`
	TargetY     float64 `json:"targetY"`
	RemainingMS int     `json:"remainingMs"`
}

type publicBuilding struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Team        string  `json:"team"`
	Kind        string  `json:"kind"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Health      int     `json:"health"`
	MaxHealth   int     `json:"maxHealth"`
	Alive       bool    `json:"alive"`
	DestroyedBy string  `json:"destroyedBy,omitempty"`
}

type publicRoshan struct {
	Alive                bool    `json:"alive"`
	X                    float64 `json:"x"`
	Y                    float64 `json:"y"`
	Pit                  string  `json:"pit"`
	RespawnMinSeconds    int     `json:"respawnMinSeconds,omitempty"`
	RespawnMaxSeconds    int     `json:"respawnMaxSeconds,omitempty"`
	RespawnWindowSeconds int     `json:"respawnWindowSeconds,omitempty"`
}

type buildingLayout struct {
	Key       string
	Team      string
	Kind      string
	Name      string
	X         float64
	Y         float64
	MaxHealth int
}

// Positions are calibrated against the building layers bundled with the
// current 320x320 Dota minimap. T1/T2 values use the exact icon centres; base
// structures use current replay entity positions projected onto the same layer.
var currentBuildingLayout = []buildingLayout{
	{Key: "dota_goodguys_tower1_top", Team: "radiant", Kind: "tower", Name: "Башня Radiant T1, верхняя линия", X: 17.19, Y: 38.44, MaxHealth: 1800},
	{Key: "dota_goodguys_tower2_top", Team: "radiant", Kind: "tower", Name: "Башня Radiant T2, верхняя линия", X: 17.19, Y: 53.13, MaxHealth: 2500},
	{Key: "dota_goodguys_tower3_top", Team: "radiant", Kind: "tower", Name: "Башня Radiant T3, верхняя линия", X: 16.34, Y: 67.52, MaxHealth: 2500},
	{Key: "dota_goodguys_tower1_mid", Team: "radiant", Kind: "tower", Name: "Башня Radiant T1, центральная линия", X: 42.22, Y: 56.50, MaxHealth: 1800},
	{Key: "dota_goodguys_tower2_mid", Team: "radiant", Kind: "tower", Name: "Башня Radiant T2, центральная линия", X: 33.16, Y: 64.31, MaxHealth: 2500},
	{Key: "dota_goodguys_tower3_mid", Team: "radiant", Kind: "tower", Name: "Башня Radiant T3, центральная линия", X: 26.12, Y: 71.43, MaxHealth: 2500},
	{Key: "dota_goodguys_tower1_bot", Team: "radiant", Kind: "tower", Name: "Башня Radiant T1, нижняя линия", X: 77.81, Y: 82.50, MaxHealth: 1800},
	{Key: "dota_goodguys_tower2_bot", Team: "radiant", Kind: "tower", Name: "Башня Radiant T2, нижняя линия", X: 49.38, Y: 83.44, MaxHealth: 2500},
	{Key: "dota_goodguys_tower3_bot", Team: "radiant", Kind: "tower", Name: "Башня Radiant T3, нижняя линия", X: 29.92, Y: 81.18, MaxHealth: 2500},
	{Key: "dota_goodguys_tower4_top", Team: "radiant", Kind: "tower", Name: "Башня Radiant T4, верхняя", X: 20.64, Y: 74.62, MaxHealth: 2600},
	{Key: "dota_goodguys_tower4_bot", Team: "radiant", Kind: "tower", Name: "Башня Radiant T4, нижняя", X: 22.15, Y: 76.79, MaxHealth: 2600},
	{Key: "good_rax_melee_top", Team: "radiant", Kind: "barracks", Name: "Казарма ближнего боя Radiant, верх", X: 17.67, Y: 69.10, MaxHealth: 2200},
	{Key: "good_rax_range_top", Team: "radiant", Kind: "barracks", Name: "Казарма дальнего боя Radiant, верх", X: 15.01, Y: 69.10, MaxHealth: 1300},
	{Key: "good_rax_melee_mid", Team: "radiant", Kind: "barracks", Name: "Казарма ближнего боя Radiant, центр", X: 26.04, Y: 73.14, MaxHealth: 2200},
	{Key: "good_rax_range_mid", Team: "radiant", Kind: "barracks", Name: "Казарма дальнего боя Radiant, центр", X: 24.35, Y: 71.56, MaxHealth: 1300},
	{Key: "good_rax_melee_bot", Team: "radiant", Kind: "barracks", Name: "Казарма ближнего боя Radiant, низ", X: 28.40, Y: 82.48, MaxHealth: 2200},
	{Key: "good_rax_range_bot", Team: "radiant", Kind: "barracks", Name: "Казарма дальнего боя Radiant, низ", X: 28.40, Y: 79.83, MaxHealth: 1300},
	{Key: "dota_goodguys_fort", Team: "radiant", Kind: "ancient", Name: "Древний Radiant", X: 19.43, Y: 77.21, MaxHealth: 4500},

	{Key: "dota_badguys_tower1_top", Team: "dire", Kind: "tower", Name: "Башня Dire T1, верхняя линия", X: 23.44, Y: 19.06, MaxHealth: 1800},
	{Key: "dota_badguys_tower2_top", Team: "dire", Kind: "tower", Name: "Башня Dire T2, верхняя линия", X: 49.69, Y: 18.75, MaxHealth: 2500},
	{Key: "dota_badguys_tower3_top", Team: "dire", Kind: "tower", Name: "Башня Dire T3, верхняя линия", X: 68.94, Y: 19.82, MaxHealth: 2500},
	{Key: "dota_badguys_tower1_mid", Team: "dire", Kind: "tower", Name: "Башня Dire T1, центральная линия", X: 52.78, Y: 46.94, MaxHealth: 1800},
	{Key: "dota_badguys_tower2_mid", Team: "dire", Kind: "tower", Name: "Башня Dire T2, центральная линия", X: 63.72, Y: 38.81, MaxHealth: 2500},
	{Key: "dota_badguys_tower3_mid", Team: "dire", Kind: "tower", Name: "Башня Dire T3, центральная линия", X: 72.82, Y: 30.36, MaxHealth: 2500},
	{Key: "dota_badguys_tower1_bot", Team: "dire", Kind: "tower", Name: "Башня Dire T1, нижняя линия", X: 83.13, Y: 60.94, MaxHealth: 1800},
	{Key: "dota_badguys_tower2_bot", Team: "dire", Kind: "tower", Name: "Башня Dire T2, нижняя линия", X: 83.13, Y: 49.06, MaxHealth: 2500},
	{Key: "dota_badguys_tower3_bot", Team: "dire", Kind: "tower", Name: "Башня Dire T3, нижняя линия", X: 83.57, Y: 34.24, MaxHealth: 2500},
	{Key: "dota_badguys_tower4_top", Team: "dire", Kind: "tower", Name: "Башня Dire T4, верхняя", X: 76.58, Y: 25.06, MaxHealth: 2600},
	{Key: "dota_badguys_tower4_bot", Team: "dire", Kind: "tower", Name: "Башня Dire T4, нижняя", X: 78.13, Y: 26.62, MaxHealth: 2600},
	{Key: "bad_rax_melee_top", Team: "dire", Kind: "barracks", Name: "Казарма ближнего боя Dire, верх", X: 71.18, Y: 21.21, MaxHealth: 2200},
	{Key: "bad_rax_range_top", Team: "dire", Kind: "barracks", Name: "Казарма дальнего боя Dire, верх", X: 71.16, Y: 18.50, MaxHealth: 1300},
	{Key: "bad_rax_melee_mid", Team: "dire", Kind: "barracks", Name: "Казарма ближнего боя Dire, центр", X: 75.28, Y: 30.19, MaxHealth: 2200},
	{Key: "bad_rax_range_mid", Team: "dire", Kind: "barracks", Name: "Казарма дальнего боя Dire, центр", X: 72.99, Y: 27.93, MaxHealth: 1300},
	{Key: "bad_rax_melee_bot", Team: "dire", Kind: "barracks", Name: "Казарма ближнего боя Dire, низ", X: 84.90, Y: 31.97, MaxHealth: 2200},
	{Key: "bad_rax_range_bot", Team: "dire", Kind: "barracks", Name: "Казарма дальнего боя Dire, низ", X: 82.19, Y: 32.01, MaxHealth: 1300},
	{Key: "dota_badguys_fort", Team: "dire", Kind: "ancient", Name: "Древний Dire", X: 79.44, Y: 23.82, MaxHealth: 4500},
}

func (c *coordinator) gsiAPI(w http.ResponseWriter, r *http.Request) {
	observerID := strings.TrimPrefix(r.URL.Path, "/api/gsi/")
	if !observerIDPattern.MatchString(observerID) || strings.Contains(observerID, "/") {
		http.NotFound(w, r)
		return
	}
	var input gsiEnvelope
	if err := decodeGSIJSON(r, &input); err != nil {
		http.Error(w, "invalid GSI payload", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(input.Auth.Token)), c.token) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Erase the request copy as soon as authentication is complete. It is never
	// written to snapshots or included in an error response.
	input.Auth.Token = ""
	if input.Provider.AppID != 570 {
		http.Error(w, "unexpected game provider", http.StatusBadRequest)
		return
	}
	matchID := uint64(input.Map.MatchID)
	if matchID == 0 {
		http.Error(w, "match id is required", http.StatusBadRequest)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	assigned, ok := c.assignments[observerID]
	if !ok || assigned.MatchID != matchID {
		http.Error(w, "match is not assigned to observer", http.StatusConflict)
		return
	}
	if c.telemetryByMatch == nil {
		c.telemetryByMatch = make(map[uint64]*telemetryState)
	}
	state := c.telemetryByMatch[matchID]
	if state == nil {
		state = &telemetryState{
			Players:        make(map[string]*telemetryPlayerState),
			DestroyedBy:    make(map[string]string),
			SeenEvents:     make(map[string]struct{}),
			BuildingAlive:  make(map[string]bool),
			TeleportOwners: make(map[string]string),
		}
		c.telemetryByMatch[matchID] = state
	}
	snapshot, err := c.transformGSILocked(observerID, assigned, input, state)
	if err != nil {
		http.Error(w, "GSI payload is incomplete", http.StatusBadRequest)
		return
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		http.Error(w, "encode live snapshot", http.StatusInternalServerError)
		return
	}
	if err := writeAtomic(filepath.Join(c.publicDir, "matches", assigned.PublicID+".json"), data); err != nil {
		http.Error(w, "write live snapshot", http.StatusInternalServerError)
		return
	}
	c.stateByMatch[matchID] = snapshot.State
	if err := c.publishOverviewLocked(); err != nil {
		http.Error(w, "publish overview failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *coordinator) transformGSILocked(observerID string, assigned assignment, input gsiEnvelope, state *telemetryState) (publicLiveSnapshot, error) {
	if len(input.Hero["team2"])+len(input.Hero["team3"]) == 0 && strings.Contains(strings.ToUpper(input.Map.GameState), "IN_PROGRESS") {
		return publicLiveSnapshot{}, errors.New("spectator hero data is missing")
	}
	radiantCandidate, direCandidate := c.assignmentTeamNamesLocked(observerID, assigned.MatchID)
	playerNamesByIndex := make(map[int]string)
	players := make([]publicLivePlayer, 0, 10)
	radiantName, direName := radiantCandidate, direCandidate
	radiantNetWorth, direNetWorth := 0, 0
	towerDamageDeltas := make(map[string]int)
	teleportStarts := map[string][]string{"radiant": {}, "dire": {}}

	for _, group := range []struct {
		Key  string
		Team string
	}{
		{Key: "team2", Team: "radiant"},
		{Key: "team3", Team: "dire"},
	} {
		heroes := input.Hero[group.Key]
		playerData := input.Player[group.Key]
		keys := unionSortedKeys(heroes, playerData)
		for _, key := range keys {
			hero, hasHero := heroes[key]
			player := playerData[key]
			if !hasHero || hero.ID <= 0 {
				continue
			}
			name := limitedText(player.Name, 80)
			if name == "" {
				name = "Игрок"
			}
			if index, ok := playerIndex(key); ok {
				playerNamesByIndex[index] = name
			}
			if group.Team == "radiant" {
				radiantNetWorth += max(0, player.NetWorth)
				if radiantName == "" && strings.TrimSpace(player.TeamName) != "" {
					radiantName = limitedText(player.TeamName, 80)
				}
			} else {
				direNetWorth += max(0, player.NetWorth)
				if direName == "" && strings.TrimSpace(player.TeamName) != "" {
					direName = limitedText(player.TeamName, 80)
				}
			}
			id := group.Team + "-" + key
			x, y := currentMapPosition(hero.X, hero.Y)
			playerState := state.Players[id]
			if playerState == nil {
				playerState = &telemetryPlayerState{}
				state.Players[id] = playerState
			}
			if playerState.HasSample && hero.Health < playerState.LastHealth {
				playerState.DamageSequence++
			}
			if playerState.HasSample && player.TowerDamage > playerState.LastTowerDamage {
				towerDamageDeltas[name] += player.TowerDamage - playerState.LastTowerDamage
			}
			tpCooldown := teleportItemCooldown(input.Items[group.Key][key])
			if playerState.HasSample && playerState.LastTPCooldown <= 0.05 && tpCooldown > 0.05 {
				teleportStarts[group.Team] = append(teleportStarts[group.Team], id)
			}
			if !hero.Alive {
				if !playerState.HasSample || playerState.WasAlive || !playerState.HasDeathPoint {
					playerState.DeathX, playerState.DeathY, playerState.HasDeathPoint = x, y, true
				}
				x, y = playerState.DeathX, playerState.DeathY
			} else {
				playerState.HasDeathPoint = false
			}
			playerState.LastHealth = hero.Health
			playerState.LastTowerDamage = player.TowerDamage
			playerState.LastTPCooldown = tpCooldown
			playerState.WasAlive = hero.Alive
			playerState.HasSample = true

			publicPlayer := publicLivePlayer{
				ID: id, PlayerName: name, Team: group.Team, HeroID: hero.ID, Level: hero.Level,
				Kills: player.Kills, Deaths: player.Deaths, Assists: player.Assists,
				LastHits: player.LastHits, Denies: player.Denies, NetWorth: max(0, player.NetWorth),
				Gold: max(0, player.Gold), GPM: max(0, player.GPM), XPM: max(0, player.XPM),
				Health: max(0, hero.Health), MaxHealth: max(0, hero.MaxHealth),
				Alive: hero.Alive, RespawnSeconds: max(0, hero.RespawnSeconds),
				X: x, Y: y, DamageSequence: playerState.DamageSequence, StatsAvailable: true,
			}
			if observation, ok := input.Teleports[key]; ok && observation.Verified && observation.Channeling {
				targetX, targetY := currentMapPosition(observation.TargetX, observation.TargetY)
				publicPlayer.Teleport = &publicTeleport{
					Channeling: true, TargetX: targetX, TargetY: targetY,
					RemainingMS: max(0, observation.RemainingMS),
				}
			}
			players = append(players, publicPlayer)
		}
	}
	sort.Slice(players, func(i, j int) bool {
		if players[i].Team != players[j].Team {
			return players[i].Team == "radiant"
		}
		return players[i].ID < players[j].ID
	})
	applyMinimapTeleports(players, input.Minimap, state, teleportStarts)

	if radiantName == "" {
		radiantName = "Radiant"
	}
	if direName == "" {
		direName = "Dire"
	}
	captureObjectiveEvents(input.Events, state, playerNamesByIndex)
	captureBuildingTransitions(input.Buildings, state, towerDamageDeltas)
	status, message := publicStateFromGSI(input.Map.GameState, input.Map.Paused)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	buildings := publicBuildings(input.Buildings, input.Minimap, state)
	roshan := publicRoshanState(input.Map, input.Roshan, input.Events, state)
	return publicLiveSnapshot{
		ID: assigned.PublicID, MatchID: assigned.MatchID, State: status, Message: message,
		UpdatedAt: now, GameTimeSeconds: input.Map.ClockTime,
		Radiant: team{Name: radiantName}, Dire: team{Name: direName},
		Score: publicScore{
			Radiant: input.Map.RadiantScore, Dire: input.Map.DireScore,
			RadiantGoldLead: radiantNetWorth - direNetWorth,
		},
		Map: publicMap{
			ImageURL: "/dota-map-7.41.png", Players: players,
			Buildings: buildings, Roshan: roshan,
		},
		Players: players, Paused: input.Map.Paused,
	}, nil
}

func (c *coordinator) assignmentTeamNamesLocked(observerID string, matchID uint64) (string, string) {
	for _, item := range c.candidatesByObserver[observerID] {
		if item.MatchID == matchID {
			return limitedText(item.TeamRadiant, 80), limitedText(item.TeamDire, 80)
		}
	}
	return "", ""
}

func decodeGSIJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxGSIRequestBytes+1))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func publicStateFromGSI(gameState string, paused bool) (string, string) {
	value := strings.ToUpper(strings.TrimSpace(gameState))
	switch {
	case strings.Contains(value, "POST_GAME") || strings.Contains(value, "DISCONNECT"):
		return "finished", "Матч завершён"
	case strings.Contains(value, "GAME_IN_PROGRESS"):
		if paused {
			return "live", "В игре пауза. Позиции соответствуют последнему кадру трансляции Valve."
		}
		return "live", "Матч идёт. Позиции обновляются с задержкой внутриигровой трансляции."
	default:
		return "not_started", "Матч не начался"
	}
}

func currentMapPosition(worldX, worldY float64) (float64, float64) {
	const (
		mapPixels = 320.0
		xScale    = 2.1406114929301476
		xOffset   = -110.94336756939519
		yScale    = -2.1239747844162697
		yOffset   = 429.9343596313761
	)
	// OpenDota/Clarity's precise location is (world / 128); the entity sample
	// used for calibration stores it on the 0..256 grid with origin at 128.
	gridX := worldX/128.0 + 128.0
	gridY := worldY/128.0 + 128.0
	return clampPercent((xScale*gridX + xOffset) / mapPixels * 100.0),
		clampPercent((yScale*gridY + yOffset) / mapPixels * 100.0)
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func unionSortedKeys(left map[string]gsiHero, right map[string]gsiPlayer) []string {
	set := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		set[key] = struct{}{}
	}
	for key := range right {
		set[key] = struct{}{}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		leftIndex, leftOK := playerIndex(keys[i])
		rightIndex, rightOK := playerIndex(keys[j])
		if leftOK && rightOK {
			return leftIndex < rightIndex
		}
		return keys[i] < keys[j]
	})
	return keys
}

func playerIndex(key string) (int, bool) {
	value := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(key)), "player")
	index, err := strconv.Atoi(value)
	return index, err == nil && index >= 0 && index < 32
}

func teleportItemCooldown(items map[string]gsiItem) float64 {
	cooldown := 0.0
	for key, item := range items {
		name := strings.ToLower(item.Name)
		if !strings.HasPrefix(key, "teleport") &&
			!strings.Contains(name, "tpscroll") &&
			!strings.Contains(name, "travel_boots") {
			continue
		}
		if item.Cooldown > cooldown {
			cooldown = item.Cooldown
		}
	}
	return cooldown
}

func applyMinimapTeleports(players []publicLivePlayer, minimap map[string]gsiMinimapElement, state *telemetryState, starts map[string][]string) {
	type marker struct {
		ID      string
		Team    string
		Element gsiMinimapElement
	}
	markers := make([]marker, 0)
	active := make(map[string]struct{})
	for id, element := range minimap {
		if !strings.EqualFold(element.Image, "minimap_ping_teleporting") || element.EventDuration <= 1 {
			continue
		}
		teamName := ""
		if element.Team == 2 {
			teamName = "radiant"
		} else if element.Team == 3 {
			teamName = "dire"
		}
		if teamName == "" {
			continue
		}
		markers = append(markers, marker{ID: id, Team: teamName, Element: element})
		active[id] = struct{}{}
	}
	sort.Slice(markers, func(i, j int) bool { return markers[i].ID < markers[j].ID })
	for markerID := range state.TeleportOwners {
		if _, ok := active[markerID]; !ok {
			delete(state.TeleportOwners, markerID)
		}
	}
	for _, teamName := range []string{"radiant", "dire"} {
		unowned := make([]string, 0)
		for _, item := range markers {
			if item.Team == teamName && state.TeleportOwners[item.ID] == "" {
				unowned = append(unowned, item.ID)
			}
		}
		if len(unowned) == 1 && len(starts[teamName]) == 1 {
			state.TeleportOwners[unowned[0]] = starts[teamName][0]
		}
	}
	playerByID := make(map[string]*publicLivePlayer, len(players))
	for index := range players {
		playerByID[players[index].ID] = &players[index]
	}
	for _, item := range markers {
		player := playerByID[state.TeleportOwners[item.ID]]
		if player == nil || player.Team != item.Team {
			continue
		}
		targetX, targetY := currentMapPosition(item.Element.X, item.Element.Y)
		remaining := int(item.Element.RemainingTime * 1000)
		if remaining <= 0 {
			remaining = int(item.Element.EventDuration * 1000)
		}
		player.Teleport = &publicTeleport{
			Channeling: true, TargetX: targetX, TargetY: targetY,
			RemainingMS: max(500, remaining),
		}
	}
}

func captureBuildingTransitions(input map[string]map[string]gsiBuilding, state *telemetryState, towerDamageDeltas map[string]int) {
	if len(input["radiant"])+len(input["dire"]) == 0 {
		return
	}
	destroyedNow := make([]string, 0)
	next := make(map[string]bool, len(currentBuildingLayout))
	for _, layout := range currentBuildingLayout {
		_, alive := input[layout.Team][layout.Key]
		next[layout.Key] = alive
		if state.HasBuildingSample && state.BuildingAlive[layout.Key] && !alive {
			destroyedNow = append(destroyedNow, layout.Key)
		}
	}
	if len(destroyedNow) == 1 {
		killer := ""
		for playerName, delta := range towerDamageDeltas {
			if delta <= 0 {
				continue
			}
			if killer != "" {
				killer = ""
				break
			}
			killer = playerName
		}
		if killer != "" && state.DestroyedBy[destroyedNow[0]] == "" {
			state.DestroyedBy[destroyedNow[0]] = limitedText(killer, 80)
		}
	}
	state.BuildingAlive = next
	state.HasBuildingSample = true
}

func publicBuildings(input map[string]map[string]gsiBuilding, minimap map[string]gsiMinimapElement, state *telemetryState) []publicBuilding {
	if len(input["radiant"])+len(input["dire"]) == 0 {
		return nil
	}
	positions := minimapBuildingPositions(minimap)
	result := make([]publicBuilding, 0, len(currentBuildingLayout))
	for _, layout := range currentBuildingLayout {
		building, alive := input[layout.Team][layout.Key]
		maxHealth := building.MaxHealth
		if maxHealth <= 0 {
			maxHealth = layout.MaxHealth
		}
		x, y := layout.X, layout.Y
		if position, ok := positions[layout.Key]; ok {
			x, y = position[0], position[1]
		}
		result = append(result, publicBuilding{
			ID: layout.Key, Name: layout.Name, Team: layout.Team, Kind: layout.Kind,
			X: x, Y: y, Health: max(0, building.Health), MaxHealth: maxHealth,
			Alive: alive, DestroyedBy: state.DestroyedBy[layout.Key],
		})
	}
	return result
}

func minimapBuildingPositions(minimap map[string]gsiMinimapElement) map[string][2]float64 {
	result := make(map[string][2]float64)
	for _, element := range minimap {
		if strings.TrimSpace(element.UnitName) == "" {
			continue
		}
		x, y := currentMapPosition(element.X, element.Y)
		key := matchBuildingKeyAt(element.UnitName, x, y)
		if key != "" {
			result[key] = [2]float64{x, y}
		}
	}
	return result
}

func captureObjectiveEvents(events []map[string]json.RawMessage, state *telemetryState, playerNames map[int]string) {
	if len(state.SeenEvents) > 512 {
		state.SeenEvents = make(map[string]struct{})
	}
	for _, event := range events {
		eventType := rawString(event, "event_type", "type", "event")
		gameTime := rawInt64(event, "game_time", "time")
		killer := int(rawInt64(event, "killer_player_id", "player_id", "killer_id"))
		target := rawString(event, "building", "building_name", "tower", "target", "key")
		signature := fmt.Sprintf("%d:%s:%d:%s", gameTime, eventType, killer, target)
		if _, seen := state.SeenEvents[signature]; seen {
			continue
		}
		state.SeenEvents[signature] = struct{}{}
		switch strings.ToLower(eventType) {
		case "roshan_killed":
			state.RoshanKilledGameTime = gameTime
		case "building_killed", "building_kill", "tower_killed", "tower_kill":
			layoutKey := matchBuildingKey(target)
			if layoutKey != "" {
				state.DestroyedBy[layoutKey] = limitedText(playerNames[killer], 80)
			}
		}
	}
}

func matchBuildingKey(raw string) string {
	return matchBuildingKeyAt(raw, -1, -1)
}

func matchBuildingKeyAt(raw string, x, y float64) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.TrimPrefix(value, "npc_dota_")
	value = strings.Replace(value, "goodguys_melee_rax_", "good_rax_melee_", 1)
	value = strings.Replace(value, "goodguys_range_rax_", "good_rax_range_", 1)
	value = strings.Replace(value, "badguys_melee_rax_", "bad_rax_melee_", 1)
	value = strings.Replace(value, "badguys_range_rax_", "bad_rax_range_", 1)
	if strings.HasPrefix(value, "goodguys_") || strings.HasPrefix(value, "badguys_") {
		value = "dota_" + value
	}
	if value == "dota_goodguys_tower4" || value == "dota_badguys_tower4" {
		if x < 0 || y < 0 {
			return ""
		}
		bestKey, bestDistance := "", 1e9
		for _, layout := range currentBuildingLayout {
			if !strings.HasPrefix(layout.Key, value+"_") {
				continue
			}
			distance := (layout.X-x)*(layout.X-x) + (layout.Y-y)*(layout.Y-y)
			if distance < bestDistance {
				bestKey, bestDistance = layout.Key, distance
			}
		}
		return bestKey
	}
	for _, layout := range currentBuildingLayout {
		if value == layout.Key || strings.Contains(value, layout.Key) {
			return layout.Key
		}
	}
	return ""
}

func publicRoshanState(gameMap gsiMap, direct *gsiRoshan, events []map[string]json.RawMessage, state *telemetryState) *publicRoshan {
	for _, event := range events {
		if strings.EqualFold(rawString(event, "event_type", "type", "event"), "roshan_killed") {
			state.RoshanKilledGameTime = rawInt64(event, "game_time", "time")
		}
	}
	rawState := strings.ToLower(strings.TrimSpace(gameMap.RoshanState))
	if rawState == "" && state.RoshanKilledGameTime == 0 && direct == nil {
		return nil
	}
	pit, x, y := currentRoshanPit(gameMap)
	if direct != nil && (direct.X != 0 || direct.Y != 0) {
		x, y = currentMapPosition(direct.X, direct.Y)
		if x < 50 {
			pit = "dire"
		} else {
			pit = "radiant"
		}
	}
	result := &publicRoshan{Alive: true, X: x, Y: y, Pit: pit}
	if direct != nil {
		result.Alive = direct.Alive
	}
	if (direct != nil && !direct.Alive) || strings.Contains(rawState, "dead") || strings.Contains(rawState, "respawn") || state.RoshanKilledGameTime > 0 {
		result.Alive = false
		maximum := max(0, gameMap.RoshanStateEndSecond)
		if maximum == 0 && state.RoshanKilledGameTime > 0 {
			elapsed := max(int64(0), gameMap.ClockTime-state.RoshanKilledGameTime)
			maximum = max(0, 11*60-int(elapsed))
		}
		result.RespawnMaxSeconds = maximum
		result.RespawnMinSeconds = max(0, maximum-3*60)
		result.RespawnWindowSeconds = 11 * 60
	}
	if (direct != nil && direct.Alive) || strings.Contains(rawState, "alive") {
		result.Alive = true
		state.RoshanKilledGameTime = 0
	}
	return result
}

func currentRoshanPit(gameMap gsiMap) (string, float64, float64) {
	// Current map: Dire pit is north-west, Radiant pit is south-east. Roshan
	// begins in the Radiant pit and, after 15:00, occupies Dire by day and
	// Radiant by night.
	if gameMap.ClockTime < 15*60 || !gameMap.Daytime {
		return "radiant", 66.0, 66.0
	}
	return "dire", 30.0, 30.0
}

func rawString(values map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) == nil {
			return value
		}
	}
	return ""
}

func rawInt64(values map[string]json.RawMessage, keys ...string) int64 {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok {
			continue
		}
		var number json.Number
		if json.Unmarshal(raw, &number) == nil {
			if value, err := number.Int64(); err == nil {
				return value
			}
		}
	}
	return 0
}
