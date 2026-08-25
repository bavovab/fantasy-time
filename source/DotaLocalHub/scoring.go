package main

import "math"

type fantasyAverages struct {
	Matches                int
	Kills                  float64
	Deaths                 float64
	CS                     float64
	GPM                    float64
	Madstone               float64
	TowerKills             float64
	WardsPlaced            float64
	CampsStacked           float64
	RunesGrabbed           float64
	Watchers               float64
	Lotuses                float64
	RoshanKills            float64
	TeamfightParticipation float64
	Stuns                  float64
	Tormentors             float64
	CourierKills           float64
	FirstBlood             float64
	Smokes                 float64
}

func calculateFantasyStats(avg fantasyAverages) PlayerFantasyStats {
	if avg.Matches == 0 {
		metrics := fantasyMetrics(avg)
		for index := range metrics {
			metrics[index].AveragePoints = 0
		}
		return PlayerFantasyStats{Metrics: metrics}
	}
	metrics := fantasyMetrics(avg)
	var total float64
	for _, metric := range metrics {
		total += metric.AveragePoints
	}
	return PlayerFantasyStats{
		Matches:     avg.Matches,
		Metrics:     metrics,
		TotalPoints: round2(total),
	}
}

func fantasyMetrics(avg fantasyAverages) []FantasyMetric {
	return []FantasyMetric{
		metric("kills", "Убийства", avg.Kills, avg.Kills*107),
		metric("deaths", "Смерти", avg.Deaths, 1950-avg.Deaths*195),
		metric("cs", "Крипы", avg.CS, avg.CS*3),
		metric("gpm", "Золото в минуту", avg.GPM, avg.GPM*2),
		metric("madstone", "Безумруды", avg.Madstone, avg.Madstone*13),
		metric("towerKills", "Башни", avg.TowerKills, avg.TowerKills*352),
		metric("wardsPlaced", "Observer Ward", avg.WardsPlaced, avg.WardsPlaced*117),
		metric("campsStacked", "Стаки лагерей", avg.CampsStacked, avg.CampsStacked*234),
		metric("runesGrabbed", "Руны", avg.RunesGrabbed, avg.RunesGrabbed*141),
		metric("watchers", "Смотрители", avg.Watchers, avg.Watchers*147),
		metric("lotuses", "Лотосы", avg.Lotuses, avg.Lotuses*176),
		metric("roshanKills", "Рошан", avg.RoshanKills, avg.RoshanKills*1172),
		metric("teamfightParticipation", "Командные сражения", avg.TeamfightParticipation, avg.TeamfightParticipation*2124),
		metric("stuns", "Оглушения", avg.Stuns, avg.Stuns*10),
		metric("tormentors", "Терзатели", avg.Tormentors, avg.Tormentors*879),
		metric("courierKills", "Курьеры", avg.CourierKills, avg.CourierKills*703),
		metric("firstBlood", "Первая кровь", avg.FirstBlood, avg.FirstBlood*1934),
		metric("smokes", "Smoke of Deceit", avg.Smokes, avg.Smokes*293),
	}
}

func metric(key, label string, average, points float64) FantasyMetric {
	return FantasyMetric{
		Key:           key,
		Label:         label,
		Average:       round2(average),
		AveragePoints: round2(points),
	}
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func fantasyPointsForPlayer(player PlayerRecord) float64 {
	return calculateFantasyStats(fantasyAverages{
		Matches: 1, Kills: float64(player.Kills), Deaths: float64(player.Deaths),
		CS: float64(player.CS), GPM: player.GPM, Madstone: float64(player.Madstone),
		TowerKills: float64(player.TowerKills), WardsPlaced: float64(player.WardsPlaced),
		CampsStacked: float64(player.CampsStacked), RunesGrabbed: float64(player.RunesGrabbed),
		Watchers: float64(player.Watchers), Lotuses: float64(player.Lotuses),
		RoshanKills: float64(player.RoshanKills), TeamfightParticipation: float64(player.TeamfightParticipation),
		Stuns: float64(player.Stuns), Tormentors: float64(player.Tormentors),
		CourierKills: float64(player.CourierKills), FirstBlood: float64(player.FirstBlood),
		Smokes: float64(player.Smokes),
	}).TotalPoints
}
