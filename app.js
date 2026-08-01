const PUBLIC_MODE = window.DOTA_HUB_MODE === "public";
const isLocalPage = ["localhost", "127.0.0.1"].includes(location.hostname);
const API_BASE = PUBLIC_MODE ? "" : (isLocalPage ? "" : (localStorage.getItem("dotaHubApi") || "http://127.0.0.1:8787"));
const DEFAULT_ROUTE = PUBLIC_MODE ? "my-team" : "matches";

const elements = {
  connection: document.querySelector("#connectionBadge"),
  brand: document.querySelector(".brand"),
  navLinks: [...document.querySelectorAll(".main-nav a")],
  views: {
    matches: document.querySelector("#matchesView"),
    ti: document.querySelector("#tiView"),
    live: document.querySelector("#liveView"),
    builder: document.querySelector("#fantasyBuilderView"),
    team: document.querySelector("#teamView"),
    player: document.querySelector("#playerView"),
  },
  tiTeamsSection: document.querySelector("#tiTeamsSection"),
  tiPlayersSection: document.querySelector("#tiPlayersSection"),
  tiSectionSwitcher: document.querySelector("#tiSectionSwitcher"),
  form: document.querySelector("#parseForm"),
  matchId: document.querySelector("#matchId"),
  parseButton: document.querySelector("#parseButton"),
  replayDropzone: document.querySelector("#replayDropzone"),
  replayFile: document.querySelector("#replayFile"),
  jobPanel: document.querySelector("#jobPanel"),
  jobState: document.querySelector("#jobState"),
  jobMessage: document.querySelector("#jobMessage"),
  jobProgress: document.querySelector("#jobProgress"),
  jobCounter: document.querySelector("#jobCounter"),
  progressBar: document.querySelector("#progressBar"),
  jobError: document.querySelector("#jobError"),
  parseQueuePanel: document.querySelector("#parseQueuePanel"),
  gcSummary: document.querySelector("#gcSummary"),
  queueSummary: document.querySelector("#queueSummary"),
  replaySummary: document.querySelector("#replaySummary"),
  lastSuccessSummary: document.querySelector("#lastSuccessSummary"),
  matchesGrid: document.querySelector("#matchesGrid"),
  emptyState: document.querySelector("#emptyState"),
  refreshButton: document.querySelector("#refreshButton"),
  teamsGrid: document.querySelector("#teamsGrid"),
  refreshTeamsButton: document.querySelector("#refreshTeamsButton"),
  syncAllTeamsButton: document.querySelector("#syncAllTeamsButton"),
  allTeamsJobPanel: document.querySelector("#allTeamsJobPanel"),
  backToTeams: document.querySelector("#backToTeams"),
  teamPage: document.querySelector("#teamPage"),
  playersTable: document.querySelector("#playersTable"),
  globalSelectionFilters: document.querySelector("#globalSelectionFilters"),
  playerSearch: document.querySelector("#playerSearch"),
  playerRole: document.querySelector("#playerRole"),
  playersTableActions: document.querySelector("#playersTableActions"),
  playersTableToggle: document.querySelector("#playersTableToggle"),
  playerPage: document.querySelector("#playerPage"),
  backFromPlayer: document.querySelector("#backFromPlayer"),
  cardTemplate: document.querySelector("#matchCardTemplate"),
  dialog: document.querySelector("#matchDialog"),
  backToSeries: document.querySelector("#backToSeries"),
  closeDialog: document.querySelector("#closeDialog"),
  dialogEyebrow: document.querySelector("#dialogEyebrow"),
  dialogTitle: document.querySelector("#dialogTitle"),
  dialogMeta: document.querySelector("#dialogMeta"),
  dialogContent: document.querySelector("#dialogContent"),
  playerDialog: document.querySelector("#playerDialog"),
  closePlayerDialog: document.querySelector("#closePlayerDialog"),
  playerDialogTitle: document.querySelector("#playerDialogTitle"),
  playerDialogMeta: document.querySelector("#playerDialogMeta"),
  playerDialogContent: document.querySelector("#playerDialogContent"),
  seriesDialog: document.querySelector("#seriesDialog"),
  closeSeriesDialog: document.querySelector("#closeSeriesDialog"),
  seriesDialogEyebrow: document.querySelector("#seriesDialogEyebrow"),
  seriesDialogTitle: document.querySelector("#seriesDialogTitle"),
  seriesDialogMeta: document.querySelector("#seriesDialogMeta"),
  seriesDialogContent: document.querySelector("#seriesDialogContent"),
};

function applyRuntimeMode() {
  if (!PUBLIC_MODE) return;
  document.body.classList.add("public-mode");
  document.title = "Fantasy Time · TI 2026";
  if (elements.brand) {
    elements.brand.href = "#teams";
    const title = elements.brand.querySelector("strong");
    const subtitle = elements.brand.querySelector("small");
    if (title) title.textContent = "Fantasy Time";
    if (subtitle) subtitle.textContent = "TI 2026 analytics";
  }
}

const stateLabels = {
  queued: "В очереди",
  metadata: "Данные матча",
  downloading: "Загрузка",
  decompressing: "Распаковка",
  parsing: "Парсинг",
  saving: "База данных",
  profiles: "Профили игроков",
  team_history: "История команды",
  roster_check: "Проверка состава",
  team_parsing: "Матчи команды",
  done: "Готово",
  error: "Ошибка",
};

let currentTeam = null;
let pendingSelection = new Map();
const watchedJobs = new Set();
let heroNames = {};
let heroImages = {};
let tournamentPlayers = [];
let publicPlayerFilterData = {};
let publicPlayerFilterDataPromise = null;
let publicTournamentFilter = { initialized: false, leagueNames: new Set(), limit: 20, overview: null };
let globalSelectionOverview = null;
let playerSort = { key: null, direction: null };
let currentPlayerDetail = null;
let playerMatchScope = "all";
let currentSeriesContext = null;
const knownSeriesByMatch = new Map();
let pendingTeamLeagues = new Set();
let pendingTeamLimit = 1;
let currentBaseRoute = null;
let activeOverlayKey = "";
let activeTISection = "teams";
let tiNavigationTarget = null;
let tiNavigationTimer = 0;
let tiScrollFrame = 0;
let teamsLoaded = false;
let teamsLoadPromise = null;
let tournamentPlayersLoaded = false;
let tournamentPlayersAutoLoadAttempted = false;
let tournamentPlayersLoadPromise = null;
let playersTableExpanded = false;

const PLAYERS_PREVIEW_LIMIT = 20;
const PUBLIC_TOURNAMENT_FILTER_KEY = "fantasyTimeTournamentFilter:v2";

const SCROLL_STORAGE_PREFIX = "dotaHubScroll:";
const PLAYER_VIEW_STORAGE_KEY = "dotaHubPlayerView:v1";
if ("scrollRestoration" in history) history.scrollRestoration = "manual";

function restorePlayerViewPreference() {
  try {
    const saved = JSON.parse(localStorage.getItem(PLAYER_VIEW_STORAGE_KEY) || "null");
    if (!saved || typeof saved !== "object") return;
    elements.playerSearch.value = typeof saved.search === "string" ? saved.search : "";
    const role = String(saved.role || "0");
    if ([...elements.playerRole.options].some(option => option.value === role)) {
      elements.playerRole.value = role;
    }
    if (saved.sort && typeof saved.sort.key === "string" && ["asc", "desc"].includes(saved.sort.direction)) {
      playerSort = { key: saved.sort.key, direction: saved.sort.direction };
    }
  } catch {
    localStorage.removeItem(PLAYER_VIEW_STORAGE_KEY);
  }
}

function savePlayerViewPreference() {
  localStorage.setItem(PLAYER_VIEW_STORAGE_KEY, JSON.stringify({
    search: elements.playerSearch.value || "",
    role: elements.playerRole.value || "0",
    sort: playerSort,
  }));
}

async function api(path, options = {}) {
  const method = String(options.method || "GET").toUpperCase();
  if (PUBLIC_MODE && !["GET", "HEAD"].includes(method)) {
    throw new Error("Публичная версия доступна только для просмотра");
  }
  const staticRead = PUBLIC_MODE && window.DOTA_HUB_STATIC_API === true && ["GET", "HEAD"].includes(method);
  const headers = { ...(options.headers || {}) };
  if (options.body !== undefined && !headers["Content-Type"]) headers["Content-Type"] = "application/json";
  const requestOptions = {
    ...options,
    credentials: PUBLIC_MODE ? "omit" : "include",
    headers,
  };
  if (staticRead && requestOptions.cache === undefined) requestOptions.cache = "no-store";

  const retryDelays = staticRead ? [0, 400, 1200] : [0];
  let response;
  for (let attempt = 0; attempt < retryDelays.length; attempt += 1) {
    if (retryDelays[attempt]) {
      await new Promise(resolve => setTimeout(resolve, retryDelays[attempt]));
    }
    response = await fetch(await resolveAPIURL(path, attempt), requestOptions);
    if (response.status !== 404 || attempt === retryDelays.length - 1) break;
  }
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const message = staticRead && response.status === 404
      ? "Данные сайта обновляются. Перезагрузите страницу через несколько секунд."
      : (body.error || `HTTP ${response.status}`);
    throw new Error(message);
  }
  return body;
}

async function staticAPISegmentKey(value) {
  const bytes = new TextEncoder().encode(value);
  const digest = await window.crypto.subtle.digest("SHA-256", bytes);
  return [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, "0")).join("");
}

async function resolveAPIURL(path, retryAttempt = 0) {
  if (!PUBLIC_MODE || window.DOTA_HUB_STATIC_API !== true) {
    return `${API_BASE}${path}`;
  }
  const [pathname, ...queryParts] = String(path || "").split("?");
  let relativePath = pathname.replace(/^\/+/, "");
  const detailRoute = relativePath.match(/^api\/(teams|tournament-players)\/([^/]+)$/);
  if (detailRoute) {
    let segment = detailRoute[2];
    try {
      segment = decodeURIComponent(segment);
    } catch {
      // Keep the original segment so malformed input remains isolated to its own key.
    }
    relativePath = `api/${detailRoute[1]}/${await staticAPISegmentKey(segment)}`;
  }
  const params = new URLSearchParams(queryParts.join("?"));
  const release = String(window.DOTA_HUB_STATIC_RELEASE || "").trim();
  if (release) params.set("release", release);
  if (retryAttempt > 0) params.set("retry", `${retryAttempt}-${Date.now()}`);
  const query = params.toString();
  return `/${relativePath}.json${query ? `?${query}` : ""}`;
}

function applyDesignMode(mode) {
  const enabled = mode === "dashboard";
  document.body.classList.toggle("design-lab", enabled);
  if (elements.themeToggle) {
    elements.themeToggle.setAttribute("aria-pressed", enabled ? "true" : "false");
    elements.themeToggle.innerHTML = enabled
      ? `<span>Тестовый дизайн</span><strong>TI Dashboard включён</strong>`
      : `<span>Тестовый дизайн</span><strong>Классика</strong>`;
  }
}

function toggleDesignMode() {
  const next = document.body.classList.contains("design-lab") ? "classic" : "dashboard";
  localStorage.setItem(THEME_STORAGE_KEY, next);
  applyDesignMode(next);
}

async function checkConnection() {
  try {
    const health = await api("/api/health");
    elements.connection.className = "connection online";
    if (PUBLIC_MODE) {
      const generatedAt = health?.generatedAt || health?.details?.snapshotGeneratedAt;
      elements.connection.innerHTML = `<span></span> Данные ${relativeTime(generatedAt).toLowerCase()}`;
      return;
    }
    const gc = health?.details?.gcMonitoring || null;
    const gcOnline = Boolean(gc?.enabled) && !["offline", "error"].includes(String(gc?.state || "").toLowerCase());
    elements.connection.innerHTML = `<span></span> ${gcOnline ? "GC активен" : "Сервер подключён"}`;
    if (elements.gcSummary) {
      elements.gcSummary.textContent = gcOnline
        ? `Активен · каждые ${Math.max(1, Math.round(Number(gc.cycleIntervalSeconds || 180) / 60))} мин`
        : "Мониторинг выключен";
      elements.gcSummary.dataset.status = gcOnline ? "healthy" : "warning";
    }
    if (elements.replaySummary) {
      const waiting = Number(gc?.lastCycleWaitingReplay || 0);
      elements.replaySummary.textContent = waiting ? `${waiting} ожидают Valve` : "Нет ожидания";
      elements.replaySummary.dataset.status = waiting ? "warning" : "neutral";
    }
    if (elements.lastSuccessSummary) {
      elements.lastSuccessSummary.textContent = relativeTime(gc?.lastSuccessfulCycleAt || health?.lastSuccessfulOperationAt);
    }
    elements.parseButton.disabled = false;
  } catch {
    elements.connection.className = "connection offline";
    elements.connection.innerHTML = "<span></span> Сервер выключен";
    if (elements.gcSummary) {
      elements.gcSummary.textContent = "Нет соединения";
      elements.gcSummary.dataset.status = "error";
    }
    elements.parseButton.disabled = true;
  }
}

async function loadHeroNames() {
  try {
    const heroes = await api("/api/heroes");
    heroNames = {};
    heroImages = {};
    for (const [id, hero] of Object.entries(heroes || {})) {
      heroNames[id] = typeof hero === "string" ? hero : hero.name;
      heroImages[id] = typeof hero === "object" ? hero.imageUrl : "";
    }
  } catch {
    heroNames = {};
    heroImages = {};
  }
}

function parseRouteLocation(hashValue = location.hash) {
  const raw = String(hashValue || "").replace(/^#/, "") || DEFAULT_ROUTE;
  const queryIndex = raw.indexOf("?");
  const basePath = queryIndex >= 0 ? raw.slice(0, queryIndex) : raw;
  const query = queryIndex >= 0 ? raw.slice(queryIndex + 1) : "";
  return { basePath: basePath || DEFAULT_ROUTE, params: new URLSearchParams(query) };
}

function isPublicRoute(basePath) {
  return basePath === "teams" || basePath === "players" || basePath === "my-team"
    || basePath === "live" || basePath.startsWith("live/")
    || basePath.startsWith("team/") || basePath.startsWith("player/");
}

function route() {
  const routeState = parseRouteLocation();
  let hash = routeState.basePath;
  if (PUBLIC_MODE && !location.hash) {
    history.replaceState({ dotaHubEntry: true, baseRoute: DEFAULT_ROUTE, scrollY: 0 }, "", `#${DEFAULT_ROUTE}`);
  }
  if (PUBLIC_MODE && !isPublicRoute(hash)) {
    hash = DEFAULT_ROUTE;
    routeState.basePath = hash;
    history.replaceState({ dotaHubEntry: true, baseRoute: hash, scrollY: 0 }, "", `#${DEFAULT_ROUTE}`);
  }
  const previousBase = currentBaseRoute;
  currentBaseRoute = hash;
  let loading;

  if (hash.startsWith("player/")) {
    const [, encodedAlias, tab = "overview"] = hash.split("/");
    showView("player");
    loading = loadPlayer(decodeURIComponent(encodedAlias || ""), tab);
  } else if (hash.startsWith("team/")) {
    const slug = decodeURIComponent(hash.slice(5));
    showView("team");
    loading = loadTeam(slug);
  } else if (hash === "teams" || hash === "players") {
    showView("ti");
    loading = loadTIPage(hash);
  } else if (PUBLIC_MODE && hash === "my-team") {
    showView("builder");
    loading = typeof loadFantasyBuilder === "function" ? loadFantasyBuilder() : Promise.resolve();
  } else if (PUBLIC_MODE && (hash === "live" || hash.startsWith("live/"))) {
    showView("live");
  } else {
    currentBaseRoute = "matches";
    showView("matches");
    loading = loadMatches();
  }

  syncOverlayFromRoute(routeState);
  if (previousBase === null || previousBase !== currentBaseRoute) {
    Promise.resolve(loading).finally(() => restoreScrollPosition(currentBaseRoute));
  }
}

function showView(name) {
  for (const [viewName, node] of Object.entries(elements.views)) {
    if (!node) continue;
    node.classList.toggle("hidden", viewName !== name);
  }
  const tiVisible = name === "ti";
  document.body.classList.toggle("ti-page", tiVisible);
  elements.tiSectionSwitcher.classList.toggle("hidden", !tiVisible);
  elements.tiSectionSwitcher.setAttribute("aria-hidden", tiVisible ? "false" : "true");
  elements.navLinks.forEach(link => {
    const navName = ["ti", "team", "player"].includes(name) ? "teams" : name === "builder" ? "my-team" : name;
    const active = link.dataset.route === navName;
    link.classList.toggle("active", active);
    if (active) link.setAttribute("aria-current", "page");
    else link.removeAttribute("aria-current");
  });
}

function scrollStorageKey(baseRoute) {
  return `${SCROLL_STORAGE_PREFIX}${baseRoute || "matches"}`;
}

function saveScrollPosition(baseRoute = currentBaseRoute) {
  if (!baseRoute) return;
  const scrollY = Math.max(0, Math.round(window.scrollY));
  sessionStorage.setItem(scrollStorageKey(baseRoute), String(scrollY));
  if (parseRouteLocation().basePath === baseRoute) {
    history.replaceState({ ...(history.state || {}), baseRoute, scrollY }, "", location.href);
  }
}

function restoreScrollPosition(baseRoute) {
  const stateValue = history.state?.baseRoute === baseRoute && typeof history.state.scrollY === "number"
    ? history.state.scrollY
    : NaN;
  const storedRaw = sessionStorage.getItem(scrollStorageKey(baseRoute));
  const storedValue = storedRaw === null ? NaN : Number(storedRaw);
  const fallback = baseRoute === "players" ? tiSectionScrollTop(elements.tiPlayersSection) : 0;
  const target = Number.isFinite(stateValue) ? stateValue : (Number.isFinite(storedValue) ? storedValue : fallback);
  if (["teams", "players"].includes(baseRoute)) tiNavigationTarget = baseRoute;
  requestAnimationFrame(() => requestAnimationFrame(() => {
    window.scrollTo({ top: target, behavior: "instant" });
    requestAnimationFrame(() => {
      if (tiNavigationTarget === baseRoute) tiNavigationTarget = null;
      updateTISectionFromScroll();
    });
  }));
}

function navigateToBase(baseRoute, options = {}) {
  if (PUBLIC_MODE && !isPublicRoute(baseRoute)) baseRoute = "teams";
  saveScrollPosition();
  const method = options.replace ? "replaceState" : "pushState";
  const storedScroll = sessionStorage.getItem(scrollStorageKey(baseRoute));
  history[method]({
    dotaHubEntry: true,
    fromBaseRoute: currentBaseRoute,
    baseRoute,
    scrollY: storedScroll === null ? null : Number(storedScroll),
  }, "", `#${baseRoute}`);
  route();
}

function loadTIPage(section) {
  tiNavigationTarget = section;
  setActiveTISection(section, { syncUrl: false });
  const teamsLoading = loadTeams();
  const playersLoading = section === "players" ? loadTournamentPlayers() : Promise.resolve();
  return Promise.all([teamsLoading, playersLoading]).finally(() => {
    requestAnimationFrame(updateTISectionFromScroll);
  });
}

function tiSectionScrollTop(section) {
  if (!section) return 0;
  const topbar = document.querySelector(".topbar");
  const offset = (topbar?.offsetHeight || 0) + 20;
  return Math.max(0, Math.round(window.scrollY + section.getBoundingClientRect().top - offset));
}

function setActiveTISection(section, options = {}) {
  const next = section === "players" ? "players" : "teams";
  activeTISection = next;
  elements.tiTeamsSection.classList.toggle("is-current", next === "teams");
  elements.tiPlayersSection.classList.toggle("is-current", next === "players");

  const target = next === "teams" ? "players" : "teams";
  elements.tiSectionSwitcher.dataset.target = target;
  elements.tiSectionSwitcher.classList.toggle("points-up", target === "teams");
  elements.tiSectionSwitcher.querySelector(".ti-switcher-label").textContent = target === "teams" ? "Команды TI" : "Игроки TI";
  elements.tiSectionSwitcher.querySelector(".ti-switcher-icon").textContent = target === "teams" ? "↑" : "↓";
  elements.tiSectionSwitcher.setAttribute("aria-label", target === "teams" ? "Перейти к командам TI" : "Перейти к игрокам TI");

  if (!options.syncUrl || elements.views.ti.classList.contains("hidden")) return;
  const routeState = parseRouteLocation();
  if (!["teams", "players"].includes(routeState.basePath) || routeState.params.has("overlay")) return;
  if (routeState.basePath === next) return;

  currentBaseRoute = next;
  const scrollY = Math.max(0, Math.round(window.scrollY));
  sessionStorage.setItem(scrollStorageKey(next), String(scrollY));
  history.replaceState({ ...(history.state || {}), baseRoute: next, scrollY }, "", `#${next}`);
}

function updateTISectionFromScroll() {
  if (elements.views.ti.classList.contains("hidden")) return;
  maybeLoadTIPlayers();
  const topbar = document.querySelector(".topbar");
  const activationLine = Math.max((topbar?.offsetHeight || 0) + 24, window.innerHeight * 0.38);
  const detected = elements.tiPlayersSection.getBoundingClientRect().top <= activationLine ? "players" : "teams";
  if (tiNavigationTarget && detected !== tiNavigationTarget) return;
  setActiveTISection(detected, { syncUrl: true });
}

function maybeLoadTIPlayers() {
  if (tournamentPlayersLoaded || tournamentPlayersLoadPromise || tournamentPlayersAutoLoadAttempted) return;
  const distance = elements.tiPlayersSection.getBoundingClientRect().top - window.innerHeight;
  if (distance <= 700) {
    tournamentPlayersAutoLoadAttempted = true;
    loadTournamentPlayers();
  }
}

function navigateToTISection(section) {
  const target = section === "players" ? "players" : "teams";
  saveScrollPosition(currentBaseRoute);
  currentBaseRoute = target;
  tiNavigationTarget = target;
  window.clearTimeout(tiNavigationTimer);
  history.pushState({
    dotaHubEntry: true,
    fromBaseRoute: activeTISection,
    baseRoute: target,
    scrollY: null,
  }, "", `#${target}`);
  setActiveTISection(target, { syncUrl: false });
  const sectionLoading = target === "players" ? loadTournamentPlayers() : Promise.resolve();
  const targetSection = target === "players" ? elements.tiPlayersSection : elements.tiTeamsSection;
  const targetTop = tiSectionScrollTop(targetSection);
  const startTop = window.scrollY;
  const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  requestAnimationFrame(() => {
    window.scrollTo({ top: targetTop, behavior: reducedMotion ? "auto" : "smooth" });
  });
  window.setTimeout(() => {
    const didNotMove = Math.abs(window.scrollY - startTop) < 2;
    if (didNotMove && Math.abs(targetTop - startTop) > 40) {
      window.scrollTo({ top: targetTop, behavior: "auto" });
    }
  }, 280);
  Promise.resolve(sectionLoading).finally(() => {
    if (tiNavigationTarget !== target || parseRouteLocation().basePath !== target) return;
    const correctedTop = tiSectionScrollTop(targetSection);
    if (Math.abs(window.scrollY - correctedTop) > 24) {
      window.scrollTo({ top: correctedTop, behavior: reducedMotion ? "auto" : "smooth" });
    }
  });
  tiNavigationTimer = window.setTimeout(() => {
    tiNavigationTarget = null;
    updateTISectionFromScroll();
  }, 1400);
}

function navigateBackOr(fallbackRoute) {
  if (history.state?.dotaHubEntry && history.length > 1) history.back();
  else navigateToBase(fallbackRoute, { replace: true });
}

function overlayHash(params) {
  const baseRoute = parseRouteLocation().basePath || currentBaseRoute || DEFAULT_ROUTE;
  return `#${baseRoute}?${params.toString()}`;
}

function navigateToOverlay(params, state = {}) {
  saveScrollPosition();
  history.pushState({
    ...(history.state || {}),
    dotaHubEntry: true,
    overlayOpened: true,
    baseRoute: currentBaseRoute || parseRouteLocation().basePath,
    scrollY: window.scrollY,
    ...state,
  }, "", overlayHash(params));
  route();
}

function closeOverlay() {
  const parsed = parseRouteLocation();
  if (!parsed.params.get("overlay")) {
    closeOverlayDialogs();
    return;
  }
  if (history.state?.overlayOpened && history.length > 1) {
    history.back();
    return;
  }
  history.replaceState({ dotaHubEntry: true, baseRoute: parsed.basePath, scrollY: window.scrollY }, "", `#${parsed.basePath}`);
  route();
}

function closeOverlayDialogs(except = "") {
  if (except !== "match" && elements.dialog.open) elements.dialog.close();
  if (except !== "player" && elements.playerDialog.open) elements.playerDialog.close();
  if (except !== "series" && elements.seriesDialog.open) elements.seriesDialog.close();
  if (!except) activeOverlayKey = "";
}

function seriesRouteParams(series) {
  const params = new URLSearchParams({
    overlay: "series",
    matches: (series.matchIds || []).join(","),
    label: series.label || "BO-серия",
    opponent: series.opponent || "Неизвестная команда",
    league: series.league || "Турнир не указан",
    date: String(Number(series.startTime || 0)),
    wins: String(Number(series.wins || 0)),
    losses: String(Number(series.losses || 0)),
    team: series.teamName || "Команда",
  });
  return params;
}

function seriesFromRoute(params) {
  return {
    matchIds: String(params.get("matches") || "").split(",").map(Number).filter(Boolean),
    label: params.get("label") || "BO-серия",
    opponent: params.get("opponent") || "Неизвестная команда",
    league: params.get("league") || "Турнир не указан",
    startTime: Number(params.get("date") || 0),
    wins: Number(params.get("wins") || 0),
    losses: Number(params.get("losses") || 0),
    teamName: params.get("team") || "Команда",
  };
}

function syncOverlayFromRoute(routeState) {
  const overlay = routeState.params.get("overlay") || "";
  if (overlay === "match") {
    const matchId = Number(routeState.params.get("match"));
    if (!matchId) return closeOverlayDialogs();
    const key = `match:${matchId}`;
    closeOverlayDialogs("match");
    if (activeOverlayKey !== key || !elements.dialog.open) {
      activeOverlayKey = key;
      showMatchDialog(matchId, { returnToSeries: Boolean(history.state?.returnToSeries) });
    }
    return;
  }
  if (overlay === "series") {
    const series = seriesFromRoute(routeState.params);
    if (!series.matchIds.length) return closeOverlayDialogs();
    const key = `series:${series.matchIds.join(",")}`;
    closeOverlayDialogs("series");
    if (activeOverlayKey !== key || !elements.seriesDialog.open) {
      activeOverlayKey = key;
      showSeriesDialog(series);
    }
    return;
  }
  closeOverlayDialogs();
}

function relativeTime(value) {
  const timestamp = Date.parse(value || "");
  if (!Number.isFinite(timestamp)) return "Пока нет данных";
  const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000));
  if (seconds < 60) return "Только что";
  if (seconds < 3600) return `${Math.floor(seconds / 60)} мин назад`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} ч назад`;
  return `${Math.floor(seconds / 86400)} дн назад`;
}

async function loadMatches() {
  try {
    const [matches, retries] = await Promise.all([
      api("/api/matches"),
      api("/api/matches/retries"),
    ]);
    renderMatches(matches, retries);
  } catch (error) {
    elements.matchesGrid.innerHTML = "";
    elements.emptyState.classList.remove("hidden");
    elements.emptyState.querySelector("h3").textContent = "Нет связи с приложением";
    elements.emptyState.querySelector("p").textContent = error.message;
  }
}

function renderMatches(matches, retries = []) {
  matches = Array.isArray(matches) ? matches : [];
  retries = Array.isArray(retries) ? retries : [];
  elements.matchesGrid.innerHTML = "";
  elements.emptyState.classList.toggle("hidden", matches.length + retries.length > 0);

  for (const match of matches) {
    const card = elements.cardTemplate.content.firstElementChild.cloneNode(true);
    card.querySelector(".match-id").textContent = `#${match.matchId}`;
    const winner = match.radiantWin ? "radiant" : "dire";
    const winnerPill = card.querySelector(".winner-pill");
    winnerPill.classList.add(winner);
    winnerPill.textContent = match.radiantWin ? "Radiant" : "Dire";
    card.querySelector(".radiant-score").textContent = match.radiantKills;
    card.querySelector(".dire-score").textContent = match.direKills;
    card.querySelector(".match-date").textContent = formatDate(match.startTime);
    card.querySelector(".match-duration").textContent = formatDuration(match.duration);
    card.addEventListener("click", () => openMatch(match.matchId));
    elements.matchesGrid.append(card);
  }
  for (const retry of retries) {
    elements.matchesGrid.append(renderRetryMatchCard(retry));
  }
}

function renderRetryMatchCard(retry) {
  const card = document.createElement("article");
  card.className = `match-card retry-match-card ${retry.status === "stopped" ? "stopped" : "waiting"}`;
  const nextText = retry.status === "stopped"
    ? "Автоповторы остановлены"
    : `Следующая попытка: ${formatDateTime(retry.nextAttemptAt)}`;
  const reason = retryErrorLabel(retry.error);
  const recovery = replayRecoveryAdvice(retry.error);
  const noteText = recovery ? recovery.retryNote : nextText;
  card.innerHTML = `
    <div class="match-card-top">
      <span class="match-id">#${escapeHTML(retry.matchId)}</span>
      <span class="winner-pill retry">${retry.status === "stopped" ? "Stopped" : "Retry"}</span>
    </div>
    <div class="retry-match-title">${escapeHTML(reason)}</div>
    <p class="retry-match-note" title="${escapeHTML(nextText)}">${escapeHTML(noteText)}</p>
    <div class="match-card-bottom">
      <span>${retry.attempts || 0} попыток</span>
      <span>${retry.source === "auto" ? "фон" : "вручную"}</span>
    </div>
    <div class="retry-match-actions">
      <button class="retry-match-button secondary" type="button" data-action="fill">Ввести ID</button>
      <button class="retry-match-button danger" type="button" data-action="stop" ${retry.status === "stopped" ? "disabled" : ""}>
        ${retry.status === "stopped" ? "Остановлено" : "Остановить попытки"}
      </button>
    </div>
  `;
  card.querySelector('[data-action="fill"]').addEventListener("click", () => {
    elements.matchId.value = retry.matchId;
    elements.matchId.focus();
    window.scrollTo({ top: 0, behavior: "smooth" });
  });
  const stopButton = card.querySelector('[data-action="stop"]');
  stopButton.addEventListener("click", () => stopRetryMatch(retry.matchId, stopButton));
  return card;
}

async function stopRetryMatch(matchId, button) {
  const previousText = button.textContent;
  button.disabled = true;
  button.textContent = "Останавливаю...";
  try {
    await api(`/api/matches/retries/${encodeURIComponent(matchId)}/stop`, {
      method: "POST",
      body: "{}",
    });
    await loadMatches();
  } catch (error) {
    button.disabled = false;
    button.textContent = previousText;
    showJobError(error.message);
  }
}

function retryErrorLabel(error) {
  const message = String(error || "").toLowerCase();
  if (message.includes("replay_salt")) return "Жду ссылку на replay";
  if (message.includes("steam web api") && message.includes("500")) return "Steam временно не отдал данные";
  if (message.includes("stratz") && message.includes("403")) return "STRATZ не пустил запрос";
  if (message.includes("opendota")) return "OpenDota не отдала replay";
  if (message.includes("valve")) return "Valve replay пока недоступен";
  return "Replay пока не удалось получить";
}

function replayRecoveryAdvice(error) {
  const message = String(error || "").toLowerCase();
  const markers = [
    "replay_salt",
    "replay salt",
    "доступный реплей",
    "срок его хранения",
    "не удалось скачать реплей valve",
    "не удалось скачать replay",
    "сервер реплеев",
    "steam web api",
    "valve replay",
    "replay пока недоступен",
    "пока нет replay",
  ];
  if (!markers.some(marker => message.includes(marker))) return null;
  return {
    title: "Быстрее: загрузить replay вручную",
    body: "Valve пока не отдала replay. Автоповтор продолжится, но ожидание может растянуться на дни. Если файл есть на компьютере, загрузи .dem или .dem.bz2.",
    retryNote: "Если replay есть на ПК, перетащи .dem/.dem.bz2. Иначе ожидание может занять дни.",
  };
}

async function loadTeams(force = false) {
  if (!force && teamsLoaded) return;
  if (teamsLoadPromise) return teamsLoadPromise;
  elements.teamsGrid.innerHTML = `<div class="loading-card">Загружаю участников TI 2026…</div>`;
  teamsLoadPromise = (async () => {
    try {
      const teams = await api("/api/teams");
      renderTeams(teams);
      if (!PUBLIC_MODE) restoreAllTeamsJob();
      teamsLoaded = true;
    } catch (error) {
      teamsLoaded = false;
      elements.teamsGrid.innerHTML = `<div class="job-error">${escapeHTML(error.message)}</div>`;
    }
  })();
  try {
    await teamsLoadPromise;
  } finally {
    teamsLoadPromise = null;
  }
}

function renderTeams(teams) {
  teams = Array.isArray(teams) ? teams : [];
  elements.teamsGrid.innerHTML = teams.map(team => {
    const disabled = team.status === "tbd";
    const matchCount = Math.max(0, Number(team.matchCount || 0));
    const parsedCount = Math.max(0, Number(team.parsedCount || 0));
    const progress = matchCount > 0 ? Math.min(100, Math.round(parsedCount / matchCount * 100)) : 0;
    const logoClass = `${wideTeamLogo(team.slug) ? "logo-wide" : "logo-square"} team-logo-${team.slug}`;
    const logoUrl = preferredTeamEmblem(team.slug, team.logoUrl, team.name);
    const logo = logoUrl
      ? `<img src="${escapeAttribute(logoUrl)}" alt="" loading="lazy" decoding="async">`
      : `<span class="team-logo-fallback ${team.slug === "1w" ? "onew-mark" : ""}">${disabled ? "?" : escapeHTML(team.slug === "1w" ? "1W" : team.tag.slice(0, 2))}</span>`;
    const statusLabel = team.status === "invited" ? "Invited" :
      team.status === "qualifier" ? "Qualifier" : "Qualifier";
    return `
      <button class="team-card ${disabled ? "tbd" : ""}" type="button"
              data-team="${escapeAttribute(team.slug)}" ${disabled ? "disabled" : ""}>
        <span class="team-identity">
          <span class="team-logo ${escapeAttribute(logoClass)} ${team.slug === "1w" ? "onew-logo" : ""}">${logo}</span>
          <span>
            <strong>${escapeHTML(team.name)}</strong>
            ${disabled ? "" : `<small>${team.parsedCount}/${team.matchCount} матчей в базе</small>`}
          </span>
        </span>
        <span class="team-card-side">
          <span class="qualification ${team.status}">${statusLabel}</span>
          <span class="chevron">›</span>
        </span>
        ${disabled ? "" : `<span class="team-card-progress" aria-hidden="true"><i style="width:${progress}%"></i></span>`}
      </button>
    `;
  }).join("");

  elements.teamsGrid.querySelectorAll("[data-team]").forEach(button => {
    button.addEventListener("click", () => {
      navigateToBase(`team/${encodeURIComponent(button.dataset.team)}`);
    });
  });
}

async function loadTeam(slug) {
  elements.teamPage.innerHTML = `<div class="loading-card">Загружаю страницу команды…</div>`;
  try {
    if (PUBLIC_MODE) {
      [currentTeam] = await Promise.all([
        api(`/api/teams/${encodeURIComponent(slug)}`),
        loadPublicPlayerFilterData(),
      ]);
    } else {
      currentTeam = await api(`/api/teams/${encodeURIComponent(slug)}`);
    }
    if (!PUBLIC_MODE) {
      pendingSelection = new Map((currentTeam.matches || []).map(match => [match.matchId, Boolean(match.included)]));
      initializeTeamFilter(currentTeam.matches || []);
    }
    renderTeamPage(currentTeam);
    if (!PUBLIC_MODE) restoreTeamJob(slug);
  } catch (error) {
    elements.teamPage.innerHTML = `<div class="job-error">${escapeHTML(error.message)}</div>`;
  }
}

function renderTeamPage(detail) {
  const team = detail?.team || {};
  const allMatches = Array.isArray(detail?.matches) ? detail.matches : [];
  const matches = PUBLIC_MODE ? publicFilteredMatches(allMatches) : allMatches;
  const sourceRoster = Array.isArray(team.roster) ? team.roster : [];
  const roster = PUBLIC_MODE
    ? sourceRoster.map(player => publicFilteredRosterPlayer(player, team.slug))
    : sourceRoster;
  const logoUrl = preferredTeamEmblem(team.slug, team.logoUrl, team.name);
  const logo = logoUrl
    ? `<img src="${escapeAttribute(logoUrl)}" alt="" loading="lazy" decoding="async">`
    : `<span class="team-logo-fallback large ${team.slug === "1w" ? "onew-mark" : ""}">${escapeHTML(team.slug === "1w" ? "1W" : (team.tag || "?").slice(0, 2))}</span>`;
  const palette = teamPalette(team.slug);
  const logoClass = `${wideTeamLogo(team.slug) ? "logo-wide" : "logo-square"} team-logo-${team.slug}`;
  const leagues = leagueSummaries(allMatches.filter(match => match.parseStatus === "done"));
  const leagueGroups = tournamentFilterGroups(leagues);
  const includedCount = PUBLIC_MODE
    ? matches.length
    : allMatches.filter(match => match.included && match.parseStatus === "done").length;
  const doneCount = allMatches.filter(match => match.parseStatus === "done").length;
  const statusLabel = team.status === "invited" ? "Invited" : "Qualifier";
  const syncControl = PUBLIC_MODE ? "" : `
      <button id="syncTeamButton" class="primary-button" type="button">
        <strong>Обновить матчи</strong><small>Найти и обработать новые</small>
      </button>`;
  const jobPanel = PUBLIC_MODE ? "" : `<section id="teamJobPanel" class="job-panel hidden"></section>`;
  const selectionPanel = PUBLIC_MODE ? renderPublicTournamentFilterPanel({
    matches: allMatches,
    scope: "team",
  }) : `
    <section class="selection-panel">
      <div>
        <p class="eyebrow">Выборка fantasy</p>
        <h2>Учитывается ${includedCount} матчей</h2>
      </div>
      <div class="selection-presets">
        <button type="button" data-selection-mode="none">Снять все</button>
      </div>
      <div class="league-filters">
        ${renderSelectAllLeagues("team", leagueGroups.length > 0 && leagueGroups.every(group => groupIsSelected(pendingTeamLeagues, group)))}
        ${leagueGroups.map(group => {
          const checked = groupIsSelected(pendingTeamLeagues, group);
          return `<label><input type="checkbox" data-league-group="${escapeAttribute(group.key)}"
            data-league-names="${encodeLeagueNames(group.names)}" ${checked ? "checked" : ""}>
            <span>${escapeHTML(group.name)}</span><small>${group.matchCount}</small></label>`;
        }).join("")}
      </div>
      ${renderMatchLimitControl("team", doneCount, pendingTeamLimit)}
      <button id="applySelectionButton" class="primary-button apply-selection hidden" type="button">
        Применить выбор
      </button>
    </section>`;

  elements.teamPage.innerHTML = `
    <section class="team-hero" style="--team-color:${palette.primary};--team-color-2:${palette.secondary};--team-logo:${logoUrl ? `url('${escapeAttribute(logoUrl)}')` : "none"}">
      <div class="team-hero-main">
        <span class="team-logo hero-logo ${escapeAttribute(logoClass)} ${team.slug === "1w" ? "onew-logo" : ""}">${logo}</span>
        <div class="team-hero-copy">
          <div class="team-hero-kicker">
            <p class="eyebrow">The International 2026</p>
            <span class="qualification ${team.status}">${statusLabel}</span>
          </div>
          <h1>${escapeHTML(team.name)}</h1>
          <p class="team-summary"><span><strong>${team.parsedCount}</strong> матчей в базе</span><i></i><span><strong>${includedCount}</strong> учитываются в fantasy</span></p>
        </div>
      </div>
      ${syncControl}
    </section>

    ${jobPanel}
    ${selectionPanel}

    <section class="team-section">
      <div class="section-heading">
        <div>
          <p class="eyebrow">Турнирная пятёрка</p>
          <h2>Состав команды</h2>
        </div>
      </div>
      <div class="roster-role-grid" style="--team-color:${palette.primary};--team-color-2:${palette.secondary}">
        ${buildTeamRosterRoles(roster).map(renderRosterRoleCard).join("")}
      </div>
    </section>

    <section class="team-section">
      <div class="section-heading">
        <div>
          <p class="eyebrow">Матчи и серии</p>
          <h2>История матчей</h2>
        </div>
        <span class="matches-count">${matches.length}</span>
      </div>
      ${renderTeamMatches(matches)}
    </section>
  `;

  if (PUBLIC_MODE) {
    bindPublicTournamentFilter(elements.teamPage);
  } else {
    document.querySelector("#syncTeamButton")?.addEventListener("click", () => syncTeam(team.slug));
    elements.teamPage.querySelectorAll("[data-selection-mode]").forEach(button => {
      button.addEventListener("click", () => stageSelectionMode(button.dataset.selectionMode));
    });
    bindTeamLimitControl();
    elements.teamPage.querySelector('[data-select-all-leagues="team"]')?.addEventListener("change", event => {
      const checked = event.currentTarget.checked;
      pendingTeamLeagues = checked ? new Set(leagues.map(league => league.name)) : new Set();
      stageTeamFilterSelection();
      syncSelectionControls();
      markSelectionDirty();
    });
    elements.teamPage.querySelectorAll("[data-league-group]").forEach(input => {
      input.addEventListener("change", () => stageLeagueGroupSelection(leagueNamesFromInput(input), input.checked));
    });
    elements.teamPage.querySelectorAll("[data-include-match]").forEach(input => {
      input.addEventListener("change", () => {
        pendingSelection.set(Number(input.dataset.includeMatch), input.checked);
        markSelectionDirty();
      });
    });
    document.querySelector("#applySelectionButton")?.addEventListener("click", applySelection);
  }
  bindImageFallbacks(elements.teamPage);
  elements.teamPage.querySelectorAll("[data-player]").forEach(button => {
    button.addEventListener("click", () => {
      const player = roster.find(item => Number(item.accountId) === Number(button.dataset.player));
      if (player) navigateToBase(`player/${encodeURIComponent(playerAlias(player.name))}/overview`);
    });
  });
  elements.teamPage.querySelectorAll("[data-match]").forEach(button => {
    button.addEventListener("click", () => openMatch(Number(button.dataset.match)));
  });
  bindSeriesOpeners(elements.teamPage, team.name);
}

function teamPalette(slug) {
  return {
    aurora: { primary: "#079d98", secondary: "#123f49" },
    boomboys: { primary: "#e12f43", secondary: "#481722" },
    falcons: { primary: "#16a56e", secondary: "#103c31" },
    liquid: { primary: "#315c91", secondary: "#142c49" },
    "1w": { primary: "#e9e7e1", secondary: "#393b40" },
    xtreme: { primary: "#c6c2b8", secondary: "#34363b" },
    yandex: { primary: "#e9e9e9", secondary: "#7d1720" },
    resilience: { primary: "#d64a25", secondary: "#461b14" },
    vici: { primary: "#b8b8b8", secondary: "#3b3b3b" },
    og: { primary: "#38aaa2", secondary: "#174744" },
    lgd: { primary: "#dd313d", secondary: "#4a1720" },
  }[slug] || { primary: "#b88945", secondary: "#34291d" };
}

function wideTeamLogo(slug) {
  return ["aurora", "boomboys", "falcons", "xtreme", "yandex", "huligani", "resilience", "gamerlegion", "lgd"].includes(slug);
}

function preferredTeamEmblem(teamSlug, fallback = "", teamName = "") {
  const assets = globalThis.FantasyAssets;
  if (!assets) return String(teamSlug || "").toLowerCase() === "1w"
    ? "assets/ti2026/teams/10150413-iron-wing.webp"
    : fallback;
  const byName = typeof assets.teamEmblemByName === "function"
    ? assets.teamEmblemByName(teamName, fallback)
    : fallback;
  return assets.teamEmblem(teamSlug, byName);
}

function renderPlayerCard(player) {
  const stats = player.stats || { matches: 0, totalPoints: 0, metrics: [] };
  const metrics = Array.isArray(stats.metrics) ? stats.metrics : [];
  const topMetrics = [...metrics]
    .sort((a, b) => b.averagePoints - a.averagePoints)
    .slice(0, 2);
  return `
    <button class="player-card" type="button" data-player="${player.accountId}">
      ${playerImage(player, "player-avatar")}
      <div class="player-card-body">
        <div class="player-card-head">
          <span class="position-badge">Поз. ${player.position}</span>
          <span>
          <strong>${escapeHTML(player.name)}</strong>
          ${player.personaName ? `<em>${escapeHTML(player.personaName)}</em>` : ""}
          <small>${stats.matches} карт</small>
          </span>
        </div>
        <div class="fantasy-total">
          <span>Средние очки</span><strong>${formatPoints(stats.totalPoints)}</strong>
        </div>
        <div class="metric-preview">
          ${topMetrics.map(metric => `
            <span><small>${escapeHTML(metric.label)}</small><b>+${formatPoints(metric.averagePoints)}</b></span>
          `).join("") || `<span class="muted-placeholder">Нет данных</span>`}
        </div>
      </div>
      <span class="player-card-open" aria-hidden="true">›</span>
    </button>
  `;
}

function buildTeamRosterRoles(roster) {
  return fantasyRoleGroups.map(role => {
    const members = roster
      .filter(player => role.positions.includes(Number(player.position || 0)))
      .sort((left, right) => Number(left.position || 0) - Number(right.position || 0));
    if (!members.length) return null;

    const roleMatches = buildFantasyRoleMatches(members);
    const stats = roleMatches.length
      ? summarizePlayerMatches(roleMatches).stats
      : averageRosterRoleStats(members);
    return { ...role, members, stats };
  }).filter(Boolean);
}

function averageRosterRoleStats(members) {
  const memberStats = members.map(member => member.stats || {}).filter(Boolean);
  const divisor = Math.max(memberStats.length, 1);
  const metricMap = new Map();
  memberStats.forEach(stats => (stats.metrics || []).forEach(metric => {
    const current = metricMap.get(metric.key) || {
      key: metric.key,
      label: metric.label,
      average: 0,
      averagePoints: 0,
    };
    current.average += Number(metric.average || 0) / divisor;
    current.averagePoints += Number(metric.averagePoints || 0) / divisor;
    metricMap.set(metric.key, current);
  }));
  return {
    matches: memberStats.length ? Math.min(...memberStats.map(stats => Number(stats.matches || 0))) : 0,
    totalPoints: memberStats.reduce((sum, stats) => sum + Number(stats.totalPoints || 0), 0) / divisor,
    metrics: [...metricMap.values()],
  };
}

function renderRosterRoleCard(role) {
  const stats = role.stats || { matches: 0, totalPoints: 0, metrics: [] };
  const topMetrics = [...(stats.metrics || [])]
    .sort((left, right) => Number(right.averagePoints || 0) - Number(left.averagePoints || 0))
    .slice(0, 2);
  const positions = role.positions.length > 1
    ? `позиции ${role.positions.join(" + ")}`
    : `позиция ${role.positions[0]}`;
  return `
    <article class="roster-role-card roster-role-${escapeAttribute(role.key)}">
      <header class="roster-role-head">
        <strong>${escapeHTML(role.label)}</strong>
        <span>${escapeHTML(positions)}</span>
      </header>
      <div class="roster-role-players">
        ${role.members.map(player => `
          <button class="roster-role-player" type="button" data-player="${player.accountId}"
                  title="Открыть профиль ${escapeAttribute(player.name)}">
            ${playerImage(player, "roster-role-avatar")}
            <span>
              <strong>${escapeHTML(player.name)}</strong>
              ${player.personaName ? `<em>${escapeHTML(player.personaName)}</em>` : ""}
              <small>Поз. ${player.position} · ${Number(player.stats?.matches || 0)} карт</small>
            </span>
          </button>
        `).join("")}
      </div>
      <div class="roster-role-summary">
        <span><small>Средние очки</small><strong>${formatPoints(stats.totalPoints)}</strong></span>
        <span><small>Совместных карт</small><strong>${Number(stats.matches || 0)}</strong></span>
      </div>
      <div class="roster-role-metrics">
        ${topMetrics.map(metric => `
          <span><small class="metric-inline-label">${renderFantasyMetricIcon(metric.key, "metric-inline-icon")}${escapeHTML(metric.label)}</small><b>+${formatPoints(metric.averagePoints)}</b></span>
        `).join("") || `<span class="muted-placeholder">Нет данных</span>`}
      </div>
    </article>
  `;
}

function renderTeamMatches(matches) {
  matches = Array.isArray(matches) ? matches : [];
  if (!matches.length) {
    return `
      <div class="empty-state team-empty">
        <div class="empty-rune">◈</div>
        <h3>История ещё не обновлялась</h3>
        <p>${PUBLIC_MODE ? "В опубликованном снимке пока нет матчей этой пятёрки." : "Нажми «Обновить и запарсить матчи» — приложение найдёт игры этой пятёрки."}</p>
      </div>
    `;
  }
  const groups = groupMatchesBySeries(matches);
  registerSeriesGroups(groups, currentTeam?.team?.name || "Команда");
  return `
    <div class="team-series-list">
      ${groups.map(group => `
        <section class="team-series">
          ${renderSeriesSummaryButton(group, { className: "team-series-head", winField: "teamWon" })}
          <div class="team-match-list">
          ${group.matches.map((match, index) => {
            const parsed = match.parseStatus === "done";
            const resultClass = parsed ? (match.teamWon ? "win" : "loss") : "pending";
            const resultLabel = parsed ? (match.teamWon ? "W" : "L") : "·";
            return `
              <div class="team-match-row ${match.included ? "included" : "excluded"}">
                ${PUBLIC_MODE ? "" : `<label class="match-checkbox" title="Учитывать карту в fantasy">
                  <input type="checkbox" data-include-match="${match.matchId}"
                         ${match.included ? "checked" : ""} ${parsed ? "" : "disabled"}>
                </label>`}
                <button class="team-match-open" type="button"
                        data-match="${match.matchId}" ${parsed ? "" : "disabled"}>
                  <span class="result-mark ${resultClass}">${resultLabel}</span>
                  <span class="series-map-index"><strong>Карта ${index + 1}</strong><small>#${match.matchId}</small></span>
                  <strong class="team-match-score">${match.teamScore} : ${match.opponentScore}</strong>
                  <span class="team-match-date">${formatDate(match.startTime)}</span>
                  <span class="parse-pill ${match.parseStatus}">${parseStatusLabel(match.parseStatus)}</span>
                  ${match.parseError ? `<span class="match-error">${escapeHTML(match.parseError)}</span>` : ""}
                </button>
              </div>`;
          }).join("")}
          </div>
        </section>
      `).join("")}
    </div>
  `;
}

function renderSeriesSummaryButton(group, options = {}) {
  const winField = options.winField === "won" ? "won" : "teamWon";
  const wins = group.matches.filter(match => Boolean(match[winField])).length;
  const losses = Math.max(0, group.matches.length - wins);
  const resultClass = wins > losses ? "series-win" : wins < losses ? "series-loss" : "series-draw";
  const resultLabel = wins > losses ? "Победа" : wins < losses ? "Поражение" : "Ничья";
  const className = options.className === "player-series-head" ? "player-series-head" : "team-series-head";
  const opponentName = group.opponentName || "Неизвестная команда";
  const opponentMark = opponentName.split(/\s+/).filter(Boolean).map(word => word[0]).join("").slice(0, 2).toUpperCase() || "?";
  const opponentLogoUrl = preferredTeamEmblem("", group.opponentLogo, opponentName);
  const opponentLogo = opponentLogoUrl
    ? `<img src="${escapeAttribute(opponentLogoUrl)}" alt="" loading="lazy" decoding="async">`
    : `<span>${escapeHTML(opponentMark)}</span>`;
  return `
    <button class="${className} ${resultClass}" type="button"
            data-series-matches="${group.matches.map(match => match.matchId).join(",")}"
            data-series-label="${seriesLabel(group)}"
            data-series-opponent="${escapeAttribute(opponentName)}"
            data-series-league="${escapeAttribute(group.leagueName || "Турнир не указан")}"
            data-series-date="${group.startTime}"
            data-series-wins="${wins}"
            data-series-losses="${losses}">
      <span class="series-format">${seriesLabel(group)}</span>
      <span class="series-opponent-logo">${opponentLogo}</span>
      <span class="series-opponent-copy">
        <strong>${escapeHTML(opponentName)}</strong>
        <small>${escapeHTML(group.leagueName || "Турнир не указан")} · ${formatDate(group.startTime)}</small>
      </span>
      <span class="series-summary-result"><small>${resultLabel}</small><strong>${wins}<i>:</i>${losses}</strong></span>
      <span class="series-chevron" aria-hidden="true">›</span>
    </button>`;
}

function groupMatchesBySeries(matches) {
  const groups = new Map();
  matches.forEach(match => {
    const key = Number(match.seriesId) || Number(match.matchId);
    if (!groups.has(key)) {
      groups.set(key, {
        key, seriesType: Number(match.seriesType || 0), startTime: match.startTime,
        opponentName: match.opponentName, opponentLogo: match.opponentLogo,
        leagueName: match.leagueName, matches: [],
      });
    }
    groups.get(key).matches.push(match);
    groups.get(key).startTime = Math.max(groups.get(key).startTime || 0, match.startTime || 0);
  });
  return [...groups.values()]
    .map(group => ({ ...group, matches: group.matches.sort((a, b) => a.startTime - b.startTime) }))
    .sort((a, b) => b.startTime - a.startTime);
}

function seriesLabel(group) {
  if (group.seriesType === 1) return "BO3";
  if (group.seriesType === 2) return "BO5";
  if (group.matches.length > 1) return `BO${Math.max(3, group.matches.length)}`;
  return "BO1";
}

function leagueSummaries(matches) {
  const leagues = new Map();
  matches.forEach(match => {
    const name = match.leagueName || "Без турнира";
    const item = leagues.get(name) || { name, matchCount: 0, includedCount: 0, firstMatch: Number(match.startTime || 0) };
    item.matchCount += 1;
    item.includedCount += match.included ? 1 : 0;
    item.firstMatch = Math.min(item.firstMatch || Number(match.startTime || 0), Number(match.startTime || 0));
    leagues.set(name, item);
  });
  return [...leagues.values()].sort((a, b) => a.firstMatch - b.firstMatch || a.name.localeCompare(b.name, "ru"));
}

function isTIQualifierLeague(name) {
  const league = String(name || "").toLowerCase();
  return league.includes("the international") && league.includes("qualif");
}

function tournamentFilterGroups(leagues) {
  const groups = [];
  let qualifiers = null;
  leagues.forEach(league => {
    if (isTIQualifierLeague(league.name)) {
      if (!qualifiers) {
        qualifiers = {
          key: "ti-qualifiers",
          name: "TI 2026 · квалификации",
          names: [],
          matchCount: 0,
          includedCount: 0,
          firstMatch: league.firstMatch,
          lastMatch: league.lastMatch,
        };
        groups.push(qualifiers);
      }
      qualifiers.names.push(league.name);
      qualifiers.matchCount += league.matchCount;
      qualifiers.includedCount += league.includedCount;
      qualifiers.firstMatch = Math.min(qualifiers.firstMatch, league.firstMatch);
      qualifiers.lastMatch = Math.max(qualifiers.lastMatch || league.lastMatch, league.lastMatch || 0);
      return;
    }
    groups.push({ ...league, key: league.name, names: [league.name] });
  });
  return groups.sort((a, b) => a.firstMatch - b.firstMatch || a.name.localeCompare(b.name, "ru"));
}

function encodeLeagueNames(names) {
  return escapeAttribute(names.join("\u001f"));
}

function leagueNamesFromInput(input) {
  return String(input?.dataset.leagueNames || "").split("\u001f").filter(Boolean);
}

function groupIsSelected(selected, group) {
  return group.names.every(name => selected.has(name));
}

function selectionPreferenceKey(scope) {
  return `dotaHubSelection:${scope}`;
}

function readSelectionPreference(scope) {
  try {
    return JSON.parse(localStorage.getItem(selectionPreferenceKey(scope)) || "null");
  } catch {
    return null;
  }
}

function saveSelectionPreference(scope, leagueNames, limit) {
  localStorage.setItem(selectionPreferenceKey(scope), JSON.stringify({
    leagueNames: [...leagueNames],
    limit: Number(limit || 1),
  }));
}

function clearTeamSelectionPreferences() {
  for (let index = localStorage.length - 1; index >= 0; index -= 1) {
    const key = localStorage.key(index);
    if (key?.startsWith("dotaHubSelection:team:")) localStorage.removeItem(key);
  }
}

function renderSelectAllLeagues(prefix, checked) {
  return `<label class="select-all-leagues">
    <input type="checkbox" data-select-all-leagues="${prefix}" ${checked ? "checked" : ""}>
    <span>Выбрать все турниры</span>
  </label>`;
}

function initializeTeamFilter(matches) {
  const done = matches.filter(match => match.parseStatus === "done");
  const included = done.filter(match => match.included);
  const available = new Set(done.map(match => match.leagueName || "Без турнира"));
  const preference = readSelectionPreference(`team:${currentTeam?.team?.slug}`);
  const preferredLeagues = (preference?.leagueNames || []).filter(league => available.has(league));
  pendingTeamLeagues = new Set(preferredLeagues.length ? preferredLeagues :
    included.map(match => match.leagueName || "Без турнира"));
  pendingTeamLimit = Math.min(Math.max(1, done.length), Math.max(1,
    Number(preference?.limit || included.length || Math.min(20, done.length || 1))));
}

function stageTeamFilterSelection() {
  const done = (currentTeam?.matches || []).filter(match => match.parseStatus === "done");
  let selected = 0;
  done.forEach(match => {
    const league = match.leagueName || "Без турнира";
    const include = pendingTeamLeagues.has(league) && selected < pendingTeamLimit;
    pendingSelection.set(match.matchId, include);
    if (include) selected += 1;
  });
}

function renderMatchLimitControl(prefix, maxMatches, value) {
  const max = Math.max(1, Number(maxMatches || 0));
  const current = Math.min(max, Math.max(1, Number(value || 1)));
  return `
    <div class="match-limit-control" data-limit-control="${prefix}">
      <div class="match-limit-heading">
        <span>Количество последних карт</span>
        <strong data-limit-label>${current} из ${max}</strong>
      </div>
      <div class="match-limit-row">
        <input type="range" min="1" max="${max}" value="${current}" data-match-limit="${prefix}">
        <button type="button" class="limit-all-button ${current >= max ? "active" : ""}" data-limit-all="${prefix}">Все</button>
      </div>
    </div>`;
}

function updateMatchLimitControl(root, prefix, value, maxMatches) {
  const max = Math.max(1, Number(maxMatches || 0));
  const range = root?.querySelector(`[data-match-limit="${prefix}"]`);
  const button = root?.querySelector(`[data-limit-all="${prefix}"]`);
  const label = root?.querySelector(`[data-limit-control="${prefix}"] [data-limit-label]`);
  if (range) {
    range.max = String(max);
    range.value = String(Math.min(max, Math.max(1, Number(value || 1))));
  }
  button?.classList.toggle("active", Number(value) >= max);
  if (label) label.textContent = `${Math.min(max, Math.max(1, Number(value || 1)))} из ${max}`;
}

function bindTeamLimitControl() {
  const range = elements.teamPage.querySelector('[data-match-limit="team"]');
  const allButton = elements.teamPage.querySelector('[data-limit-all="team"]');
  range?.addEventListener("input", () => {
    pendingTeamLimit = Number(range.value);
    stageTeamFilterSelection();
    syncSelectionControls();
    markSelectionDirty();
  });
  allButton?.addEventListener("click", () => {
    pendingTeamLimit = Number(range?.max || 1);
    stageTeamFilterSelection();
    syncSelectionControls();
    markSelectionDirty();
  });
}

function stageSelectionMode(mode) {
  const done = (currentTeam?.matches || []).filter(match => match.parseStatus === "done");
  if (mode === "all" || mode === "none") {
    done.forEach(match => pendingSelection.set(match.matchId, mode === "all"));
  } else if (mode === "last20") {
    const selected = new Set(done.slice(0, 20).map(match => match.matchId));
    done.forEach(match => pendingSelection.set(match.matchId, selected.has(match.matchId)));
  } else if (mode === "ti") {
    done.forEach(match => pendingSelection.set(match.matchId, /international/i.test(match.leagueName || "")));
  }
  if (mode === "none") {
    pendingTeamLeagues.clear();
  }
  syncSelectionControls();
  markSelectionDirty();
}

function stageLeagueGroupSelection(leagues, included) {
  leagues.forEach(league => {
    if (included) pendingTeamLeagues.add(league);
    else pendingTeamLeagues.delete(league);
  });
  stageTeamFilterSelection();
  syncSelectionControls();
  markSelectionDirty();
}

function syncSelectionControls() {
  elements.teamPage.querySelectorAll("[data-include-match]").forEach(input => {
    input.checked = pendingSelection.get(Number(input.dataset.includeMatch)) === true;
    input.closest(".team-match-row")?.classList.toggle("excluded", !input.checked);
  });
  elements.teamPage.querySelectorAll("[data-league-group]").forEach(input => {
    input.checked = leagueNamesFromInput(input).every(league => pendingTeamLeagues.has(league));
  });
  const leagueInputs = [...elements.teamPage.querySelectorAll("[data-league-group]")];
  const selectAll = elements.teamPage.querySelector('[data-select-all-leagues="team"]');
  if (selectAll) selectAll.checked = leagueInputs.length > 0 && leagueInputs.every(input => input.checked);
  updateMatchLimitControl(elements.teamPage, "team", pendingTeamLimit,
    (currentTeam?.matches || []).filter(match => match.parseStatus === "done").length);
}

function markSelectionDirty() {
  document.querySelector("#applySelectionButton")?.classList.remove("hidden");
}

async function applySelection() {
  if (!currentTeam?.team?.slug) return;
  const button = document.querySelector("#applySelectionButton");
  button.disabled = true;
  const matches = [...pendingSelection.entries()].map(([matchId, included]) => ({ matchId, included }));
  try {
    await api(`/api/teams/${encodeURIComponent(currentTeam.team.slug)}/selection`, {
      method: "POST", body: JSON.stringify({ matches }),
    });
    saveSelectionPreference(`team:${currentTeam.team.slug}`, pendingTeamLeagues, pendingTeamLimit);
    tournamentPlayers = [];
    currentPlayerDetail = null;
    await loadTeam(currentTeam.team.slug);
  } finally {
    button.disabled = false;
  }
}

async function syncTeam(slug) {
  const button = document.querySelector("#syncTeamButton");
  button.disabled = true;
  try {
    const job = await api(`/api/teams/${encodeURIComponent(slug)}/sync`, {
      method: "POST",
      body: "{}",
    });
    renderTeamJob(job, slug);
    await watchJob(job.id, { teamSlug: slug });
  } catch (error) {
    renderTeamJob({ state: "error", message: "Синхронизация не запущена", error: error.message, progress: 0 }, slug);
  } finally {
    button.disabled = false;
  }
}

function renderTeamJob(job, slug = job.teamSlug) {
  if (currentTeam?.team?.slug !== slug) return;
  const panel = document.querySelector("#teamJobPanel");
  if (!panel) return;
  panel.classList.remove("hidden");
  panel.innerHTML = `
    <div class="job-line">
      <div>
        <p class="job-state">${escapeHTML(stateLabels[job.state] || job.state)}</p>
        <p class="job-message">${escapeHTML(job.message || "")}</p>
      </div>
      <strong>${job.progress || 0}%</strong>
    </div>
    <div class="progress-track"><div class="progress-bar" style="width:${job.progress || 0}%"></div></div>
    ${job.total ? `<p class="job-counter">${job.completed || 0} из ${job.total} матчей обработано</p>` : ""}
    ${job.error ? `<p class="job-error">${escapeHTML(job.error)}</p>` : ""}
  `;
}

async function restoreTeamJob(slug) {
  try {
    const result = await api(`/api/jobs/active?teamSlug=${encodeURIComponent(slug)}`);
    if (result.active && result.job) {
      renderTeamJob(result.job, slug);
      watchJob(result.job.id, { teamSlug: slug });
    }
  } catch {}
}

async function syncAllTeams() {
  elements.syncAllTeamsButton.disabled = true;
  try {
    const job = await api("/api/teams/sync-all", { method: "POST", body: "{}" });
    renderAllTeamsJob(job);
    await watchJob(job.id, { allTeams: true });
  } finally {
    elements.syncAllTeamsButton.disabled = false;
  }
}

function renderAllTeamsJob(job) {
  const panel = elements.allTeamsJobPanel;
  if (!panel) return;
  panel.classList.remove("hidden");
  panel.innerHTML = `
    <div class="job-line"><div><p class="job-state">${escapeHTML(stateLabels[job.state] || job.state)}</p>
      <p class="job-message">${escapeHTML(job.message || "")}</p></div><strong>${job.progress || 0}%</strong></div>
    <div class="progress-track"><div class="progress-bar" style="width:${job.progress || 0}%"></div></div>
    ${job.total ? `<p class="job-counter">${job.completed || 0} из ${job.total} команд обработано${job.failed ? ` · ошибок: ${job.failed}` : ""}</p>` : ""}
    ${job.error ? `<p class="job-error">${escapeHTML(job.error)}</p>` : ""}`;
}

async function restoreAllTeamsJob() {
  try {
    const result = await api("/api/jobs/active");
    if (result.active && result.job) {
      renderAllTeamsJob(result.job);
      watchJob(result.job.id, { allTeams: true });
    } else if (elements.allTeamsJobPanel) {
      elements.allTeamsJobPanel.classList.add("hidden");
      elements.allTeamsJobPanel.innerHTML = "";
    }
  } catch {}
}

async function startParse(event) {
  event.preventDefault();
  const matchId = elements.matchId.value.trim();
  if (!/^\d+$/.test(matchId)) {
    showJobError("Match ID должен состоять только из цифр.");
    return;
  }

  elements.parseButton.disabled = true;
  resetJobPanel();
  try {
    const job = await api("/api/matches/parse", {
      method: "POST",
      body: JSON.stringify({ matchId }),
    });
    await refreshParseQueue({ fallbackJob: job });
    await watchJob(job.id, false);
  } catch (error) {
    showJobError(error.message);
  } finally {
    elements.parseButton.disabled = false;
  }
}

async function uploadReplayFile(file) {
  if (!file) return;
  if (!/\.(dem|bz2)$/i.test(file.name || "")) {
    showJobError("Поддерживаются только replay-файлы .dem и .dem.bz2.");
    return;
  }
  const matchId = elements.matchId.value.trim();
  if (matchId && !/^\d+$/.test(matchId)) {
    showJobError("Если указываешь Match ID рядом с replay, он должен состоять только из цифр.");
    return;
  }

  const form = new FormData();
  form.append("replay", file, file.name);
  if (matchId) form.append("matchId", matchId);

  elements.parseButton.disabled = true;
  elements.replayDropzone?.classList.add("uploading");
  resetJobPanel();
  try {
    const response = await fetch(`${API_BASE}/api/replays/upload`, {
      method: "POST",
      credentials: "include",
      body: form,
    });
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(body.error || `HTTP ${response.status}`);
    await refreshParseQueue({ fallbackJob: body });
    await watchJob(body.id, false);
  } catch (error) {
    showJobError(error.message);
  } finally {
    elements.parseButton.disabled = false;
    elements.replayDropzone?.classList.remove("uploading", "drag-over");
    if (elements.replayFile) elements.replayFile.value = "";
  }
}

async function watchJob(jobId, options = {}) {
  if (watchedJobs.has(jobId)) return;
  watchedJobs.add(jobId);
  try {
    while (true) {
      await sleep(850);
      const job = await api(`/api/jobs/${encodeURIComponent(jobId)}`);
    if (options.teamSlug) renderTeamJob(job, options.teamSlug);
    else if (options.allTeams) renderAllTeamsJob(job);
    else await refreshParseQueue({ fallbackJob: job });

      if (job.state === "done") {
      if (options.teamSlug) {
        tournamentPlayers = [];
        currentPlayerDetail = null;
        if (currentTeam?.team?.slug === options.teamSlug) await loadTeam(options.teamSlug);
      } else if (options.allTeams) {
        tournamentPlayers = [];
        currentPlayerDetail = null;
        await loadTeams(true);
      } else {
        await loadMatches();
        elements.matchId.value = "";
        const snapshot = await refreshParseQueue();
        if (!snapshot?.running && !(snapshot?.queued || []).length) {
          setTimeout(() => elements.jobPanel.classList.add("hidden"), 3000);
        }
      }
        return;
      }
      if (job.state === "error") {
        await refreshParseQueue({ fallbackJob: job });
        if (!options.teamSlug && !options.allTeams) await loadMatches();
        return;
      }
    }
  } finally {
    watchedJobs.delete(jobId);
  }
}

function resetJobPanel() {
  elements.jobPanel.classList.remove("hidden");
  elements.jobError.classList.add("hidden");
  elements.jobError.innerHTML = "";
  elements.jobCounter.classList.add("hidden");
  elements.parseQueuePanel?.classList.add("hidden");
  renderJob({ state: "queued", message: "Создаю задачу", progress: 1 });
}

function renderJob(job) {
  elements.jobPanel.classList.remove("hidden");
  elements.jobState.textContent = stateLabels[job.state] || job.state;
  elements.jobMessage.textContent = job.message || "";
  elements.jobProgress.textContent = `${job.progress || 0}%`;
  elements.progressBar.style.width = `${job.progress || 0}%`;
  if (job.total) {
    elements.jobCounter.textContent = `${job.completed || 0} из ${job.total} матчей обработано`;
    elements.jobCounter.classList.remove("hidden");
  } else {
    elements.jobCounter.classList.add("hidden");
  }
  if (job.error) {
    renderJobError(job.error);
  } else {
    elements.jobError.classList.add("hidden");
    elements.jobError.innerHTML = "";
  }
}

function renderJobError(message) {
  const recovery = replayRecoveryAdvice(message);
  elements.jobError.innerHTML = `
    <p class="job-error-text">${escapeHTML(message)}</p>
    ${recovery ? `
      <div class="replay-advice">
        <strong>${escapeHTML(recovery.title)}</strong>
        <p>${escapeHTML(recovery.body)}</p>
        <button class="replay-advice-button" type="button" data-action="upload-replay">Выбрать файл</button>
      </div>
    ` : ""}
  `;
  elements.jobError.classList.remove("hidden");
  elements.jobError.querySelector('[data-action="upload-replay"]')
    ?.addEventListener("click", focusReplayUpload);
}

function focusReplayUpload() {
  elements.replayDropzone?.scrollIntoView({ behavior: "smooth", block: "center" });
  elements.replayDropzone?.classList.add("attention");
  window.setTimeout(() => elements.replayDropzone?.classList.remove("attention"), 1400);
  elements.replayFile?.click();
}

async function refreshParseQueue(options = {}) {
  if (!elements.parseQueuePanel) return null;
  try {
    const snapshot = await api("/api/jobs/parse-queue");
    renderParseQueue(snapshot, options);
    return snapshot;
  } catch {
    elements.parseQueuePanel.classList.add("hidden");
    if (options.fallbackJob) renderJob(options.fallbackJob);
    return null;
  }
}

function renderParseQueue(snapshot, options = {}) {
  const running = snapshot?.running || null;
  const queued = Array.isArray(snapshot?.queued) ? snapshot.queued : [];
  if (elements.queueSummary) {
    elements.queueSummary.textContent = running
      ? `1 в работе${queued.length ? ` · ${queued.length} ждут` : ""}`
      : (queued.length ? `${queued.length} в очереди` : "Очередь свободна");
    elements.queueSummary.dataset.status = running || queued.length ? "warning" : "neutral";
  }
  if (running) {
    renderJob(running);
  } else if (options.fallbackJob && options.fallbackJob.state !== "queued") {
    renderJob(options.fallbackJob);
  } else if (queued.length > 0) {
    renderJob({ state: "queued", message: "Жду текущую обработку", progress: 1 });
  }

  if (!running && queued.length === 0) {
    elements.parseQueuePanel.classList.add("hidden");
    elements.parseQueuePanel.innerHTML = "";
    return;
  }

  const rows = [];
  if (running) {
    rows.push(queueRow(running, "Сейчас", true));
  }
  queued.forEach((job, index) => rows.push(queueRow(job, `#${index + 1}`, false)));

  elements.parseQueuePanel.innerHTML = `
    <div class="parse-queue-head">
      <span>Очередь обработки</span>
      <b>${queued.length ? `ожидают ${queued.length}` : "без ожидания"}</b>
    </div>
    <div class="parse-queue-list">${rows.join("")}</div>
  `;
  elements.parseQueuePanel.classList.remove("hidden");
}

function queueRow(job, marker, active) {
  const title = queueJobTitle(job);
  const message = active ? (job.message || "В работе") : (job.message || "Ждёт своей очереди");
  return `
    <div class="parse-queue-row${active ? " active" : ""}">
      <span class="parse-queue-marker">${escapeHTML(marker)}</span>
      <div>
        <strong>${escapeHTML(title)}</strong>
        <small>${escapeHTML(message)}</small>
      </div>
      <em>${Number(job.progress || 0)}%</em>
    </div>
  `;
}

function queueJobTitle(job) {
  if (job.kind === "local-replay") {
    if (job.originalName) return job.originalName;
    if (job.matchId) return `Локальный replay #${job.matchId}`;
    return "Локальный replay";
  }
  if (job.matchId) return `Match ID ${job.matchId}`;
  return stateLabels[job.kind] || "Задача";
}

function showJobError(message) {
  resetJobPanel();
  renderJob({ state: "error", message: "Не удалось запустить обработку", progress: 0, error: message });
}

function openMatch(matchId, options = {}) {
  const params = new URLSearchParams({ overlay: "match", match: String(matchId) });
  const seriesContext = options.seriesContext
    || knownSeriesByMatch.get(Number(matchId))
    || (options.returnToSeries ? currentSeriesContext : null);
  navigateToOverlay(params, {
    returnToSeries: Boolean(options.returnToSeries),
    seriesContext,
  });
}

async function showMatchDialog(matchId, options = {}) {
  if (history.state?.seriesContext) {
    currentSeriesContext = history.state.seriesContext;
    knownSeriesByMatch.set(Number(matchId), currentSeriesContext);
  }
  elements.backToSeries.classList.toggle("hidden", !options.returnToSeries || !currentSeriesContext);
  elements.dialogEyebrow.textContent = `Матч #${matchId}`;
  elements.dialogTitle.textContent = "Загружаю статистику…";
  elements.dialogMeta.textContent = "";
  elements.dialogContent.innerHTML = "";
  if (!elements.dialog.open) elements.dialog.showModal();

  try {
    const match = await api(`/api/matches/${matchId}`);
    renderMatchDetails(match);
  } catch (error) {
    elements.dialogTitle.textContent = "Ошибка";
    elements.dialogContent.innerHTML = `<p class="job-error">${escapeHTML(error.message)}</p>`;
  }
}

function bindSeriesOpeners(root, teamName) {
  root?.querySelectorAll("[data-series-matches]").forEach(button => {
    button.addEventListener("click", () => {
      const matchIds = String(button.dataset.seriesMatches || "").split(",").map(Number).filter(Boolean);
      openSeries({
        matchIds,
        label: button.dataset.seriesLabel || "BO-серия",
        opponent: button.dataset.seriesOpponent || "Неизвестная команда",
        league: button.dataset.seriesLeague || "Турнир не указан",
        startTime: Number(button.dataset.seriesDate || 0),
        wins: Number(button.dataset.seriesWins || 0),
        losses: Number(button.dataset.seriesLosses || 0),
        teamName,
      });
    });
  });
}

function registerSeriesGroups(groups, teamName) {
  (groups || []).forEach(group => {
    const matchIds = group.matches.map(match => Number(match.matchId)).filter(Boolean);
    if (!matchIds.length) return;
    const wins = group.matches.filter(match => Boolean(match.teamWon ?? match.won)).length;
    const series = {
      matchIds,
      label: seriesLabel(group),
      opponent: group.opponentName || "Неизвестная команда",
      league: group.leagueName || "Турнир не указан",
      startTime: Number(group.startTime || 0),
      wins,
      losses: Math.max(0, group.matches.length - wins),
      teamName: teamName || "Команда",
    };
    matchIds.forEach(matchId => knownSeriesByMatch.set(matchId, series));
  });
}

function fallbackSeriesForMatch(match) {
  return {
    matchIds: [Number(match.matchId)],
    label: "BO1",
    opponent: "Серия не найдена",
    league: "Открыта только текущая карта",
    startTime: Number(match.startTime || 0),
    wins: match.radiantWin ? 1 : 0,
    losses: match.radiantWin ? 0 : 1,
    teamName: "Radiant",
  };
}

function openSeries(series) {
  navigateToOverlay(seriesRouteParams(series), { returnToSeries: false });
}

async function showSeriesDialog(series) {
  currentSeriesContext = series;
  elements.seriesDialogEyebrow.textContent = `${series.label} · ${series.wins}:${series.losses}`;
  elements.seriesDialogTitle.textContent = `${series.teamName} vs ${series.opponent}`;
  elements.seriesDialogMeta.textContent = `${series.league} · ${formatDate(series.startTime)}`;
  elements.seriesDialogContent.innerHTML = `<div class="loading-card">Собираю карты серии…</div>`;
  if (!elements.seriesDialog.open) elements.seriesDialog.showModal();
  try {
    const matches = await Promise.all(series.matchIds.map(matchId => api(`/api/matches/${matchId}`)));
    renderSeriesDetails(series, matches);
  } catch (error) {
    elements.seriesDialogContent.innerHTML = `<p class="job-error">${escapeHTML(error.message)}</p>`;
  }
}

function renderSeriesDetails(series, matches) {
  const playerTotals = new Map();
  matches.forEach(match => match.players.forEach(player => {
    const key = player.accountId || `${player.name}-${player.teamSlot}`;
    const won = player.team === "radiant" ? match.radiantWin : !match.radiantWin;
    const item = playerTotals.get(key) || {
      name: player.proName || player.name || "Игрок",
      persona: player.name || "",
      alias: player.proName ? playerAlias(player.proName) : "",
      games: 0, points: 0, wins: 0, heroes: [],
    };
    item.games += 1;
    item.points += Number(player.fantasyPoints || 0);
    item.wins += won ? 1 : 0;
    item.heroes.push({ heroId: player.heroId, won, matchId: match.matchId });
    playerTotals.set(key, item);
  }));
  const ranking = [...playerTotals.values()]
    .map(player => ({ ...player, average: player.points / player.games }))
    .sort((a, b) => b.points - a.points);
  const best = ranking[0];
  const outcomeClass = series.wins > series.losses ? "series-win" : series.wins < series.losses ? "series-loss" : "series-draw";
  const outcomeLabel = series.wins > series.losses ? "Победа" : series.wins < series.losses ? "Поражение" : "Ничья";
  elements.seriesDialogContent.innerHTML = `
    <section class="series-scoreboard ${outcomeClass}">
      <div class="series-score-team series-score-home"><small>Команда</small><span>${escapeHTML(series.teamName)}</span><strong>${series.wins}</strong></div>
      <div class="series-score-center"><span>${series.label}</span><strong>${outcomeLabel}</strong><small>Серия завершена</small></div>
      <div class="series-score-team series-score-away"><strong>${series.losses}</strong><span>${escapeHTML(series.opponent)}</span><small>Соперник</small></div>
    </section>
    ${best ? `<section class="series-mvp">
      <div class="series-mvp-crown">MVP</div>
      <button type="button" class="series-player-link mvp-name" ${best.alias ? `data-series-player="${escapeAttribute(best.alias)}"` : "disabled"}>
        <span>Лучший игрок серии</span><strong>${escapeHTML(best.name)}</strong>
        ${best.persona && best.persona !== best.name ? `<small>${escapeHTML(best.persona)}</small>` : ""}
      </button>
      <div class="series-mvp-heroes">${renderSeriesHeroRun(best.heroes)}</div>
      <div class="series-mvp-score"><b>${formatPoints(best.points)}</b><small>${formatPoints(best.average)} за карту</small></div>
    </section>` : ""}
    <section class="series-map-grid">
      ${matches.map((match, index) => `<button type="button" data-series-map="${match.matchId}" class="series-map-card">
        <header><span>Карта ${index + 1}</span><strong>${match.radiantWin ? "Radiant" : "Dire"} победили</strong><small>${formatDuration(match.duration)}</small></header>
        <div class="series-map-side ${match.radiantWin ? "winner" : ""}">
          <span>Radiant</span><strong>${match.radiantKills}</strong>
          <div>${renderMapHeroTeam(match.players.filter(player => player.team === "radiant"))}</div>
        </div>
        <div class="series-map-side ${!match.radiantWin ? "winner" : ""}">
          <span>Dire</span><strong>${match.direKills}</strong>
          <div>${renderMapHeroTeam(match.players.filter(player => player.team === "dire"))}</div>
        </div>
        <footer><span>#${match.matchId}</span><strong>Открыть</strong><b>›</b></footer>
      </button>`).join("")}
    </section>
    <section class="series-ranking">
      <div class="series-ranking-head"><span>Игрок и герои</span><span>В/П</span><span>За карту</span><span>За серию</span></div>
      ${ranking.map((player, index) => `<div class="series-ranking-row">
        <div class="series-player-cell">
          <i>${index + 1}</i>
          <button type="button" class="series-player-link" ${player.alias ? `data-series-player="${escapeAttribute(player.alias)}"` : "disabled"}>
            <strong>${escapeHTML(player.name)}</strong>
            ${player.persona && player.persona !== player.name ? `<small>${escapeHTML(player.persona)}</small>` : ""}
          </button>
          <div class="series-player-heroes">${renderSeriesHeroRun(player.heroes)}</div>
        </div>
        <b class="series-record">${player.wins}:${player.games - player.wins}</b>
        <b>${formatPoints(player.average)}</b><strong>${formatPoints(player.points)}</strong>
      </div>`).join("")}
    </section>`;
  elements.seriesDialogContent.querySelectorAll("[data-series-map]").forEach(button => {
    button.addEventListener("click", () => {
      elements.seriesDialog.close();
      openMatch(Number(button.dataset.seriesMap), { returnToSeries: true });
    });
  });
  elements.seriesDialogContent.querySelectorAll("[data-series-player]").forEach(button => {
    button.addEventListener("click", () => {
      elements.seriesDialog.close();
      navigateToBase(`player/${encodeURIComponent(button.dataset.seriesPlayer)}/overview`);
    });
  });
}

function renderMapHeroTeam(players) {
  return players.map(player => heroImage(player.heroId)
    ? `<span class="series-map-hero" title="${escapeAttribute(player.proName || player.name || heroName(player.heroId))}">
        <img src="${escapeAttribute(heroImage(player.heroId))}" alt="${escapeAttribute(heroName(player.heroId))}">
      </span>`
    : `<span class="series-map-hero missing">?</span>`).join("");
}

function renderSeriesHeroRun(heroes) {
  return heroes.map(hero => heroImage(hero.heroId)
    ? `<span class="series-run-hero ${hero.won ? "win" : "loss"}" title="${escapeAttribute(heroName(hero.heroId))}">
        <img src="${escapeAttribute(heroImage(hero.heroId))}" alt="${escapeAttribute(heroName(hero.heroId))}">
      </span>`
    : "").join("");
}

function renderMatchDetails(match) {
  const radiantKills = sum(match.players.filter(player => player.team === "radiant"), "kills");
  const direKills = sum(match.players.filter(player => player.team === "dire"), "kills");
  const series = knownSeriesByMatch.get(Number(match.matchId)) || fallbackSeriesForMatch(match);
  elements.dialogTitle.textContent = `${radiantKills} : ${direKills}`;
  elements.dialogMeta.textContent =
    `${formatDate(match.startTime)} · ${formatDuration(match.duration)} · победа ${match.radiantWin ? "Radiant" : "Dire"}`;
  elements.dialogContent.innerHTML = ["radiant", "dire"]
    .map(team => renderMatchTeam(team, match.players.filter(player => player.team === team)))
    .join("");
  elements.dialogContent.insertAdjacentHTML("afterbegin", `
    <div class="match-series-toolbar">
      <span>${escapeHTML(series.league)} · ${formatDate(series.startTime)}</span>
      <button type="button" data-open-match-series="${match.matchId}">
        ${escapeHTML(series.label)} · открыть серию →
      </button>
    </div>
  `);
  elements.dialogContent.querySelector("[data-open-match-series]")?.addEventListener("click", () => {
    elements.dialog.close();
    openSeries(series);
  });
  elements.dialogContent.querySelectorAll("[data-match-player]").forEach(button => {
    button.addEventListener("click", () => {
      elements.dialog.close();
      navigateToBase(`player/${encodeURIComponent(button.dataset.matchPlayer)}/overview`);
    });
  });
}

function renderMatchTeam(team, players) {
  const columns = [
    ["Очки", player => `<b class="match-fantasy-points">${formatPoints(player.fantasyPoints)}</b>`],
    ["Убийства", player => player.kills, "kills"], ["Смерти", player => player.deaths, "deaths"],
    ["Ассисты", player => player.assists], ["Крипы", player => player.cs, "cs"],
    ["GPM", player => Math.round(player.gpm), "gpm"], ["Безумруды", player => player.madstone, "madstone"],
    ["Башни", player => player.towerKills, "towerKills"], ["Варды", player => player.wardsPlaced, "wardsPlaced"],
    ["Стаки", player => player.campsStacked, "campsStacked"], ["Руны", player => player.runesGrabbed, "runesGrabbed"],
    ["Смотрители", player => player.watchers, "watchers"], ["Лотосы", player => player.lotuses, "lotuses"],
    ["Рошан", player => player.roshanKills, "roshanKills"],
    ["Тимфайт", player => `${Math.round(player.teamfightParticipation * 100)}%`, "teamfightParticipation"],
    ["Станы", player => player.stuns.toFixed(1), "stuns"], ["Терзатели", player => player.tormentors, "tormentors"],
    ["Курьеры", player => player.courierKills, "courierKills"], ["Первая кровь", player => player.firstBlood, "firstBlood"],
    ["Smoke", player => player.smokes, "smokes"],
  ];
  return `
    <section class="team-block">
      <h3 class="team-title ${team}">${team === "radiant" ? "Radiant" : "Dire"}</h3>
      <div class="stats-scroller">
        <table class="stats-table">
          <thead><tr><th>Игрок</th>${columns.map(([label, , metric]) => `<th>${metric
            ? `<span class="metric-table-heading" title="${escapeAttribute(label)}">${renderFantasyMetricIcon(metric, "metric-table-icon")}<span>${escapeHTML(label)}</span></span>`
            : escapeHTML(label)}</th>`).join("")}</tr></thead>
          <tbody>${players.map(player => `
            <tr>
              <td class="player-cell">
                ${player.proName ? `<button type="button" class="match-player-identity profile-link" data-match-player="${escapeAttribute(playerAlias(player.proName))}">` : `<span class="match-player-identity">`}
                  ${heroImage(player.heroId) ? `<img class="hero-portrait" src="${escapeAttribute(heroImage(player.heroId))}" alt="">` : ""}
                  <span><strong>${escapeHTML(player.name || player.proName || "Без имени")}</strong>
                  ${player.proName && player.proName !== player.name ? `<em>${escapeHTML(player.proName)}</em>` : ""}
                  <small>${escapeHTML(heroName(player.heroId))} · ${player.accountId}</small></span>
                ${player.proName ? `</button>` : `</span>`}</td>
              ${columns.map(([, value]) => `<td>${value(player)}</td>`).join("")}
            </tr>`).join("")}
          </tbody>
        </table>
      </div>
    </section>`;
}

function heroName(heroId) {
  return heroNames[String(heroId)] || heroNames[heroId] || "Неизвестный герой";
}

function heroImage(heroId) {
  return heroImages[String(heroId)] || heroImages[heroId] || "";
}

async function loadPublicPlayerFilterData() {
  if (Object.keys(publicPlayerFilterData).length) return publicPlayerFilterData;
  if (publicPlayerFilterDataPromise) return publicPlayerFilterDataPromise;
  publicPlayerFilterDataPromise = api("/api/player-filter-data")
    .then(data => {
      publicPlayerFilterData = data && typeof data === "object" ? data : {};
      if (PUBLIC_MODE) initializePublicTournamentFilter();
      return publicPlayerFilterData;
    });
  try {
    return await publicPlayerFilterDataPromise;
  } finally {
    publicPlayerFilterDataPromise = null;
  }
}

function buildPublicTournamentFilterOverview(details) {
  const teams = new Map();
  const seenTeamMatches = new Set();
  const uniqueMatches = new Map();
  Object.values(details || {}).forEach(detail => {
    const teamSlug = String(detail?.player?.teamSlug || "unknown");
    const team = teams.get(teamSlug) || { matches: 0, included: 0 };
    (detail?.matches || []).forEach(match => {
      const key = `${teamSlug}:${match.matchId}`;
      if (!seenTeamMatches.has(key)) {
        seenTeamMatches.add(key);
        team.matches += 1;
        if (match.included) team.included += 1;
      }
      const matchKey = String(match.matchId || key);
      const existing = uniqueMatches.get(matchKey);
      if (existing) {
        existing.included = existing.included || Boolean(match.included);
      } else {
        uniqueMatches.set(matchKey, {
          leagueId: Number(match.leagueId || 0),
          leagueName: match.leagueName || "Без турнира",
          startTime: Number(match.startTime || 0),
          included: Boolean(match.included),
        });
      }
    });
    teams.set(teamSlug, team);
  });
  const leagues = new Map();
  uniqueMatches.forEach(match => {
    const name = match.leagueName;
    const league = leagues.get(name) || {
      leagueId: match.leagueId,
      name,
      matchCount: 0,
      includedCount: 0,
      firstMatch: match.startTime,
      lastMatch: match.startTime,
    };
    league.matchCount += 1;
    if (match.included) league.includedCount += 1;
    if (!league.leagueId && match.leagueId) league.leagueId = match.leagueId;
    league.firstMatch = Math.min(league.firstMatch || match.startTime, match.startTime);
    league.lastMatch = Math.max(league.lastMatch || match.startTime, match.startTime);
    leagues.set(name, league);
  });
  return {
    leagues: [...leagues.values()].sort((left, right) =>
      left.firstMatch - right.firstMatch || left.name.localeCompare(right.name, "ru")),
    maxMatches: Math.max(1, ...[...teams.values()].map(team => team.matches)),
    selectedPerTeam: Math.max(1, ...[...teams.values()].map(team => team.included)),
  };
}

function tournamentVisual(name) {
  const normalized = String(name || "").toLowerCase();
  if (normalized.includes("esports world cup") || normalized.includes("road to ewc")) {
    return { tone: "ewc", mark: "EWC", logo: "assets/tournaments/ewc.svg?v=2" };
  }
  if (normalized.includes("blast slam")) {
    return { tone: "blast", mark: "BLAST", logo: "assets/tournaments/blast-slam.png" };
  }
  if (normalized.includes("dreamleague")) {
    return { tone: "dreamleague", mark: "DL", logo: "assets/tournaments/dreamleague.png" };
  }
  if (normalized.includes("games of the future")) {
    return { tone: "future", mark: "GOTF", logo: "assets/tournaments/games-of-the-future.svg" };
  }
  if (normalized.includes("international") && normalized.includes("qualif")) {
    return { tone: "qualifier", mark: "Q26", logo: "" };
  }
  if (normalized.includes("esl")) return { tone: "esl", mark: "ESL", logo: "" };
  if (normalized.includes("1win")) return { tone: "onewin", mark: "1W", logo: "assets/tournaments/1win.svg" };
  const words = String(name || "Турнир").split(/\s+/).filter(Boolean);
  return { tone: "default", mark: words.slice(0, 2).map(word => word[0]).join("").toUpperCase(), logo: "" };
}

function tournamentPeriod(firstMatch, lastMatch) {
  const first = Number(firstMatch || 0);
  const last = Number(lastMatch || first);
  if (!first) return "Дата не указана";
  const formatter = new Intl.DateTimeFormat("ru-RU", { day: "numeric", month: "short" });
  const start = formatter.format(new Date(first * 1000)).replace(".", "");
  const end = formatter.format(new Date(last * 1000)).replace(".", "");
  return start === end ? start : `${start} – ${end}`;
}

function countLabel(value, forms) {
  const count = Math.abs(Number(value || 0)) % 100;
  const last = count % 10;
  const form = count > 10 && count < 20 ? forms[2] : last === 1 ? forms[0] : last >= 2 && last <= 4 ? forms[1] : forms[2];
  return `${value} ${form}`;
}

function renderTournamentFilterLogo(group) {
  const visual = tournamentVisual(group.name);
  const content = visual.logo
    ? `<img src="${visual.logo}" alt="" loading="lazy">`
    : `<span>${escapeHTML(visual.mark)}</span>`;
  return `<span class="tournament-filter-logo tournament-filter-logo-${visual.tone}" aria-hidden="true">${content}</span>`;
}

function readPublicTournamentFilterPreference() {
  try {
    return JSON.parse(localStorage.getItem(PUBLIC_TOURNAMENT_FILTER_KEY) || "null");
  } catch {
    localStorage.removeItem(PUBLIC_TOURNAMENT_FILTER_KEY);
    return null;
  }
}

function initializePublicTournamentFilter(reset = false) {
  if (!PUBLIC_MODE || !Object.keys(publicPlayerFilterData).length) return;
  if (reset) localStorage.removeItem(PUBLIC_TOURNAMENT_FILTER_KEY);
  const overview = buildPublicTournamentFilterOverview(publicPlayerFilterData);
  const available = new Set(overview.leagues.map(league => league.name));
  const preference = readPublicTournamentFilterPreference();
  const savedLeagues = Array.isArray(preference?.leagueNames)
    ? preference.leagueNames.filter(name => available.has(name)) : null;
  const knownLeagues = new Set(Array.isArray(preference?.knownLeagueNames)
    ? preference.knownLeagueNames : []);
  const selectedLeagues = new Set(savedLeagues === null || knownLeagues.size === 0
    ? available
    : savedLeagues);
  available.forEach(name => {
    if (!knownLeagues.has(name)) selectedLeagues.add(name);
  });
  publicTournamentFilter = {
    initialized: true,
    leagueNames: selectedLeagues,
    knownLeagueNames: [...available],
    limit: Math.min(overview.maxMatches, Math.max(1,
      Number(preference?.limit || overview.selectedPerTeam || Math.min(20, overview.maxMatches)))),
    overview,
  };
}

function savePublicTournamentFilterPreference() {
  localStorage.setItem(PUBLIC_TOURNAMENT_FILTER_KEY, JSON.stringify({
    leagueNames: [...publicTournamentFilter.leagueNames],
    knownLeagueNames: publicTournamentFilter.knownLeagueNames || [...publicTournamentFilter.overview.leagues.map(league => league.name)],
    limit: publicTournamentFilter.limit,
  }));
}

function publicFilteredMatches(matches) {
  const source = Array.isArray(matches) ? matches : [];
  if (!publicTournamentFilter.initialized) return source.filter(match => match.included);
  return source
    .filter(match => publicTournamentFilter.leagueNames.has(match.leagueName || "Без турнира"))
    .sort((left, right) => Number(right.startTime || 0) - Number(left.startTime || 0))
    .slice(0, Math.max(1, Number(publicTournamentFilter.limit || 1)))
    .sort((left, right) => Number(left.startTime || 0) - Number(right.startTime || 0));
}

function publicFilteredPlayer(player) {
  const detail = publicPlayerFilterData[player.alias];
  if (!detail) return player;
  const matches = publicFilteredMatches(detail.matches);
  const summary = summarizePlayerMatches(matches);
  const strengthAdjustedPoints = matches.length
    ? matches.reduce((sum, match) => sum + Number(match.fantasyPoints || 0) * Number(match.opponentStrength || 1), 0) / matches.length
    : 0;
  return {
    ...player,
    stats: summary.stats,
    stability: summary.stability,
    stabilityConfidence: summary.stabilityConfidence,
    opponentStrength: summary.opponentStrength,
    opponentStrengthConfidence: summary.opponentStrengthConfidence,
    strengthAdjustedPoints,
  };
}

function publicFilteredRosterPlayer(player, teamSlug) {
  const detail = Object.values(publicPlayerFilterData).find(candidate =>
    String(candidate?.player?.teamSlug || "") === String(teamSlug || "") &&
    Number(candidate?.player?.accountId || 0) === Number(player?.accountId || 0));
  return detail ? { ...player, ...publicFilteredPlayer(detail.player) } : player;
}

function publicFilteredMapCount() {
  const byTeam = new Map();
  const seen = new Set();
  Object.values(publicPlayerFilterData).forEach(detail => {
    const teamSlug = String(detail?.player?.teamSlug || "unknown");
    const matches = byTeam.get(teamSlug) || [];
    (detail?.matches || []).forEach(match => {
      const key = `${teamSlug}:${match.matchId}`;
      if (seen.has(key)) return;
      seen.add(key);
      matches.push(match);
    });
    byTeam.set(teamSlug, matches);
  });
  return [...byTeam.values()].reduce((total, matches) => total + publicFilteredMatches(matches).length, 0);
}

function scopedPublicTournamentFilterOverview(matches) {
  return buildPublicTournamentFilterOverview({
    scoped: {
      player: { teamSlug: "scoped" },
      matches: Array.isArray(matches) ? matches : [],
    },
  });
}

function renderPublicTournamentFilterPanel(options = {}) {
  const globalOverview = publicTournamentFilter.overview;
  if (!globalOverview) return `<div class="loading-card">Фильтр турниров загружается…</div>`;
  const scopedMatches = Array.isArray(options.matches) ? options.matches : null;
  const scoped = scopedMatches !== null;
  const scopedOverview = scoped ? scopedPublicTournamentFilterOverview(scopedMatches) : null;
  const overview = scoped
    ? { ...scopedOverview, maxMatches: globalOverview.maxMatches }
    : globalOverview;
  const scope = scoped ? String(options.scope || "profile") : "global";
  const eyebrow = scope === "player" ? "Игры игрока" : scope === "team" ? "Игры команды" : "Выборка статистики";
  const countForms = scoped ? ["игра", "игры", "игр"] : ["матч", "матча", "матчей"];
  const leagueGroups = tournamentFilterGroups(overview.leagues);
  const allChecked = leagueGroups.length > 0 && leagueGroups.every(group =>
    groupIsSelected(publicTournamentFilter.leagueNames, group));
  const selectedGroups = leagueGroups.filter(group => groupIsSelected(publicTournamentFilter.leagueNames, group));
  const selectedMatches = selectedGroups.reduce((total, group) => total + Number(group.matchCount || 0), 0);
  return `
    <section class="selection-panel global-selection-panel public-tournament-filter" data-public-tournament-filter data-public-filter-scope="${scope}">
      <div class="public-filter-heading">
        <div><p class="eyebrow">${eyebrow}</p><h2>Турниры</h2></div>
        <div class="public-filter-stats">
          <span>${selectedGroups.length} из ${leagueGroups.length}</span>
          <strong>${countLabel(selectedMatches, countForms)}</strong>
        </div>
      </div>
      <div class="public-filter-toolbar">
        ${renderSelectAllLeagues("public", allChecked)}
        <button type="button" class="ghost-button public-filter-reset" data-reset-public-filter><span aria-hidden="true">↺</span> Сбросить</button>
      </div>
      <div class="tournament-filter-grid">
        ${leagueGroups.map(group => {
          const visual = tournamentVisual(group.name);
          return `<label class="tournament-filter-card tournament-filter-card-${visual.tone}">
          <input type="checkbox" data-public-league data-league-names="${encodeLeagueNames(group.names)}"
            ${groupIsSelected(publicTournamentFilter.leagueNames, group) ? "checked" : ""}>
          <span class="tournament-filter-state" aria-hidden="true"></span>
          ${renderTournamentFilterLogo(group)}
          <span class="tournament-filter-copy">
            <strong>${escapeHTML(group.name || "Без турнира")}</strong>
            <small><span>${countLabel(group.matchCount, countForms)}</span><i></i><span>${tournamentPeriod(group.firstMatch, group.lastMatch)}</span></small>
          </span>
        </label>`;
        }).join("")}
      </div>
      <div class="public-filter-footer">
        ${renderMatchLimitControl("public", overview.maxMatches, publicTournamentFilter.limit)}
        <div class="public-filter-map-total"><span>${scope === "player" ? "В выборке игрока" : scope === "team" ? "В выборке команды" : "В выборке игроков"}</span><strong>${scoped
          ? countLabel(publicFilteredMatches(scopedMatches).length, ["игра", "игры", "игр"])
          : countLabel(publicFilteredMapCount(), ["командная карта", "командные карты", "командных карт"])}</strong></div>
      </div>
    </section>`;
}

function rerenderPublicTournamentFilterViews() {
  renderGlobalSelectionFilters();
  if (currentBaseRoute?.startsWith("team/") && currentTeam) {
    renderTeamPage(currentTeam);
    return;
  }
  if (currentBaseRoute?.startsWith("player/") && currentPlayerDetail) {
    renderPlayerPage(currentPlayerDetail, currentBaseRoute.split("/")[2] || "overview");
    return;
  }
  renderTournamentPlayers();
}

function bindPublicTournamentFilter(root) {
  const panel = root?.querySelector("[data-public-tournament-filter]");
  if (!panel) return;
  const commit = () => {
    const visibleLeagueNames = new Set([...panel.querySelectorAll("[data-public-league]")]
      .flatMap(input => leagueNamesFromInput(input)));
    const nextLeagueNames = new Set([...publicTournamentFilter.leagueNames]
      .filter(name => !visibleLeagueNames.has(name)));
    [...panel.querySelectorAll("[data-public-league]:checked")]
      .flatMap(input => leagueNamesFromInput(input))
      .forEach(name => nextLeagueNames.add(name));
    publicTournamentFilter.leagueNames = nextLeagueNames;
    publicTournamentFilter.limit = Number(panel.querySelector('[data-match-limit="public"]')?.value || 1);
    savePublicTournamentFilterPreference();
    rerenderPublicTournamentFilterViews();
  };
  panel.querySelector('[data-select-all-leagues="public"]')?.addEventListener("change", event => {
    panel.querySelectorAll("[data-public-league]").forEach(input => {
      input.checked = event.currentTarget.checked;
    });
    commit();
  });
  panel.querySelectorAll("[data-public-league]").forEach(input => {
    input.addEventListener("change", () => {
      syncSelectAllLeagues(panel, "public", "[data-public-league]");
      commit();
    });
  });
  const range = panel.querySelector('[data-match-limit="public"]');
  range?.addEventListener("input", () => {
    updateMatchLimitControl(panel, "public", Number(range.value), Number(range.max));
  });
  range?.addEventListener("change", commit);
  panel.querySelector('[data-limit-all="public"]')?.addEventListener("click", () => {
    if (range) range.value = range.max;
    commit();
  });
  panel.querySelector("[data-reset-public-filter]")?.addEventListener("click", () => {
    initializePublicTournamentFilter(true);
    rerenderPublicTournamentFilterViews();
  });
}

async function loadTournamentPlayers(force = false) {
  if (!force && tournamentPlayersLoaded) {
    renderGlobalSelectionFilters();
    renderTournamentPlayers();
    return;
  }
  if (tournamentPlayersLoadPromise) return tournamentPlayersLoadPromise;
  elements.playersTable.innerHTML = `<div class="loading-card">Собираю fantasy-рейтинг игроков…</div>`;
  tournamentPlayersLoadPromise = (async () => {
    try {
      if (PUBLIC_MODE) {
        [tournamentPlayers] = await Promise.all([
          api("/api/tournament-players"),
          loadPublicPlayerFilterData(),
        ]);
        globalSelectionOverview = null;
      } else {
        [tournamentPlayers, globalSelectionOverview] = await Promise.all([
          api("/api/tournament-players"),
          api("/api/selection/global"),
          loadPublicPlayerFilterData(),
        ]);
      }
      tournamentPlayersLoaded = true;
      renderGlobalSelectionFilters();
      renderTournamentPlayers();
    } catch (error) {
      tournamentPlayersLoaded = false;
      elements.playersTable.innerHTML = `<div class="job-error">${escapeHTML(error.message)}</div>`;
    }
  })();
  try {
    await tournamentPlayersLoadPromise;
  } finally {
    tournamentPlayersLoadPromise = null;
  }
}

function renderGlobalSelectionFilters() {
  if (PUBLIC_MODE) {
    if (elements.globalSelectionFilters) {
      elements.globalSelectionFilters.innerHTML = renderPublicTournamentFilterPanel();
      bindPublicTournamentFilter(elements.globalSelectionFilters);
    }
    return;
  }
  if (!elements.globalSelectionFilters || !globalSelectionOverview) return;
  const overview = globalSelectionOverview;
  const leagues = Array.isArray(overview.leagues) ? overview.leagues : [];
  const leagueGroups = tournamentFilterGroups(leagues.map((league, index) => ({
    ...league,
    firstMatch: index,
  })));
  const max = Math.max(1, Number(overview.maxMatches || 0));
  const preference = readSelectionPreference("global");
  const preferredLeagues = new Set(preference?.leagueNames || []);
  const hasPreference = preferredLeagues.size > 0;
  const limit = Math.min(max, Math.max(1, Number(preference?.limit || overview.selectedPerTeam || 20)));
  const selectedLeagues = hasPreference
    ? preferredLeagues
    : new Set(leagues.filter(league => league.includedCount > 0).map(league => league.name));
  const allLeaguesChecked = leagueGroups.length > 0 && leagueGroups.every(group => groupIsSelected(selectedLeagues, group));
  elements.globalSelectionFilters.innerHTML = `
    <section class="selection-panel global-selection-panel">
      <div class="selection-title-row">
        <div><p class="eyebrow">Общая выборка</p><h2>Фильтр всех игроков</h2></div>
        <p>Лимит применяется отдельно к каждой команде.</p>
      </div>
      <div class="league-filters">
        ${renderSelectAllLeagues("global", allLeaguesChecked)}
        ${leagueGroups.map(group => `<label>
          <input type="checkbox" data-global-league data-league-names="${encodeLeagueNames(group.names)}"
            ${groupIsSelected(selectedLeagues, group) ? "checked" : ""}>
          <span>${escapeHTML(group.name || "Без турнира")}</span><small>${group.matchCount}</small>
        </label>`).join("")}
      </div>
      ${renderMatchLimitControl("global", max, limit)}
      <div class="selection-apply-row">
        <small data-global-selection-summary>Сейчас учитывается ${overview.selectedMatches} карт</small>
        <button type="button" class="primary-button" data-apply-global-selection disabled>Применить ко всем командам</button>
      </div>
    </section>`;
  bindGenericLimitControl(elements.globalSelectionFilters, "global");
  bindSelectAllLeagues(elements.globalSelectionFilters, "global", "[data-global-league]", markGlobalSelectionDirty);
  elements.globalSelectionFilters.querySelectorAll("[data-global-league]").forEach(input => {
    input.addEventListener("change", () => {
      syncSelectAllLeagues(elements.globalSelectionFilters, "global", "[data-global-league]");
      markGlobalSelectionDirty();
    });
  });
  elements.globalSelectionFilters.querySelector("[data-apply-global-selection]")?.addEventListener("click", applyGlobalSelection);
}

function bindGenericLimitControl(root, prefix) {
  const range = root.querySelector(`[data-match-limit="${prefix}"]`);
  const allButton = root.querySelector(`[data-limit-all="${prefix}"]`);
  const markDirty = prefix === "global" ? markGlobalSelectionDirty : markPlayerSelectionDirty;
  range?.addEventListener("input", () => {
    allButton?.classList.toggle("active", Number(range.value) >= Number(range.max));
    const label = root.querySelector(`[data-limit-control="${prefix}"] [data-limit-label]`);
    if (label) label.textContent = `${range.value} из ${range.max}`;
    markDirty();
  });
  allButton?.addEventListener("click", () => {
    if (range) range.value = range.max;
    allButton.classList.add("active");
    const label = root.querySelector(`[data-limit-control="${prefix}"] [data-limit-label]`);
    if (label) label.textContent = `${range?.value || 1} из ${range?.max || 1}`;
    markDirty();
  });
}

function bindSelectAllLeagues(root, prefix, itemSelector, markDirty) {
  root.querySelector(`[data-select-all-leagues="${prefix}"]`)?.addEventListener("change", event => {
    root.querySelectorAll(itemSelector).forEach(input => {
      input.checked = event.currentTarget.checked;
    });
    markDirty();
  });
}

function syncSelectAllLeagues(root, prefix, itemSelector) {
  const inputs = [...root.querySelectorAll(itemSelector)];
  const selectAll = root.querySelector(`[data-select-all-leagues="${prefix}"]`);
  if (selectAll) selectAll.checked = inputs.length > 0 && inputs.every(input => input.checked);
}

function markGlobalSelectionDirty() {
  const button = elements.globalSelectionFilters?.querySelector("[data-apply-global-selection]");
  if (button) button.disabled = false;
}

async function applyGlobalSelection() {
  const root = elements.globalSelectionFilters;
  const button = root.querySelector("[data-apply-global-selection]");
  const leagueNames = [...new Set([...root.querySelectorAll("[data-global-league]:checked")]
    .flatMap(input => leagueNamesFromInput(input)))];
  if (!leagueNames.length) {
    alert("Выбери хотя бы один турнир.");
    return;
  }
  const range = root.querySelector('[data-match-limit="global"]');
  button.disabled = true;
  button.textContent = "Применяю…";
  try {
    await api("/api/selection/global", {
      method: "POST",
      body: JSON.stringify({ leagueNames, limit: Number(range?.value || 1), all: false }),
    });
    saveSelectionPreference("global", leagueNames, Number(range?.value || 1));
    clearTeamSelectionPreferences();
    tournamentPlayers = [];
    publicPlayerFilterData = {};
    publicPlayerFilterDataPromise = null;
    currentPlayerDetail = null;
    await loadTournamentPlayers(true);
  } catch (error) {
    alert(error.message);
    button.disabled = false;
    button.textContent = "Применить ко всем командам";
  }
}

const fantasyRoleGroups = [
  { key: "cores", label: "Cores", positions: [1, 3], order: 1 },
  { key: "mid", label: "Mid", positions: [2], order: 2 },
  { key: "supps", label: "Supps", positions: [4, 5], order: 3 },
];

function playerFilterDetail(player) {
  const direct = publicPlayerFilterData[player.alias];
  if (direct && String(direct.player?.teamSlug || "") === String(player.teamSlug || "") &&
      Number(direct.player?.accountId || 0) === Number(player.accountId || 0)) return direct;
  return Object.values(publicPlayerFilterData).find(detail =>
    String(detail?.player?.teamSlug || "") === String(player.teamSlug || "") &&
    Number(detail?.player?.accountId || 0) === Number(player.accountId || 0));
}

function selectedFantasyMatches(detail) {
  const matches = Array.isArray(detail?.matches) ? detail.matches : [];
  return PUBLIC_MODE ? publicFilteredMatches(matches) : matches.filter(match => match.included);
}

function averageRoleMetrics(matches) {
  const metricMap = new Map();
  matches.forEach(match => (match.metrics || []).forEach(metric => {
    const item = metricMap.get(metric.key) || {
      key: metric.key, label: metric.label, average: 0, averagePoints: 0, count: 0,
    };
    item.average += Number(metric.average || 0);
    item.averagePoints += Number(metric.averagePoints || 0);
    item.count += 1;
    metricMap.set(metric.key, item);
  }));
  return [...metricMap.values()].map(metric => ({
    key: metric.key,
    label: metric.label,
    average: metric.average / Math.max(metric.count, 1),
    averagePoints: metric.averagePoints / Math.max(metric.count, 1),
  }));
}

function buildFantasyRoleMatches(members) {
  const matches = new Map();
  members.forEach(member => {
    const detail = playerFilterDetail(member);
    selectedFantasyMatches(detail).forEach(match => {
      const entry = matches.get(match.matchId) || { match, players: new Map() };
      entry.players.set(member.accountId, match);
      matches.set(match.matchId, entry);
    });
  });
  return [...matches.values()]
    .filter(entry => entry.players.size === members.length)
    .map(entry => {
      const contributions = [...entry.players.values()];
      return {
        ...entry.match,
        fantasyPoints: contributions.reduce((sum, match) => sum + Number(match.fantasyPoints || 0), 0) / contributions.length,
        opponentStrength: contributions.reduce((sum, match) => sum + Number(match.opponentStrength || 1), 0) / contributions.length,
        opponentStrengthConfidence: contributions.reduce((sum, match) => sum + Number(match.opponentStrengthConfidence || 0), 0) / contributions.length,
        metrics: averageRoleMetrics(contributions),
      };
    })
    .sort((left, right) => Number(left.startTime || 0) - Number(right.startTime || 0));
}

function buildFantasyRoleRows(players) {
  const teams = new Map();
  players.forEach(player => {
    const team = teams.get(player.teamSlug) || [];
    team.push(player);
    teams.set(player.teamSlug, team);
  });
  const rows = [];
  teams.forEach(teamPlayers => fantasyRoleGroups.forEach(role => {
    const members = teamPlayers
      .filter(player => role.positions.includes(Number(player.position || 0)))
      .sort((left, right) => left.position - right.position);
    if (members.length !== role.positions.length) return;
    const roleMatches = buildFantasyRoleMatches(members);
    const summary = summarizePlayerMatches(roleMatches);
    rows.push({
      alias: `${members[0].teamSlug}-${role.key}`,
      name: members.map(member => member.name).join(" + "),
      personaName: members.map(member => member.personaName || "").filter(Boolean).join(" + "),
      teamSlug: members[0].teamSlug,
      teamName: members[0].teamName,
      teamLogoUrl: members[0].teamLogoUrl,
      roleKey: role.key,
      roleLabel: role.label,
      roleOrder: role.order,
      members,
      roleMatches,
      stats: summary.stats,
      stagePoints: Number(summary.stats.bestSeries?.points || 0),
      stability: summary.stability,
      stabilityConfidence: summary.stabilityConfidence,
      opponentStrength: summary.opponentStrength,
      opponentStrengthConfidence: summary.opponentStrengthConfidence,
      strengthAdjustedPoints: roleMatches.length
        ? roleMatches.reduce((sum, match) => sum + Number(match.fantasyPoints || 0) * Number(match.opponentStrength || 1), 0) / roleMatches.length
        : 0,
    });
  }));
  return rows;
}

function renderFantasyRoleMembers(role) {
  return `<div class="fantasy-role-members">${role.members.map(player => `
    <button type="button" class="fantasy-role-member" data-player-link="${escapeAttribute(player.alias)}" title="Открыть профиль ${escapeAttribute(player.name)}">
      ${playerImage(player, "ranking-avatar")}
      <span><strong>${escapeHTML(player.name)}</strong><small>позиция ${player.position}</small></span>
    </button>`).join("")}</div>`;
}

function renderFantasyTeamMark(role) {
  const logo = preferredTeamEmblem(role.teamSlug, role.teamLogoUrl, role.teamName);
  const fallback = escapeHTML((role.teamName || role.teamSlug || "?").slice(0, 2));
  return `<span class="fantasy-team-mark" title="${escapeAttribute(role.teamName || role.teamSlug)}">
    ${logo ? `<img src="${escapeAttribute(logo)}" alt="${escapeAttribute(role.teamName || "Логотип команды")}" loading="lazy" decoding="async">` : `<b>${fallback}</b>`}
  </span>`;
}

function renderTournamentPlayers() {
  const query = (elements.playerSearch.value || "").trim().toLowerCase();
  const role = elements.playerRole.value || "";
  const playerSource = buildFantasyRoleRows(tournamentPlayers);
  const metricSource = playerSource.find(player => player.stats?.metrics?.length);
  const metricKeys = metricSource?.stats?.metrics?.map(metric => ({
    key: `metric:${metric.key}`, label: metric.label, metric: metric.key,
  })) || [];
  let rows = playerSource.filter(player => {
    const searchable = `${player.name} ${player.personaName || ""} ${player.teamName} ${player.roleLabel}`.toLowerCase();
    return (!query || searchable.includes(query)) && (!role || player.roleKey === role);
  });
  if (playerSort.key && playerSort.direction) {
    const multiplier = playerSort.direction === "desc" ? -1 : 1;
    rows = rows.map((player, index) => ({ player, index })).sort((a, b) => {
      const left = playerSortValue(a.player, playerSort.key);
      const right = playerSortValue(b.player, playerSort.key);
      if (typeof left === "string") return multiplier * left.localeCompare(right, "ru");
      return multiplier * (Number(left || 0) - Number(right || 0)) || a.index - b.index;
    }).map(item => item.player);
  } else {
    rows = rows.map((player, index) => ({ player, index })).sort((left, right) =>
      Number(right.player.stagePoints || 0) - Number(left.player.stagePoints || 0) ||
      left.index - right.index).map(item => item.player);
  }
  const totalRows = rows.length;
  const visibleRows = playersTableExpanded ? rows : rows.slice(0, PLAYERS_PREVIEW_LIMIT);
  const columns = [
    { key: "name", label: "Состав роли" },
    { key: "position", label: "Роль" },
    { key: "matches", label: "Карт" },
    { key: "totalPoints", label: "За карту", help: "Средний счёт роли за карту. Для Cores и Supps сначала усредняются очки двух игроков." },
    { key: "stagePoints", label: "Очки за серию", help: "Сумма двух лучших карт самой результативной серии в выбранной выборке." },
    { key: "stability", label: "Стабильность", help: "0–100: насколько ровно роль набирает fantasy-очки от карты к карте. Учитываются медиана, MAD, IQR и редкие выбросы." },
    { key: "opponentStrength", label: "Сила соперников", help: "Средний коэффициент Elo соперников на выбранных картах. B — ниже 1,01; A — от 1,01 до 1,04; S — от 1,05. Число рядом показывает точный коэффициент." },
    ...metricKeys,
  ];
  elements.playersTable.innerHTML = `
    <table class="players-table">
      <thead><tr>${columns.map(column => `
        <th>${column.plain ? column.label : `<button type="button" data-player-sort="${escapeAttribute(column.key)}">
          ${column.metric ? `<span class="metric-column-label">${renderFantasyMetricIcon(column.metric, "metric-column-icon")}<span>${escapeHTML(column.label)}</span></span>` : escapeHTML(column.label)} ${column.help ? `<span class="column-help" title="${escapeAttribute(column.help)}" aria-label="${escapeAttribute(column.help)}">i</span>` : ""} ${sortMark(column.key)}
        </button>`}</th>`).join("")}</tr></thead>
      <tbody>${visibleRows.map((player, index) => `
        <tr class="fantasy-role-row">
          <td class="ranking-player fantasy-role-cell">
            <div class="fantasy-role-identity">
              <span class="fantasy-role-rank">${index + 1}</span>
              ${renderFantasyTeamMark(player)}
              ${renderFantasyRoleMembers(player)}
            </div>
          </td>
          <td><span class="position-badge compact role-name-badge">${player.roleLabel}</span></td>
          <td>${player.stats?.matches || 0}</td>
          <td class="points-cell">${formatPoints(player.stats?.totalPoints)}</td>
          <td class="points-cell stage-points-cell">${formatPoints(player.stagePoints)}</td>
          <td>${stabilityBadge(player.stability, player.stabilityConfidence, player.stats?.matches || 0)}</td>
          <td>${strengthBadge(player.opponentStrength, player.opponentStrengthConfidence)}</td>
          ${metricKeys.map(column => `<td>${formatPoints(playerSortValue(player, column.key))}</td>`).join("")}
        </tr>`).join("") || `<tr><td colspan="${columns.length}" class="empty-table">Никого не найдено</td></tr>`}
      </tbody>
    </table>`;
  renderPlayersTableToggle(totalRows);
  bindImageFallbacks(elements.playersTable);
  elements.playersTable.querySelectorAll("[data-player-sort]").forEach(button => {
    button.addEventListener("click", event => {
      event.stopPropagation();
      cyclePlayerSort(button.dataset.playerSort);
    });
  });
  elements.playersTable.querySelectorAll("[data-player-link]").forEach(button => {
    button.addEventListener("click", event => {
      event.stopPropagation();
      navigateToBase(`player/${encodeURIComponent(button.dataset.playerLink)}/overview`);
    });
  });
}

function renderPlayersTableToggle(totalRows) {
  const canExpand = totalRows > PLAYERS_PREVIEW_LIMIT;
  elements.playersTableActions.classList.toggle("hidden", !canExpand);
  if (!canExpand) {
    elements.playersTableToggle.setAttribute("aria-expanded", "false");
    return;
  }
  const hiddenCount = Math.max(0, totalRows - PLAYERS_PREVIEW_LIMIT);
  elements.playersTableToggle.setAttribute("aria-expanded", playersTableExpanded ? "true" : "false");
  elements.playersTableToggle.classList.toggle("is-expanded", playersTableExpanded);
  elements.playersTableToggle.querySelector("span").textContent = playersTableExpanded
    ? "Свернуть до 20"
    : `Показать ещё ${hiddenCount}`;
  elements.playersTableToggle.querySelector("strong").textContent = playersTableExpanded ? "↑" : "↓";
}

function togglePlayersTable() {
  const collapsing = playersTableExpanded;
  playersTableExpanded = !playersTableExpanded;
  renderTournamentPlayers();
  if (collapsing) {
    requestAnimationFrame(() => window.scrollTo({
      top: tiSectionScrollTop(elements.playersTable),
      behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth",
    }));
  }
}

function playerSortValue(player, key) {
  if (key.startsWith("metric:")) {
    return player.stats?.metrics?.find(metric => metric.key === key.slice(7))?.averagePoints || 0;
  }
  return {
    name: player.name, teamName: player.teamName, position: player.roleOrder ?? player.position,
    matches: player.stats?.matches || 0, totalPoints: player.stats?.totalPoints || 0,
    stagePoints: player.stagePoints || 0,
    strengthAdjustedPoints: player.strengthAdjustedPoints || 0,
    stability: player.stability || 0, opponentStrength: player.opponentStrength || 0,
  }[key] ?? 0;
}

function cyclePlayerSort(key) {
  if (playerSort.key !== key) playerSort = { key, direction: "desc" };
  else if (playerSort.direction === "desc") playerSort.direction = "asc";
  else if (playerSort.direction === "asc") playerSort = { key: null, direction: null };
  else playerSort = { key, direction: "desc" };
  savePlayerViewPreference();
  renderTournamentPlayers();
}

function sortMark(key) {
  if (playerSort.key !== key) return "";
  return playerSort.direction === "desc" ? "↓" : "↑";
}

function teamMiniIdentity(player) {
  const logo = preferredTeamEmblem(player.teamSlug, player.teamLogoUrl, player.teamName);
  return `<span class="ranking-team">${logo ? `<img src="${escapeAttribute(logo)}" alt="">` : ""}
    ${escapeHTML(player.teamName)}</span>`;
}

async function loadPlayer(alias, tab = "overview") {
  if (!alias) {
    navigateToBase("players", { replace: true });
    return;
  }
  elements.playerPage.innerHTML = `<div class="loading-card">Открываю профиль игрока…</div>`;
  try {
    if (!currentPlayerDetail || currentPlayerDetail.player?.alias !== alias) {
      if (PUBLIC_MODE) {
        [currentPlayerDetail] = await Promise.all([
          api(`/api/tournament-players/${encodeURIComponent(alias)}`),
          loadPublicPlayerFilterData(),
        ]);
        if (!Object.keys(publicPlayerFilterData).length) {
          publicPlayerFilterData = { [alias]: currentPlayerDetail };
          initializePublicTournamentFilter();
        }
      } else {
        currentPlayerDetail = await api(`/api/tournament-players/${encodeURIComponent(alias)}`);
      }
    }
    renderPlayerPage(currentPlayerDetail, ["overview", "chart", "matches"].includes(tab) ? tab : "overview");
  } catch (error) {
    elements.playerPage.innerHTML = `<div class="job-error">${escapeHTML(error.message)}</div>`;
  }
}

function renderPlayerPage(detail, tab) {
  const basePlayer = detail.player;
  const allMatches = Array.isArray(detail.matches) ? detail.matches : [];
  const selected = scopedPlayerMatches(allMatches);
  const summary = summarizePlayerMatches(selected);
  const player = {
    ...basePlayer,
    stats: summary.stats,
    stability: summary.stability,
    stabilityConfidence: summary.stabilityConfidence,
    opponentStrength: summary.opponentStrength,
    opponentStrengthConfidence: summary.opponentStrengthConfidence,
  };
  const heroes = favoriteHeroes(selected);
  elements.playerPage.innerHTML = `
    <section class="player-profile-hero">
      ${playerImage(player, "profile-player-photo")}
      <div class="player-profile-copy">
        <div class="profile-topline">
          <div class="profile-identity">
            <p class="eyebrow">Позиция ${player.position}</p>
            <h1>${escapeHTML(player.name)}</h1>
            ${player.personaName ? `<p class="profile-persona">${escapeHTML(player.personaName)}</p>` : ""}
            <a class="profile-team-link" href="#team/${encodeURIComponent(player.teamSlug)}">${escapeHTML(player.teamName)} <span>›</span></a>
          </div>
          ${renderFavoriteHeroes(heroes)}
        </div>
        <div class="profile-stat-strip">
          <span><small>Средние fantasy-очки</small><strong>${formatPoints(player.stats?.totalPoints)}</strong></span>
          <span><small>Карт в выборке</small><strong>${player.stats?.matches || 0}</strong></span>
          <span><small>${stabilityDisplayLabel(player.stabilityConfidence, player.stats?.matches)}</small><strong class="${stabilityDisplayClass(player.stability, player.stabilityConfidence, player.stats?.matches)}">${stabilityDisplayValue(player.stability, player.stabilityConfidence, player.stats?.matches)}</strong></span>
          <span><small>Сила соперников · ${formatNumber(player.opponentStrengthConfidence)}% уверенности</small><strong class="${strengthClass(player.opponentStrength)}">${strengthGrade(player.opponentStrength)} · ${formatNumber(player.opponentStrength)}×</strong></span>
        </div>
      </div>
    </section>
    ${typeof renderPlayerEmblemRecommendations === "function" ? renderPlayerEmblemRecommendations(player, selected) : ""}
    ${renderPlayerSelectionFilter(detail)}
    <nav class="player-tabs">
      ${playerTab(player.alias, "overview", "Статистика", tab)}
      ${playerTab(player.alias, "chart", "График по картам", tab)}
      ${playerTab(player.alias, "matches", `Матчи (${selected.length})`, tab)}
    </nav>
    <section class="player-tab-content">
      ${tab === "chart" ? renderPlayerChart(selected) : tab === "matches" ? renderPlayerMatches(selected) : renderPlayerOverview(player, selected)}
    </section>`;
  bindImageFallbacks(elements.playerPage);
  bindFavoriteHeroInteractions();
  bindPlayerSelectionFilter(detail, tab);
  elements.playerPage.querySelectorAll("[data-match]").forEach(button => {
    button.addEventListener("click", () => openMatch(Number(button.dataset.match)));
  });
  elements.playerPage.querySelectorAll("[data-record-match]").forEach(button => {
    button.addEventListener("click", () => openMatch(Number(button.dataset.recordMatch)));
  });
  bindChartTooltips(selected);
  bindSeriesOpeners(elements.playerPage, player.teamName);
}

function renderPlayerSelectionFilter(detail) {
  if (PUBLIC_MODE) return renderPublicTournamentFilterPanel({
    matches: Array.isArray(detail.matches) ? detail.matches : [],
    scope: "player",
  });
  const matches = Array.isArray(detail.matches) ? detail.matches : [];
  const leagues = leagueSummaries(matches);
  const leagueGroups = tournamentFilterGroups(leagues);
  const included = matches.filter(match => match.included).length;
  const preference = readSelectionPreference(`team:${detail.player.teamSlug}`);
  const preferredLeagues = new Set(preference?.leagueNames || []);
  const hasPreference = preferredLeagues.size > 0;
  const limit = Math.min(Math.max(1, matches.length), Math.max(1,
    Number(preference?.limit || included || Math.min(20, matches.length || 1))));
  const selectedLeagues = hasPreference
    ? preferredLeagues
    : new Set(leagues.filter(league => league.includedCount > 0).map(league => league.name));
  const allLeaguesChecked = leagueGroups.length > 0 && leagueGroups.every(group => groupIsSelected(selectedLeagues, group));
  return `
    <section class="selection-panel player-selection-panel">
      <div class="selection-title-row">
        <div><p class="eyebrow">Выборка команды</p><h2>Матчи для статистики ${escapeHTML(detail.player.teamName)}</h2></div>
        <span class="selection-current-count"><strong>${included}</strong> из ${matches.length} карт</span>
      </div>
      <div class="league-filters">
        ${renderSelectAllLeagues("player", allLeaguesChecked)}
        ${leagueGroups.map(group => `<label>
          <input type="checkbox" data-player-league data-league-names="${encodeLeagueNames(group.names)}"
            ${groupIsSelected(selectedLeagues, group) ? "checked" : ""}>
          <span>${escapeHTML(group.name)}</span><small>${group.matchCount}</small>
        </label>`).join("")}
      </div>
      ${renderMatchLimitControl("player", matches.length, limit)}
      <div class="selection-apply-row">
        <small>Единая выборка для всех игроков ${escapeHTML(detail.player.teamName)}</small>
        <button type="button" class="primary-button" data-apply-player-selection disabled>Применить выбор</button>
      </div>
    </section>`;
}

function bindPlayerSelectionFilter(detail, tab) {
  if (PUBLIC_MODE) {
    bindPublicTournamentFilter(elements.playerPage);
    return;
  }
  bindGenericLimitControl(elements.playerPage, "player");
  bindSelectAllLeagues(elements.playerPage, "player", "[data-player-league]", markPlayerSelectionDirty);
  elements.playerPage.querySelectorAll("[data-player-league]").forEach(input => {
    input.addEventListener("change", () => {
      syncSelectAllLeagues(elements.playerPage, "player", "[data-player-league]");
      markPlayerSelectionDirty();
    });
  });
  elements.playerPage.querySelector("[data-apply-player-selection]")?.addEventListener("click", async () => {
    const button = elements.playerPage.querySelector("[data-apply-player-selection]");
    const leagueNames = [...new Set([...elements.playerPage.querySelectorAll("[data-player-league]:checked")]
      .flatMap(input => leagueNamesFromInput(input)))];
    if (!leagueNames.length) {
      alert("Выбери хотя бы один турнир.");
      return;
    }
    const range = elements.playerPage.querySelector('[data-match-limit="player"]');
    button.disabled = true;
    button.textContent = "Применяю…";
    try {
      await api(`/api/teams/${encodeURIComponent(detail.player.teamSlug)}/selection`, {
        method: "POST",
        body: JSON.stringify({ leagueNames, limit: Number(range?.value || 1), all: false }),
      });
      saveSelectionPreference(`team:${detail.player.teamSlug}`, leagueNames, Number(range?.value || 1));
      tournamentPlayers = [];
      globalSelectionOverview = null;
      currentTeam = null;
      currentPlayerDetail = null;
      await loadPlayer(detail.player.alias, tab);
    } catch (error) {
      alert(error.message);
      button.disabled = false;
      button.textContent = "Применить выбор";
    }
  });
}

function markPlayerSelectionDirty() {
  const button = elements.playerPage?.querySelector("[data-apply-player-selection]");
  if (button) button.disabled = false;
}

function scopeButton(scope, label) {
  return `<button type="button" data-player-scope="${scope}" class="${playerMatchScope === scope ? "active" : ""}">${label}</button>`;
}

function isMainTI(match) {
  const league = String(match.leagueName || "").toLowerCase();
  return league.includes("the international") && !league.includes("qualif");
}

function scopedPlayerMatches(matches) {
  if (PUBLIC_MODE) return publicFilteredMatches(matches);
  const tiStart = matches.filter(isMainTI).reduce((earliest, match) =>
    earliest === 0 ? Number(match.startTime || 0) : Math.min(earliest, Number(match.startTime || 0)), 0);
  return matches.filter(match => {
    if (!match.included) return false;
    if (playerMatchScope === "ti") return isMainTI(match);
    if (playerMatchScope === "pre-ti") return tiStart ? Number(match.startTime || 0) < tiStart : !isMainTI(match);
    return true;
  });
}

function summarizePlayerMatches(matches) {
  if (!matches.length) {
    return {
      stats: { matches: 0, totalPoints: 0, metrics: [], bestMatch: {}, bestSeries: {} },
      stability: 0,
      stabilityConfidence: 0,
      opponentStrength: 0,
      opponentStrengthConfidence: 0,
    };
  }
  const metricMap = new Map();
  matches.forEach(match => (match.metrics || []).forEach(metric => {
    const item = metricMap.get(metric.key) || { key: metric.key, label: metric.label, average: 0, averagePoints: 0 };
    item.average += Number(metric.average || 0);
    item.averagePoints += Number(metric.averagePoints || 0);
    metricMap.set(metric.key, item);
  }));
  const metrics = [...metricMap.values()].map(metric => ({
    ...metric,
    average: metric.average / matches.length,
    averagePoints: metric.averagePoints / matches.length,
  }));
  const totalPoints = matches.reduce((sum, match) => sum + Number(match.fantasyPoints || 0), 0) / matches.length;
  const best = [...matches].sort((a, b) => b.fantasyPoints - a.fantasyPoints)[0];
  const series = new Map();
  matches.forEach(match => {
    const key = match.seriesId || match.matchId;
    const record = series.get(key) || { games: [] };
    record.games.push({ points: Number(match.fantasyPoints || 0), matchId: match.matchId });
    series.set(key, record);
  });
  const bestSeries = [...series.values()].map(record => {
    const games = record.games.sort((left, right) => right.points - left.points).slice(0, 2);
    return {
      points: games.reduce((sum, game) => sum + game.points, 0),
      matchIds: games.map(game => game.matchId),
    };
  }).sort((a, b) => b.points - a.points)[0] || {};
  const stability = calculatePlayerStability(matches.map(match => Number(match.fantasyPoints || 0)));
  const stabilityConfidence = sampleConfidence(matches.length, 6);
  const opponentStrength = matches.reduce((sum, match) => sum + Number(match.opponentStrength || 0), 0) / matches.length;
  const opponentStrengthConfidence = matches.reduce((sum, match) =>
    sum + Number(match.opponentStrengthConfidence || 0), 0) / matches.length;
  return {
    stats: {
      matches: matches.length, totalPoints, metrics,
      bestMatch: { points: best.fantasyPoints, matchIds: [best.matchId] },
      bestSeries,
    },
    stability,
    stabilityConfidence,
    opponentStrength,
    opponentStrengthConfidence,
  };
}

function calculatePlayerStability(points) {
  if (points.length < 2) return 0;
  const sorted = [...points].sort((left, right) => left - right);
  const center = sortedQuantile(sorted, .5);
  const deviations = sorted.map(value => Math.abs(value - center)).sort((left, right) => left - right);
  const madSigma = 1.4826 * sortedQuantile(deviations, .5);
  const iqrSigma = (sortedQuantile(sorted, .75) - sortedQuantile(sorted, .25)) / 1.349;
  const robustSigma = Math.max(madSigma, iqrSigma);
  const mean = sorted.reduce((sum, value) => sum + value, 0) / sorted.length;
  const deviation = Math.sqrt(sorted.reduce((sum, value) => sum + (value - mean) ** 2, 0) / sorted.length);
  const dispersion = .8 * robustSigma + .2 * deviation;
  return 100 / (1 + 2 * dispersion / Math.max(Math.abs(center), 1));
}

function sortedQuantile(values, probability) {
  if (!values.length) return 0;
  const position = (values.length - 1) * probability;
  const lower = Math.floor(position);
  const upper = Math.ceil(position);
  if (lower === upper) return values[lower];
  const weight = position - lower;
  return values[lower] * (1 - weight) + values[upper] * weight;
}

function sampleConfidence(count, scale) {
  return 100 * (1 - Math.exp(-Number(count || 0) / scale));
}

function favoriteHeroes(matches) {
  const heroes = new Map();
  matches.forEach(match => {
    const item = heroes.get(match.heroId) || { heroId: match.heroId, matches: 0, points: 0 };
    item.matches += 1;
    item.points += Number(match.fantasyPoints || 0);
    heroes.set(match.heroId, item);
  });
  return [...heroes.values()]
    .map(item => ({ ...item, averagePoints: item.points / item.matches }))
    .sort((a, b) => b.matches - a.matches || b.averagePoints - a.averagePoints)
    .slice(0, 5);
}

function renderFavoriteHeroes(heroes) {
  if (!heroes.length) return `<div class="favorite-heroes empty"><span>Нет выбранных карт</span></div>`;
  return `<div class="favorite-heroes">
    <p>Частые герои</p>
    <div class="favorite-hero-stack">
      ${heroes.map((hero, index) => `<article class="favorite-hero ${index === 0 ? "active" : ""}" data-favorite-hero>
        ${heroImage(hero.heroId) ? `<img src="${escapeAttribute(heroImage(hero.heroId))}" alt="${escapeAttribute(heroName(hero.heroId))}">` : ""}
        <div><strong>${escapeHTML(heroName(hero.heroId))}</strong><span>${hero.matches} карт · ${formatPoints(hero.averagePoints)}</span></div>
      </article>`).join("")}
    </div>
  </div>`;
}

function bindFavoriteHeroInteractions() {
  const stack = elements.playerPage.querySelector(".favorite-hero-stack");
  if (!stack) return;
  const cards = [...stack.querySelectorAll("[data-favorite-hero]")];
  cards.forEach(card => card.addEventListener("mouseenter", () => {
    stack.classList.add("has-hover");
    cards.forEach(item => item.classList.toggle("active", item === card));
  }));
  stack.addEventListener("mouseleave", () => {
    stack.classList.remove("has-hover");
    cards.forEach((item, index) => item.classList.toggle("active", index === 0));
  });
}

function playerTab(alias, key, label, active) {
  return `<a href="#player/${encodeURIComponent(alias)}/${key}" class="${key === active ? "active" : ""}">${label}</a>`;
}

function renderPlayerOverview(player, matches = []) {
  const metrics = player.stats?.metrics || [];
  return `
    <div class="player-insight-grid">
      <article><span>Рекорд за карту</span><strong>${formatPoints(player.stats?.bestMatch?.points)}</strong>
        <small>${formatRecordMatches(player.stats?.bestMatch)}</small></article>
      <article><span>Лучшая серия</span><strong>${formatPoints(player.stats?.bestSeries?.points)}</strong>
        <small>${formatBestSeriesRecord(player.stats?.bestSeries, player.teamName)}</small></article>
    </div>
    <div class="fantasy-breakdown profile-breakdown">
      <div class="fantasy-breakdown-head"><span>Действие</span><span>Среднее</span><span>Очки за карту</span></div>
      ${metrics.map(metric => `<div class="fantasy-breakdown-row">
        <strong class="metric-profile-label">${renderFantasyMetricIcon(metric.key, "metric-profile-icon")}<span>${escapeHTML(metric.label)}</span></strong><span>${formatMetricAverage(metric)}</span>
        <b>${metric.averagePoints >= 0 ? "+" : ""}${formatPoints(metric.averagePoints)}</b>
      </div>`).join("")}
    </div>`;
}

function renderPlayerChart(matches) {
  if (!matches.length) return `<div class="empty-state team-empty"><h3>Нет выбранных карт</h3><p>Выбери матчи на странице команды.</p></div>`;
  const width = 1080, height = 430, padX = 58, padY = 42;
  const values = matches.map(match => Number(match.fantasyPoints || 0));
  const min = Math.min(...values), max = Math.max(...values);
  const span = Math.max(max - min, 1);
  const x = index => padX + index * ((width - padX * 2) / Math.max(matches.length - 1, 1));
  const y = value => height - padY - ((value - min) / span) * (height - padY * 2);
  const series = new Map();
  matches.forEach((match, index) => {
    const key = String(match.seriesId || match.matchId);
    if (!series.has(key)) series.set(key, []);
    series.get(key).push({ match, index });
  });
  const colors = ["#dfbb70", "#df4b3f", "#71c79b", "#7ca9d8", "#c28ad8", "#e29a62"];
  const seriesColor = new Map([...series.keys()].map((key, index) => [key, colors[index % colors.length]]));
  return `
    <div class="chart-card">
      <div class="chart-heading"><div><p class="eyebrow">Отдельные карты</p><h2>Fantasy-очки по матчам</h2></div>
        <p>Одинаковый цвет и подложка объединяют карты одной BO-серии.</p></div>
      <div class="fantasy-chart-scroll"><svg class="fantasy-chart" viewBox="0 0 ${width} ${height}" role="img">
        ${[0, .25, .5, .75, 1].map(step => {
          const value = min + span * step, lineY = y(value);
          return `<line x1="${padX}" y1="${lineY}" x2="${width-padX}" y2="${lineY}" class="chart-grid"/>
            <text x="${padX-10}" y="${lineY+4}" class="chart-axis">${formatPoints(value)}</text>`;
        }).join("")}
        ${[...series.entries()].map(([key, points]) => {
          const first = points[0], last = points[points.length - 1], color = seriesColor.get(key);
          const left = Math.max(padX, x(first.index) - 16), right = Math.min(width-padX, x(last.index) + 16);
          return `<rect x="${left}" y="${padY-12}" width="${Math.max(right-left, 12)}" height="${height-padY*2+24}"
            rx="12" fill="${color}" class="series-band"/>`;
        }).join("")}
        <polyline points="${matches.map((match, index) => `${x(index)},${y(match.fantasyPoints)}`).join(" ")}" class="chart-line"/>
        ${matches.map((match, index) => {
          const color = seriesColor.get(String(match.seriesId || match.matchId));
          return `<g class="chart-point" data-match="${match.matchId}" data-chart-match="${match.matchId}">
            <circle cx="${x(index)}" cy="${y(match.fantasyPoints)}" r="7" fill="${color}" class="${match.won ? "won" : "lost"}"/>
          </g>`;
        }).join("")}
      </svg></div>
      <div class="chart-tooltip hidden"></div>
      <div class="chart-series-legend">${[...series.entries()].map(([key, points]) =>
        `<span><i style="background:${seriesColor.get(key)}"></i>${seriesLabel({seriesType: points[0].match.seriesType, matches: points})} · ${escapeHTML(points[0].match.opponentName || "Соперник")}</span>`
      ).join("")}</div>
    </div>`;
}

function renderPlayerMatches(matches) {
  if (!matches.length) return `<div class="empty-state team-empty"><h3>Матчи ещё не распарсены</h3></div>`;
  const groups = groupMatchesBySeries(matches);
  registerSeriesGroups(groups, currentPlayerDetail?.player?.teamName || "Команда");
  return `<div class="player-match-series">${groups.map(group => `
    <section class="player-series-card">
      ${renderSeriesSummaryButton(group, { className: "player-series-head", winField: "won" })}
      <div class="player-series-maps">
      ${group.matches.map((match, index) => `<button type="button" data-match="${match.matchId}" class="player-match-button ${match.included ? "" : "excluded"}">
        <span class="result-mark ${match.won ? "win" : "loss"}">${match.won ? "W" : "L"}</span>
        <span class="player-map-index"><strong>Карта ${index + 1}</strong><small>#${match.matchId}</small></span>
        <span class="player-map-hero">${heroImage(match.heroId) ? `<img src="${escapeAttribute(heroImage(match.heroId))}" alt="">` : ""}<span><strong>${escapeHTML(heroName(match.heroId))}</strong><small>${formatDate(match.startTime)}</small></span></span>
        <span class="player-map-points"><small>Fantasy</small><b>${formatPoints(match.fantasyPoints)}</b></span>
        <span class="player-map-strength"><small>Соперники</small><em class="${strengthClass(match.opponentStrength)}">${strengthGrade(match.opponentStrength)} · ${formatNumber(match.opponentStrength)}×</em></span>
        ${renderFantasyMetricHighlights(match.metrics, 3)}
        <span class="series-chevron" aria-hidden="true">›</span>
      </button>`).join("")}
      </div>
    </section>`).join("")}</div>`;
}

function bindChartTooltips(matches) {
  const card = elements.playerPage.querySelector(".chart-card");
  const tooltip = card?.querySelector(".chart-tooltip");
  if (!card || !tooltip) return;
  const byId = new Map(matches.map(match => [Number(match.matchId), match]));
  card.querySelectorAll("[data-chart-match]").forEach(point => {
    const target = point.querySelector("circle") || point;
    target.addEventListener("mouseenter", event => {
      const match = byId.get(Number(point.dataset.chartMatch));
      if (!match) return;
      const metrics = [...(match.metrics || [])].sort((a, b) => b.averagePoints - a.averagePoints);
      tooltip.innerHTML = `
        <div class="chart-tooltip-head">
          ${heroImage(match.heroId) ? `<img src="${escapeAttribute(heroImage(match.heroId))}" alt="">` : ""}
          <span><strong>${escapeHTML(heroName(match.heroId))}</strong>
          <small>${match.won ? "Победа" : "Поражение"} · ${seriesLabel({ seriesType: match.seriesType, matches: [match] })}</small></span>
          <b>${formatPoints(match.fantasyPoints)}</b>
        </div>
        <p>${escapeHTML(match.opponentName || "Неизвестный соперник")} · ${escapeHTML(match.leagueName || "Турнир не указан")}</p>
        <div class="chart-tooltip-metrics">${metrics.map(metric => `<span>
          <small class="metric-inline-label">${renderFantasyMetricIcon(metric.key, "metric-inline-icon")}${escapeHTML(metric.label)}</small><b>${metric.averagePoints >= 0 ? "+" : ""}${formatPoints(metric.averagePoints)}</b>
        </span>`).join("")}</div>`;
      tooltip.classList.remove("hidden");
      positionChartTooltip(event, card, tooltip);
    });
    target.addEventListener("mousemove", event => positionChartTooltip(event, card, tooltip));
    target.addEventListener("mouseleave", () => tooltip.classList.add("hidden"));
  });
}

function positionChartTooltip(event, card, tooltip) {
  const rect = card.getBoundingClientRect();
  const left = Math.min(Math.max(event.clientX - rect.left + 16, 10), rect.width - tooltip.offsetWidth - 10);
  const top = Math.min(Math.max(event.clientY - rect.top + 16, 10), rect.height - tooltip.offsetHeight - 10);
  tooltip.style.left = `${left}px`;
  tooltip.style.top = `${top}px`;
}

function playerAlias(name) {
  return String(name || "").trim().toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, "-").replace(/^-+|-+$/g, "");
}

function openPlayer(player, teamName) {
  const stats = player.stats || { matches: 0, totalPoints: 0, metrics: [] };
  const metrics = Array.isArray(stats.metrics) ? stats.metrics : [];
  elements.playerDialogTitle.textContent = player.name;
  elements.playerDialogMeta.textContent =
    `${teamName} · позиция ${player.position} · ${stats.matches} матчей текущего состава`;
  elements.playerDialogContent.innerHTML = `
    ${playerImage(player, "player-dialog-avatar")}
    <div class="player-total-panel">
      <span>Средняя сумма fantasy-очков</span>
      <strong>${formatPoints(stats.totalPoints)}</strong>
    </div>
    <div class="record-grid">
      <div><span>Лучший матч</span><strong>${formatPoints(stats.bestMatch?.points || 0)}</strong>
        <small>${formatRecordMatches(stats.bestMatch)}</small></div>
      <div><span>Лучшая BO-серия</span><strong>${formatPoints(stats.bestSeries?.points || 0)}</strong>
        <small>${formatRecordMatches(stats.bestSeries)}</small></div>
    </div>
    <div class="fantasy-breakdown">
      <div class="fantasy-breakdown-head">
        <span>Действие</span><span>Среднее</span><span>Средние очки</span>
      </div>
      ${metrics.map(metric => `
        <div class="fantasy-breakdown-row">
          <strong class="metric-profile-label">${renderFantasyMetricIcon(metric.key, "metric-profile-icon")}<span>${escapeHTML(metric.label)}</span></strong>
          <span>${formatMetricAverage(metric)}</span>
          <b>${metric.averagePoints >= 0 ? "+" : ""}${formatPoints(metric.averagePoints)}</b>
        </div>
      `).join("")}
    </div>`;
  bindImageFallbacks(elements.playerDialogContent);
  elements.playerDialogContent.querySelectorAll("[data-record-match]").forEach(button => {
    button.addEventListener("click", () => {
      elements.playerDialog.close();
      openMatch(Number(button.dataset.recordMatch));
    });
  });
  elements.playerDialog.showModal();
}

function playerImage(player, className, loading = "lazy") {
  const primary = PUBLIC_MODE ? player.portraitUrl : (player.portraitUrl || player.avatarUrl);
  if (!primary) {
    return `<div class="${className} player-avatar-placeholder">Портрет пока не найден</div>`;
  }
  const fallback = !PUBLIC_MODE && player.portraitUrl && player.avatarUrl && player.portraitUrl !== player.avatarUrl
    ? ` data-fallback="${escapeAttribute(player.avatarUrl)}"`
    : "";
  return `<img class="${className}" src="${escapeAttribute(primary)}"${fallback} alt="" loading="${loading}" decoding="async">`;
}

function bindImageFallbacks(root) {
  root?.querySelectorAll("img[data-fallback]").forEach(image => {
    image.addEventListener("error", () => {
      const fallback = image.dataset.fallback;
      if (!fallback || image.src === fallback) return;
      image.removeAttribute("data-fallback");
      image.src = fallback;
    }, { once: true });
  });
}

function formatRecordMatches(record) {
  const ids = Array.isArray(record?.matchIds) ? record.matchIds : [];
  return ids.length
    ? `<span class="record-links">${ids.map(id => `<button type="button" data-record-match="${id}">#${id}</button>`).join("")}</span>`
    : "Нет выбранных матчей";
}

function formatBestSeriesRecord(record, teamName) {
  const ids = Array.isArray(record?.matchIds) ? record.matchIds.map(Number) : [];
  const matches = (currentPlayerDetail?.matches || []).filter(match => ids.includes(Number(match.matchId)));
  if (!matches.length) return "Нет выбранной серии";
  const group = {
    matches,
    seriesType: matches[0].seriesType,
    opponentName: matches[0].opponentName,
    leagueName: matches[0].leagueName,
    startTime: Math.max(...matches.map(match => Number(match.startTime || 0))),
  };
  return `<button type="button" class="best-series-link"
    data-series-matches="${ids.join(",")}"
    data-series-label="${seriesLabel(group)}"
    data-series-opponent="${escapeAttribute(group.opponentName || "Неизвестная команда")}"
    data-series-league="${escapeAttribute(group.leagueName || "Турнир не указан")}"
    data-series-date="${group.startTime}"
    data-series-wins="${matches.filter(match => match.won).length}"
    data-series-losses="${matches.filter(match => !match.won).length}">
    Открыть ${seriesLabel(group)} · ${escapeHTML(group.opponentName || "Соперник")}
  </button>`;
}

function formatMetricAverage(metric) {
  if (metric.key === "teamfightParticipation") return `${formatNumber(metric.average * 100)}%`;
  return formatNumber(metric.average);
}

function renderFantasyMetricIcon(metric, className = "") {
  return globalThis.FantasyAssets?.metricIcon(metric, className) || "";
}

function renderFantasyMetricHighlights(metrics, limit = 3) {
  const highlights = [...(metrics || [])]
    .filter(metric => globalThis.FantasyAssets?.metrics?.[metric.key])
    .sort((left, right) => Math.abs(Number(right.averagePoints || 0)) - Math.abs(Number(left.averagePoints || 0)))
    .slice(0, limit);
  if (!highlights.length) return "";
  return `<span class="player-map-metrics" aria-label="Главные источники очков">${highlights.map(metric => `
    <i title="${escapeAttribute(metric.label)}: ${formatSignedMetricPoints(metric.averagePoints)}">
      ${renderFantasyMetricIcon(metric.key, "player-map-metric-icon")}
      <b>${formatSignedMetricPoints(metric.averagePoints)}</b>
    </i>`).join("")}</span>`;
}

function formatSignedMetricPoints(value) {
  const number = Number(value || 0);
  return `${number > 0 ? "+" : ""}${formatPoints(number)}`;
}

function parseStatusLabel(status) {
  return {
    done: "В базе",
    pending: "Ожидает",
    parsing: "Парсится",
    unavailable: "Нет replay",
    error: "Ошибка",
  }[status] || status;
}

function sum(items, field) {
  return items.reduce((total, item) => total + Number(item[field] || 0), 0);
}

function formatDate(timestamp) {
  if (!timestamp) return "Дата неизвестна";
  return new Intl.DateTimeFormat("ru-RU", {
    day: "2-digit", month: "short", year: "numeric",
    hour: "2-digit", minute: "2-digit",
  }).format(new Date(timestamp * 1000));
}

function formatDateTime(timestamp) {
  const value = Number(timestamp || 0);
  if (!value) return "не запланирована";
  return new Intl.DateTimeFormat("ru-RU", {
    day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit",
  }).format(new Date(value * 1000));
}

function formatDuration(seconds) {
  const value = Number(seconds || 0);
  return `${Math.floor(value / 60)}:${String(Math.floor(value % 60)).padStart(2, "0")}`;
}

function formatNumber(value) {
  return new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 2 }).format(Number(value || 0));
}

function formatPoints(value) {
  return Number(value || 0).toLocaleString("ru-RU", {
    useGrouping: false,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

function stabilityClass(value) {
  const score = Number(value || 0);
  if (score < 50) return "stability-low";
  if (score < 65) return "stability-medium";
  return "stability-high";
}

function hasReliableStability(confidence, matches) {
  return Number(matches || 0) >= 3 && Number(confidence || 0) >= 30;
}

function stabilityDisplayClass(value, confidence, matches) {
  return hasReliableStability(confidence, matches) ? stabilityClass(value) : "rating-unknown";
}

function stabilityDisplayValue(value, confidence, matches) {
  return hasReliableStability(confidence, matches) ? formatNumber(value) : "—";
}

function stabilityDisplayLabel(confidence, matches) {
  if (!hasReliableStability(confidence, matches)) return "стабильность · нужно 3 карты";
  return `стабильность · уверенность ${formatNumber(confidence)}%`;
}

function stabilityBadge(value, confidence, matches) {
  const available = hasReliableStability(confidence, matches);
  const title = available
    ? `Робастная стабильность по ${matches} картам · уверенность ${formatNumber(confidence)}%`
    : `Недостаточно данных: ${matches} из 3 карт`;
  return `<strong class="rating-value ${available ? stabilityClass(value) : "rating-unknown"}" title="${escapeAttribute(title)}">${available ? formatNumber(value) : "—"}</strong><small class="cell-unit">${available ? "/100" : "мало карт"}</small>`;
}

function strengthGrade(value) {
  const strength = Number(value || 0);
  if (strength >= 1.05) return "S";
  if (strength >= 1.01) return "A";
  return "B";
}

function strengthClass(value) {
  return `strength-${strengthGrade(value).toLowerCase()}`;
}

function strengthBadge(value, confidence) {
  const title = `Elo по реальным результатам соперников · уверенность ${formatNumber(confidence)}%`;
  return `<strong class="rating-value ${strengthClass(value)}" title="${escapeAttribute(title)}">${strengthGrade(value)} · ${formatNumber(value)}</strong><small class="cell-unit">×</small>`;
}

function escapeHTML(value) {
  return String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#039;");
}

function escapeAttribute(value) {
  return escapeHTML(value);
}

function sleep(milliseconds) {
  return new Promise(resolve => setTimeout(resolve, milliseconds));
}

if (!PUBLIC_MODE) {
  elements.form.addEventListener("submit", startParse);
  if (elements.replayDropzone && elements.replayFile) {
    elements.replayDropzone.addEventListener("click", () => elements.replayFile.click());
    elements.replayDropzone.addEventListener("keydown", event => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        elements.replayFile.click();
      }
    });
    elements.replayFile.addEventListener("change", () => uploadReplayFile(elements.replayFile.files?.[0]));
    ["dragenter", "dragover"].forEach(type => {
      elements.replayDropzone.addEventListener(type, event => {
        event.preventDefault();
        elements.replayDropzone.classList.add("drag-over");
      });
    });
    ["dragleave", "drop"].forEach(type => {
      elements.replayDropzone.addEventListener(type, event => {
        event.preventDefault();
        if (type === "drop") uploadReplayFile(event.dataTransfer?.files?.[0]);
        elements.replayDropzone.classList.remove("drag-over");
      });
    });
  }
  elements.refreshButton.addEventListener("click", loadMatches);
  elements.refreshTeamsButton.addEventListener("click", () => loadTeams(true));
  elements.syncAllTeamsButton.addEventListener("click", syncAllTeams);
}
elements.backToTeams.addEventListener("click", () => navigateBackOr("teams"));
elements.tiSectionSwitcher.addEventListener("click", () => {
  navigateToTISection(elements.tiSectionSwitcher.dataset.target || "players");
});
elements.playerSearch.addEventListener("input", () => {
  savePlayerViewPreference();
  renderTournamentPlayers();
});
elements.playerRole.addEventListener("change", () => {
  savePlayerViewPreference();
  renderTournamentPlayers();
});
elements.playersTableToggle.addEventListener("click", togglePlayersTable);
elements.backFromPlayer.addEventListener("click", () => navigateBackOr("players"));
elements.closeDialog.addEventListener("click", closeOverlay);
elements.backToSeries.addEventListener("click", closeOverlay);
elements.closePlayerDialog.addEventListener("click", () => elements.playerDialog.close());
elements.closeSeriesDialog.addEventListener("click", closeOverlay);
[elements.dialog, elements.playerDialog, elements.seriesDialog].forEach(dialog => {
  dialog.addEventListener("click", event => {
    if (event.target === dialog) {
      if (dialog === elements.playerDialog) dialog.close();
      else closeOverlay();
    }
  });
  dialog.addEventListener("cancel", event => {
    if (dialog === elements.playerDialog) return;
    event.preventDefault();
    closeOverlay();
  });
});
elements.brand?.addEventListener("click", event => {
  event.preventDefault();
  navigateToBase(DEFAULT_ROUTE);
});
elements.navLinks.forEach(link => link.addEventListener("click", event => {
  event.preventDefault();
  navigateToBase(link.dataset.route || DEFAULT_ROUTE);
}));
window.addEventListener("hashchange", () => {
  saveScrollPosition(currentBaseRoute);
  route();
});
window.addEventListener("scroll", () => {
  if (tiScrollFrame) return;
  tiScrollFrame = requestAnimationFrame(() => {
    tiScrollFrame = 0;
    updateTISectionFromScroll();
  });
}, { passive: true });
window.addEventListener("resize", () => requestAnimationFrame(updateTISectionFromScroll));
window.addEventListener("beforeunload", () => saveScrollPosition(currentBaseRoute));

applyRuntimeMode();
restorePlayerViewPreference();
checkConnection();
route();
loadHeroNames().finally(route);
let connectionTimer = 0;
function scheduleConnectionCheck() {
  window.clearTimeout(connectionTimer);
  if (window.DOTA_HUB_STATIC_API === true) return;
  const interval = document.hidden ? 120000 : (PUBLIC_MODE ? 60000 : 10000);
  connectionTimer = window.setTimeout(async () => {
    await checkConnection();
    scheduleConnectionCheck();
  }, interval);
}
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) checkConnection();
  scheduleConnectionCheck();
});
scheduleConnectionCheck();
