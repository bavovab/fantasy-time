(function initializeFantasyAssets(global) {
  const assetRoot = "assets/ti2026/fantasy";
  const teamRoot = "assets/ti2026/teams";

  const metrics = Object.freeze({
    kills: { label: "Убийства", color: "red", asset: "fantasy_emblem_kills_png.png" },
    deaths: { label: "Смерти", color: "red", asset: "fantasy_emblem_deaths_png.png" },
    cs: { label: "Крипы", color: "red", asset: "fantasy_emblem_creep_score_png.png" },
    gpm: { label: "Золото в минуту", color: "red", asset: "fantasy_emblem_gpm_png.png" },
    madstone: { label: "Безумруды", color: "red", asset: "fantasy_emblem_neutral_token_png.png" },
    towerKills: { label: "Башни", color: "red", asset: "fantasy_emblem_towers_killed_png.png" },
    wardsPlaced: { label: "Observer Ward", color: "blue", asset: "fantasy_emblem_wards_placed_png.png" },
    campsStacked: { label: "Стаки лагерей", color: "blue", asset: "fantasy_emblem_creeps_stacked_png.png" },
    runesGrabbed: { label: "Руны", color: "blue", asset: "fantasy_emblem_rune_png.png" },
    watchers: { label: "Смотрители", color: "blue", asset: "fantasy_emblem_sentinel_png.png" },
    lotuses: { label: "Лотосы", color: "blue", asset: "fantasy_emblem_lotus_png.png" },
    smokes: { label: "Smoke of Deceit", color: "blue", asset: "fantasy_emblem_smoke_png.png" },
    roshanKills: { label: "Рошан", color: "green", asset: "fantasy_emblem_roshan_png.png" },
    teamfightParticipation: { label: "Командные сражения", color: "green", asset: "fantasy_emblem_teamfight_png.png" },
    stuns: { label: "Оглушения", color: "green", asset: "fantasy_emblem_stuns_png.png" },
    tormentors: { label: "Терзатели", color: "green", asset: "fantasy_emblem_tormentor_png.png" },
    courierKills: { label: "Курьеры", color: "green", asset: "fantasy_emblem_courier_kill_png.png" },
    firstBlood: { label: "Первая кровь", color: "green", asset: "fantasy_emblem_first_blood_png.png" },
  });

  const teamEmblems = Object.freeze({
    "1w": `${teamRoot}/10150413-iron-wing.webp`,
    aurora: `${teamRoot}/9467224-aurora-gaming.webp`,
    boomboys: `${teamRoot}/8255888-boomboys.webp`,
    falcons: `${teamRoot}/9247354-team-falcons.webp`,
    liquid: `${teamRoot}/2163-team-liquid.webp`,
    xtreme: `${teamRoot}/8261500-xtreme-gaming.webp`,
    yandex: `${teamRoot}/9823272-team-yandex.webp`,
    spirit: `${teamRoot}/7119388-team-spirit.webp`,
    vision: `${teamRoot}/9572001-team-vision.webp`,
    nigma: `${teamRoot}/10136357-nigma-galaxy.webp`,
    huligani: `${teamRoot}/10149530-huligani.webp`,
    resilience: `${teamRoot}/5017210-team-resilience.webp`,
    vici: `${teamRoot}/726228-vici-gaming.webp`,
    og: `${teamRoot}/2586976-og.webp`,
    gamerlegion: `${teamRoot}/9964962-gamerlegion.webp`,
    lgd: `${teamRoot}/10150538-lgd-gaming.webp`,
  });

  const teamAliases = Object.freeze({
    "1w": "1w",
    "1wteam": "1w",
    "ironwing": "1w",
    "aurora": "aurora",
    "auroragaming": "aurora",
    "boomboys": "boomboys",
    "teamfalcons": "falcons",
    "falcons": "falcons",
    "teamliquid": "liquid",
    "liquid": "liquid",
    "xtremegaming": "xtreme",
    "xtreme": "xtreme",
    "teamyandex": "yandex",
    "yandex": "yandex",
    "teamspirit": "spirit",
    "spirit": "spirit",
    "teamvision": "vision",
    "vision": "vision",
    "nigmagalaxy": "nigma",
    "nigma": "nigma",
    "huligani": "huligani",
    "teamresilience": "resilience",
    "resilience": "resilience",
    "vicigaming": "vici",
    "vici": "vici",
    "og": "og",
    "gamerlegion": "gamerlegion",
    "lgdgaming": "lgd",
    "lgd": "lgd",
  });

  function cleanClassName(value) {
    return String(value || "").replace(/[^a-zA-Z0-9_-]+/g, " ").trim();
  }

  function metricIcon(metric, className = "", labelled = false) {
    const item = metrics[metric];
    if (!item) return "";
    const classes = ["fantasy-metric-image", cleanClassName(className)].filter(Boolean).join(" ");
    const label = labelled ? ` alt="${item.label}" title="${item.label}"` : ' alt="" aria-hidden="true"';
    return `<img class="${classes}" src="${assetRoot}/${item.asset}"${label}>`;
  }

  function teamEmblem(teamSlug, fallback = "") {
    return teamEmblems[String(teamSlug || "").toLowerCase()] || fallback;
  }

  function normalizeTeamName(value) {
    return String(value || "").toLowerCase().replace(/[^a-z0-9]+/g, "");
  }

  function teamEmblemByName(teamName, fallback = "") {
    const slug = teamAliases[normalizeTeamName(teamName)];
    return teamEmblem(slug, fallback);
  }

  const api = Object.freeze({ assetRoot, metrics, teamEmblems, teamAliases, metricIcon, teamEmblem, teamEmblemByName });
  global.FantasyAssets = api;
  if (typeof module !== "undefined" && module.exports) module.exports = api;
})(typeof globalThis !== "undefined" ? globalThis : this);
