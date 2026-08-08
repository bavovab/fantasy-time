const fantasyAssets = typeof module !== "undefined" && module.exports
  ? require("./fantasy-assets.js")
  : globalThis.FantasyAssets;
const FANTASY_BUILDER_STORAGE_KEY = "fantasyTimeTeamBuilder:v1";
const FANTASY_BUILDER_VERSION = 1;
const fantasyMetricCatalog = fantasyAssets.metrics;

const fantasyRoleRules = {
  cores: { label: "Основа", short: "Cores", positions: [1, 3], colors: ["red", "green", "red"] },
  mid: { label: "Центр", short: "Mid", positions: [2], colors: ["red", "blue", "green"] },
  supps: { label: "Поддержка", short: "Supps", positions: [4, 5], colors: ["blue", "green", "blue"] },
};

const fantasyTierBonuses = { 1: .10, 2: .30, 3: .60, 4: 1, 5: 1.50 };

const fantasyTraits = {
  none: { label: "Не выбрано", description: "Свойство пока не внесено.", bonus: 0 },
  fractal: { label: "Фрактальная", description: "+60%, если все три эмблемы имеют разные разряды." },
  benevolent: { label: "Благотворная", description: "+20% соседним эмблемам." },
  vampiric: { label: "Вампирическая", description: "+50% этой эмблеме и -10% соседним." },
  unique: { label: "Уникальная", description: "+30%, если на знамени нет другой уникальной эмблемы." },
  friendly: { label: "Дружелюбная", description: "+50%, если все три эмблемы дружелюбные." },
};

function fantasyHeroIdSet(values) {
  return new Set(values);
}

const fantasyTitlePrefixes = {
  none: { label: "Без префикса", bonus: 0, description: "Префикс не выбран.", heroes: new Set() },
  crimson: {
    label: "Багровый", bonus: .06, description: "+6% при игре за красного героя.",
    heroIds: fantasyHeroIdSet([2, 4, 11, 14, 18, 25, 35, 37, 38, 49, 51, 61, 64, 65, 69, 77, 78, 79, 81, 87, 88, 95, 104, 106, 110, 120, 128, 129, 131, 137]),
  },
  cerulean: {
    label: "Лазурный", bonus: .11, description: "+11% при игре за синего героя.",
    heroIds: fantasyHeroIdSet([5, 9, 10, 12, 13, 15, 17, 18, 20, 22, 31, 39, 48, 52, 59, 60, 63, 64, 68, 71, 84, 91, 92, 102, 111, 112, 113, 138, 145]),
  },
  emerald: {
    label: "Изумрудный", bonus: .06, description: "+6% при игре за зелёного героя.",
    heroIds: fantasyHeroIdSet([21, 29, 36, 40, 42, 44, 45, 47, 53, 58, 76, 83, 85, 86, 89, 94, 107, 108, 114, 119, 123, 138, 155]),
  },
  royal: {
    label: "Королевский", bonus: .10, description: "+10% при игре за фиолетового героя.",
    heroIds: fantasyHeroIdSet([1, 3, 6, 26, 28, 30, 32, 33, 41, 46, 50, 55, 67, 70, 75, 98, 102, 109, 119, 126]),
  },
  golden: {
    label: "Золотой", bonus: .08, description: "+8% при игре за жёлтого или коричневого героя.",
    heroIds: fantasyHeroIdSet([7, 16, 19, 27, 34, 56, 62, 65, 66, 72, 73, 80, 83, 86, 90, 96, 97, 99, 103, 105, 110, 131, 135, 137, 155]),
  },
  elemental: {
    label: "Элементальный", bonus: .08, description: "+8% при игре за водного, огненного или ледяного героя.",
    heroIds: fantasyHeroIdSet([5, 6, 10, 23, 25, 28, 29, 31, 49, 56, 59, 64, 65, 68, 69, 74, 78, 84, 89, 93, 100, 105, 106, 110, 112, 135]),
  },
  otherworldly: {
    label: "Потусторонний", bonus: .07, description: "+7% при игре за нежить, демона или духа.",
    heroIds: fantasyHeroIdSet([11, 14, 17, 20, 23, 26, 31, 36, 39, 42, 43, 45, 54, 56, 59, 67, 69, 79, 85, 106, 107, 108, 109, 121, 126, 138]),
  },
  heroic: {
    label: "Героический", bonus: .09, description: "+9% при игре за героя в маске или плаще.",
    heroIds: fantasyHeroIdSet([4, 5, 6, 8, 18, 21, 26, 27, 34, 35, 37, 44, 45, 51, 53, 57, 62, 65, 72, 74, 79, 81, 86, 102, 111, 113, 114, 121, 136, 138]),
  },
};

const fantasyTitleSuffixes = {
  none: { label: "Без суффикса", bonus: 0, description: "Суффикс не выбран.", support: "exact" },
  tormented: { label: "Мученик", bonus: .23, description: "+23%, если игрок умер от Терзателя.", support: "missing", reason: "Архив не хранит причину смерти." },
  acolyte: { label: "Жрец Бескожих близнецов", bonus: .09, description: "+9%, если первая кровь пролита до стартового горна.", support: "missing", reason: "Архив не хранит точное время первой крови." },
  patient: { label: "Выжидатель", bonus: .23, description: "+23%, если первая кровь не пролилась до 10:00.", support: "missing", reason: "Архив не хранит точное время первой крови." },
  underdog: { label: "Аутсайдер", bonus: .06, description: "+6%, если команда игрока проиграла.", support: "exact" },
  decisive: { label: "Удалец", bonus: .24, description: "+24%, если карта длилась менее 25 минут.", support: "exact" },
  clutch: { label: "Творец победы", bonus: .16, description: "+16% в последней возможной карте серии.", support: "exact" },
  lucky: { label: "Везунчик", bonus: .21, description: "+21%, если длительность карты заканчивается на 8.", support: "exact" },
  cruel: { label: "Мучитель", bonus: .13, description: "+13%, если игрока убили у его же фонтана.", support: "missing", reason: "Архив не хранит координату смерти." },
};

let fantasyBuilderState = null;
let fantasyBuilderInitialized = false;
let fantasyBuilderActiveDialog = null;
let fantasyFiltersExpanded = false;

function defaultFantasyEmblems(roleKey) {
  const fallback = { red: "cs", blue: "wardsPlaced", green: "teamfightParticipation" };
  return fantasyRoleRules[roleKey].colors.map(color => ({ color, metric: fallback[color], tier: 1, trait: "none" }));
}

function defaultFantasyBuilderState() {
  return {
    version: FANTASY_BUILDER_VERSION,
    leagues: [],
    knownLeagues: [],
    limit: 20,
    mode: "map",
    title: { prefix: "none", suffix: "none" },
    slots: Object.fromEntries(Object.keys(fantasyRoleRules).map(key => [key, {
      teamSlug: "", emblems: defaultFantasyEmblems(key),
    }])),
  };
}

function readFantasyBuilderState() {
  try {
    const saved = JSON.parse(localStorage.getItem(FANTASY_BUILDER_STORAGE_KEY) || "null");
    if (!saved || saved.version !== FANTASY_BUILDER_VERSION) return defaultFantasyBuilderState();
    return saved;
  } catch {
    return defaultFantasyBuilderState();
  }
}

function saveFantasyBuilderState() {
  localStorage.setItem(FANTASY_BUILDER_STORAGE_KEY, JSON.stringify(fantasyBuilderState));
}

function fantasyBuilderRoot() {
  return document.querySelector("#fantasyBuilderPage");
}

function fantasyBuilderDialog() {
  return document.querySelector("#fantasyBuilderDialog");
}

async function loadFantasyBuilder() {
  if (!PUBLIC_MODE) return;
  const root = fantasyBuilderRoot();
  if (!root) return;
  if (!fantasyBuilderInitialized) {
    fantasyBuilderInitialized = true;
    fantasyBuilderState = readFantasyBuilderState();
    bindFantasyBuilderShell();
  }
  root.innerHTML = `<div class="loading-card">Собираю данные фэнтези-калькулятора...</div>`;
  try {
    await Promise.all([loadTournamentPlayers(), loadPublicPlayerFilterData()]);
    initializeFantasyBuilderState();
    renderFantasyBuilder();
  } catch (error) {
    root.innerHTML = `<div class="job-error">${escapeHTML(error.message)}</div>`;
  }
}

function fantasyRoleCandidates(roleKey) {
  const rule = fantasyRoleRules[roleKey];
  const teams = new Map();
  tournamentPlayers.forEach(player => {
    if (!rule.positions.includes(Number(player.position || 0))) return;
    const members = teams.get(player.teamSlug) || [];
    members.push(player);
    teams.set(player.teamSlug, members);
  });
  return [...teams.values()].filter(members => members.length === rule.positions.length)
    .map(members => ({
      roleKey,
      teamSlug: members[0].teamSlug,
      teamName: members[0].teamName,
      teamLogoUrl: members[0].teamLogoUrl,
      members: members.sort((left, right) => left.position - right.position),
    }));
}

function initializeFantasyBuilderState() {
  const overview = buildPublicTournamentFilterOverview(publicPlayerFilterData);
  const availableLeagues = new Set(overview.leagues.map(league => league.name));
  fantasyBuilderState.slots = fantasyBuilderState.slots && typeof fantasyBuilderState.slots === "object"
    ? fantasyBuilderState.slots : {};
  fantasyBuilderState.title = fantasyBuilderState.title && typeof fantasyBuilderState.title === "object"
    ? fantasyBuilderState.title : {};
  fantasyBuilderState.title.prefix = fantasyTitlePrefixes[fantasyBuilderState.title.prefix]
    ? fantasyBuilderState.title.prefix : "none";
  fantasyBuilderState.title.suffix = fantasyTitleSuffixes[fantasyBuilderState.title.suffix]
    ? fantasyBuilderState.title.suffix : "none";
  fantasyBuilderState.mode = ["map", "series"].includes(fantasyBuilderState.mode)
    ? fantasyBuilderState.mode : "map";
  const savedLeagues = Array.isArray(fantasyBuilderState.leagues)
    ? fantasyBuilderState.leagues.filter(name => availableLeagues.has(name)) : null;
  const knownLeagues = new Set(Array.isArray(fantasyBuilderState.knownLeagues)
    ? fantasyBuilderState.knownLeagues : []);
  const selectedLeagues = new Set(savedLeagues === null || knownLeagues.size === 0
    ? availableLeagues
    : savedLeagues);
  availableLeagues.forEach(name => {
    if (!knownLeagues.has(name)) selectedLeagues.add(name);
  });
  fantasyBuilderState.leagues = [...selectedLeagues];
  fantasyBuilderState.knownLeagues = [...availableLeagues];
  fantasyBuilderState.limit = Math.min(overview.maxMatches, Math.max(1, Number(fantasyBuilderState.limit || 20)));
  Object.keys(fantasyRoleRules).forEach(roleKey => {
    const candidates = fantasyRoleCandidates(roleKey);
    const slot = fantasyBuilderState.slots?.[roleKey] || { teamSlug: "", emblems: defaultFantasyEmblems(roleKey) };
    if (!candidates.some(candidate => candidate.teamSlug === slot.teamSlug)) slot.teamSlug = candidates[0]?.teamSlug || "";
    if (!Array.isArray(slot.emblems) || slot.emblems.length !== 3) slot.emblems = defaultFantasyEmblems(roleKey);
    slot.emblems = slot.emblems.map((emblem, index) => normalizeFantasyEmblem(emblem, fantasyRoleRules[roleKey].colors[index]));
    fantasyBuilderState.slots[roleKey] = slot;
  });
  saveFantasyBuilderState();
}

function normalizeFantasyEmblem(emblem, color) {
  const allowed = fantasyMetricsForColor(color);
  const metric = allowed.some(item => item.key === emblem?.metric) ? emblem.metric : allowed[0]?.key;
  return {
    color,
    metric,
    tier: fantasyTierBonuses[Number(emblem?.tier)] !== undefined ? Number(emblem.tier) : 1,
    trait: fantasyTraits[emblem?.trait] ? emblem.trait : "none",
  };
}

function fantasyMetricsForColor(color) {
  return Object.entries(fantasyMetricCatalog).filter(([, metric]) => metric.color === color)
    .map(([key, metric]) => ({ key, ...metric }));
}

function fantasyBuilderSelectedMatches(detail) {
  const selected = new Set(fantasyBuilderState.leagues || []);
  return (detail?.matches || []).filter(match => selected.has(match.leagueName || "Без турнира"))
    .sort((left, right) => Number(right.startTime || 0) - Number(left.startTime || 0))
    .slice(0, Math.max(1, Number(fantasyBuilderState.limit || 1)))
    .sort((left, right) => Number(left.startTime || 0) - Number(right.startTime || 0));
}

function fantasyDetailForPlayer(player) {
  return playerFilterDetail(player) || Object.values(publicPlayerFilterData).find(detail =>
    Number(detail?.player?.accountId || 0) === Number(player.accountId || 0));
}

function fantasyCandidateBySlot(roleKey) {
  const slug = fantasyBuilderState.slots[roleKey].teamSlug;
  return fantasyRoleCandidates(roleKey).find(candidate => candidate.teamSlug === slug) || fantasyRoleCandidates(roleKey)[0];
}

function fantasyMetricPoints(match, key) {
  return Number((match?.metrics || []).find(metric => metric.key === key)?.averagePoints || 0);
}

function fantasyEmblemModifiers(emblems) {
  const modifiers = emblems.map(emblem => Number(fantasyTierBonuses[emblem.tier] || 0));
  const property = emblems.map(() => 0);
  const allTiersDifferent = new Set(emblems.map(emblem => emblem.tier)).size === emblems.length;
  const uniqueCount = emblems.filter(emblem => emblem.trait === "unique").length;
  const friendlyCount = emblems.filter(emblem => emblem.trait === "friendly").length;
  emblems.forEach((emblem, index) => {
    if (emblem.trait === "fractal" && allTiersDifferent) property[index] += .60;
    if (emblem.trait === "unique" && uniqueCount === 1) property[index] += .30;
    if (emblem.trait === "friendly" && friendlyCount >= 3) property[index] += .50;
    if (emblem.trait === "vampiric") {
      property[index] += .50;
      if (index > 0) property[index - 1] -= .10;
      if (index < emblems.length - 1) property[index + 1] -= .10;
    }
    if (emblem.trait === "benevolent") {
      if (index > 0) property[index - 1] += .20;
      if (index < emblems.length - 1) property[index + 1] += .20;
    }
  });
  return modifiers.map((tier, index) => ({ tier, property: property[index], total: tier + property[index] }));
}

function fantasyPrefixApplies(prefixKey, heroId) {
  if (prefixKey === "none") return true;
  return fantasyTitlePrefixes[prefixKey]?.heroIds?.has(Number(heroId)) || false;
}

function fantasySuffixResult(suffixKey, match) {
  const suffix = fantasyTitleSuffixes[suffixKey] || fantasyTitleSuffixes.none;
  if (suffixKey === "none") return { known: true, active: true };
  if (suffix.support === "missing") return { known: false, active: false };
  if (suffixKey === "underdog") return { known: true, active: !match.won };
  if (suffixKey === "decisive") return { known: Number(match.duration || 0) > 0, active: Number(match.duration || 0) > 0 && Number(match.duration) < 1500 };
  if (suffixKey === "clutch") return { known: Boolean(match.fantasySeriesKnown), active: Boolean(match.fantasyLastPossibleMap) };
  if (suffixKey === "lucky") return { known: Number(match.duration || 0) > 0, active: Number(match.duration || 0) > 0 && Math.floor(Number(match.duration)) % 10 === 8 };
  return { known: false, active: false };
}

function annotateFantasySeries(matches) {
  const groups = new Map();
  matches.forEach(match => {
    const key = String(match.seriesId || match.matchId);
    const group = groups.get(key) || [];
    group.push(match);
    groups.set(key, group);
  });
  groups.forEach(group => {
    group.sort((left, right) => Number(left.startTime || 0) - Number(right.startTime || 0));
    const seriesType = Number(group[0]?.seriesType || 0);
    const lastPossible = seriesType === 1 ? 3 : seriesType === 2 ? 5 : 0;
    group.forEach((match, index) => {
      match.fantasySeriesKnown = lastPossible > 0;
      match.fantasyLastPossibleMap = lastPossible > 0 && index + 1 === lastPossible;
    });
  });
  return matches;
}

function calculateFantasyMemberMap(match, emblems, title) {
  const base = (match.metrics || []).reduce((sum, metric) => sum + Number(metric.averagePoints || 0), 0);
  const modifiers = fantasyEmblemModifiers(emblems);
  const emblemBreakdown = emblems.map((emblem, index) => {
    const metricPoints = fantasyMetricPoints(match, emblem.metric);
    const tierPoints = metricPoints * modifiers[index].tier;
    const propertyPoints = metricPoints * modifiers[index].property;
    return { ...emblem, metricPoints, tierPoints, propertyPoints, points: tierPoints + propertyPoints, multiplier: 1 + modifiers[index].total };
  });
  const emblemsTotal = emblemBreakdown.reduce((sum, emblem) => sum + emblem.points, 0);
  const beforeTitle = base + emblemsTotal;
  const prefix = fantasyTitlePrefixes[title.prefix] || fantasyTitlePrefixes.none;
  const prefixKnown = title.prefix === "none" || Number(match.heroId || 0) > 0;
  const prefixActive = prefixKnown && fantasyPrefixApplies(title.prefix, match.heroId);
  const prefixPoints = prefixActive ? beforeTitle * prefix.bonus : 0;
  const suffix = fantasyTitleSuffixes[title.suffix] || fantasyTitleSuffixes.none;
  const suffixResult = fantasySuffixResult(title.suffix, match);
  const suffixPoints = suffixResult.active ? beforeTitle * suffix.bonus : 0;
  return {
    match,
    base,
    emblemBreakdown,
    emblemsTotal,
    prefixPoints,
    suffixPoints,
    prefixKnown,
    prefixActive,
    suffixKnown: suffixResult.known,
    suffixActive: suffixResult.active,
    total: beforeTitle + prefixPoints + suffixPoints,
  };
}

function averageFantasyCalculations(calculations, match) {
  const count = Math.max(1, calculations.length);
  return {
    match,
    members: calculations,
    base: calculations.reduce((sum, item) => sum + item.base, 0) / count,
    emblemsTotal: calculations.reduce((sum, item) => sum + item.emblemsTotal, 0) / count,
    prefixPoints: calculations.reduce((sum, item) => sum + item.prefixPoints, 0) / count,
    suffixPoints: calculations.reduce((sum, item) => sum + item.suffixPoints, 0) / count,
    total: calculations.reduce((sum, item) => sum + item.total, 0) / count,
    emblemBreakdown: calculations[0].emblemBreakdown.map((emblem, index) => ({
      ...emblem,
      points: calculations.reduce((sum, item) => sum + item.emblemBreakdown[index].points, 0) / count,
      tierPoints: calculations.reduce((sum, item) => sum + item.emblemBreakdown[index].tierPoints, 0) / count,
      propertyPoints: calculations.reduce((sum, item) => sum + item.emblemBreakdown[index].propertyPoints, 0) / count,
    })),
  };
}

function fantasyRoleMapCalculations(candidate, slot, title = fantasyBuilderState.title) {
  if (!candidate) return [];
  const memberMatches = candidate.members.map(member => {
    const detail = fantasyDetailForPlayer(member);
    return new Map(annotateFantasySeries(fantasyBuilderSelectedMatches(detail)).map(match => [Number(match.matchId), match]));
  });
  const commonIds = [...(memberMatches[0]?.keys() || [])].filter(id => memberMatches.every(matches => matches.has(id)));
  return commonIds.map(matchId => {
    const matches = memberMatches.map(items => items.get(matchId));
    const calculations = matches.map(match => calculateFantasyMemberMap(match, slot.emblems, title));
    return averageFantasyCalculations(calculations, matches[0]);
  }).sort((left, right) => Number(left.match.startTime || 0) - Number(right.match.startTime || 0));
}

function fantasySeriesCalculations(maps) {
  const groups = new Map();
  maps.forEach(map => {
    const key = String(map.match.seriesId || map.match.matchId);
    const group = groups.get(key) || [];
    group.push(map);
    groups.set(key, group);
  });
  return [...groups.values()].map(group => {
    const best = [...group].sort((left, right) => right.total - left.total).slice(0, 2);
    const sumField = field => best.reduce((sum, map) => sum + Number(map[field] || 0), 0);
    return {
      maps: group,
      countedMaps: best,
      match: group[0].match,
      base: sumField("base"),
      emblemsTotal: sumField("emblemsTotal"),
      prefixPoints: sumField("prefixPoints"),
      suffixPoints: sumField("suffixPoints"),
      total: sumField("total"),
      emblemBreakdown: best[0]?.emblemBreakdown.map((emblem, index) => ({
        ...emblem,
        points: best.reduce((sum, map) => sum + map.emblemBreakdown[index].points, 0),
        tierPoints: best.reduce((sum, map) => sum + map.emblemBreakdown[index].tierPoints, 0),
        propertyPoints: best.reduce((sum, map) => sum + map.emblemBreakdown[index].propertyPoints, 0),
      })) || [],
    };
  }).sort((left, right) => Number(left.match.startTime || 0) - Number(right.match.startTime || 0));
}

function fantasyAverageProjection(records) {
  if (!records.length) return { total: 0, base: 0, emblemsTotal: 0, prefixPoints: 0, suffixPoints: 0, emblemBreakdown: [] };
  const average = field => records.reduce((sum, record) => sum + Number(record[field] || 0), 0) / records.length;
  return {
    total: average("total"), base: average("base"), emblemsTotal: average("emblemsTotal"),
    prefixPoints: average("prefixPoints"), suffixPoints: average("suffixPoints"),
    emblemBreakdown: records[0].emblemBreakdown.map((emblem, index) => ({
      ...emblem,
      points: records.reduce((sum, record) => sum + record.emblemBreakdown[index].points, 0) / records.length,
      tierPoints: records.reduce((sum, record) => sum + record.emblemBreakdown[index].tierPoints, 0) / records.length,
      propertyPoints: records.reduce((sum, record) => sum + record.emblemBreakdown[index].propertyPoints, 0) / records.length,
    })),
  };
}

function fantasyRoleProjection(roleKey, candidate = fantasyCandidateBySlot(roleKey), title = fantasyBuilderState.title) {
  const slot = fantasyBuilderState.slots[roleKey];
  const maps = fantasyRoleMapCalculations(candidate, slot, title);
  const series = fantasySeriesCalculations(maps);
  return {
    candidate, maps, series,
    mapAverage: fantasyAverageProjection(maps),
    seriesAverage: fantasyAverageProjection(series),
  };
}

function fantasyRosterProjection(title) {
  const mode = fantasyBuilderState.mode === "map" ? "map" : "series";
  const projections = Object.fromEntries(Object.keys(fantasyRoleRules).map(roleKey => [
    roleKey,
    fantasyRoleProjection(roleKey, fantasyCandidateBySlot(roleKey), title),
  ]));
  const selected = Object.values(projections).map(projection => mode === "map"
    ? { average: projection.mapAverage, records: projection.maps }
    : { average: projection.seriesAverage, records: projection.series });
  return {
    mode,
    projections,
    recordCount: selected.reduce((sum, item) => sum + item.records.length, 0),
    total: selected.reduce((sum, item) => sum + Number(item.average.total || 0), 0),
    prefixPoints: selected.reduce((sum, item) => sum + Number(item.average.prefixPoints || 0), 0),
    suffixPoints: selected.reduce((sum, item) => sum + Number(item.average.suffixPoints || 0), 0),
  };
}

function fantasyTitleEvidenceLabel(kind, key) {
  if (kind === "prefix") return "Подходящих героев";
  return {
    underdog: "Поражений",
    decisive: "Карт короче 25 минут",
    clutch: "Решающих карт",
    lucky: "Длительность закончилась на 8",
  }[key] || "Срабатываний";
}

function fantasyMemberTitleAverage(roleProjection, memberIndex, field, mode) {
  if (mode === "map") {
    const values = roleProjection.maps.map(map => Number(map.members?.[memberIndex]?.[field] || 0));
    return values.reduce((sum, value) => sum + value, 0) / Math.max(1, values.length);
  }
  const values = roleProjection.series.map(series => series.countedMaps
    .reduce((sum, map) => sum + Number(map.members?.[memberIndex]?.[field] || 0), 0));
  return values.reduce((sum, value) => sum + value, 0) / Math.max(1, values.length);
}

function fantasyTitleEvidence(kind, key, projection) {
  if (!projection?.projections || key === "none") return null;
  const pointsField = kind === "prefix" ? "prefixPoints" : "suffixPoints";
  const knownField = kind === "prefix" ? "prefixKnown" : "suffixKnown";
  const activeField = kind === "prefix" ? "prefixActive" : "suffixActive";
  const heroCounts = new Map();
  const matches = new Map();
  const roles = [];
  let total = 0;
  let known = 0;
  let hits = 0;

  Object.entries(projection.projections).forEach(([roleKey, roleProjection]) => {
    if (!roleProjection?.candidate) return;
    const countedMapIds = new Set(roleProjection.series.flatMap(series =>
      series.countedMaps.map(map => Number(map.match.matchId))));
    const members = roleProjection.candidate.members.map((member, memberIndex) => ({
      name: member.name,
      position: Number(member.position || 0),
      total: 0,
      known: 0,
      hits: 0,
      points: fantasyMemberTitleAverage(roleProjection, memberIndex, pointsField, projection.mode),
    }));

    roleProjection.maps.forEach(map => {
      let mapActive = false;
      const activePlayers = [];
      const activeHeroes = [];
      map.members.forEach((calculation, memberIndex) => {
        const member = members[memberIndex];
        if (!member) return;
        member.total += 1;
        total += 1;
        if (calculation[knownField]) {
          member.known += 1;
          known += 1;
        }
        if (!calculation[activeField]) return;
        member.hits += 1;
        hits += 1;
        mapActive = true;
        activePlayers.push(member.name);
        const name = heroName(calculation.match.heroId);
        if (kind === "prefix" && name) {
          activeHeroes.push(name);
          heroCounts.set(name, (heroCounts.get(name) || 0) + 1);
        }
      });
      if (!mapActive) return;
      const matchId = Number(map.match.matchId);
      const item = matches.get(matchId) || {
        matchId,
        startTime: Number(map.match.startTime || 0),
        opponentName: map.match.opponentName || "Соперник",
        leagueName: map.match.leagueName || "Турнир",
        roles: new Set(),
        players: new Set(),
        heroes: new Set(),
        points: 0,
        counted: false,
      };
      item.roles.add(fantasyRoleRules[roleKey].label);
      activePlayers.forEach(name => item.players.add(name));
      activeHeroes.forEach(name => item.heroes.add(name));
      item.points += Number(map[pointsField] || 0);
      item.counted ||= projection.mode === "map" || countedMapIds.has(matchId);
      matches.set(matchId, item);
    });

    const roleAverage = projection.mode === "map" ? roleProjection.mapAverage : roleProjection.seriesAverage;
    roles.push({
      roleKey,
      label: fantasyRoleRules[roleKey].label,
      teamName: roleProjection.candidate.teamName,
      points: Number(roleAverage[pointsField] || 0),
      members,
    });
  });

  return {
    kind,
    key,
    mode: projection.mode,
    metricLabel: fantasyTitleEvidenceLabel(kind, key),
    total,
    known,
    hits,
    roles,
    heroes: [...heroCounts.entries()].map(([name, count]) => ({ name, count }))
      .sort((left, right) => right.count - left.count || left.name.localeCompare(right.name, "ru")),
    matches: [...matches.values()].sort((left, right) => right.startTime - left.startTime),
  };
}

function fantasyTitleRecommendations(kind, title, projectTitle = fantasyRosterProjection) {
  const catalog = kind === "prefix" ? fantasyTitlePrefixes : fantasyTitleSuffixes;
  const recommendations = Object.entries(catalog).map(([key, item]) => {
    const candidateTitle = { ...title, [kind]: key };
    const projection = projectTitle(candidateTitle);
    const available = item.support !== "missing" && projection.recordCount > 0;
    return {
      key,
      item,
      available,
      points: kind === "prefix" ? projection.prefixPoints : projection.suffixPoints,
      total: projection.total,
      mode: projection.mode,
      evidence: fantasyTitleEvidence(kind, key, projection),
    };
  });
  const ranked = recommendations.filter(item => item.available)
    .sort((left, right) => right.points - left.points || right.total - left.total);
  const bestKey = ranked[0]?.key || "";
  return recommendations.map(item => ({ ...item, recommended: item.key === bestKey }));
}

function renderFantasyBuilder(viewState = null) {
  const root = fantasyBuilderRoot();
  if (!root) return;
  const overview = buildPublicTournamentFilterOverview(publicPlayerFilterData);
  const roleProjections = Object.fromEntries(Object.keys(fantasyRoleRules).map(key => [key, fantasyRoleProjection(key)]));
  const mode = fantasyBuilderState.mode === "map" ? "map" : "series";
  const total = Object.values(roleProjections).reduce((sum, projection) =>
    sum + Number(mode === "map" ? projection.mapAverage.total : projection.seriesAverage.total), 0);
  root.innerHTML = `
    <section class="fantasy-builder-hero">
      <div>
        <p class="eyebrow">TI 2026 Fantasy Lab</p>
        <h1>Моя команда TI</h1>
        <p>Соберите три готовые ролевые связки и перенесите параметры эмблем из клиента.</p>
      </div>
    </section>
    ${renderFantasyBuilderFilters(overview)}
    <section class="fantasy-builder-summary">
      <div class="fantasy-score-mode" role="group" aria-label="Режим прогноза">
        <button type="button" data-fantasy-mode="map" class="${mode === "map" ? "active" : ""}">Среднее за карту</button>
        <button type="button" data-fantasy-mode="series" class="${mode === "series" ? "active" : ""}">Среднее за серию</button>
      </div>
      <button type="button" class="fantasy-title-button" data-fantasy-title>
        <span>Тренерский титул</span>
        <strong>${escapeHTML(fantasyTitlePrefixes[fantasyBuilderState.title.prefix].label)} ${escapeHTML(fantasyTitleSuffixes[fantasyBuilderState.title.suffix].label)}</strong>
      </button>
      <div><span>Прогноз состава</span><strong>${formatPoints(total)}</strong></div>
    </section>
    <section class="fantasy-banner-grid">
      ${Object.keys(fantasyRoleRules).map(roleKey => renderFantasyRoleBanner(roleKey, roleProjections[roleKey], mode)).join("")}
    </section>
    <section class="fantasy-builder-note">
      <strong>Как читать прогноз</strong>
      <p>Для пары сначала считается каждый игрок, затем их результат усредняется. В серии учитываются две лучшие карты, как в правилах фэнтези.</p>
    </section>`;
  bindImageFallbacks(root);
  if (viewState) {
    const restoreView = () => {
      const filterList = root.querySelector(".fantasy-filter-list");
      if (filterList) {
        filterList.scrollLeft = Number(viewState.filterScrollLeft || 0);
        filterList.scrollTop = Number(viewState.filterScrollTop || 0);
      }
      window.scrollTo(Number(viewState.scrollX || 0), Number(viewState.scrollY || 0));
    };
    requestAnimationFrame(() => {
      restoreView();
      requestAnimationFrame(restoreView);
    });
  }
}

function rerenderFantasyBuilderPreservingView() {
  const filterList = fantasyBuilderRoot()?.querySelector(".fantasy-filter-list");
  renderFantasyBuilder({
    scrollX: window.scrollX,
    scrollY: window.scrollY,
    filterScrollLeft: filterList?.scrollLeft || 0,
    filterScrollTop: filterList?.scrollTop || 0,
  });
}

function fantasyTournamentSortTime(group) {
  return Math.max(Number(group?.lastMatch || 0), Number(group?.firstMatch || 0));
}

function fantasyTournamentDate(group) {
  const first = Number(group?.firstMatch || 0);
  const last = Number(group?.lastMatch || first);
  if (!first && !last) return "Дата не указана";
  const formatter = new Intl.DateTimeFormat("ru-RU", {
    day: "2-digit", month: "2-digit", year: "numeric", timeZone: "Europe/Moscow",
  });
  const firstLabel = formatter.format(new Date((first || last) * 1000));
  const lastLabel = formatter.format(new Date((last || first) * 1000));
  return firstLabel === lastLabel ? firstLabel : `${firstLabel} - ${lastLabel}`;
}

function renderFantasyBuilderFilters(overview) {
  const groups = tournamentFilterGroups(overview.leagues).sort((left, right) =>
    fantasyTournamentSortTime(right) - fantasyTournamentSortTime(left)
      || String(left.name || "").localeCompare(String(right.name || ""), "ru"));
  const selected = new Set(fantasyBuilderState.leagues);
  const selectedGroupCount = groups.filter(group => groupIsSelected(selected, group)).length;
  const tournamentCount = groups.reduce((total, group) => total + group.names.length, 0);
  return `<section class="fantasy-builder-filters">
    <div class="fantasy-filter-heading">
      <span>Турниры для расчёта</span>
      <div><strong>${selectedGroupCount} из ${groups.length} групп · ${tournamentCount} турниров</strong>${groups.length > 1 ? `<button type="button" data-fantasy-filters-toggle aria-expanded="${fantasyFiltersExpanded}">${fantasyFiltersExpanded ? "Свернуть" : "Показать все турниры"}</button>` : ""}</div>
    </div>
    <div class="fantasy-filter-list ${fantasyFiltersExpanded ? "is-expanded" : ""}">${groups.map(group => `<label data-fantasy-tournament-date="${fantasyTournamentSortTime(group)}">
      <input type="checkbox" data-fantasy-league data-league-names="${encodeLeagueNames(group.names)}" ${groupIsSelected(selected, group) ? "checked" : ""}>
      ${renderTournamentFilterLogo(group)}
      <span><strong>${escapeHTML(group.name)}</strong><small><time>${fantasyTournamentDate(group)}</time><span>${countLabel(group.matchCount, ["карта", "карты", "карт"])}</span></small></span>
    </label>`).join("")}</div>
    <label class="fantasy-limit"><span>Последних карт на команду</span><input type="number" min="1" max="${overview.maxMatches}" value="${fantasyBuilderState.limit}" data-fantasy-limit></label>
  </section>`;
}

function renderFantasyRoleBanner(roleKey, projection, mode) {
  const rule = fantasyRoleRules[roleKey];
  const candidate = projection.candidate;
  const slot = fantasyBuilderState.slots[roleKey];
  const score = mode === "map" ? projection.mapAverage : projection.seriesAverage;
  const records = mode === "map" ? projection.maps : projection.series;
  const logo = fantasyAssets.teamEmblem(candidate?.teamSlug, candidate?.teamLogoUrl);
  const members = candidate?.members || [];
  const memberNames = members.map(member => member.name).join(" и ");
  return `<article class="fantasy-banner fantasy-banner-${roleKey}">
    <div class="fantasy-banner-stage">
      <section class="fantasy-banner-roster">
        <header class="fantasy-banner-heading">
          <span>${rule.label}</span>
          <strong>${escapeHTML(memberNames || "Команда не выбрана")}</strong>
          <small>${escapeHTML(candidate?.teamName || "Выберите состав")}</small>
        </header>
        <button type="button" class="fantasy-change-team" data-fantasy-change-player="${roleKey}">Сменить команду</button>
        <div class="fantasy-banner-logo">${logo ? `<img src="${escapeAttribute(logo)}" alt="">` : `<span>${escapeHTML((candidate?.teamName || "?").slice(0, 2))}</span>`}</div>
        <div class="fantasy-banner-players">${members.map(member => `
          <a href="#player/${encodeURIComponent(member.alias)}/overview" title="Открыть профиль ${escapeAttribute(member.name)}">
            ${playerImage(member, "fantasy-banner-player-photo", "eager")}
          </a>`).join("")}</div>
      </section>
      <div class="fantasy-emblem-stack">${slot.emblems.map((emblem, index) => renderFantasyEmblem(roleKey, emblem, score.emblemBreakdown[index], index)).join("")}</div>
      <button type="button" class="fantasy-banner-score" data-fantasy-history="${roleKey}" data-history-mode="${mode}">
        <span>${mode === "map" ? "Среднее за карту" : "Среднее за серию"}</span>
        <strong>${formatPoints(score.total)}</strong>
        <small>${records.length} ${mode === "map" ? "карт" : "серий"} в расчёте</small>
      </button>
    </div>
    <section class="fantasy-banner-details">
      <div class="fantasy-banner-details-heading"><span>Подробный расчёт и рекомендации</span><strong>${formatPoints(score.total)}</strong></div>
      <div class="fantasy-banner-details-grid">
        <div class="fantasy-contribution">
          <span>Из чего сложились очки</span>
          <div>
            <p><span>Базовая статистика</span><strong>${formatSignedFantasyPoints(score.base)}</strong></p>
            ${score.emblemBreakdown.map((emblem, index) => `<p><span class="fantasy-contribution-metric">${fantasyMetricIcon(emblem.metric, "fantasy-contribution-icon")}<i>Эмблема ${index + 1}: ${escapeHTML(fantasyMetricCatalog[emblem.metric]?.label || emblem.metric)}</i></span><strong>${formatSignedFantasyPoints(emblem.points)}</strong></p>`).join("")}
            <p><span>Префикс титула</span><strong>${formatSignedFantasyPoints(score.prefixPoints)}</strong></p>
            <p><span>Суффикс титула</span><strong>${formatSignedFantasyPoints(score.suffixPoints)}</strong></p>
          </div>
        </div>
        ${renderFantasyEmblemAdvice(projection, roleKey)}
      </div>
    </section>
  </article>`;
}

function renderFantasyEmblem(roleKey, emblem, breakdown, index) {
  const trait = fantasyTraits[emblem.trait];
  const multiplier = breakdown?.multiplier || (1 + fantasyTierBonuses[emblem.tier]);
  return `<button type="button" class="fantasy-emblem fantasy-emblem-${emblem.color}" data-fantasy-emblem="${roleKey}:${index}">
    <span class="fantasy-emblem-icon" aria-hidden="true">${fantasyMetricIcon(emblem.metric)}</span>
    <span class="fantasy-emblem-copy">
      <span class="fantasy-emblem-heading"><strong>${escapeHTML(fantasyMetricCatalog[emblem.metric]?.label || emblem.metric)}</strong><em>${Math.round(multiplier * 100)}%</em></span>
      <small><i>${romanFantasyTier(emblem.tier)} разряд</i><b>+${Math.round(fantasyTierBonuses[emblem.tier] * 100)}%</b></small>
      <small><i>${escapeHTML(trait.label)}</i><b>${fantasyTraitBonusLabel(emblem.trait)}</b></small>
    </span>
  </button>`;
}

function fantasyMetricIcon(metric, className = "") {
  return fantasyAssets.metricIcon(metric, className);
}

function fantasyTraitBonusLabel(traitKey) {
  return { fractal: "+60%", benevolent: "+20%", vampiric: "+50%", unique: "+30%", friendly: "+50%" }[traitKey] || "0%";
}

function fantasyMetricRecommendations(projection, roleKey) {
  const maps = projection.maps;
  const averages = new Map();
  maps.forEach(map => map.members.forEach(member => (member.match.metrics || []).forEach(metric => {
    const item = averages.get(metric.key) || { key: metric.key, total: 0, count: 0 };
    item.total += Number(metric.averagePoints || 0);
    item.count += 1;
    averages.set(metric.key, item);
  })));
  const byColor = {};
  fantasyRoleRules[roleKey].colors.forEach(color => {
    byColor[color] = fantasyMetricsForColor(color).map(metric => ({
      ...metric, points: (averages.get(metric.key)?.total || 0) / Math.max(1, averages.get(metric.key)?.count || 0),
    })).sort((left, right) => right.points - left.points);
  });
  return byColor;
}

function renderFantasyEmblemAdvice(projection, roleKey) {
  const advice = fantasyMetricRecommendations(projection, roleKey);
  const colors = [...new Set(fantasyRoleRules[roleKey].colors)];
  return `<div class="fantasy-advice"><span>Рекомендации под выбранных игроков</span>${colors.map(color => {
    const items = (advice[color] || []).slice(0, 3);
    return `<p><i class="fantasy-color-dot ${color}"></i><strong>${fantasyColorLabel(color)}:</strong> ${items.map(item => `<span class="fantasy-advice-metric">${fantasyMetricIcon(item.key)}<i>${escapeHTML(item.label)} ${formatPoints(item.points)}</i></span>`).join("")}</p>`;
  }).join("")}</div>`;
}

function formatSignedFantasyPoints(value) {
  const number = Number(value || 0);
  return `${number > 0 ? "+" : ""}${formatPoints(number)}`;
}

function romanFantasyTier(value) {
  return ["", "I", "II", "III", "IV", "V"][Number(value || 1)] || "I";
}

function fantasyColorLabel(color) {
  return { red: "Красные", blue: "Синие", green: "Зелёные" }[color] || color;
}

function bindFantasyBuilderShell() {
  const root = fantasyBuilderRoot();
  const dialog = fantasyBuilderDialog();
  root?.addEventListener("click", event => {
    const target = event.target.closest("button");
    if (!target) return;
    if (target.dataset.fantasyMode) {
      fantasyBuilderState.mode = target.dataset.fantasyMode;
      saveFantasyBuilderState();
      rerenderFantasyBuilderPreservingView();
    } else if (target.hasAttribute("data-fantasy-filters-toggle")) {
      fantasyFiltersExpanded = !fantasyFiltersExpanded;
      const filterList = root.querySelector(".fantasy-filter-list");
      filterList?.classList.toggle("is-expanded", fantasyFiltersExpanded);
      target.setAttribute("aria-expanded", String(fantasyFiltersExpanded));
      target.textContent = fantasyFiltersExpanded ? "Свернуть" : "Показать все турниры";
    } else if (target.dataset.fantasyChangePlayer) {
      openFantasyPlayerPicker(target.dataset.fantasyChangePlayer);
    } else if (target.dataset.fantasyEmblem) {
      const [roleKey, index] = target.dataset.fantasyEmblem.split(":");
      openFantasyEmblemEditor(roleKey, Number(index));
    } else if (target.hasAttribute("data-fantasy-title")) {
      openFantasyTitleEditor();
    } else if (target.dataset.fantasyHistory) {
      openFantasyHistory(target.dataset.fantasyHistory, target.dataset.historyMode);
    }
  });
  root?.addEventListener("change", event => {
    if (event.target.matches("[data-fantasy-league]")) {
      const checked = [...root.querySelectorAll("[data-fantasy-league]:checked")]
        .flatMap(input => leagueNamesFromInput(input));
      if (!checked.length) {
        event.target.checked = true;
        return;
      }
      fantasyBuilderState.leagues = [...new Set(checked)];
      saveFantasyBuilderState();
      rerenderFantasyBuilderPreservingView();
    }
    if (event.target.matches("[data-fantasy-limit]")) {
      fantasyBuilderState.limit = Math.max(1, Number(event.target.value || 1));
      saveFantasyBuilderState();
      rerenderFantasyBuilderPreservingView();
    }
  });
  dialog?.addEventListener("click", event => {
    if (event.target === dialog || event.target.closest("[data-close-fantasy-dialog]")) {
      dialog.close();
      return;
    }
    const titleDetails = event.target.closest("[data-fantasy-title-details]");
    if (titleDetails) {
      const option = titleDetails.closest(".fantasy-title-option");
      const willOpen = !option?.classList.contains("is-open");
      dialog.querySelectorAll(".fantasy-title-option.is-open").forEach(item => {
        item.classList.remove("is-open");
        item.querySelector("[data-fantasy-title-details]")?.setAttribute("aria-expanded", "false");
      });
      option?.classList.toggle("is-open", willOpen);
      titleDetails.setAttribute("aria-expanded", String(willOpen));
      return;
    }
    const evidenceExpand = event.target.closest("[data-fantasy-evidence-expand]");
    if (evidenceExpand) {
      evidenceExpand.closest(".fantasy-title-evidence-matches")
        ?.querySelectorAll("[data-fantasy-evidence-match][hidden]")
        .forEach(button => button.removeAttribute("hidden"));
      evidenceExpand.remove();
      return;
    }
    const evidenceMatch = event.target.closest("[data-fantasy-evidence-match]");
    if (evidenceMatch) {
      dialog.close();
      openMatch(Number(evidenceMatch.dataset.fantasyEvidenceMatch));
      return;
    }
    const playerButton = event.target.closest("[data-fantasy-pick-team]");
    if (playerButton && fantasyBuilderActiveDialog?.type === "player") {
      fantasyBuilderActiveDialog.selectedTeamSlug = playerButton.dataset.fantasyPickTeam;
      dialog.querySelectorAll("[data-fantasy-pick-team]").forEach(button => {
        const selected = button === playerButton;
        button.classList.toggle("pending", selected);
        button.setAttribute("aria-pressed", String(selected));
      });
      const confirm = dialog.querySelector("[data-fantasy-confirm-player]");
      if (confirm) confirm.disabled = false;
    }
    const confirmPlayer = event.target.closest("[data-fantasy-confirm-player]");
    if (confirmPlayer && fantasyBuilderActiveDialog?.type === "player" && fantasyBuilderActiveDialog.selectedTeamSlug) {
      fantasyBuilderState.slots[fantasyBuilderActiveDialog.roleKey].teamSlug = fantasyBuilderActiveDialog.selectedTeamSlug;
      saveFantasyBuilderState(); dialog.close(); renderFantasyBuilder();
    }
    const seriesButton = event.target.closest("[data-fantasy-open-series]");
    if (seriesButton && fantasyBuilderActiveDialog?.projection) {
      const record = fantasyBuilderActiveDialog.projection.series[Number(seriesButton.dataset.fantasyOpenSeries)];
      if (record) {
        dialog.close();
        openSeries({
          matchIds: record.maps.map(map => map.match.matchId),
          label: seriesLabel({ seriesType: record.match.seriesType, matches: record.maps.map(map => map.match) }),
          opponent: record.match.opponentName,
          league: record.match.leagueName,
          startTime: record.match.startTime,
          wins: record.maps.filter(map => map.match.won).length,
          losses: record.maps.filter(map => !map.match.won).length,
          teamName: fantasyBuilderActiveDialog.projection.candidate.teamName,
        });
      }
    }
    const mapButton = event.target.closest("[data-fantasy-open-map]");
    if (mapButton) { dialog.close(); openMatch(Number(mapButton.dataset.fantasyOpenMap)); }
  });
  dialog?.addEventListener("change", event => {
    const form = event.target.closest("[data-fantasy-title-form]");
    if (!form || !event.target.matches('input[name="prefix"], input[name="suffix"]')) return;
    const data = new FormData(form);
    refreshFantasyTitleRecommendations(form, {
      prefix: String(data.get("prefix")),
      suffix: String(data.get("suffix")),
    });
  });
  dialog?.addEventListener("submit", event => {
    event.preventDefault();
    const form = event.target;
    if (form.matches("[data-fantasy-emblem-form]")) {
      const { roleKey, index } = fantasyBuilderActiveDialog;
      fantasyBuilderState.slots[roleKey].emblems[index] = normalizeFantasyEmblem({
        metric: new FormData(form).get("metric"), tier: Number(new FormData(form).get("tier")), trait: new FormData(form).get("trait"),
      }, fantasyRoleRules[roleKey].colors[index]);
      saveFantasyBuilderState(); dialog.close(); renderFantasyBuilder();
    }
    if (form.matches("[data-fantasy-title-form]")) {
      const data = new FormData(form);
      fantasyBuilderState.title = { prefix: String(data.get("prefix")), suffix: String(data.get("suffix")) };
      saveFantasyBuilderState(); dialog.close(); renderFantasyBuilder();
    }
  });
}

function setFantasyDialog(title, eyebrow, content, className = "") {
  const dialog = fantasyBuilderDialog();
  dialog.className = `fantasy-builder-dialog ${className}`.trim();
  dialog.querySelector("[data-fantasy-dialog-eyebrow]").textContent = eyebrow;
  dialog.querySelector("[data-fantasy-dialog-title]").textContent = title;
  dialog.querySelector("[data-fantasy-dialog-content]").innerHTML = content;
  if (!dialog.open) dialog.showModal();
  bindImageFallbacks(dialog);
}

function openFantasyPlayerPicker(roleKey) {
  const candidates = fantasyRoleCandidates(roleKey).map(candidate => ({ candidate, projection: fantasyRoleProjection(roleKey, candidate) }));
  const candidateScore = item => Number(item.projection.mapAverage.total);
  const leaderRanks = new Map([...candidates]
    .sort((left, right) => candidateScore(right) - candidateScore(left))
    .slice(0, 3)
    .map((item, index) => [item.candidate.teamSlug, index + 1]));
  fantasyBuilderActiveDialog = {
    type: "player",
    roleKey,
    selectedTeamSlug: fantasyBuilderState.slots[roleKey].teamSlug,
  };
  const pickerTitle = {
    cores: "Выберите дуэт основы",
    mid: "Выберите игрока центра",
    supps: "Выберите дуэт поддержки",
  }[roleKey] || `Выберите ${fantasyRoleRules[roleKey].short}`;
  setFantasyDialog(pickerTitle, "Готовые турнирные составы", `
    <div class="fantasy-picker-list">${candidates.map(({ candidate, projection }) => {
      const logo = fantasyAssets.teamEmblem(candidate.teamSlug, candidate.teamLogoUrl);
      const ratings = fantasyCandidateRatings(candidate);
      const score = projection.mapAverage.total;
      const seriesScore = projection.seriesAverage.total;
      const leaderRank = leaderRanks.get(candidate.teamSlug);
      const classes = [
        candidate.teamSlug === fantasyBuilderState.slots[roleKey].teamSlug ? "selected" : "",
        leaderRank ? "fantasy-picker-top" : "",
      ].filter(Boolean).join(" ");
      return `<button type="button" data-fantasy-pick-team="${escapeAttribute(candidate.teamSlug)}" data-fantasy-score="${score}" class="${classes}" aria-pressed="false">
        ${leaderRank ? `<span class="fantasy-picker-rank">Топ ${leaderRank}</span>` : ""}
        <span class="fantasy-picker-team"><strong>${escapeHTML(candidate.teamName)}</strong></span>
        <span class="fantasy-picker-visual">
          <span class="fantasy-picker-crest">${logo ? `<img src="${escapeAttribute(logo)}" alt="">` : `<b>${escapeHTML(candidate.teamName.slice(0, 2))}</b>`}</span>
          <span class="fantasy-picker-portraits">${candidate.members.map(member => `<span>${playerImage(member, "fantasy-picker-player-photo", "eager")}</span>`).join("")}</span>
        </span>
        <span class="fantasy-picker-members">${candidate.members.map(member => `<i><strong>${escapeHTML(member.name)}</strong><small>позиция ${Number(member.position || 0)}</small></i>`).join("")}</span>
        <span class="fantasy-picker-ratings">
          <i title="Стабильность пары по сыгранным картам"><small>Стабильность</small><strong>${ratings.stabilityAvailable ? `${formatNumber(ratings.stability)}/100` : "мало карт"}</strong></i>
          <i title="Средний коэффициент силы соперников пары"><small>Сила соперников</small><strong class="${strengthClass(ratings.opponentStrength)}">${strengthGrade(ratings.opponentStrength)} · ${formatNumber(ratings.opponentStrength)}×</strong></i>
        </span>
        <span class="fantasy-picker-score"><small>среднее за карту</small><strong>${formatPoints(score)}</strong><i>За серию: ${formatPoints(seriesScore)}</i></span>
      </button>`;
    }).join("")}</div>
    <div class="fantasy-picker-actions"><button type="button" data-fantasy-confirm-player disabled>Принять</button></div>`, "fantasy-picker-dialog");
}

function fantasyCandidateRatings(candidate) {
  const members = candidate?.members || [];
  const stableMembers = members.filter(member => Number(member.stats?.matches || 0) >= 3 && Number(member.stabilityConfidence || 0) > 0);
  const strengthMembers = members.filter(member => Number(member.opponentStrength || 0) > 0);
  return {
    stabilityAvailable: stableMembers.length === members.length && members.length > 0,
    stability: stableMembers.reduce((sum, member) => sum + Number(member.stability || 0), 0) / Math.max(1, stableMembers.length),
    opponentStrength: strengthMembers.reduce((sum, member) => sum + Number(member.opponentStrength || 1), 0) / Math.max(1, strengthMembers.length),
  };
}

function openFantasyEmblemEditor(roleKey, index) {
  const emblem = fantasyBuilderState.slots[roleKey].emblems[index];
  fantasyBuilderActiveDialog = { type: "emblem", roleKey, index };
  setFantasyDialog(`Эмблема ${index + 1}`, `${fantasyRoleRules[roleKey].label} · ${fantasyColorLabel(emblem.color)}`, `
    <form data-fantasy-emblem-form class="fantasy-editor-form">
      <fieldset><legend>Показатель</legend><div class="fantasy-option-grid metric-options">${fantasyMetricsForColor(emblem.color).map(metric => `<label><input type="radio" name="metric" value="${metric.key}" ${metric.key === emblem.metric ? "checked" : ""}><span>${fantasyMetricIcon(metric.key)}<strong>${escapeHTML(metric.label)}</strong></span></label>`).join("")}</div></fieldset>
      <fieldset><legend>Разряд</legend><div class="fantasy-tier-options">${Object.keys(fantasyTierBonuses).map(tier => `<label><input type="radio" name="tier" value="${tier}" ${Number(tier) === emblem.tier ? "checked" : ""}><span>${romanFantasyTier(tier)}<small>+${Math.round(fantasyTierBonuses[tier] * 100)}%</small></span></label>`).join("")}</div></fieldset>
      <fieldset><legend>Свойство</legend><div class="fantasy-option-grid trait-options">${Object.entries(fantasyTraits).map(([key, trait]) => `<label><input type="radio" name="trait" value="${key}" ${key === emblem.trait ? "checked" : ""}><span><strong>${escapeHTML(trait.label)}</strong><small>${escapeHTML(trait.description)}</small></span></label>`).join("")}</div></fieldset>
      <button type="submit" class="fantasy-dialog-primary">Применить</button>
    </form>`, "fantasy-editor-dialog");
}

function openFantasyTitleEditor() {
  fantasyBuilderActiveDialog = { type: "title" };
  setFantasyDialog("Изменить титул", "Один титул для всего состава", `
    <form data-fantasy-title-form class="fantasy-title-form">
      <p class="fantasy-title-summary">Прогноз учитывает текущий состав, эмблемы, выбранные турниры и режим <strong>${fantasyBuilderState.mode === "series" ? "среднее за серию" : "среднее за карту"}</strong>.</p>
      <fieldset><legend>Префикс</legend><div class="fantasy-title-options" data-fantasy-title-options="prefix">${renderFantasyTitleOptions("prefix", fantasyBuilderState.title)}</div></fieldset>
      <fieldset><legend>Суффикс</legend><div class="fantasy-title-options" data-fantasy-title-options="suffix">${renderFantasyTitleOptions("suffix", fantasyBuilderState.title)}</div></fieldset>
      <button type="submit" class="fantasy-dialog-primary">Применить титул</button>
    </form>`, "fantasy-title-dialog");
}

function renderFantasyTitleOptions(kind, title) {
  const selectedKey = title[kind];
  return fantasyTitleRecommendations(kind, title).map(({ key, item, available, points, total, recommended, evidence }) => {
    const classes = [
      item.support === "missing" ? "data-missing" : "",
      recommended ? "is-recommended" : "",
    ].filter(Boolean).join(" ");
    const forecast = available
      ? `<span class="fantasy-title-forecast"><span><small>Добавит к составу</small><strong>${formatSignedFantasyPoints(points)}</strong></span><span><small>Итог состава</small><b>${formatPoints(total)}</b></span></span>`
      : `<span class="fantasy-title-forecast unavailable"><strong>${item.support === "missing" ? "Точный расчёт недоступен" : "Нет матчей для расчёта"}</strong></span>`;
    return `<div class="fantasy-title-option ${classes}">
      <label>
        <input type="radio" name="${kind}" value="${key}" ${key === selectedKey ? "checked" : ""}>
        <span>
          <span class="fantasy-title-option-heading"><strong>${escapeHTML(item.label)}</strong>${recommended ? "<b>Лучший выбор</b>" : ""}</span>
          <small>${escapeHTML(item.description)}</small>
          ${forecast}
          ${item.support === "missing" ? `<em>${escapeHTML(item.reason)}</em>` : ""}
        </span>
      </label>
      ${key === "none" ? "" : `<button type="button" class="fantasy-title-details-trigger" data-fantasy-title-details aria-expanded="false">Почему столько очков?</button>${renderFantasyTitleEvidence(item, evidence, available)}`}
    </div>`;
  }).join("");
}

function renderFantasyTitleEvidence(item, evidence, available) {
  if (!evidence) return "";
  const checked = evidence.known || evidence.total;
  const rate = evidence.known ? Math.round(evidence.hits / evidence.known * 100) : 0;
  const modeNote = evidence.mode === "series"
    ? "Условия показаны по картам, а очки рассчитаны по лучшим картам каждой серии."
    : "Очки рассчитаны как среднее за карту.";
  const roles = evidence.roles.map(role => `<article class="fantasy-title-evidence-role">
    <header><span><strong>${escapeHTML(role.label)}</strong><small>${escapeHTML(role.teamName || "Команда")}</small></span><b>${formatSignedFantasyPoints(role.points)}</b></header>
    <div>${role.members.map(member => `<p><span><strong>${escapeHTML(member.name)}</strong><small>позиция ${member.position || "?"}</small></span><span><b>${member.hits} из ${member.known || member.total}</b><small>${formatSignedFantasyPoints(member.points)}</small></span></p>`).join("")}</div>
  </article>`).join("");
  const heroes = evidence.kind === "prefix" && evidence.heroes.length
    ? `<section class="fantasy-title-evidence-heroes"><h4>Герои, которые активировали префикс</h4><div>${evidence.heroes.map(hero => `<span><strong>${escapeHTML(hero.name)}</strong><b>${hero.count}</b></span>`).join("")}</div></section>`
    : "";
  const matches = evidence.matches.map((match, index) => `<button type="button" data-fantasy-evidence-match="${match.matchId}" ${index >= 5 ? "hidden" : ""}>
    <span><strong>#${match.matchId} · ${[...match.roles].map(escapeHTML).join(", ")}</strong><small>${escapeHTML(match.leagueName)} · ${formatDate(match.startTime)}</small></span>
    <span><small>${escapeHTML([...match.players].join(", "))}${match.heroes.size ? ` · ${escapeHTML([...match.heroes].join(", "))}` : ""}</small><b>${formatSignedFantasyPoints(match.points)}</b></span>
  </button>`).join("");
  return `<aside class="fantasy-title-evidence" role="dialog" aria-label="Расчёт: ${escapeAttribute(item.label)}">
    <header><span><small>${escapeHTML(evidence.metricLabel)}</small><strong>${evidence.hits} из ${checked} · ${rate}%</strong></span><b>${available ? "Точный расчёт" : "Данных недостаточно"}</b></header>
    <p class="fantasy-title-evidence-note">${escapeHTML(modeNote)}</p>
    <section class="fantasy-title-evidence-roles">${roles}</section>
    ${heroes}
    <section class="fantasy-title-evidence-matches"><h4>Карты, на которых условие сработало</h4>${matches || `<p>Подходящих карт в выбранных турнирах нет.</p>`}${evidence.matches.length > 5 ? `<button type="button" data-fantasy-evidence-expand>Показать все ${evidence.matches.length}</button>` : ""}</section>
  </aside>`;
}

function refreshFantasyTitleRecommendations(form, title) {
  ["prefix", "suffix"].forEach(kind => {
    const options = form.querySelector(`[data-fantasy-title-options="${kind}"]`);
    if (options) options.innerHTML = renderFantasyTitleOptions(kind, title);
  });
}

function openFantasyHistory(roleKey, mode) {
  const projection = fantasyRoleProjection(roleKey);
  fantasyBuilderActiveDialog = { type: "history", roleKey, projection };
  const isSeries = mode === "series";
  const rows = isSeries ? projection.series : projection.maps;
  setFantasyDialog(isSeries ? "Исторические серии" : "Исторические карты", projection.candidate.teamName, `
    <div class="fantasy-history-list">${rows.map((record, index) => {
      const match = record.match;
      return `<button type="button" ${isSeries ? `data-fantasy-open-series="${index}"` : `data-fantasy-open-map="${match.matchId}"`}>
        <span><strong>${escapeHTML(match.opponentName || "Соперник")}</strong><small>${escapeHTML(match.leagueName || "Турнир")} · ${formatDate(match.startTime)}</small></span>
        ${isSeries ? `<i>${record.countedMaps.length} лучшие карты из ${record.maps.length}</i>` : `<i>${heroName(record.members?.[0]?.match.heroId) ? escapeHTML(heroName(record.members[0].match.heroId)) : `#${match.matchId}`}</i>`}
        <b>${formatPoints(record.total)}</b>
      </button>`;
    }).join("") || `<div class="empty-table">Нет матчей в выбранных турнирах</div>`}</div>`, "fantasy-history-dialog");
}

function renderPlayerEmblemRecommendations(player, matches) {
  if (!PUBLIC_MODE || !Array.isArray(matches) || !matches.length) return "";
  const roleKey = Number(player.position) === 2 ? "mid" : [1, 3].includes(Number(player.position)) ? "cores" : "supps";
  const colors = fantasyRoleRules[roleKey].colors;
  const metricAverages = new Map();
  matches.forEach(match => (match.metrics || []).forEach(metric => {
    const item = metricAverages.get(metric.key) || { total: 0, count: 0 };
    item.total += Number(metric.averagePoints || 0); item.count += 1; metricAverages.set(metric.key, item);
  }));
  const used = new Set();
  const recommendations = colors.map(color => {
    const choices = fantasyMetricsForColor(color).map(metric => ({
      ...metric, points: (metricAverages.get(metric.key)?.total || 0) / Math.max(1, metricAverages.get(metric.key)?.count || 0),
    })).sort((left, right) => right.points - left.points);
    const choice = choices.find(item => !used.has(item.key)) || choices[0];
    used.add(choice.key);
    return choice;
  });
  const prefix = Object.entries(fantasyTitlePrefixes).filter(([key]) => key !== "none").map(([key, item]) => {
    const active = matches.filter(match => fantasyPrefixApplies(key, match.heroId)).length;
    return { key, item, rate: active / matches.length, expected: active / matches.length * item.bonus };
  }).sort((left, right) => right.expected - left.expected)[0];
  return `<section class="profile-emblem-recommendations">
    <div><p class="eyebrow">Подбор под игрока</p><h2>Рекомендуемые эмблемы</h2></div>
    <div class="profile-emblem-grid">${recommendations.map(item => `<article class="${item.color}"><span>${fantasyMetricIcon(item.key)}</span><div><strong>${escapeHTML(item.label)}</strong><small>${fantasyColorLabel(item.color)} · ${formatPoints(item.points)} базовых очков</small></div></article>`).join("")}</div>
    ${prefix ? `<p class="profile-title-advice"><span>Лучший префикс в выбранных турнирах</span><strong>${escapeHTML(prefix.item.label)}</strong><small>срабатывал в ${formatNumber(prefix.rate * 100)}% карт</small></p>` : ""}
    <a href="#my-team">Открыть калькулятор состава</a>
  </section>`;
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    fantasyEmblemModifiers,
    calculateFantasyMemberMap,
    fantasySeriesCalculations,
    fantasyAverageProjection,
    fantasyTitlePrefixes,
    fantasyTitleSuffixes,
    fantasyTitleRecommendations,
  };
}
