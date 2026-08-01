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

function normalizeFantasyHeroName(value) {
  return String(value || "").toLowerCase().replace(/[^a-z0-9]+/g, " ").trim();
}

function fantasyHeroSet(values) {
  return new Set(values.split("|").map(normalizeFantasyHeroName));
}

const fantasyTitlePrefixes = {
  none: { label: "Без префикса", bonus: 0, description: "Префикс не выбран.", heroes: new Set() },
  crimson: {
    label: "Багровый", bonus: .06, description: "+6% при игре за красного героя.",
    heroes: fantasyHeroSet("Axe|Beastmaster|Bloodseeker|Brewmaster|Centaur Warrunner|Clockwerk|Dawnbreaker|Disruptor|Doom|Dragon Knight|Ember Spirit|Grimstroke|Huskar|Legion Commander|Lina|Lion|Lycan|Mars|Monkey King|Pangolier|Primal Beast|Queen of Pain|Shadow Fiend|Snapfire|Sven|Timbersaw|Troll Warlord|Underlord|Warlock|Windranger|Wraith King"),
  },
  cerulean: {
    label: "Лазурный", bonus: .11, description: "+11% при игре за синего героя.",
    heroes: fantasyHeroSet("Abaddon|Ancient Apparition|Anti-Mage|Arc Warden|Bane|Crystal Maiden|Drow Ranger|Enigma|Faceless Void|Jakiro|Keeper of the Light|Kunkka|Lich|Luna|Mirana|Morphling|Muerta|Naga Siren|Oracle|Outworld Destroyer|Phantom Assassin|Puck|Razor|Riki|Slardar|Spirit Breaker|Storm Spirit|Templar Assassin|Tiny|Tusk|Vengeful Spirit|Visage|Winter Wyvern|Zeus"),
  },
  emerald: {
    label: "Изумрудный", bonus: .06, description: "+6% при игре за зелёного героя.",
    heroes: fantasyHeroSet("Bristleback|Broodmother|Chen|Dark Seer|Death Prophet|Earth Spirit|Enchantress|Hoodwink|Medusa|Meepo|Nature's Prophet|Necrophos|Nyx Assassin|Pugna|Rubick|Sand King|Shadow Shaman|Treant Protector|Tidehunter|Undying|Venomancer|Viper|Weaver|Windranger"),
  },
  royal: {
    label: "Королевский", bonus: .10, description: "+10% при игре за фиолетового героя.",
    heroes: fantasyHeroSet("Bane|Dark Seer|Dazzle|Enigma|Faceless Void|Invoker|Leshrac|Lion|Magnus|Night Stalker|Oracle|Outworld Destroyer|Puck|Queen of Pain|Riki|Shadow Demon|Spectre|Templar Assassin|Void Spirit|Witch Doctor"),
  },
  golden: {
    label: "Золотой", bonus: .08, description: "+8% при игре за жёлтого или коричневого героя.",
    heroes: fantasyHeroSet("Bounty Hunter|Brewmaster|Bristleback|Broodmother|Chen|Clinkz|Dawnbreaker|Earthshaker|Elder Titan|Hoodwink|Keeper of the Light|Legion Commander|Lone Druid|Monkey King|Nyx Assassin|Omniknight|Pangolier|Sand King|Shadow Shaman|Snapfire|Templar Assassin|Venomancer"),
  },
  elemental: {
    label: "Элементальный", bonus: .08, description: "+8% при игре за водного, огненного или ледяного героя.",
    heroes: fantasyHeroSet("Ancient Apparition|Batrider|Crystal Maiden|Ember Spirit|Huskar|Jakiro|Kunkka|Lina|Luna|Morphling|Naga Siren|Phoenix|Slardar|Slark|Storm Spirit|Tidehunter|Tusk|Winter Wyvern"),
  },
  otherworldly: {
    label: "Потусторонний", bonus: .07, description: "+7% при игре за нежить, демона или духа.",
    heroes: fantasyHeroSet("Abaddon|Death Prophet|Doom|Earth Spirit|Ember Spirit|Grimstroke|Lich|Muerta|Necrophos|Night Stalker|Pugna|Queen of Pain|Shadow Demon|Shadow Fiend|Spectre|Spirit Breaker|Storm Spirit|Undying|Vengeful Spirit|Visage|Void Spirit|Wraith King"),
  },
  heroic: {
    label: "Героический", bonus: .09, description: "+9% при игре за героя в маске или плаще.",
    heroes: fantasyHeroSet("Anti-Mage|Bounty Hunter|Clinkz|Drow Ranger|Grimstroke|Juggernaut|Luna|Muerta|Oracle|Phantom Assassin|Riki|Rubick|Shadow Demon|Silencer|Sven|Templar Assassin|Terrorblade|Void Spirit"),
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
  const name = normalizeFantasyHeroName(heroName(heroId));
  return Boolean(name && fantasyTitlePrefixes[prefixKey]?.heroes.has(name));
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
  const prefixKnown = title.prefix === "none" || Boolean(heroName(match.heroId));
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

function fantasyRoleMapCalculations(candidate, slot) {
  if (!candidate) return [];
  const memberMatches = candidate.members.map(member => {
    const detail = fantasyDetailForPlayer(member);
    return new Map(annotateFantasySeries(fantasyBuilderSelectedMatches(detail)).map(match => [Number(match.matchId), match]));
  });
  const commonIds = [...(memberMatches[0]?.keys() || [])].filter(id => memberMatches.every(matches => matches.has(id)));
  return commonIds.map(matchId => {
    const matches = memberMatches.map(items => items.get(matchId));
    const calculations = matches.map(match => calculateFantasyMemberMap(match, slot.emblems, fantasyBuilderState.title));
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

function fantasyRoleProjection(roleKey, candidate = fantasyCandidateBySlot(roleKey)) {
  const slot = fantasyBuilderState.slots[roleKey];
  const maps = fantasyRoleMapCalculations(candidate, slot);
  const series = fantasySeriesCalculations(maps);
  return {
    candidate, maps, series,
    mapAverage: fantasyAverageProjection(maps),
    seriesAverage: fantasyAverageProjection(series),
  };
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
  const logo = candidate?.teamSlug === "1w"
    ? "1w-logo.png"
    : fantasyAssets.teamEmblem(candidate?.teamSlug, candidate?.teamLogoUrl);
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
    if (event.target === dialog || event.target.closest("[data-close-fantasy-dialog]")) dialog.close();
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
      const logo = candidate.teamSlug === "1w"
        ? "1w-logo.png"
        : fantasyAssets.teamEmblem(candidate.teamSlug, candidate.teamLogoUrl);
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
      <fieldset><legend>Префикс</legend><div class="fantasy-title-options">${Object.entries(fantasyTitlePrefixes).map(([key, item]) => `<label><input type="radio" name="prefix" value="${key}" ${key === fantasyBuilderState.title.prefix ? "checked" : ""}><span><strong>${escapeHTML(item.label)}</strong><small>${escapeHTML(item.description)}</small></span></label>`).join("")}</div></fieldset>
      <fieldset><legend>Суффикс</legend><div class="fantasy-title-options">${Object.entries(fantasyTitleSuffixes).map(([key, item]) => `<label class="${item.support === "missing" ? "data-missing" : ""}"><input type="radio" name="suffix" value="${key}" ${key === fantasyBuilderState.title.suffix ? "checked" : ""}><span><strong>${escapeHTML(item.label)}</strong><small>${escapeHTML(item.description)}</small>${item.support === "missing" ? `<em>${escapeHTML(item.reason)}</em>` : ""}</span></label>`).join("")}</div></fieldset>
      <button type="submit" class="fantasy-dialog-primary">Применить титул</button>
    </form>`, "fantasy-title-dialog");
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
  };
}
