(() => {
  "use strict";

  const pageLimits = Object.freeze({
    sessions: { initial: 20, step: 20, maximum: 100 },
    events: { initial: 100, step: 100, maximum: 500 },
  });
  const config = globalThis.BELAY_LOCAL_CONFIG || {};
  const state = {
    token: resolveToken(config),
    apiBase: normalizeApiBase(config.apiBase),
    sessions: [],
    sessionLimit: pageLimits.sessions.initial,
    sessionHasMore: false,
    selectedSessionID: "",
    selectedEventTotal: 0,
    eventLimit: pageLimits.events.initial,
    eventHasMore: false,
    activeFilter: "all",
    search: "",
    lastAction: "sessions",
  };

  const elements = {
    refreshButton: document.querySelector("#refresh-button"),
    sessionCount: document.querySelector("#session-count"),
    sessionSearch: document.querySelector("#session-search"),
    filterChips: Array.from(document.querySelectorAll(".filter-chip")),
    sessionList: document.querySelector("#session-list"),
    sessionsPagination: document.querySelector("#sessions-pagination"),
    sessionsPageStatus: document.querySelector("#sessions-page-status"),
    sessionsLoadMore: document.querySelector("#sessions-load-more"),
    sessionsLoading: document.querySelector("#sessions-loading"),
    sessionsEmpty: document.querySelector("#sessions-empty"),
    welcomeState: document.querySelector("#welcome-state"),
    timelineView: document.querySelector("#timeline-view"),
    backButton: document.querySelector("#back-button"),
    selectedAvatar: document.querySelector("#selected-avatar"),
    selectedHarness: document.querySelector("#selected-harness"),
    selectedOutcome: document.querySelector("#selected-outcome"),
    selectedHistorical: document.querySelector("#selected-historical"),
    selectedSessionID: document.querySelector("#selected-session-id"),
    selectedEventCount: document.querySelector("#selected-event-count"),
    selectedDuration: document.querySelector("#selected-duration"),
    selectedEndedAt: document.querySelector("#selected-ended-at"),
    timelineFreshness: document.querySelector("#timeline-freshness"),
    eventList: document.querySelector("#event-list"),
    eventsPagination: document.querySelector("#events-pagination"),
    eventsPageStatus: document.querySelector("#events-page-status"),
    eventsLoadMore: document.querySelector("#events-load-more"),
    timelineLoading: document.querySelector("#timeline-loading"),
    eventsEmpty: document.querySelector("#events-empty"),
    errorBanner: document.querySelector("#error-banner"),
    errorTitle: document.querySelector("#error-title"),
    errorDetail: document.querySelector("#error-detail"),
    errorRetry: document.querySelector("#error-retry"),
  };

  bindEvents();
  loadSessions();

  function bindEvents() {
    elements.refreshButton.addEventListener("click", () => loadSessions(true));
    elements.sessionsLoadMore.addEventListener("click", loadMoreSessions);
    elements.eventsLoadMore.addEventListener("click", loadMoreEvents);
    elements.backButton.addEventListener("click", closeTimeline);
    elements.errorRetry.addEventListener("click", retryLastAction);

    elements.sessionSearch.addEventListener("input", (event) => {
      state.search = event.target.value.trim().toLocaleLowerCase();
      renderSessions();
    });

    elements.filterChips.forEach((chip) => {
      chip.addEventListener("click", () => {
        state.activeFilter = chip.dataset.filter || "all";
        elements.filterChips.forEach((candidate) => {
          candidate.classList.toggle("is-active", candidate === chip);
        });
        renderSessions();
      });
    });

    globalThis.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && state.selectedSessionID) {
        closeTimeline();
      }
    });
  }

  async function loadSessions(preserveSelection = false) {
    state.lastAction = "sessions";
    hideError();
    elements.sessionsLoading.hidden = false;
    elements.sessionsEmpty.hidden = true;
    elements.sessionList.hidden = true;
    elements.refreshButton.disabled = true;
    elements.sessionsLoadMore.disabled = true;

    try {
      const response = await apiGet(
        `/v1/sessions?limit=${encodeURIComponent(state.sessionLimit)}`,
      );
      state.sessions = Array.isArray(response.data) ? response.data : [];
      state.sessionHasMore = response.has_more === true;
      elements.sessionCount.textContent = state.sessionHasMore
        ? `${state.sessions.length}+`
        : String(state.sessions.length);
      renderSessions();
      renderSessionPagination();

      if (
        preserveSelection &&
        state.selectedSessionID &&
        state.sessions.some(
          (session) => readText(session.session_id) === state.selectedSessionID,
        )
      ) {
        await selectSession(state.selectedSessionID, true);
      }
    } catch (error) {
      state.sessions = [];
      state.sessionHasMore = false;
      elements.sessionCount.textContent = "—";
      renderSessions();
      renderSessionPagination();
      showError("Unable to load sessions", error);
    } finally {
      elements.sessionsLoading.hidden = true;
      elements.sessionList.hidden = false;
      elements.refreshButton.disabled = false;
      elements.sessionsLoadMore.disabled = false;
    }
  }

  function loadMoreSessions() {
    if (
      !state.sessionHasMore ||
      state.sessionLimit >= pageLimits.sessions.maximum
    ) {
      return;
    }
    state.sessionLimit = Math.min(
      pageLimits.sessions.maximum,
      state.sessionLimit + pageLimits.sessions.step,
    );
    loadSessions(false);
  }

  function renderSessions() {
    const visibleSessions = state.sessions.filter(sessionMatches);
    const fragment = document.createDocumentFragment();

    visibleSessions.forEach((session) => {
      fragment.append(createSessionCard(session));
    });

    elements.sessionList.replaceChildren(fragment);
    elements.sessionsEmpty.hidden =
      state.sessions.length !== 0 && visibleSessions.length !== 0;

    if (state.sessions.length === 0 && elements.sessionsLoading.hidden) {
      elements.sessionsEmpty.hidden = false;
    }
  }

  function renderSessionPagination() {
    const atMaximum = state.sessionLimit >= pageLimits.sessions.maximum;
    const expanded = state.sessionLimit > pageLimits.sessions.initial;
    elements.sessionsPagination.hidden = !state.sessionHasMore && !expanded;
    elements.sessionsLoadMore.hidden = !state.sessionHasMore || atMaximum;

    if (state.sessionHasMore && atMaximum) {
      elements.sessionsPageStatus.textContent =
        `Showing the first ${state.sessions.length} sessions. ` +
        "Additional older sessions exist beyond the browser limit.";
      return;
    }
    if (state.sessionHasMore) {
      elements.sessionsPageStatus.textContent =
        `Showing the first ${state.sessions.length} sessions. More are available.`;
      return;
    }
    elements.sessionsPageStatus.textContent =
      `Showing all ${state.sessions.length} loaded sessions.`;
  }

  function sessionMatches(session) {
    const harness = readText(session.harness).toLocaleLowerCase();
    const outcome = normalizeOutcome(session.outcome);
    const sessionID = readText(session.session_id).toLocaleLowerCase();
    const matchesSearch =
      !state.search ||
      harness.includes(state.search) ||
      outcome.includes(state.search) ||
      sessionID.includes(state.search);

    if (!matchesSearch) {
      return false;
    }

    if (state.activeFilter === "failed") {
      return outcome === "failed";
    }

    if (state.activeFilter === "historical") {
      return Boolean(session.historical);
    }

    return true;
  }

  function createSessionCard(session) {
    const sessionID = readText(session.session_id);
    const harness = displayHarness(session.harness);
    const outcome = normalizeOutcome(session.outcome);
    const button = createElement("button", "session-card");
    button.type = "button";
    button.classList.toggle("is-selected", sessionID === state.selectedSessionID);
    button.setAttribute("aria-pressed", String(sessionID === state.selectedSessionID));
    button.addEventListener("click", () => selectSession(sessionID));

    const top = createElement("span", "session-card-top");
    const avatar = createElement("span", "harness-avatar", harnessInitial(harness));
    avatar.setAttribute("aria-hidden", "true");

    const title = createElement("span", "session-card-title");
    title.append(
      createElement("strong", "", harness),
      createElement("small", "", compactID(sessionID)),
    );

    const outcomeBadge = createElement(
      "span",
      "status-badge",
      displayOutcome(outcome),
    );
    outcomeBadge.dataset.tone = outcome;
    top.append(avatar, title, outcomeBadge);

    const meta = createElement("span", "session-card-meta");
    const dot = createElement("span", "status-dot");
    dot.dataset.tone = outcome;
    dot.setAttribute("aria-hidden", "true");
    meta.append(
      dot,
      createElement(
        "span",
        "",
        formatEventCount(toFiniteNumber(session.event_count)),
      ),
    );

    if (session.historical) {
      meta.append(createElement("span", "", "Historical"));
    }

    const time = createElement("time", "", formatRelativeTime(session.ended_at));
    const endedAt = parseDate(session.ended_at);
    if (endedAt) {
      time.dateTime = endedAt.toISOString();
      time.title = formatFullDate(endedAt);
    }
    meta.append(time);

    button.append(top, meta);
    return button;
  }

  async function selectSession(sessionID, preserveEventLimit = false) {
    const session = state.sessions.find(
      (candidate) => readText(candidate.session_id) === sessionID,
    );
    if (!session) {
      return;
    }

    state.selectedSessionID = sessionID;
    state.selectedEventTotal = toFiniteNumber(session.event_count);
    if (!preserveEventLimit) {
      state.eventLimit = pageLimits.events.initial;
    }
    state.lastAction = "timeline";
    renderSessions();
    renderSessionHeader(session);
    hideError();
    elements.welcomeState.hidden = true;
    elements.timelineView.hidden = false;
    elements.timelineLoading.hidden = false;
    elements.eventsEmpty.hidden = true;
    elements.eventList.replaceChildren();
    elements.eventsPagination.hidden = true;
    document.body.classList.add("is-timeline-open");

    await loadTimeline(sessionID);
  }

  async function loadTimeline(sessionID) {
    elements.timelineLoading.hidden = false;
    elements.eventsLoadMore.disabled = true;

    try {
      const response = await apiGet(
        `/v1/sessions/${encodeURIComponent(sessionID)}/events?limit=${encodeURIComponent(state.eventLimit)}`,
      );
      if (state.selectedSessionID !== sessionID) {
        return;
      }

      const events = Array.isArray(response.data) ? response.data : [];
      state.eventHasMore = response.has_more === true;
      renderEvents(events);
      renderEventPagination(events.length);
      elements.timelineFreshness.textContent = formatFreshness(
        response.data_through,
      );
    } catch (error) {
      if (state.selectedSessionID === sessionID) {
        state.eventHasMore = false;
        renderEventPagination(0);
        showError("Unable to load this timeline", error);
      }
    } finally {
      if (state.selectedSessionID === sessionID) {
        elements.timelineLoading.hidden = true;
        elements.eventsLoadMore.disabled = false;
      }
    }
  }

  function loadMoreEvents() {
    if (!state.eventHasMore || state.eventLimit >= pageLimits.events.maximum) {
      return;
    }
    state.eventLimit = Math.min(
      pageLimits.events.maximum,
      state.eventLimit + pageLimits.events.step,
    );
    loadTimeline(state.selectedSessionID);
  }

  function renderSessionHeader(session) {
    const harness = displayHarness(session.harness);
    const outcome = normalizeOutcome(session.outcome);
    const startedAt = parseDate(session.started_at);
    const endedAt = parseDate(session.ended_at);

    elements.selectedAvatar.textContent = harnessInitial(harness);
    elements.selectedHarness.textContent = harness;
    elements.selectedOutcome.textContent = displayOutcome(outcome);
    elements.selectedOutcome.dataset.tone = outcome;
    elements.selectedHistorical.hidden = !session.historical;
    elements.selectedSessionID.textContent = readText(session.session_id);
    elements.selectedSessionID.title = readText(session.session_id);
    elements.selectedEventCount.textContent = String(state.selectedEventTotal);
    elements.selectedDuration.textContent = formatDuration(startedAt, endedAt);
    elements.selectedEndedAt.textContent = endedAt
      ? formatRelativeTime(endedAt)
      : "Unknown";
    elements.selectedEndedAt.title = endedAt ? formatFullDate(endedAt) : "";
    elements.timelineFreshness.textContent = "";
  }

  function renderEvents(events) {
    const fragment = document.createDocumentFragment();
    events.forEach((event) => fragment.append(createEventRow(event)));
    elements.eventList.replaceChildren(fragment);
    elements.eventsEmpty.hidden = events.length !== 0;
  }

  function renderEventPagination(returnedCount) {
    const atMaximum = state.eventLimit >= pageLimits.events.maximum;
    const expanded = state.eventLimit > pageLimits.events.initial;
    elements.eventsPagination.hidden = !state.eventHasMore && !expanded;
    elements.eventsLoadMore.hidden = !state.eventHasMore || atMaximum;

    if (state.eventHasMore && atMaximum) {
      elements.eventsPageStatus.textContent =
        `Showing the first ${returnedCount} of ${state.selectedEventTotal} events. ` +
        "Additional events exist beyond the browser limit.";
      return;
    }
    if (state.eventHasMore) {
      elements.eventsPageStatus.textContent =
        `Showing the first ${returnedCount} of ${state.selectedEventTotal} events.`;
      return;
    }
    elements.eventsPageStatus.textContent =
      `Showing all ${returnedCount} events in this session.`;
  }

  function createEventRow(event) {
    const observation = isRecord(event.observation) ? event.observation : {};
    const source = isRecord(event.source) ? event.source : {};
    const coverage = isRecord(event.coverage) ? event.coverage : {};
    const historical = isRecord(event.historical) ? event.historical : {};
    const resource = isRecord(observation.resource) ? observation.resource : {};
    const outcome = normalizeOutcome(observation.outcome);
    const occurredAt = parseDate(event.occurred_at);

    const row = createElement("article", "event-row");
    const time = createElement(
      "time",
      "event-time",
      occurredAt ? formatClockTime(occurredAt) : "—",
    );
    if (occurredAt) {
      time.dateTime = occurredAt.toISOString();
      time.title = formatFullDate(occurredAt);
    }

    const rail = createElement("div", "event-rail");
    const node = createElement("span", "event-node");
    node.dataset.tone = outcome;
    node.setAttribute("aria-hidden", "true");
    rail.append(node);

    const card = createElement("div", "event-card");
    const heading = createElement("div", "event-heading");
    const action = readableLabel(observation.action, "Activity");
    const actor = readableLabel(observation.actor, "");
    const title = actor ? `${action} · ${actor}` : action;
    const eventTitle = createElement("strong", "", title);
    const type = createElement(
      "span",
      "event-type",
      readText(observation.type) || "event",
    );
    heading.append(eventTitle);
    if (isExplicitEventOutcome(outcome)) {
      const outcomeBadge = createElement(
        "span",
        "status-badge",
        displayOutcome(outcome),
      );
      outcomeBadge.dataset.tone = outcome;
      heading.append(outcomeBadge);
    }
    heading.append(type);
    card.append(heading);

    const summary = readText(observation.summary);
    if (summary) {
      card.append(createElement("p", "event-summary", summary));
    }

    const metadata = [];
    appendMetadata(metadata, "Harness", source.agent);
    appendMetadata(metadata, "Source", source.kind);
    appendMetadata(metadata, "Resource", resource.name || resource.kind);
    appendMetadata(metadata, "Coverage", coverage.depth);
    appendMetadata(metadata, "Confidence", coverage.confidence);
    if (outcome === "unknown") {
      metadata.push("Outcome · Not reported by source");
    }

    if (historical.is_historical) {
      appendMetadata(
        metadata,
        "Historical",
        historical.reconstruction_source || "reconstructed",
      );
    }

    const eventID = readText(event.event_id);
    if (eventID) {
      appendMetadata(metadata, "Event", compactID(eventID));
    }

    if (metadata.length) {
      const meta = createElement("div", "event-meta");
      metadata.forEach((value) => meta.append(createElement("span", "", value)));
      card.append(meta);
    }

    row.append(time, rail, card);
    return row;
  }

  function appendMetadata(metadata, label, value) {
    const text = readText(value);
    if (text) {
      metadata.push(`${label}: ${text}`);
    }
  }

  function closeTimeline() {
    state.selectedSessionID = "";
    state.selectedEventTotal = 0;
    state.eventLimit = pageLimits.events.initial;
    state.eventHasMore = false;
    document.body.classList.remove("is-timeline-open");
    elements.timelineView.hidden = true;
    elements.welcomeState.hidden = false;
    elements.eventsPagination.hidden = true;
    renderSessions();
  }

  function retryLastAction() {
    if (state.lastAction === "timeline" && state.selectedSessionID) {
      selectSession(state.selectedSessionID, true);
      return;
    }
    loadSessions();
  }

  async function apiGet(path) {
    if (!state.token) {
      throw new Error(
        "No launch token was provided. Open the URL supplied by Belay Local.",
      );
    }

    const response = await fetch(`${state.apiBase}${path}`, {
      method: "GET",
      cache: "no-store",
      credentials: "omit",
      headers: {
        Accept: "application/json",
        Authorization: `Bearer ${state.token}`,
      },
    });

    const body = await readResponseBody(response);
    if (!response.ok) {
      const detail =
        isRecord(body) && readText(body.detail)
          ? readText(body.detail)
          : `Local API returned ${response.status}.`;
      throw new Error(detail);
    }

    if (!isRecord(body)) {
      throw new Error("Local API returned an invalid response.");
    }

    return body;
  }

  async function readResponseBody(response) {
    const contentType = response.headers.get("content-type") || "";
    if (!contentType.toLocaleLowerCase().includes("json")) {
      return null;
    }

    try {
      return await response.json();
    } catch {
      return null;
    }
  }

  function resolveToken(bootstrapConfig) {
    const fromConfig = readText(
      bootstrapConfig.token || bootstrapConfig.authToken,
    );
    const historyState = isRecord(globalThis.history.state)
      ? globalThis.history.state
      : {};
    const fromHistory = readText(historyState.belayToken);
    const fragment = globalThis.location.hash.slice(1);
    let fromFragment = "";

    if (fragment) {
      const parameters = new URLSearchParams(fragment);
      fromFragment = readText(
        parameters.get("token") || parameters.get("access_token"),
      );

      if (!fromFragment && !fragment.includes("=")) {
        try {
          fromFragment = decodeURIComponent(fragment);
        } catch {
          fromFragment = fragment;
        }
      }

      globalThis.history.replaceState(
        { ...historyState, belayToken: fromFragment || fromConfig || fromHistory },
        document.title,
        `${globalThis.location.pathname}${globalThis.location.search}`,
      );
    }

    return fromFragment || fromConfig || fromHistory;
  }

  function normalizeApiBase(value) {
    const base = readText(value).trim();
    if (!base) {
      return "";
    }

    try {
      const url = new URL(base, globalThis.location.origin);
      if (url.origin !== globalThis.location.origin) {
        return "";
      }
      return url.pathname.replace(/\/+$/, "");
    } catch {
      return "";
    }
  }

  function showError(title, error) {
    elements.errorTitle.textContent = title;
    elements.errorDetail.textContent =
      error instanceof Error ? error.message : "An unexpected error occurred.";
    elements.errorBanner.hidden = false;
  }

  function hideError() {
    elements.errorBanner.hidden = true;
    elements.errorDetail.textContent = "";
  }

  function createElement(tagName, className, text) {
    const element = document.createElement(tagName);
    if (className) {
      element.className = className;
    }
    if (text !== undefined && text !== null) {
      element.textContent = String(text);
    }
    return element;
  }

  function isRecord(value) {
    return Boolean(value) && typeof value === "object" && !Array.isArray(value);
  }

  function readText(value) {
    if (typeof value === "string") {
      return value;
    }
    if (typeof value === "number" && Number.isFinite(value)) {
      return String(value);
    }
    return "";
  }

  function toFiniteNumber(value) {
    const number = Number(value);
    return Number.isFinite(number) && number >= 0 ? number : 0;
  }

  function displayHarness(value) {
    return readableLabel(value, "Unknown harness");
  }

  function readableLabel(value, fallback) {
    const text = readText(value).trim();
    if (!text) {
      return fallback;
    }
    return text
      .split(/[-_.\s]+/)
      .filter(Boolean)
      .map((part) => part.charAt(0).toLocaleUpperCase() + part.slice(1))
      .join(" ");
  }

  function harnessInitial(harness) {
    return Array.from(harness.trim())[0] || "?";
  }

  function normalizeOutcome(value) {
    const outcome = readText(value).toLocaleLowerCase();
    return ["succeeded", "failed", "interrupted", "incomplete"].includes(outcome)
      ? outcome
      : "unknown";
  }

  function isExplicitEventOutcome(outcome) {
    return ["succeeded", "failed", "interrupted"].includes(outcome);
  }

  function displayOutcome(outcome) {
    return outcome === "incomplete" ? "Incomplete" : readableLabel(outcome, "Unknown");
  }

  function compactID(value) {
    const text = readText(value);
    if (text.length <= 22) {
      return text;
    }
    return `${text.slice(0, 11)}…${text.slice(-7)}`;
  }

  function parseDate(value) {
    if (value instanceof Date && Number.isFinite(value.getTime())) {
      return value;
    }
    const date = new Date(readText(value));
    return Number.isFinite(date.getTime()) ? date : null;
  }

  function formatEventCount(count) {
    return `${count} ${count === 1 ? "event" : "events"}`;
  }

  function formatClockTime(date) {
    return new Intl.DateTimeFormat(undefined, {
      hour: "numeric",
      minute: "2-digit",
      second: "2-digit",
    }).format(date);
  }

  function formatFullDate(date) {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "medium",
    }).format(date);
  }

  function formatRelativeTime(value) {
    const date = value instanceof Date ? value : parseDate(value);
    if (!date) {
      return "Unknown";
    }

    const seconds = Math.round((date.getTime() - Date.now()) / 1000);
    const absolute = Math.abs(seconds);
    let amount = seconds;
    let unit = "second";

    if (absolute >= 86400) {
      amount = Math.round(seconds / 86400);
      unit = "day";
    } else if (absolute >= 3600) {
      amount = Math.round(seconds / 3600);
      unit = "hour";
    } else if (absolute >= 60) {
      amount = Math.round(seconds / 60);
      unit = "minute";
    }

    return new Intl.RelativeTimeFormat(undefined, {
      numeric: "auto",
    }).format(amount, unit);
  }

  function formatDuration(startedAt, endedAt) {
    if (!startedAt || !endedAt) {
      return "Unknown";
    }

    const milliseconds = Math.max(0, endedAt.getTime() - startedAt.getTime());
    const minutes = Math.floor(milliseconds / 60000);
    const hours = Math.floor(minutes / 60);

    if (hours) {
      return `${hours}h ${minutes % 60}m`;
    }
    if (minutes) {
      return `${minutes}m`;
    }
    return `${Math.max(1, Math.round(milliseconds / 1000))}s`;
  }

  function formatFreshness(value) {
    const date = parseDate(value);
    return date ? `Data current ${formatRelativeTime(date)}` : "";
  }
})();
