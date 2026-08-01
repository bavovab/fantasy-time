(() => {
  if (window.DOTA_HUB_MODE !== "public") return;

  const view = document.querySelector("#liveView");
  if (!view) return;

  const elements = {
    view,
    nav: document.querySelector('.main-nav a[data-route="live"]'),
    heading: document.querySelector("#liveTitle"),
    notice: document.querySelector("#liveNotice"),
    updated: document.querySelector("#liveUpdated"),
    scheduleTitle: document.querySelector("#liveScheduleTitle"),
    schedule: document.querySelector("#liveSchedule"),
    groups: document.querySelector("#liveGroups"),
    tiebreakers: document.querySelector("#liveTiebreakers"),
    bracket: document.querySelector("#liveBracket"),
    tracker: document.querySelector("#liveTracker"),
    back: document.querySelector("#liveBack"),
    title: document.querySelector("#liveTrackerTitle"),
    state: document.querySelector("#liveTrackerState"),
    message: document.querySelector("#liveTrackerMessage"),
    clock: document.querySelector("#liveGameClock"),
    layout: document.querySelector("#liveTracker .live-tracker-layout"),
    mapSize: document.querySelector("#liveMapSize"),
    map: document.querySelector("#liveMap"),
    scoreboard: document.querySelector("#liveScoreboard"),
    radiantName: document.querySelector("#liveRadiantName"),
    direName: document.querySelector("#liveDireName"),
    radiantPlayers: document.querySelector("#liveRadiantPlayers"),
    direPlayers: document.querySelector("#liveDirePlayers"),
  };

  const stateText = {
    not_started: "Матч не начался",
    queued: "Ожидает свободного наблюдателя",
    live: "Идёт в SourceTV",
    dota_tv_unavailable: "Трансляция Valve временно недоступна",
    finished: "Матч завершён",
    scheduled: "Запланирован",
  };

  const heroNodes = new Map();
  const trailNodes = new Map();
  const movementHistory = new Map();
  const lastDamageSequence = new Map();
  let overview = null;
  let selectedMatch = null;
  let heroCatalog = null;
  let refreshTimer = 0;
  let lastOverviewFetch = 0;
  let requestInFlight = false;
  let mapLayers = null;

  function setMapExpanded(expanded) {
    const value = Boolean(expanded);
    elements.layout?.classList.toggle("map-expanded", value);
    if (elements.mapSize) {
      elements.mapSize.setAttribute("aria-pressed", String(value));
      elements.mapSize.textContent = value ? "Сделать компактной" : "Увеличить карту";
    }
  }

  function api(path) {
    const url = window.DOTA_HUB_STATIC_API === true
      ? `${String(path).replace(/^\/+/, "")}.json`
      : path;
    return fetch(url, { credentials: "omit", cache: "no-store" }).then(async response => {
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(body.error || `HTTP ${response.status}`);
      return body;
    });
  }

  async function ensureHeroCatalog() {
    if (heroCatalog) return heroCatalog;
    try {
      const heroes = await api("/api/heroes");
      heroCatalog = new Map(Object.entries(heroes || {}).map(([id, hero]) => [String(id), hero]));
    } catch {
      heroCatalog = new Map();
    }
    return heroCatalog;
  }

  function heroInfo(player) {
    const hero = heroCatalog?.get(String(player?.heroId || "")) || {};
    return { name: player?.heroName || hero.name || "Герой", imageUrl: player?.heroImageUrl || hero.imageUrl || "" };
  }

  function asMoscowDate(value) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "—";
    return `${new Intl.DateTimeFormat("ru-RU", {
      day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit", timeZone: "Europe/Moscow",
    }).format(date)} МСК`;
  }

  function clamp(value) {
    return Math.max(0, Math.min(100, Number(value) || 0));
  }

  function setRoute(route) {
    location.hash = route;
  }

  function routeMatchID() {
    return String(location.hash || "").replace(/^#live\/?/, "").split("/")[0];
  }

  function isLiveRoute() {
    return String(location.hash || "").replace(/^#/, "").startsWith("live");
  }

  function card(match) {
    const interactive = Boolean(match.matchId);
    const node = document.createElement(interactive ? "button" : "article");
    if (interactive) node.type = "button";
    node.className = `live-match-card live-state-${match.state || "not_started"}${match.interesting ? " live-interesting" : ""}${interactive ? "" : " is-static"}`;
    node.dataset.matchId = match.id || "";
    if (match.interesting) {
      node.dataset.tooltip = match.interestReason || "Интересный матч: ожидается особенно напряжённая игра";
      node.setAttribute("aria-label", `${match.radiant?.name || "TBD"} — ${match.dire?.name || "TBD"}. Интересный матч`);
    }
    const teams = document.createElement("strong");
    teams.textContent = `${match.radiant?.name || "TBD"} — ${match.dire?.name || "TBD"}`;
    const meta = document.createElement("span");
    const details = [];
    if (match.state === "live" && match.score) {
      details.push(`Карта ${Number(match.score.radiant || 0)} : ${Number(match.score.dire || 0)}`);
    }
    if (match.gameTimeSeconds != null) details.push(formatClock(match.gameTimeSeconds));
    if (match.spectators) details.push(`${Number(match.spectators).toLocaleString("ru-RU")} зрителей`);
    if (!details.length) details.push(match.scheduledAt ? asMoscowDate(match.scheduledAt) : `BO${Number(match.bestOf) || 3}`);
    if (match.bestOf && !details.some(value => value.includes(`BO${match.bestOf}`))) details.push(`BO${match.bestOf}`);
    meta.textContent = details.join(" · ");
    const status = document.createElement("em");
    status.textContent = match.label || stateText[match.state] || "Ожидание";
    node.append(teams, meta, status);
    if (interactive) node.addEventListener("click", () => openMatch(match.id));
    return node;
  }

  function seriesRow(match) {
    const interactive = Boolean(match.matchId);
    const node = document.createElement(interactive ? "button" : "article");
    if (interactive) node.type = "button";
    node.className = `live-series-row${match.state === "live" ? " is-live" : ""}`;
    const date = document.createElement("time");
    date.dateTime = match.scheduledAt || "";
    date.textContent = match.scheduledAt ? asMoscowDate(match.scheduledAt).replace(" МСК", "") : "Дата уточняется";
    const teams = document.createElement("strong");
    teams.textContent = `${match.radiant?.name || "TBD"} — ${match.dire?.name || "TBD"}`;
    const meta = document.createElement("span");
    meta.textContent = match.state === "live"
      ? `LIVE · карта ${Number(match.score?.radiant || 0)}:${Number(match.score?.dire || 0)}`
      : `BO${Number(match.bestOf) || 2}`;
    node.append(date, teams, meta);
    if (interactive) node.addEventListener("click", () => openMatch(match.id));
    return node;
  }

  function renderGroups() {
    elements.groups.replaceChildren();
    const groups = Array.isArray(overview?.groups) ? overview.groups : [];
    for (const group of groups) {
      const panel = document.createElement("section");
      panel.className = "live-group";
      const header = document.createElement("header");
      const title = document.createElement("h3");
      title.textContent = group.name || "Группа";
      const count = document.createElement("span");
      count.textContent = `${(group.teams || []).length} команд`;
      header.append(title, count);
      const teams = document.createElement("ol");
      teams.className = "live-group-teams";
      (group.teams || []).forEach(teamName => {
        const item = document.createElement("li");
        item.textContent = teamName;
        teams.append(item);
      });
      const schedule = document.createElement("div");
      schedule.className = "live-group-schedule";
      (group.matches || []).forEach(match => schedule.append(seriesRow(match)));
      panel.append(header, teams, schedule);
      elements.groups.append(panel);
    }
  }

  function renderTiebreakers() {
    elements.tiebreakers.replaceChildren();
    const data = overview?.tiebreakers || {};
    const matches = Array.isArray(data.matches) ? data.matches : [];
    if (matches.length) {
      const list = document.createElement("div");
      list.className = "live-tiebreaker-matches";
      matches.forEach(match => list.append(seriesRow(match)));
      elements.tiebreakers.append(list);
    } else {
      const empty = document.createElement("p");
      empty.className = "live-tiebreaker-empty";
      empty.textContent = "Переигровок пока нет. Они появятся здесь только после официального назначения.";
      elements.tiebreakers.append(empty);
    }
    const rules = document.createElement("ol");
    rules.className = "live-tiebreaker-rules";
    (data.rules || []).forEach(rule => {
      const item = document.createElement("li");
      item.textContent = rule;
      rules.append(item);
    });
    elements.tiebreakers.append(rules);
  }

  function renderOverview() {
    const tournament = overview?.tournament || {};
    elements.heading.textContent = tournament.name || "Live-наблюдение";
    elements.scheduleTitle.textContent = "Матчи наших команд";
    elements.notice.textContent = tournament.notice || "Технический контур live-наблюдения.";
    elements.updated.textContent = overview?.updatedAt ? `Обновлено: ${asMoscowDate(overview.updatedAt)}` : "Нет данных";
    elements.schedule.replaceChildren();
    const matches = Array.isArray(overview?.matches) ? overview.matches : [];
    elements.schedule.classList.toggle("is-sparse", matches.length < 3);
    if (!matches.length) {
      const empty = document.createElement("p");
      empty.className = "live-empty-state";
      empty.textContent = "Сейчас в SourceTV нет матчей 1win Essence II с нашими командами.";
      elements.schedule.append(empty);
    } else {
      matches.forEach(match => elements.schedule.append(card(match)));
    }
    renderGroups();
    renderTiebreakers();
    elements.bracket.replaceChildren();
    const rounds = overview?.bracket?.rounds || [];
    elements.bracket.hidden = rounds.length === 0;
    const lanes = [
      { key: "upper", name: "Верхняя сетка", rounds: rounds.filter(round => String(round.name || "").startsWith("Верхняя")) },
      { key: "lower", name: "Нижняя сетка", rounds: rounds.filter(round => String(round.name || "").startsWith("Нижняя")) },
      { key: "final", name: "Финал", rounds: rounds.filter(round => !/^(Верхняя|Нижняя)/.test(String(round.name || ""))) },
    ];
    for (const lane of lanes.filter(item => item.rounds.length)) {
      const laneNode = document.createElement("section");
      laneNode.className = `live-bracket-lane ${lane.key}`;
      const laneTitle = document.createElement("h3");
      laneTitle.textContent = lane.name;
      const columns = document.createElement("div");
      columns.className = "live-bracket-lane-rounds";
      columns.style.setProperty("--round-count", String(lane.rounds.length));
      for (const round of lane.rounds) {
        const column = document.createElement("section");
        column.className = "live-bracket-round";
        const title = document.createElement("h4");
        title.textContent = String(round.name || "Раунд").replace(/^(Верхняя|Нижняя) сетка ·\s*/, "");
        column.append(title);
        (round.matches || []).forEach(match => column.append(card(match)));
        columns.append(column);
      }
      laneNode.append(laneTitle, columns);
      elements.bracket.append(laneNode);
    }
  }

  function formatClock(seconds) {
    const value = Math.max(0, Number(seconds) || 0);
    return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, "0")}`;
  }

  function ensureMapLayers() {
    if (mapLayers) return mapLayers;
    elements.map.replaceChildren();
    const background = document.createElement("div");
    background.className = "live-map-background";
    const trails = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    trails.classList.add("live-trails");
    trails.setAttribute("viewBox", "0 0 100 100");
    trails.setAttribute("preserveAspectRatio", "none");
    const objects = document.createElement("div");
    objects.className = "live-map-objects";
    const placeholder = document.createElement("p");
    placeholder.className = "live-map-placeholder";
    elements.map.append(background, trails, objects, placeholder);
    mapLayers = { background, trails, objects, placeholder };
    return mapLayers;
  }

  function addPoint(player, now) {
    const key = String(player.id ?? player.accountId ?? player.playerSlot ?? player.heroId);
    if (!key || player.x == null || player.y == null) return { key, points: [] };
    const history = movementHistory.get(key) || [];
    const point = { x: clamp(player.x), y: clamp(player.y), at: now };
    const last = history[history.length - 1];
    if (!last || last.x !== point.x || last.y !== point.y || now - last.at >= 1000) history.push(point);
    const recent = history.filter(item => now - item.at <= 30000);
    movementHistory.set(key, recent);
    return { key, points: recent };
  }

  function updateTrail(key, points, team) {
    const layers = ensureMapLayers();
    let line = trailNodes.get(key);
    if (!line) {
      line = document.createElementNS("http://www.w3.org/2000/svg", "polyline");
      layers.trails.append(line);
      trailNodes.set(key, line);
    }
    line.setAttribute("class", `live-trail ${team === "dire" ? "dire" : "radiant"}`);
    line.setAttribute("points", points.map(point => `${point.x},${point.y}`).join(" "));
  }

  function updateHero(player, now) {
    const layers = ensureMapLayers();
    const { key, points } = addPoint(player, now);
    if (!key) return;
    updateTrail(key, points, player.team);
    let node = heroNodes.get(key);
    const info = heroInfo(player);
    if (!node) {
      node = document.createElement("span");
      node.className = "live-hero-marker";
      const image = document.createElement("img");
      image.alt = "";
      node.append(image);
      layers.objects.append(node);
      heroNodes.set(key, node);
    }
    node.className = `live-hero-marker ${player.team === "dire" ? "dire" : "radiant"}${player.alive === false ? " dead" : ""}${player.teleport?.channeling ? " teleporting" : ""}`;
    node.dataset.tooltip = `${player.playerName || "Игрок"} · ${info.name}${player.alive === false ? ` · возрождение ${Math.max(0, Number(player.respawnSeconds) || 0)} с` : ""}`;
    node.style.left = `${clamp(player.x)}%`;
    node.style.top = `${clamp(player.y)}%`;
    const image = node.querySelector("img");
    if (info.imageUrl && image.src !== new URL(info.imageUrl, location.href).href) image.src = info.imageUrl;
    image.hidden = !info.imageUrl;
    const sequence = Number(player.damageSequence || 0);
    if (sequence > (lastDamageSequence.get(key) || 0)) {
      node.classList.remove("took-damage");
      void node.offsetWidth;
      node.classList.add("took-damage");
    }
    lastDamageSequence.set(key, sequence);
    node.querySelector(".live-teleport-target")?.remove();
    if (player.teleport?.channeling && player.teleport.targetX != null && player.teleport.targetY != null) {
      const ghost = document.createElement("span");
      ghost.className = `live-teleport-target ${player.team === "dire" ? "dire" : "radiant"}`;
      ghost.style.left = `${clamp(player.teleport.targetX)}%`;
      ghost.style.top = `${clamp(player.teleport.targetY)}%`;
      if (info.imageUrl) ghost.style.backgroundImage = `url(${JSON.stringify(info.imageUrl).slice(1, -1)})`;
      layers.objects.append(ghost);
      setTimeout(() => ghost.remove(), Math.max(500, Number(player.teleport.remainingMs) || 2500));
    }
  }

  function renderBuilding(building) {
    const layers = ensureMapLayers();
    const node = document.createElement("span");
    const maxHP = Math.max(0, Number(building.maxHealth) || 0);
    const hp = Math.max(0, Number(building.health) || 0);
    const detail = building.alive === false
      ? `${building.name || "Строение"}: разрушено${building.destroyedBy ? ` · добил ${building.destroyedBy}` : ""}`
      : `${building.name || "Строение"}: ${hp.toLocaleString("ru-RU")} / ${maxHP.toLocaleString("ru-RU")} HP`;
    const kind = ["tower", "barracks", "ancient"].includes(building.kind) ? building.kind : "tower";
    node.className = `live-building live-${kind} ${building.team === "dire" ? "dire" : "radiant"}${building.alive === false ? " destroyed" : ""}`;
    node.dataset.tooltip = detail;
    node.style.left = `${clamp(building.x)}%`;
    node.style.top = `${clamp(building.y)}%`;
    node.tabIndex = 0;
    layers.objects.append(node);
  }

  function renderRoshan(roshan) {
    if (!roshan || roshan.x == null || roshan.y == null) return;
    const layers = ensureMapLayers();
    const node = document.createElement("span");
    node.style.left = `${clamp(roshan.x)}%`;
    node.style.top = `${clamp(roshan.y)}%`;
    if (roshan.alive !== false) {
      node.className = "live-roshan alive";
      node.textContent = "R";
      node.dataset.tooltip = "Рошан жив";
    } else {
      const minimum = Math.max(0, Number(roshan.respawnMinSeconds) || 0);
      const maximum = Math.max(minimum, Number(roshan.respawnMaxSeconds) || minimum);
      node.className = "live-roshan dead";
      node.style.setProperty("--roshan-progress", `${Math.min(360, maximum ? 360 * (1 - maximum / Math.max(maximum, Number(roshan.respawnWindowSeconds) || maximum)) : 0)}deg`);
      node.textContent = formatClock(minimum);
      node.dataset.tooltip = minimum === maximum
        ? `Рошан появится примерно через ${formatClock(minimum)}`
        : `Рошан может появиться через ${formatClock(minimum)}–${formatClock(maximum)}`;
    }
    layers.objects.append(node);
  }

  function renderMap(match) {
    const layers = ensureMapLayers();
    layers.objects.querySelectorAll(".live-building, .live-roshan, .live-teleport-target").forEach(node => node.remove());
    const map = match?.map || {};
    const units = Array.isArray(map.players) ? map.players : [];
    const activeKeys = new Set(units.map(player => String(player.id ?? player.accountId ?? player.playerSlot ?? player.heroId)));
    for (const [key, node] of heroNodes) {
      if (!activeKeys.has(key)) { node.remove(); heroNodes.delete(key); trailNodes.get(key)?.remove(); trailNodes.delete(key); }
    }
    const now = Date.parse(match?.updatedAt || "") || Date.now();
    units.forEach(player => updateHero(player, now));
    const buildings = Array.isArray(map.buildings) ? map.buildings : (Array.isArray(map.towers) ? map.towers : []);
    buildings.forEach(renderBuilding);
    renderRoshan(map.roshan || match?.roshan);
    layers.background.style.backgroundImage = map.imageUrl ? `url(${JSON.stringify(map.imageUrl).slice(1, -1)})` : "";
    layers.placeholder.hidden = units.length > 0;
    layers.placeholder.textContent = match?.state === "not_started"
      ? "Матч не начался. Карта появится после запуска внутриигровой трансляции."
      : "Ожидаю подтверждённые данные трансляции Valve о позициях героев.";
  }

  function renderPlayers(match) {
    elements.scoreboard.replaceChildren();
    const score = match?.score || {};
    const total = document.createElement("strong");
    total.textContent = `${score.radiant ?? 0} : ${score.dire ?? 0}`;
    const advantage = document.createElement("span");
    const lead = Number(score.radiantGoldLead || 0);
    advantage.textContent = lead === 0 ? "Перевес по золоту: равный" : `Перевес: ${lead > 0 ? "Radiant" : "Dire"} ${Math.abs(lead).toLocaleString("ru-RU")}`;
    elements.scoreboard.append(total, advantage);
    elements.radiantName.textContent = publicLiveTeamName(match?.radiant?.name || "Radiant");
    elements.direName.textContent = publicLiveTeamName(match?.dire?.name || "Dire");
    elements.radiantPlayers.replaceChildren();
    elements.direPlayers.replaceChildren();
    const players = Array.isArray(match?.players) ? match.players : [];
    if (!players.length) {
      for (const target of [elements.radiantPlayers, elements.direPlayers]) {
        const empty = document.createElement("p");
        empty.textContent = "Состав появится с данными трансляции.";
        target.append(empty);
      }
      return;
    }
    const sorted = [...players].sort((left, right) => {
      if (left.team !== right.team) return left.team === "radiant" ? -1 : 1;
      return (Number(left.position) || 99) - (Number(right.position) || 99);
    });
    sorted.forEach(player => {
      const info = heroInfo(player);
      const row = document.createElement("article");
      row.className = `live-player ${player.team === "dire" ? "dire" : "radiant"}${player.alive === false ? " dead" : ""}`;
      const identity = document.createElement("div");
      const icon = document.createElement("span");
      icon.className = "live-player-icon";
      if (info.imageUrl) {
        const image = document.createElement("img");
        image.src = info.imageUrl;
        image.alt = "";
        icon.append(image);
      }
      const names = document.createElement("span");
      const nameLine = document.createElement("span");
      nameLine.className = "live-player-name-line";
      const playerName = document.createElement("strong");
      playerName.textContent = player.playerName || "Игрок";
      const position = document.createElement("em");
      position.textContent = Number(player.position) > 0 ? `поз. ${Number(player.position)}` : "позиция —";
      const heroName = document.createElement("small");
      heroName.textContent = info.name;
      nameLine.append(playerName, position);
      names.append(nameLine, heroName);
      identity.append(icon, names);
      const kda = document.createElement("b");
      kda.className = "live-player-kda";
      kda.textContent = player.statsAvailable
        ? `${Number(player.kills) || 0}/${Number(player.deaths) || 0}/${Number(player.assists) || 0}`
        : "KDA —";
      kda.title = player.statsAvailable ? "Убийства / смерти / помощи" : "SourceTV не передаёт live-KDA";
      const worth = document.createElement("span");
      worth.className = "live-player-worth";
      worth.textContent = player.statsAvailable && Number(player.netWorth || 0) > 0
        ? `${Number(player.netWorth).toLocaleString("ru-RU")} зол.`
        : "ценность —";
      worth.title = player.statsAvailable ? "Ценность героя" : "SourceTV не передаёт live-ценность героя";
      row.append(identity, kda, worth);
      (player.team === "dire" ? elements.direPlayers : elements.radiantPlayers).append(row);
    });
  }

  function publicLiveTeamName(value) {
    const name = String(value || "").trim();
    const key = name.toLowerCase().replace(/[^a-z0-9]+/g, "");
    return key === "1w" || key === "1wteam" || key === "ironwing" ? "Iron Wing" : name;
  }

  function renderMatch(match) {
    selectedMatch = match;
    elements.tracker.classList.remove("hidden");
    elements.state.textContent = stateText[match?.state] || "Матч";
    elements.title.textContent = `${match?.radiant?.name || "TBD"} — ${match?.dire?.name || "TBD"}`;
    elements.message.textContent = match?.message || stateText[match?.state] || "Ожидание данных";
    elements.clock.textContent = match?.gameTimeSeconds == null ? "—" : formatClock(match.gameTimeSeconds);
    renderMap(match);
    renderPlayers(match);
  }

  async function openMatch(id, updateRoute = true) {
    if (!id) return;
    try {
      const match = await api(`/api/live/matches/${encodeURIComponent(id)}`);
      renderMatch(match);
      if (updateRoute && routeMatchID() !== id) setRoute(`live/${id}`);
    } catch {
      elements.message.textContent = "Не удалось получить состояние матча.";
    }
  }

  function pollDelay() {
    if (document.hidden) return selectedMatch ? 5000 : 30000;
    return selectedMatch ? 1000 : 15000;
  }

  async function refresh() {
    if (requestInFlight || !isLiveRoute()) return scheduleRefresh();
    requestInFlight = true;
    try {
      const now = Date.now();
      if (!overview || now - lastOverviewFetch >= (document.hidden ? 30000 : 15000)) {
        [overview] = await Promise.all([api("/api/live/overview"), ensureHeroCatalog()]);
        lastOverviewFetch = now;
        renderOverview();
      }
      const id = routeMatchID();
      if (id) await openMatch(id, false);
    } catch {
      elements.notice.textContent = "Live-данные временно недоступны.";
      elements.updated.textContent = "Нет соединения";
    } finally {
      requestInFlight = false;
      scheduleRefresh();
    }
  }

  function scheduleRefresh(immediate = false) {
    window.clearTimeout(refreshTimer);
    if (!isLiveRoute()) return;
    if (window.DOTA_HUB_STATIC_API === true) {
      if (immediate) refreshTimer = window.setTimeout(refresh, 0);
      return;
    }
    refreshTimer = window.setTimeout(refresh, immediate ? 0 : pollDelay());
  }

  function showView() {
    if (!isLiveRoute()) {
      view.classList.add("hidden");
      window.clearTimeout(refreshTimer);
      return;
    }
    document.querySelectorAll("main > .app-view").forEach(candidate => candidate.classList.toggle("hidden", candidate !== view));
    elements.nav?.classList.add("active");
    if (!routeMatchID()) {
      elements.tracker.classList.add("hidden");
      selectedMatch = null;
    }
    scheduleRefresh(true);
  }

  elements.back.addEventListener("click", () => setRoute("live"));
  elements.mapSize?.addEventListener("click", () => setMapExpanded(!elements.layout?.classList.contains("map-expanded")));
  elements.nav?.addEventListener("click", event => { event.preventDefault(); setRoute("live"); });
  window.addEventListener("hashchange", showView);
  document.addEventListener("visibilitychange", () => scheduleRefresh(true));
  window.addEventListener("pagehide", () => window.clearTimeout(refreshTimer), { once: true });
  showView();
})();
