package main

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"github.com/dotabuff/manta"
	"github.com/dotabuff/manta/dota"
	"golang.org/x/text/encoding/charmap"
)

type playerResourceStats struct {
	Name                   string
	SteamID                uint64
	HeroID                 int32
	TeamSlot               int32
	Kills                  int32
	Deaths                 int32
	Assists                int32
	FirstBlood             int32
	TeamfightParticipation float32
}

type teamStats struct {
	Team                string
	TeamSlot            int
	SteamID             uint64
	PlayerID            int32
	LastHits            int32
	Denies              int32
	TotalEarnedGold     int32
	Madstone            int32
	TowerKills          int32
	ObserverWardsPlaced int32
	CampsStacked        int32
	RunesGrabbed        int32
	Watchers            int32
	Lotuses             int32
	RoshanKills         int32
	Stuns               float32
	TormentorKills      int32
	CourierKills        int32
	Smokes              int32
}

func parseReplay(path string) (ParsedMatch, error) {
	file, err := os.Open(path)
	if err != nil {
		return ParsedMatch{}, err
	}
	defer file.Close()

	parser, err := manta.NewStreamParser(file)
	if err != nil {
		return ParsedMatch{}, err
	}

	playerResources := make(map[int]*playerResourceStats)
	teamData := make(map[string]*teamStats)
	lastGold := make(map[string]int32)
	madstonePickups := make(map[int32]int32)
	heroKeyByPlayerID := make(map[int32]string)
	tormentorLastHitsByHeroKey := make(map[string]int32)
	seenTormentorCombatLog := make(map[string]struct{})

	var playbackSeconds float32
	var playbackTicks int32
	var firstGoldTick uint32
	var lastGoldTick uint32
	var sawFirstGoldTick bool
	var matchID uint64
	var gameMode int
	var radiantWin bool
	var leagueID uint32
	var radiantTeamID uint64
	var direTeamID uint64
	var radiantTeamTag string
	var direTeamTag string
	var endTime uint32

	parser.Callbacks.OnCDemoFileInfo(func(message *dota.CDemoFileInfo) error {
		playbackSeconds = message.GetPlaybackTime()
		playbackTicks = message.GetPlaybackTicks()
		if gameInfo := message.GetGameInfo(); gameInfo != nil {
			if dotaInfo := gameInfo.GetDota(); dotaInfo != nil {
				matchID = dotaInfo.GetMatchId()
				gameMode = int(dotaInfo.GetGameMode())
				radiantWin = dotaInfo.GetGameWinner() == 2
				leagueID = dotaInfo.GetLeagueid()
				radiantTeamID = uint64(dotaInfo.GetRadiantTeamId())
				direTeamID = uint64(dotaInfo.GetDireTeamId())
				radiantTeamTag = dotaInfo.GetRadiantTeamTag()
				direTeamTag = dotaInfo.GetDireTeamTag()
				endTime = dotaInfo.GetEndTime()
			}
		}
		return nil
	})

	parser.OnEntity(func(entity *manta.Entity, operation manta.EntityOp) error {
		className := entity.GetClassName()
		if strings.HasPrefix(className, "CDOTA_Unit_Hero_") {
			if playerID, ok := numericInt32(entity.Get("m_iPlayerID")); ok && playerID >= 0 {
				heroKeyByPlayerID[playerID] = heroUnitKey(className)
			}
		}

		switch className {
		case "CDOTA_Item_MadstoneBundle":
			if operation.Flag(manta.EntityOpCreated) {
				playerID, ok := numericInt32(entity.Map()["m_iPlayerOwnerID"])
				if ok && playerID >= 0 {
					madstonePickups[playerID]++
				}
			}
		case "CDOTA_PlayerResource":
			readPlayerResource(entity, playerResources)
		case "CDOTA_DataRadiant", "CDOTA_DataDire":
			className := entity.GetClassName()
			teamName := "radiant"
			if className == "CDOTA_DataDire" {
				teamName = "dire"
			}

			for slot := 0; slot < 5; slot++ {
				key := fmt.Sprintf("%s.%d", className, slot)
				stats := teamData[key]
				if stats == nil {
					stats = &teamStats{Team: teamName, TeamSlot: slot}
					teamData[key] = stats
				}
				readTeamStats(entity, slot, stats)

				if previous, exists := lastGold[key]; exists && stats.TotalEarnedGold != previous {
					if !sawFirstGoldTick && stats.TotalEarnedGold > 0 {
						firstGoldTick = parser.Tick
						sawFirstGoldTick = true
					}
					lastGoldTick = parser.Tick
				}
				lastGold[key] = stats.TotalEarnedGold
			}
		}
		return nil
	})

	parser.Callbacks.OnCMsgDOTACombatLogEntry(func(entry *dota.CMsgDOTACombatLogEntry) error {
		countTormentorLastHit(parser, entry, tormentorLastHitsByHeroKey, seenTormentorCombatLog)
		return nil
	})

	parser.Callbacks.OnCDOTAUserMsg_CombatLogBulkData(func(message *dota.CDOTAUserMsg_CombatLogBulkData) error {
		for _, entry := range message.GetCombatEntries() {
			countTormentorLastHit(parser, entry, tormentorLastHitsByHeroKey, seenTormentorCombatLog)
		}
		return nil
	})

	if err := parser.Start(); err != nil {
		return ParsedMatch{}, err
	}

	gameDurationSeconds := float64(playbackSeconds)
	durationSource := "demo playback time"
	if sawFirstGoldTick && lastGoldTick > firstGoldTick && playbackSeconds > 0 && playbackTicks > 0 {
		tickRate := float64(playbackTicks) / float64(playbackSeconds)
		gameDurationSeconds = float64(lastGoldTick-firstGoldTick) / tickRate
		durationSource = "first-to-last total earned gold update"
	}

	resourcesBySteamID := make(map[uint64]struct {
		Index int
		Stats *playerResourceStats
	})
	for index, stats := range playerResources {
		if stats.SteamID != 0 {
			resourcesBySteamID[stats.SteamID] = struct {
				Index int
				Stats *playerResourceStats
			}{Index: index, Stats: stats}
		}
	}

	players := make([]PlayerRecord, 0, 10)
	for _, stats := range teamData {
		if stats.SteamID == 0 {
			continue
		}
		resource, exists := resourcesBySteamID[stats.SteamID]
		if !exists || resource.Stats == nil {
			continue
		}

		var accountID uint32
		if stats.SteamID >= steamID64Base {
			accountID = uint32(stats.SteamID - steamID64Base)
		}
		var gpm float64
		if gameDurationSeconds > 0 {
			gpm = float64(stats.TotalEarnedGold) * 60 / gameDurationSeconds
		}

		players = append(players, PlayerRecord{
			PlayerIndex:            resource.Index,
			Name:                   repairPlayerName(resource.Stats.Name),
			SteamID:                stats.SteamID,
			AccountID:              accountID,
			HeroID:                 resource.Stats.HeroID,
			Team:                   stats.Team,
			TeamSlot:               stats.TeamSlot,
			Kills:                  resource.Stats.Kills,
			Deaths:                 resource.Stats.Deaths,
			Assists:                resource.Stats.Assists,
			CS:                     stats.LastHits + stats.Denies,
			LastHits:               stats.LastHits,
			Denies:                 stats.Denies,
			GPM:                    gpm,
			TotalEarnedGold:        stats.TotalEarnedGold,
			Madstone:               stats.Madstone,
			TowerKills:             stats.TowerKills,
			WardsPlaced:            stats.ObserverWardsPlaced,
			CampsStacked:           stats.CampsStacked,
			RunesGrabbed:           stats.RunesGrabbed,
			Watchers:               stats.Watchers,
			Lotuses:                stats.Lotuses,
			RoshanKills:            stats.RoshanKills,
			TeamfightParticipation: resource.Stats.TeamfightParticipation,
			Stuns:                  stats.Stuns,
			Tormentors:             tormentorLastHitsByHeroKey[heroKeyByPlayerID[stats.PlayerID]],
			CourierKills:           stats.CourierKills,
			FirstBlood:             resource.Stats.FirstBlood,
			Smokes:                 stats.Smokes,
		})
		players[len(players)-1].Madstone = madstonePickups[stats.PlayerID]
	}

	sort.Slice(players, func(i, j int) bool {
		if players[i].Team == players[j].Team {
			return players[i].TeamSlot < players[j].TeamSlot
		}
		return players[i].Team == "radiant"
	})

	if len(players) == 0 {
		return ParsedMatch{}, fmt.Errorf("Manta не нашла игроков в реплее")
	}

	return ParsedMatch{
		PlaybackSeconds:     playbackSeconds,
		GameDurationSeconds: gameDurationSeconds,
		GameDurationSource:  durationSource,
		MatchID:             matchID,
		GameMode:            gameMode,
		RadiantWin:          radiantWin,
		LeagueID:            leagueID,
		RadiantTeamID:       radiantTeamID,
		DireTeamID:          direTeamID,
		RadiantTeamTag:      radiantTeamTag,
		DireTeamTag:         direTeamTag,
		EndTime:             endTime,
		Players:             players,
	}, nil
}

func readPlayerResource(entity *manta.Entity, players map[int]*playerResourceStats) {
	for index := 0; index < 24; index++ {
		stats := players[index]
		if stats == nil {
			stats = &playerResourceStats{}
			players[index] = stats
		}

		playerPrefix := fmt.Sprintf("m_vecPlayerData.%04d.", index)
		teamPrefix := fmt.Sprintf("m_vecPlayerTeamData.%04d.", index)

		if value, ok := entity.GetString(playerPrefix + "m_iszPlayerName"); ok && value != "" {
			stats.Name = value
		}
		if value, ok := entity.Get(playerPrefix + "m_iPlayerSteamID").(uint64); ok && value != 0 {
			stats.SteamID = value
		}
		if value, ok := entity.GetInt32(teamPrefix + "m_nSelectedHeroID"); ok && value > 0 && value < 1000000 {
			stats.HeroID = value
		}
		if value, ok := entity.GetInt32(teamPrefix + "m_iTeamSlot"); ok {
			stats.TeamSlot = value
		}
		if value, ok := entity.GetInt32(teamPrefix + "m_iKills"); ok {
			stats.Kills = value
		}
		if value, ok := entity.GetInt32(teamPrefix + "m_iDeaths"); ok {
			stats.Deaths = value
		}
		if value, ok := entity.GetInt32(teamPrefix + "m_iAssists"); ok {
			stats.Assists = value
		}
		if value, ok := entity.GetInt32(teamPrefix + "m_iFirstBloodClaimed"); ok {
			stats.FirstBlood = value
		}
		if value, ok := entity.GetFloat32(teamPrefix + "m_flTeamFightParticipation"); ok {
			stats.TeamfightParticipation = value
		}
	}
}

func readTeamStats(entity *manta.Entity, slot int, stats *teamStats) {
	prefix := fmt.Sprintf("m_vecDataTeam.%04d.", slot)

	if value, ok := entity.Get(prefix + "m_iPlayerSteamID").(uint64); ok && value != 0 {
		stats.SteamID = value
	}
	setInt32(entity, prefix+"m_nPlayerID", &stats.PlayerID)
	setInt32(entity, prefix+"m_iLastHitCount", &stats.LastHits)
	setInt32(entity, prefix+"m_iDenyCount", &stats.Denies)
	setInt32(entity, prefix+"m_iTotalEarnedGold", &stats.TotalEarnedGold)
	setInt32(entity, prefix+"m_iTowerKills", &stats.TowerKills)
	setInt32(entity, prefix+"m_iObserverWardsPlaced", &stats.ObserverWardsPlaced)
	setInt32(entity, prefix+"m_iCampsStacked", &stats.CampsStacked)
	setInt32(entity, prefix+"m_iRunePickups", &stats.RunesGrabbed)
	setInt32(entity, prefix+"m_iWatchersTaken", &stats.Watchers)
	setInt32(entity, prefix+"m_iLotusesTaken", &stats.Lotuses)
	setInt32(entity, prefix+"m_iRoshanKills", &stats.RoshanKills)
	setInt32(entity, prefix+"m_iTormentorKills", &stats.TormentorKills)
	setInt32(entity, prefix+"m_iCourierKills", &stats.CourierKills)
	setInt32(entity, prefix+"m_iSmokesUsed", &stats.Smokes)
	if value, ok := entity.GetFloat32(prefix + "m_fStuns"); ok {
		stats.Stuns = value
	}
}

func numericInt32(value any) (int32, bool) {
	switch number := value.(type) {
	case int32:
		return number, true
	case uint32:
		return int32(number), true
	case uint64:
		return int32(number), true
	default:
		return 0, false
	}
}

func setInt32(entity *manta.Entity, field string, target *int32) {
	if value, ok := numericInt32(entity.Get(field)); ok {
		*target = value
	}
}

func countTormentorLastHit(parser *manta.Parser, entry *dota.CMsgDOTACombatLogEntry, counts map[string]int32, seen map[string]struct{}) {
	if entry == nil || entry.GetType() != dota.DOTA_COMBATLOG_TYPES_DOTA_COMBATLOG_DEATH {
		return
	}

	targetName := combatLogName(parser, int32(entry.GetTargetName()))
	sourceName := combatLogName(parser, int32(entry.GetTargetSourceName()))
	if !isTormentorCombatName(targetName) && !isTormentorCombatName(sourceName) {
		return
	}

	attackerKey := heroUnitKey(combatLogName(parser, int32(entry.GetAttackerName())))
	if attackerKey == "" {
		return
	}

	eventKey := fmt.Sprintf("%d:%d:%d:%d", parser.Tick, entry.GetTargetName(), entry.GetTargetSourceName(), entry.GetAttackerName())
	if _, exists := seen[eventKey]; exists {
		return
	}
	seen[eventKey] = struct{}{}
	counts[attackerKey]++
}

func isTormentorCombatName(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "tormentor") || strings.Contains(name, "miniboss")
}

func heroUnitKey(name string) string {
	name = strings.TrimPrefix(name, "CDOTA_Unit_Hero_")
	name = strings.TrimPrefix(name, "npc_dota_hero_")
	var builder strings.Builder
	for _, value := range strings.ToLower(name) {
		if value >= 'a' && value <= 'z' || value >= '0' && value <= '9' {
			builder.WriteRune(value)
		}
	}
	return builder.String()
}

func combatLogName(parser *manta.Parser, id int32) string {
	if id < 0 {
		return ""
	}
	defer func() {
		_ = recover()
	}()

	parserValue := reflect.ValueOf(parser)
	if parserValue.Kind() != reflect.Pointer || parserValue.IsNil() {
		return ""
	}
	stringTables := parserValue.Elem().FieldByName("stringTables")
	if !stringTables.IsValid() || !stringTables.CanAddr() {
		return ""
	}
	stringTables = reflect.NewAt(stringTables.Type(), unsafe.Pointer(stringTables.UnsafeAddr())).Elem()
	method := stringTables.MethodByName("GetTableByName")
	if !method.IsValid() {
		return ""
	}
	result := method.Call([]reflect.Value{reflect.ValueOf("CombatLogNames")})
	if len(result) != 2 || !result[1].Bool() || result[0].IsNil() {
		return ""
	}
	table := result[0]
	getItem := table.MethodByName("GetItem")
	if !getItem.IsValid() {
		return ""
	}
	itemResult := getItem.Call([]reflect.Value{reflect.ValueOf(id)})
	if len(itemResult) != 1 || itemResult[0].IsNil() {
		return ""
	}
	item := itemResult[0].Elem()
	key := item.FieldByName("Key")
	if !key.IsValid() || key.Kind() != reflect.String {
		return ""
	}
	return key.String()
}

func repairPlayerName(name string) string {
	name = strings.TrimSpace(strings.TrimRight(name, "\x00"))
	if name == "" {
		return ""
	}
	if !utf8.ValidString(name) {
		if decoded, err := charmap.Windows1251.NewDecoder().Bytes([]byte(name)); err == nil && utf8.Valid(decoded) {
			name = string(decoded)
		} else {
			name = strings.ToValidUTF8(name, "")
		}
	}
	bytes, err := charmap.Windows1251.NewEncoder().Bytes([]byte(name))
	if err == nil && utf8.Valid(bytes) {
		repaired := string(bytes)
		if repaired != name {
			return repaired
		}
	}
	name = strings.Map(func(value rune) rune {
		if unicode.IsControl(value) || value == utf8.RuneError {
			return -1
		}
		return value
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "Без имени"
	}
	return name
}

func playerNameLooksBroken(name string) bool {
	if name == "" || name == "Без имени" {
		return true
	}
	var useful, suspicious int
	for _, value := range name {
		switch {
		case unicode.IsLetter(value), unicode.IsDigit(value), unicode.IsSpace(value), unicode.IsPunct(value):
			useful++
		case unicode.IsSymbol(value):
			suspicious++
		}
	}
	return useful == 0 || suspicious > useful
}
