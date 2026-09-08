(() => {
  "use strict";

  const pageLimits = Object.freeze({
    sessions: { initial: 40, step: 40, maximum: 100 },
    events: { initial: 100, step: 100, maximum: 500 },
    findings: { page: 100 },
  });
  const explicitOutcomes = new Set(["succeeded", "failed", "interrupted"]);
  const config = globalThis.BELAY_LOCAL_CONFIG || {};
  const state = {
    token: resolveToken(config),
    apiBase: normalizeApiBase(config.apiBase),
    sessions: [],
    sessionLimit: pageLimits.sessions.initial,
    sessionNextCursor: "",
    sessionHasMore: false,
    sessionServerScope: false,
    selectedSessionID: "",
    selectedEventTotal: 0,
    selectedSessionDetail: null,
    selectedOverview: null,
    events: [],
    eventLimit: pageLimits.events.initial,
    eventNextCursor: "",
    eventHasMore: false,
    findings: [],
    findingsMayHaveMore: false,
    findingsStatus: "idle",
    overviewStatus: "idle",
    sessionOccurredAfter: "",
    stats: null,
    filters: { query: "", harness: "", capture: "", outcome: "", days: "" },
    showAllEvents: false,
    lastAction: "sessions",
  };

  const elements = {
    refreshButton: document.querySelector("#refresh-button"),
    sessionCount: document.querySelector("#session-count"),
    overviewSessionCount: document.querySelector("#overview-session-count"),
    overviewEventCount: document.querySelector("#overview-event-count"),
    overviewFindingCount: document.querySelector("#overview-finding-count"),
    overviewHarnesses: document.querySelector("#overview-harnesses"),
    overviewFreshness: document.querySelector("#overview-freshness"),
    sessionSearch: document.querySelector("#session-search"),
    filterHarness: document.querySelector("#filter-harness"),
    filterCapture: document.querySelector("#filter-capture"),
    filterOutcome: document.querySelector("#filter-outcome"),
    filterDate: document.querySelector("#filter-date"),
    clearFilters: document.querySelector("#clear-filters"),
    sessionScopeNote: document.querySelector("#session-scope-note"),
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
    copySelectedSession: document.querySelector("#copy-selected-session"),
    selectedEventCount: document.querySelector("#selected-event-count"),
    selectedDuration: document.querySelector("#selected-duration"),
    selectedEndedAt: document.querySelector("#selected-ended-at"),
    overviewState: document.querySelector("#overview-state"),
    overviewQuality: document.querySelector("#overview-quality"),
    activityGrid: document.querySelector("#activity-grid"),
    resourceList: document.querySelector("#resource-list"),
    resourceDisclosure: document.querySelector("#resource-disclosure"),
    findingList: document.querySelector("#finding-list"),
    timelineFreshness: document.querySelector("#timeline-freshness"),
    timelineScope: document.querySelector("#timeline-scope"),
    allEventsToggle: document.querySelector("#all-events-toggle"),
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

  let searchTimer = 0;
  bindEvents();
  refreshAll(false);

  function bindEvents() {
    elements.refreshButton.addEventListener("click", () => refreshAll(true));
    elements.sessionsLoadMore.addEventListener("click", loadMoreSessions);
    elements.eventsLoadMore.addEventListener("click", loadMoreEvents);
    elements.backButton.addEventListener("click", closeTimeline);
    elements.errorRetry.addEventListener("click", retryLastAction);
    elements.copySelectedSession.addEventListener("click", () => {
      copyText(state.selectedSessionID, elements.copySelectedSession);
    });
    elements.allEventsToggle.addEventListener("click", () => {
      state.showAllEvents = !state.showAllEvents;
      elements.allEventsToggle.setAttribute(
        "aria-pressed",
        String(state.showAllEvents),
      );
      elements.allEventsToggle.classList.toggle("is-active", state.showAllEvents);
      renderEvents();
    });

    elements.sessionSearch.addEventListener("input", (event) => {
      state.filters.query = event.target.value.trim();
      globalThis.clearTimeout(searchTimer);
      searchTimer = globalThis.setTimeout(resetAndLoadSessions, 250);
    });

    [
      [elements.filterHarness, "harness"],
      [elements.filterCapture, "capture"],
      [elements.filterOutcome, "outcome"],
    ].forEach(([element, key]) => {
      element.addEventListener("change", () => {
        state.filters[key] = element.value;
        resetAndLoadSessions();
      });
    });

    elements.filterDate.addEventListener("change", () => {
      state.filters.days = elements.filterDate.value;
      state.sessionOccurredAfter = dateLowerBound(state.filters.days);
      resetAndLoadSessions();
    });

    elements.clearFilters.addEventListener("click", () => {
      state.filters = {
        query: "",
        harness: "",
        capture: "",
        outcome: "",
        days: "",
      };
      state.sessionOccurredAfter = "";
      elements.sessionSearch.value = "";
      elements.filterHarness.value = "";
      elements.filterCapture.value = "";
      elements.filterOutcome.value = "";
      elements.filterDate.value = "";
      resetAndLoadSessions();
    });

    globalThis.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && state.selectedSessionID) closeTimeline();
    });
  }

  async function refreshAll(preserveSelection) {
    hideError();
    elements.refreshButton.disabled = true;
    const selectedID = preserveSelection ? state.selectedSessionID : "";
    state.sessionLimit = pageLimits.sessions.initial;
    state.sessionNextCursor = "";
    await Promise.allSettled([loadStats(), loadSessions(false)]);
    elements.refreshButton.disabled = false;
    if (
      selectedID &&
      state.sessions.some((session) => readText(session.session_id) === selectedID)
    ) {
      selectSession(selectedID);
    }
  }

  async function loadStats() {
    try {
      const response = await apiGet("/v1/stats");
      state.stats = isRecord(response.data) ? response.data : null;
      renderLocalOverview(response.data_through);
    } catch {
      state.stats = null;
      renderLocalOverview("");
    }
  }

  function renderLocalOverview(dataThrough) {
    const stats = state.stats || {};
    elements.overviewSessionCount.textContent = formatNumberOrDash(
      stats.session_count,
    );
    elements.overviewEventCount.textContent = formatNumberOrDash(stats.event_count);
    elements.overviewFindingCount.textContent = formatNumberOrDash(
      stats.finding_count,
    );
    const harnessCounts = isRecord(stats.harness_counts)
      ? stats.harness_counts
      : {};
    const harnesses = Object.entries(harnessCounts)
      .filter(([, count]) => toFiniteNumber(count) > 0)
      .sort((left, right) => right[1] - left[1]);
    elements.overviewHarnesses.textContent = harnesses.length
      ? `Harnesses · ${harnesses
          .map(([harness, count]) => `${displayHarness(harness)} ${formatNumber(count)}`)
          .join(" · ")}`
      : "Harness coverage unavailable";
    elements.overviewFreshness.textContent = parseDate(dataThrough)
      ? `Freshness · ${formatRelativeTime(dataThrough)}`
      : "Data freshness unavailable";
    updateHarnessOptions(harnesses.map(([harness]) => harness));
  }

  function updateHarnessOptions(harnesses) {
    const current = state.filters.harness;
    const known = new Set(
      state.sessions.map((session) => readText(session.harness)).filter(Boolean),
    );
    if (state.stats && isRecord(state.stats.harness_counts)) {
      Object.keys(state.stats.harness_counts).forEach((harness) => {
        if (harness) known.add(harness);
      });
    }
    harnesses.forEach((harness) => known.add(harness));
    const options = [createOption("", "All harnesses")];
    Array.from(known)
      .sort((left, right) => left.localeCompare(right))
      .forEach((harness) => {
        options.push(createOption(harness, displayHarness(harness)));
      });
    elements.filterHarness.replaceChildren(...options);
    elements.filterHarness.value = known.has(current) ? current : "";
    state.filters.harness = elements.filterHarness.value;
  }

  function createOption(value, label) {
    const option = createElement("option", "", label);
    option.value = value;
    return option;
  }

  function resetAndLoadSessions() {
    state.sessionLimit = pageLimits.sessions.initial;
    state.sessionNextCursor = "";
    loadSessions(false);
  }

  async function loadSessions(append) {
    state.lastAction = "sessions";
    hideError();
    elements.sessionsLoading.hidden = append;
    elements.sessionsEmpty.hidden = true;
    elements.sessionsLoadMore.disabled = true;
    try {
      const cursor = append ? state.sessionNextCursor : "";
      const response = await apiGet(buildSessionPath(cursor));
      const page = Array.isArray(response.data) ? response.data : [];
      const nextCursor = readCursor(response.next_cursor);
      state.sessions =
        append && cursor
          ? deduplicateByID(state.sessions.concat(page), "session_id")
          : page;
      state.sessionNextCursor = nextCursor;
      state.sessionHasMore = response.has_more === true || Boolean(nextCursor);
      state.sessionServerScope = filtersAcknowledged(response);
      updateHarnessOptions([]);
      renderSessions();
      renderSessionPagination();
    } catch (error) {
      if (!append) {
        state.sessions = [];
        state.sessionHasMore = false;
        renderSessions();
      }
      renderSessionPagination();
      showError("Unable to load sessions", error);
    } finally {
      elements.sessionsLoading.hidden = true;
      elements.sessionsLoadMore.disabled = false;
      renderSessions();
      renderSessionPagination();
    }
  }

  function buildSessionPath(cursor) {
    const parameters = new URLSearchParams();
    parameters.set("limit", String(state.sessionLimit));
    if (cursor) parameters.set("cursor", cursor);
    if (state.filters.query) parameters.set("query", state.filters.query);
    if (state.filters.harness) parameters.set("harness", state.filters.harness);
    if (state.filters.capture) parameters.set("history", state.filters.capture);
    if (state.sessionOccurredAfter) {
      parameters.set("occurred_after", state.sessionOccurredAfter);
    }
    return `/v1/sessions?${parameters.toString()}`;
  }

  function loadMoreSessions() {
    if (!state.sessionHasMore) return;
    if (state.sessionNextCursor) {
      loadSessions(true);
      return;
    }
    if (state.sessionLimit >= pageLimits.sessions.maximum) return;
    state.sessionLimit = Math.min(
      pageLimits.sessions.maximum,
      state.sessionLimit + pageLimits.sessions.step,
    );
    loadSessions(false);
  }

  function renderSessions() {
    const visibleSessions = filtersActive()
      ? state.sessions.filter(sessionMatchesLocally)
      : state.sessions;
    const fragment = document.createDocumentFragment();
    let priorGroup = "";
    visibleSessions.forEach((session) => {
      const group = recencyGroup(session);
      if (group !== priorGroup) {
        fragment.append(createElement("p", "session-group-label", group));
        priorGroup = group;
      }
      fragment.append(createSessionCard(session));
    });
    elements.sessionList.replaceChildren(fragment);
    elements.sessionCount.textContent = state.sessionHasMore
      ? `${visibleSessions.length}+`
      : String(visibleSessions.length);
    elements.sessionsEmpty.hidden =
      visibleSessions.length !== 0 || !elements.sessionsLoading.hidden;
    if (state.filters.outcome) {
      elements.sessionScopeNote.textContent =
        "Outcome availability applies to loaded matches; load more before treating results as exhaustive.";
    } else if (filtersActive() && state.sessionServerScope) {
      elements.sessionScopeNote.textContent =
        "Filters are applied by the Local API across stored sessions.";
    } else if (filtersActive()) {
      elements.sessionScopeNote.textContent =
        "Filters were requested from the Local API and checked against loaded rows.";
    } else if (state.sessionHasMore) {
      elements.sessionScopeNote.textContent =
        "Showing a bounded recent subset. Load more to inspect older sessions.";
    } else {
      elements.sessionScopeNote.textContent = "Showing all returned sessions.";
    }
    elements.clearFilters.disabled = !filtersActive();
  }

  function renderSessionPagination() {
    const atMaximum =
      !state.sessionNextCursor &&
      state.sessionLimit >= pageLimits.sessions.maximum;
    elements.sessionsPagination.hidden = !state.sessionHasMore;
    elements.sessionsLoadMore.hidden = !state.sessionHasMore || atMaximum;
    if (state.sessionHasMore && atMaximum) {
      elements.sessionsPageStatus.textContent =
        `Showing ${state.sessions.length} recent sessions. ` +
        "Additional older sessions exist beyond this browser bound.";
    } else if (state.sessionHasMore) {
      elements.sessionsPageStatus.textContent =
        `Showing ${state.sessions.length} sessions. More are available.`;
    } else {
      elements.sessionsPageStatus.textContent =
        `Showing ${state.sessions.length} returned sessions.`;
    }
  }

  function filtersAcknowledged(response) {
    if (!filtersActive()) return false;
    return (
      Boolean(readCursor(response.next_cursor)) ||
      response.filters_applied === true ||
      response.query_applied === true ||
      isRecord(response.applied_filters) ||
      Array.isArray(response.applied_filters)
    );
  }

  function filtersActive() {
    return Object.values(state.filters).some(Boolean);
  }

  function sessionMatchesLocally(session) {
    const query = state.filters.query.toLocaleLowerCase();
    const harness = readText(session.harness);
    const outcome = normalizeOutcome(session.outcome);
    const capture = sessionHistory(session);
    const searchable = [harness, session.session_id]
      .map(readText)
      .join(" ")
      .toLocaleLowerCase();
    if (query && !searchable.includes(query)) return false;
    if (state.filters.harness && harness !== state.filters.harness) return false;
    if (state.filters.capture && capture !== state.filters.capture) return false;
    if (
      state.filters.outcome === "reported" &&
      !explicitOutcomes.has(outcome)
    ) {
      return false;
    }
    if (
      state.filters.outcome === "unavailable" &&
      !["unknown", "incomplete"].includes(outcome)
    ) {
      return false;
    }
    return true;
  }

  function createSessionCard(session) {
    const sessionID = readText(session.session_id);
    const harness = displayHarness(session.harness);
    const outcome = normalizeOutcome(session.outcome);
    const card = createElement("article", "session-card");
    card.classList.toggle("is-selected", sessionID === state.selectedSessionID);
    const button = createElement("button", "session-card-main");
    button.type = "button";
    button.setAttribute("aria-pressed", String(sessionID === state.selectedSessionID));
    button.addEventListener("click", () => selectSession(sessionID));
    const top = createElement("span", "session-card-top");
    const avatar = createElement("span", "harness-avatar", harnessInitial(harness));
    avatar.setAttribute("aria-hidden", "true");
    const title = createElement("span", "session-card-title");
    title.append(
      createElement("strong", "", `${harness} session`),
      createElement("small", "", formatSessionDate(session)),
    );
    top.append(avatar, title, sessionOutcomeBadge(outcome));
    const meta = createElement("span", "session-card-meta");
    meta.append(createElement("span", "", formatEventCount(session.event_count)));
    const history = sessionHistory(session);
    meta.append(
      createElement(
        "span",
        history === "live" ? "capture-label live" : "capture-label reconstructed",
        history === "mixed"
          ? "Mixed capture"
          : history === "historical"
            ? "Reconstructed"
            : "Live",
      ),
    );
    const time = createElement("time", "", formatRelativeTime(session.ended_at));
    const endedAt = parseDate(session.ended_at);
    if (endedAt) {
      time.dateTime = endedAt.toISOString();
      time.title = formatFullDate(endedAt);
    }
    meta.append(time);
    button.append(top, meta);
    const idRow = createElement("div", "session-id-row");
    const id = createElement("code", "", compactID(sessionID));
    id.title = sessionID;
    const copy = createElement("button", "copy-button", "Copy ID");
    copy.type = "button";
    copy.setAttribute("aria-label", `Copy session ID ${sessionID}`);
    copy.addEventListener("click", () => copyText(sessionID, copy));
    idRow.append(id, copy);
    card.append(button, idRow);
    return card;
  }

  function selectSession(sessionID) {
    const session = state.sessions.find(
      (candidate) => readText(candidate.session_id) === sessionID,
    );
    if (!session) return;
    state.selectedSessionID = sessionID;
    state.selectedEventTotal = toFiniteNumber(session.event_count);
    state.selectedSessionDetail = session;
    state.selectedOverview = null;
    state.events = [];
    state.findings = [];
    state.findingsMayHaveMore = true;
    state.findingsStatus = "loading";
    state.overviewStatus = "loading";
    state.eventLimit = pageLimits.events.initial;
    state.eventNextCursor = "";
    state.eventHasMore = false;
    state.showAllEvents = false;
    state.lastAction = "timeline";
    elements.allEventsToggle.setAttribute("aria-pressed", "false");
    elements.allEventsToggle.classList.remove("is-active");
    renderSessions();
    renderSessionHeader(session);
    renderSessionOverview();
    hideError();
    elements.welcomeState.hidden = true;
    elements.timelineView.hidden = false;
    elements.timelineLoading.hidden = false;
    elements.eventsEmpty.hidden = true;
    elements.eventList.replaceChildren();
    elements.eventsPagination.hidden = true;
    document.body.classList.add("is-timeline-open");
    void loadTimeline(sessionID, false);
    void loadSessionDetail(sessionID);
    void loadFindings(sessionID);
  }

  async function loadSessionDetail(sessionID) {
    try {
      const response = await apiGet(
        `/v1/sessions/${encodeURIComponent(sessionID)}`,
      );
      if (state.selectedSessionID !== sessionID) return;
      const detail = isRecord(response.data) ? response.data : {};
      state.selectedSessionDetail = detail;
      state.selectedOverview = firstRecord(
        response.overview,
        detail.overview,
        response.data_overview,
      );
      state.overviewStatus = state.selectedOverview ? "complete" : "partial";
      renderSessionHeader(detail);
      renderSessionOverview();
    } catch {
      if (state.selectedSessionID !== sessionID) return;
      state.overviewStatus = "partial";
      renderSessionOverview();
    }
  }

  async function loadFindings(sessionID) {
    state.findingsStatus = "loading";
    state.findingsMayHaveMore = true;
    renderFindingList();
    try {
      let cursor = "";
      const seenCursors = new Set();
      do {
        const parameters = new URLSearchParams({
          limit: String(pageLimits.findings.page),
          session_id: sessionID,
        });
        if (cursor) parameters.set("cursor", cursor);
        const response = await apiGet(`/v1/findings?${parameters.toString()}`);
        if (state.selectedSessionID !== sessionID) return;
        const page = Array.isArray(response.data) ? response.data : [];
        if (
          page.some(
            (finding) => readText(finding.session_id) !== sessionID,
          )
        ) {
          throw new Error("Local findings response was not session-scoped.");
        }
        state.findings = deduplicateByID(
          state.findings.concat(page),
          "finding_id",
        );
        const nextCursor = readCursor(response.next_cursor);
        if (response.has_more === true && !nextCursor) {
          throw new Error("Local findings response omitted its continuation cursor.");
        }
        if (nextCursor && seenCursors.has(nextCursor)) {
          throw new Error("Local findings response repeated a continuation cursor.");
        }
        if (nextCursor) seenCursors.add(nextCursor);
        cursor = nextCursor;
        state.findingsMayHaveMore = Boolean(cursor);
        renderSessionOverview();
        renderEvents();
      } while (cursor && state.selectedSessionID === sessionID);
      if (state.selectedSessionID === sessionID) {
        state.findingsStatus = "ready";
        state.findingsMayHaveMore = false;
        renderSessionOverview();
        renderEvents();
      }
    } catch {
      if (state.selectedSessionID !== sessionID) return;
      state.findingsStatus = "error";
      state.findingsMayHaveMore = true;
      renderSessionOverview();
      renderEvents();
    }
  }

  async function loadTimeline(sessionID, append) {
    elements.eventsLoadMore.disabled = true;
    try {
      const cursor = append ? state.eventNextCursor : "";
      const parameters = new URLSearchParams({ limit: String(state.eventLimit) });
      if (cursor) parameters.set("cursor", cursor);
      const response = await apiGet(
        `/v1/sessions/${encodeURIComponent(sessionID)}/events?${parameters.toString()}`,
      );
      if (state.selectedSessionID !== sessionID) return;
      const page = Array.isArray(response.data) ? response.data : [];
      state.events =
        append && cursor
          ? deduplicateByID(state.events.concat(page), "event_id")
          : page;
      state.eventNextCursor = readCursor(response.next_cursor);
      state.eventHasMore =
        response.has_more === true || Boolean(state.eventNextCursor);
      renderEvents();
      renderEventPagination();
      elements.timelineFreshness.textContent = formatFreshness(
        response.data_through,
      );
      renderSessionOverview();
    } catch (error) {
      if (state.selectedSessionID === sessionID) {
        state.eventHasMore = false;
        renderEventPagination();
        showError("Unable to load this timeline", error);
      }
    } finally {
      if (state.selectedSessionID === sessionID) {
        elements.timelineLoading.hidden = true;
        elements.eventsLoadMore.disabled = false;
        renderEvents();
      }
    }
  }

  function loadMoreEvents() {
    if (!state.eventHasMore || !state.selectedSessionID) return;
    if (state.eventNextCursor) {
      loadTimeline(state.selectedSessionID, true);
      return;
    }
    if (state.eventLimit >= pageLimits.events.maximum) return;
    state.eventLimit = Math.min(
      pageLimits.events.maximum,
      state.eventLimit + pageLimits.events.step,
    );
    loadTimeline(state.selectedSessionID, false);
  }

  function renderSessionHeader(session) {
    const harness = displayHarness(session.harness);
    const outcome = normalizeOutcome(session.outcome);
    const history = sessionHistory(session);
    const startedAt = parseDate(session.started_at);
    const endedAt = parseDate(session.ended_at);
    elements.selectedAvatar.textContent = harnessInitial(harness);
    elements.selectedHarness.textContent = `${harness} session`;
    setSessionOutcomeBadge(elements.selectedOutcome, outcome);
    const overview = isRecord(session.overview) ? session.overview : {};
    const outcomeDetail = isRecord(overview.outcome) ? overview.outcome : {};
    if (readText(outcomeDetail.explanation)) {
      elements.selectedOutcome.title = readText(outcomeDetail.explanation);
    }
    elements.selectedHistorical.hidden = history === "live";
    elements.selectedHistorical.textContent =
      history === "mixed" ? "Mixed capture" : "Reconstructed";
    elements.selectedSessionID.textContent = compactID(session.session_id);
    elements.selectedSessionID.title = readText(session.session_id);
    elements.selectedEventCount.textContent = formatNumber(
      state.selectedEventTotal,
    );
    elements.selectedDuration.textContent = formatDuration(startedAt, endedAt);
    elements.selectedEndedAt.textContent = endedAt
      ? formatRelativeTime(endedAt)
      : "Unavailable";
    elements.selectedEndedAt.title = endedAt ? formatFullDate(endedAt) : "";
  }

  function renderSessionOverview() {
    const overview = buildOverview();
    if (state.overviewStatus === "complete") {
      elements.overviewState.textContent = "Full-session metadata";
    } else if (state.overviewStatus === "partial") {
      elements.overviewState.textContent =
        `Partial overview · ${state.events.length} loaded ` +
        `${state.events.length === 1 ? "event" : "events"}`;
    } else {
      elements.overviewState.textContent =
        "Loading full-session overview; interim values use loaded events.";
    }
    const metrics = [
      ["Commands", overview.commands],
      ["Tools", overview.tools],
      ["Files", overview.files],
      ["Network", overview.network],
      ["Permissions", overview.permissions],
      ["Explicit failures", overview.failures],
      ["Findings", overview.findings],
      ["Outcome unreported", overview.unreported],
    ];
    const fragment = document.createDocumentFragment();
    metrics.forEach(([label, count]) => {
      const item = createElement("div");
      item.append(
        createElement("dt", "", label),
        createElement("dd", "", formatNumber(count)),
      );
      fragment.append(item);
    });
    elements.activityGrid.replaceChildren(fragment);
    elements.overviewQuality.textContent = [
      overview.coverage ? `Coverage · ${overview.coverage}` : "",
      overview.confidence ? `Confidence · ${overview.confidence}` : "",
    ]
      .filter(Boolean)
      .join("   ");
    const resourceFragment = document.createDocumentFragment();
    overview.resources.slice(0, 12).forEach((resource) => {
      resourceFragment.append(createElement("span", "resource-chip", resource));
    });
    if (!overview.resources.length) {
      resourceFragment.append(
        createElement("p", "overview-empty", "No resource metadata reported."),
      );
    } else if (overview.resources.length > 12) {
      resourceFragment.append(
        createElement(
          "span",
          "resource-chip more",
          `+${overview.resources.length - 12} more`,
        ),
      );
    }
    elements.resourceList.replaceChildren(resourceFragment);
    if (overview.partial) {
      elements.resourceDisclosure.textContent =
        "Partial: resources are derived from loaded timeline events.";
    } else if (overview.resourcesTruncated) {
      elements.resourceDisclosure.textContent =
        `Showing ${Math.min(12, overview.resources.length)} of ` +
        `${overview.resources.length} returned salient resources; ` +
        "additional resources exist beyond the API projection.";
    } else if (overview.resources.length > 12) {
      elements.resourceDisclosure.textContent =
        `Showing 12 of ${overview.resources.length} salient resources.`;
    } else {
      elements.resourceDisclosure.textContent = "";
    }
    renderFindingList();
  }

  function buildOverview() {
    const derived = deriveOverview(state.events);
    const supplied = isRecord(state.selectedOverview)
      ? state.selectedOverview
      : {};
    return {
      commands: overviewCount(supplied, derived.commands, "commands", "command_count"),
      tools: overviewCount(
        supplied,
        derived.tools,
        "tools",
        "tool_count",
        "tool_calls",
      ),
      files: overviewFileCount(supplied, derived.files),
      network: overviewCount(
        supplied,
        derived.network,
        "network",
        "network_count",
        "network_indicators",
      ),
      permissions: overviewCount(
        supplied,
        derived.permissions,
        "permissions",
        "permission_count",
        "permission_events",
      ),
      failures: overviewCount(
        supplied,
        derived.failures,
        "explicit_failures",
        "failure_count",
        "failed_events",
        "explicit_failed_events",
      ),
      findings: overviewCount(
        supplied,
        state.findings.length,
        "findings",
        "finding_count",
      ),
      unreported: overviewCount(
        supplied,
        derived.unreported,
        "unreported_outcomes",
        "unreported_outcome_count",
        "unknown_outcomes",
        "source_unreported_outcomes",
      ),
      resources: overviewResources(supplied, derived.resources),
      resourcesTruncated: supplied.salient_resources_truncated === true,
      coverage: overviewCoverage(supplied, derived.coverage, "depths"),
      confidence: overviewCoverage(supplied, derived.confidence, "confidences"),
      partial: !state.selectedOverview,
    };
  }

  function deriveOverview(events) {
    const result = {
      commands: 0,
      tools: 0,
      files: 0,
      network: 0,
      permissions: 0,
      failures: 0,
      unreported: 0,
      resources: [],
      coverage: "",
      confidence: "",
    };
    const resources = new Set();
    const coverage = new Set();
    const confidence = new Set();
    events.forEach((event) => {
      const observation = isRecord(event.observation) ? event.observation : {};
      const type = readText(observation.type).toLocaleLowerCase();
      if (type.startsWith("command.")) result.commands += 1;
      if (type.startsWith("tool.")) result.tools += 1;
      if (type.startsWith("file.")) result.files += 1;
      if (type.startsWith("network.")) result.network += 1;
      if (type.startsWith("permission.")) result.permissions += 1;
      const outcome = normalizeOutcome(observation.outcome);
      if (outcome === "failed") result.failures += 1;
      if (outcome === "unknown") result.unreported += 1;
      const resource = isRecord(observation.resource)
        ? observation.resource
        : {};
      const resourceText = readText(resource.name) || readText(resource.kind);
      if (resourceText) resources.add(resourceText);
      const eventCoverage = isRecord(event.coverage) ? event.coverage : {};
      if (readText(eventCoverage.depth)) coverage.add(readText(eventCoverage.depth));
      if (readText(eventCoverage.confidence)) {
        confidence.add(readText(eventCoverage.confidence));
      }
    });
    result.resources = Array.from(resources);
    result.coverage = joinObservedValues(coverage);
    result.confidence = joinObservedValues(confidence);
    return result;
  }

  function overviewCount(overview, fallback, ...keys) {
    const sources = [overview, overview.counts, overview.activity_counts].filter(
      isRecord,
    );
    for (const source of sources) {
      for (const key of keys) {
        if (Number.isFinite(Number(source[key]))) {
          return toFiniteNumber(source[key]);
        }
      }
    }
    return fallback;
  }

  function overviewText(overview, fallback, ...keys) {
    for (const key of keys) {
      if (readText(overview[key])) return readText(overview[key]);
    }
    return fallback;
  }

  function overviewFileCount(overview, fallback) {
    const direct = overviewCount(overview, -1, "files", "file_count");
    if (direct >= 0) return direct;
    const counts = isRecord(overview.counts) ? overview.counts : overview;
    const keys = ["file_reads", "file_writes", "file_deletes"];
    if (keys.some((key) => Number.isFinite(Number(counts[key])))) {
      return keys.reduce((total, key) => total + toFiniteNumber(counts[key]), 0);
    }
    return fallback;
  }

  function overviewCoverage(overview, fallback, key) {
    const observed = isRecord(overview.observed_coverage)
      ? overview.observed_coverage
      : {};
    if (Array.isArray(observed[key])) {
      return observed[key].map(readableLabel).filter(Boolean).join(", ");
    }
    return overviewText(
      overview,
      fallback,
      key === "depths" ? "coverage" : "confidence",
      key === "depths" ? "coverage_depth" : "coverage_confidence",
    );
  }

  function overviewResources(overview, fallback) {
    const candidates = [
      overview.salient_resources,
      overview.resources_touched,
      overview.resources,
      overview.resource_names,
    ];
    for (const candidate of candidates) {
      if (Array.isArray(candidate)) {
        return candidate
          .map((value) => {
            if (isRecord(value)) {
              return readText(value.name) || readText(value.kind);
            }
            return readText(value);
          })
          .filter(Boolean);
      }
    }
    return fallback;
  }

  function renderFindingList() {
    const fragment = document.createDocumentFragment();
    const supplied = isRecord(state.selectedOverview)
      ? state.selectedOverview
      : {};
    const expectedCount = overviewCount(
      supplied,
      -1,
      "findings",
      "finding_count",
    );
    if (!state.findings.length) {
      if (state.findingsStatus === "loading") {
        fragment.append(
          createElement(
            "p",
            "overview-empty",
            "Loading session findings from configured rules…",
          ),
        );
      } else if (state.findingsStatus === "error") {
        fragment.append(
          createElement(
            "p",
            "overview-empty",
            "Findings unavailable; cited timeline events may be incomplete.",
          ),
        );
      } else if (expectedCount > 0) {
        fragment.append(
          createElement(
            "p",
            "overview-empty",
            `The session overview reports ${expectedCount} ` +
              `${expectedCount === 1 ? "finding" : "findings"}, ` +
              "but the session-scoped findings response returned none.",
          ),
        );
      } else {
        fragment.append(
          createElement(
            "p",
            "overview-empty",
            "No findings reported by configured rules.",
          ),
        );
      }
    } else {
      state.findings.forEach((finding) => {
        const item = createElement("article", "finding-item");
        const severity = readText(finding.severity) || "reported";
        const heading = createElement("div");
        const badge = createElement("span", "finding-badge", readableLabel(severity));
        badge.dataset.tone = severity.toLocaleLowerCase();
        heading.append(
          badge,
          createElement(
            "strong",
            "",
            readText(finding.rule_id) || "Configured rule",
          ),
        );
        item.append(heading);
        const evidenceCount = findingEventIDs(finding).length;
        item.append(
          createElement(
            "small",
            "",
            `${evidenceCount} cited ${evidenceCount === 1 ? "event" : "events"}${
              readText(finding.confidence)
                ? ` · ${readText(finding.confidence)} confidence`
                : ""
            }`,
          ),
        );
        fragment.append(item);
      });
      if (state.findingsStatus === "loading" || state.findingsMayHaveMore) {
        fragment.append(
          createElement(
            "small",
            "overview-caveat",
            "Loading additional session findings…",
          ),
        );
      } else if (state.findingsStatus === "error") {
        fragment.append(
          createElement(
            "small",
            "overview-caveat",
            "Some session findings could not be loaded.",
          ),
        );
      } else if (expectedCount >= 0 && expectedCount !== state.findings.length) {
        fragment.append(
          createElement(
            "small",
            "overview-caveat",
            `Loaded ${state.findings.length} of ${expectedCount} findings ` +
              "reported by the session overview.",
          ),
        );
      }
    }
    elements.findingList.replaceChildren(fragment);
  }

  function renderEvents() {
    const cited = citedFindingMap();
    const signalEvents = state.events.filter((event) => isSignalEvent(event, cited));
    const visible = state.showAllEvents ? state.events : signalEvents;
    const hiddenCount = state.events.length - signalEvents.length;
    const fragment = document.createDocumentFragment();
    visible.forEach((event) => {
      fragment.append(createEventRow(event, cited.get(readText(event.event_id)) || []));
    });
    elements.eventList.replaceChildren(fragment);
    elements.eventsEmpty.hidden =
      visible.length !== 0 || !elements.timelineLoading.hidden;
    elements.timelineScope.textContent = state.showAllEvents
      ? `Showing all ${visible.length} loaded events.`
      : hiddenCount > 0
        ? `Showing ${visible.length} signal events; ${hiddenCount} repetitive lifecycle events are collapsed.`
        : `Showing ${visible.length} signal events.`;
    elements.allEventsToggle.disabled = state.events.length === 0;
  }

  function renderEventPagination() {
    const atMaximum =
      !state.eventNextCursor && state.eventLimit >= pageLimits.events.maximum;
    elements.eventsPagination.hidden = !state.eventHasMore;
    elements.eventsLoadMore.hidden = !state.eventHasMore || atMaximum;
    if (state.eventHasMore && atMaximum) {
      elements.eventsPageStatus.textContent =
        `Loaded ${state.events.length} of ${state.selectedEventTotal} events. ` +
        "Additional events exist beyond this browser bound.";
    } else if (state.eventHasMore) {
      elements.eventsPageStatus.textContent =
        `Loaded ${state.events.length} of ${state.selectedEventTotal} events.`;
    } else {
      elements.eventsPageStatus.textContent =
        `Loaded ${state.events.length} events returned for this session.`;
    }
  }

  function createEventRow(event, findings) {
    const observation = isRecord(event.observation) ? event.observation : {};
    const outcome = normalizeOutcome(observation.outcome);
    const occurredAt = parseDate(event.occurred_at);
    const descriptor = eventDescriptor(observation);
    const row = createElement(
      "article",
      findings.length ? "event-row has-finding" : "event-row",
    );
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
    node.dataset.tone = explicitOutcomes.has(outcome) ? outcome : "unknown";
    node.setAttribute("aria-hidden", "true");
    rail.append(node);
    const card = createElement("div", "event-card");
    const heading = createElement("div", "event-heading");
    heading.append(createElement("strong", "", descriptor.label));
    if (explicitOutcomes.has(outcome)) {
      const badge = createElement("span", "status-badge", explicitOutcomeLabel(outcome));
      badge.dataset.tone = outcome;
      heading.append(badge);
    }
    if (findings.length) {
      heading.append(
        createElement(
          "span",
          "finding-badge",
          `${findings.length} ${findings.length === 1 ? "finding" : "findings"}`,
        ),
      );
    }
    heading.append(
      createElement(
        "span",
        "event-type",
        readText(observation.type) || "event",
      ),
    );
    card.append(heading);
    if (descriptor.detail) {
      card.append(createElement("p", "event-summary", descriptor.detail));
    }
    if (outcome === "unknown") {
      card.append(
        createElement(
          "p",
          "outcome-unreported",
          "Outcome · Not reported by source",
        ),
      );
    }
    if (findings.length) {
      const findingRefs = createElement("div", "event-findings");
      findings.forEach((finding) => {
        findingRefs.append(
          createElement(
            "span",
            "",
            `Cited by ${readText(finding.rule_id) || "configured rule"}`,
          ),
        );
      });
      card.append(findingRefs);
    }
    card.append(createEvidenceDetails(event));
    row.append(time, rail, card);
    return row;
  }

  function createEvidenceDetails(event) {
    const observation = isRecord(event.observation) ? event.observation : {};
    const source = isRecord(event.source) ? event.source : {};
    const coverage = isRecord(event.coverage) ? event.coverage : {};
    const historical = isRecord(event.historical) ? event.historical : {};
    const details = createElement("details", "evidence-details");
    details.append(createElement("summary", "", "Evidence"));
    const list = createElement("dl");
    appendEvidence(list, "Event ID", event.event_id);
    appendEvidence(list, "Harness", source.agent);
    appendEvidence(list, "Source", source.kind);
    appendEvidence(list, "Coverage", coverage.depth);
    appendEvidence(list, "Confidence", coverage.confidence);
    if (historical.is_historical) {
      appendEvidence(
        list,
        "Reconstruction",
        historical.reconstruction_source || "historical source",
      );
    } else {
      appendEvidence(list, "Capture", "Live");
    }
    if (observation.exit_code !== undefined && observation.exit_code !== null) {
      appendEvidence(list, "Exit code", observation.exit_code);
    }
    details.append(list);
    return details;
  }

  function appendEvidence(list, label, value) {
    const text = readText(value);
    if (!text) return;
    const row = createElement("div");
    row.append(createElement("dt", "", label), createElement("dd", "", text));
    list.append(row);
  }

  function eventDescriptor(observation) {
    const type = readText(observation.type).toLocaleLowerCase();
    const resource = isRecord(observation.resource) ? observation.resource : {};
    const details = isRecord(observation.details) ? observation.details : {};
    const resourceName = readText(resource.name);
    const summary = readText(observation.summary);
    const labels = {
      "session.start": "Session started",
      "session.end": "Session ended",
      "command.exec": "Ran command",
      "command.result": "Command completed",
      "tool.call": "Called tool",
      "tool.result": "Tool returned",
      "file.read": "Read file",
      "file.write": "Changed file",
      "file.create": "Created file",
      "file.delete": "Deleted file",
      "permission.request": "Requested permission",
      "permission.decision": "Permission decided",
      "network.indicator": "Observed network target",
      "prompt.user": "User input lifecycle",
      "message.assistant": "Assistant lifecycle",
      "reasoning.start": "Reasoning lifecycle started",
      "reasoning.end": "Reasoning lifecycle ended",
    };
    let label = labels[type] || readableLabel(observation.action, "Activity");
    if (type.startsWith("tool.") && readText(details.mcp_tool)) {
      label = `${label} · ${readText(details.mcp_tool)}`;
    }
    return { label, detail: resourceName || summary };
  }

  function isSignalEvent(event, cited) {
    const observation = isRecord(event.observation) ? event.observation : {};
    const type = readText(observation.type).toLocaleLowerCase();
    const eventID = readText(event.event_id);
    const outcome = normalizeOutcome(observation.outcome);
    return (
      cited.has(eventID) ||
      explicitOutcomes.has(outcome) ||
      ["session.", "command.", "tool.", "file.", "permission.", "network."].some(
        (prefix) => type.startsWith(prefix),
      )
    );
  }

  function citedFindingMap() {
    const result = new Map();
    state.findings.forEach((finding) => {
      findingEventIDs(finding).forEach((eventID) => {
        const values = result.get(eventID) || [];
        values.push(finding);
        result.set(eventID, values);
      });
    });
    return result;
  }

  function findingEventIDs(finding) {
    const values = Array.isArray(finding.cited_event_ids)
      ? finding.cited_event_ids
      : Array.isArray(finding.event_ids)
        ? finding.event_ids
        : [];
    return values.map(readText).filter(Boolean);
  }

  function sessionOutcomeBadge(outcome) {
    const badge = createElement("span", "status-badge");
    setSessionOutcomeBadge(badge, outcome);
    return badge;
  }

  function setSessionOutcomeBadge(badge, outcome) {
    badge.textContent = sessionOutcomeLabel(outcome);
    badge.dataset.tone = outcome;
    badge.title = sessionOutcomeExplanation(outcome);
  }

  function sessionOutcomeLabel(outcome) {
    if (outcome === "unknown") return "Outcome unavailable";
    if (outcome === "incomplete") return "No terminal event";
    return explicitOutcomeLabel(outcome);
  }

  function sessionOutcomeExplanation(outcome) {
    if (outcome === "unknown") {
      return "The source did not report a terminal outcome.";
    }
    if (outcome === "incomplete") {
      return "Belay did not observe a terminal session event.";
    }
    return `The source explicitly reported ${explicitOutcomeLabel(outcome).toLocaleLowerCase()}.`;
  }

  function explicitOutcomeLabel(outcome) {
    return readableLabel(outcome, "Outcome unavailable");
  }

  function closeTimeline() {
    state.selectedSessionID = "";
    state.selectedEventTotal = 0;
    state.selectedSessionDetail = null;
    state.selectedOverview = null;
    state.events = [];
    state.findings = [];
    state.findingsMayHaveMore = false;
    state.findingsStatus = "idle";
    state.overviewStatus = "idle";
    state.eventNextCursor = "";
    state.eventHasMore = false;
    document.body.classList.remove("is-timeline-open");
    elements.timelineView.hidden = true;
    elements.timelineLoading.hidden = true;
    elements.welcomeState.hidden = false;
    elements.eventsPagination.hidden = true;
    renderSessions();
  }

  function retryLastAction() {
    if (state.lastAction === "timeline" && state.selectedSessionID) {
      selectSession(state.selectedSessionID);
      return;
    }
    refreshAll(false);
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
    if (!isRecord(body)) throw new Error("Local API returned an invalid response.");
    return body;
  }

  async function readResponseBody(response) {
    const contentType = response.headers.get("content-type") || "";
    if (!contentType.toLocaleLowerCase().includes("json")) return null;
    try {
      return await response.json();
    } catch {
      return null;
    }
  }

  async function copyText(value, button) {
    if (!value || !globalThis.navigator.clipboard) return;
    try {
      await globalThis.navigator.clipboard.writeText(value);
      const prior = button.textContent;
      button.textContent = "Copied";
      globalThis.setTimeout(() => {
        button.textContent = prior;
      }, 1200);
    } catch {
      button.title = "Clipboard access was unavailable.";
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
    if (!base) return "";
    try {
      const url = new URL(base, globalThis.location.origin);
      if (url.origin !== globalThis.location.origin) return "";
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
    if (className) element.className = className;
    if (text !== undefined && text !== null) element.textContent = String(text);
    return element;
  }

  function isRecord(value) {
    return Boolean(value) && typeof value === "object" && !Array.isArray(value);
  }

  function firstRecord(...values) {
    return values.find(isRecord) || null;
  }

  function readText(value) {
    if (typeof value === "string") return value;
    if (typeof value === "number" && Number.isFinite(value)) return String(value);
    return "";
  }

  function readCursor(value) {
    return typeof value === "string" && value.trim() ? value : "";
  }

  function toFiniteNumber(value) {
    const number = Number(value);
    return Number.isFinite(number) && number >= 0 ? number : 0;
  }

  function deduplicateByID(values, key) {
    const seen = new Set();
    return values.filter((value) => {
      const id = readText(value && value[key]);
      if (!id || seen.has(id)) return false;
      seen.add(id);
      return true;
    });
  }

  function displayHarness(value) {
    return readableLabel(value, "Unknown harness");
  }

  function readableLabel(value, fallback = "") {
    const text = readText(value).trim();
    if (!text) return fallback;
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

  function compactID(value) {
    const text = readText(value);
    return text.length <= 22 ? text : `${text.slice(0, 11)}…${text.slice(-7)}`;
  }

  function parseDate(value) {
    if (value instanceof Date && Number.isFinite(value.getTime())) return value;
    const date = new Date(readText(value));
    return Number.isFinite(date.getTime()) ? date : null;
  }

  function dateLowerBound(days) {
    const count = Number(days);
    if (!Number.isFinite(count) || count <= 0) return "";
    return new Date(Date.now() - count * 86400000).toISOString();
  }

  function recencyGroup(session) {
    const date = parseDate(session.ended_at) || parseDate(session.started_at);
    if (!date) return "Date unavailable";
    const today = new Date();
    const startToday = new Date(
      today.getFullYear(),
      today.getMonth(),
      today.getDate(),
    );
    const startSession = new Date(
      date.getFullYear(),
      date.getMonth(),
      date.getDate(),
    );
    const days = Math.round(
      (startToday.getTime() - startSession.getTime()) / 86400000,
    );
    if (days <= 0) return "Today";
    if (days === 1) return "Yesterday";
    if (days < 7) return "Earlier this week";
    return new Intl.DateTimeFormat(undefined, {
      month: "long",
      year: "numeric",
    }).format(date);
  }

  function formatSessionDate(session) {
    const date = parseDate(session.started_at);
    return date
      ? new Intl.DateTimeFormat(undefined, {
          month: "short",
          day: "numeric",
          hour: "numeric",
          minute: "2-digit",
        }).format(date)
      : "Start time unavailable";
  }

  function formatNumber(value) {
    return new Intl.NumberFormat().format(toFiniteNumber(value));
  }

  function formatNumberOrDash(value) {
    return Number.isFinite(Number(value)) ? formatNumber(value) : "—";
  }

  function formatEventCount(count) {
    const value = toFiniteNumber(count);
    return `${formatNumber(value)} ${value === 1 ? "event" : "events"}`;
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
    if (!date) return "Unavailable";
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
    return new Intl.RelativeTimeFormat(undefined, { numeric: "auto" }).format(
      amount,
      unit,
    );
  }

  function formatDuration(startedAt, endedAt) {
    if (!startedAt || !endedAt) return "Unavailable";
    const milliseconds = Math.max(0, endedAt.getTime() - startedAt.getTime());
    const minutes = Math.floor(milliseconds / 60000);
    const hours = Math.floor(minutes / 60);
    if (hours) return `${hours}h ${minutes % 60}m`;
    if (minutes) return `${minutes}m`;
    return `${Math.max(1, Math.round(milliseconds / 1000))}s`;
  }

  function formatFreshness(value) {
    const date = parseDate(value);
    return date ? `Data current ${formatRelativeTime(date)}` : "";
  }

  function joinObservedValues(values) {
    return Array.from(values).map(readableLabel).join(", ");
  }

  function sessionHistory(session) {
    const value = readText(session.history).toLocaleLowerCase();
    if (["historical", "live", "mixed"].includes(value)) return value;
    return session.historical ? "historical" : "live";
  }
})();
