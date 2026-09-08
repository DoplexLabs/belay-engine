(() => {
  "use strict";

  const pageLimits = Object.freeze({
    sessions: { initial: 40, step: 40, maximum: 100 },
    events: { initial: 100, step: 100, maximum: 500 },
    findings: { page: 20 },
    issues: { page: 20 },
    occurrences: { page: 20 },
  });
  const explicitOutcomes = new Set(["succeeded", "failed", "interrupted"]);
  const issueCatalog = Object.freeze({
    "issue.explicit_command_failure": Object.freeze({
      title: "Command failed",
      explanation: "The source explicitly reported a failed command result.",
    }),
    "issue.repeated_command_attempts": Object.freeze({
      title: "Command repeatedly attempted",
      explanation:
        "The same private command signature was observed multiple times in one bounded interval.",
      experimental: true,
    }),
    "issue.explicit_permission_denial": Object.freeze({
      title: "Permission denied",
      explanation: "The source explicitly reported a denied permission event.",
    }),
    "issue.verification_not_observed": Object.freeze({
      title: "Verification evidence not observed",
      explanation:
        "A supported live session ended without the required verification evidence.",
      evidenceGap: true,
    }),
    "issue.unresolved_verification_failure_at_completion": Object.freeze({
      title: "Verification still failed at session end",
      explanation:
        "A verification command explicitly failed and no later successful verification was observed before session end.",
    }),
    "issue.numbat_finding": Object.freeze({
      title: "Numbat finding",
      explanation: "A retained upstream Numbat finding was reported.",
    }),
  });
  const analysisQualifiers = Object.freeze({
    pending: "Prior retained result while reanalysis is pending.",
    failed: "Prior retained result; the latest analysis failed.",
    truncated: "Partial analysis; additional signals may be absent.",
    unknown:
      "Analysis status is unavailable; result freshness and completeness are uncertain.",
  });
  const config = globalThis.BELAY_LOCAL_CONFIG || {};
  const state = {
    token: resolveToken(config),
    apiBase: normalizeApiBase(config.apiBase),
    activeView: "attention",
    issues: createIssueBucket("issue"),
    evidenceGaps: createIssueBucket("evidence_gap"),
    issueFilters: {
      severity: "",
      recurrence: "",
      harness: "",
      category: "",
      origin: "",
      analysisStatus: "",
      experimental: false,
    },
    selectedIssueID: "",
    selectedIssueKind: "",
    selectedIssue: null,
    occurrences: [],
    occurrenceNextCursor: "",
    occurrenceHasMore: false,
    occurrenceStatus: "idle",
    occurrenceRequestGeneration: 0,
    attentionRefreshGeneration: 0,
    directSessionRequestGeneration: 0,
    issueReturnFocus: null,
    sessionReturnFocus: null,
    sessionReturnView: "",
    attentionExpiryRefresh: false,
    refreshNoticeTimer: 0,
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
    findingNextCursor: "",
    findingHasMore: false,
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
    navAttention: document.querySelector("#nav-attention"),
    navSessions: document.querySelector("#nav-sessions"),
    attentionNavCount: document.querySelector("#attention-nav-count"),
    attentionView: document.querySelector("#attention-view"),
    sessionsView: document.querySelector("#sessions-view"),
    attentionListPane: document.querySelector("#attention-list-pane"),
    attentionDetailPane: document.querySelector("#attention-detail-pane"),
    attentionCount: document.querySelector("#attention-count"),
    attentionRefreshNotice: document.querySelector("#attention-refresh-notice"),
    coverageCurrent: document.querySelector("#coverage-current"),
    coveragePending: document.querySelector("#coverage-pending"),
    coverageFailed: document.querySelector("#coverage-failed"),
    coverageTruncated: document.querySelector("#coverage-truncated"),
    coverageUnscoped: document.querySelector("#coverage-unscoped"),
    coverageThrough: document.querySelector("#coverage-through"),
    coverageCompleteness: document.querySelector("#coverage-completeness"),
    attentionFilters: document.querySelector("#attention-filters"),
    issueFilterSeverity: document.querySelector("#issue-filter-severity"),
    issueFilterRecurrence: document.querySelector("#issue-filter-recurrence"),
    issueFilterHarness: document.querySelector("#issue-filter-harness"),
    issueFilterCategory: document.querySelector("#issue-filter-category"),
    issueFilterOrigin: document.querySelector("#issue-filter-origin"),
    issueFilterStatus: document.querySelector("#issue-filter-status"),
    issueFilterExperimental: document.querySelector(
      "#issue-filter-experimental",
    ),
    clearAttentionFilters: document.querySelector("#clear-attention-filters"),
    attentionFilterNote: document.querySelector("#attention-filter-note"),
    issueFilterDisclosure: document.querySelector("#issue-filter-disclosure"),
    issueCount: document.querySelector("#issue-count"),
    issueList: document.querySelector("#issue-list"),
    issuesLoading: document.querySelector("#issues-loading"),
    issuesEmpty: document.querySelector("#issues-empty"),
    issuesEmptyTitle: document.querySelector("#issues-empty-title"),
    issuesEmptyDetail: document.querySelector("#issues-empty-detail"),
    issuesPagination: document.querySelector("#issues-pagination"),
    issuesPageStatus: document.querySelector("#issues-page-status"),
    issuesLoadMore: document.querySelector("#issues-load-more"),
    evidenceGapCount: document.querySelector("#evidence-gap-count"),
    evidenceGapList: document.querySelector("#evidence-gap-list"),
    evidenceGapsLoading: document.querySelector("#evidence-gaps-loading"),
    evidenceGapsEmpty: document.querySelector("#evidence-gaps-empty"),
    evidenceGapsEmptyTitle: document.querySelector(
      "#evidence-gaps-empty-title",
    ),
    evidenceGapsEmptyDetail: document.querySelector(
      "#evidence-gaps-empty-detail",
    ),
    evidenceGapsPagination: document.querySelector(
      "#evidence-gaps-pagination",
    ),
    evidenceGapsPageStatus: document.querySelector(
      "#evidence-gaps-page-status",
    ),
    evidenceGapsLoadMore: document.querySelector(
      "#evidence-gaps-load-more",
    ),
    attentionWelcome: document.querySelector("#attention-welcome"),
    issueDetail: document.querySelector("#issue-detail"),
    issueBackButton: document.querySelector("#issue-back-button"),
    issueDetailKind: document.querySelector("#issue-detail-kind"),
    issueDetailHeading: document.querySelector("#issue-detail-heading"),
    issueDetailBadges: document.querySelector("#issue-detail-badges"),
    issueAnalysisQualifier: document.querySelector(
      "#issue-analysis-qualifier",
    ),
    issueExplanation: document.querySelector("#issue-explanation"),
    issueScopeDisclosure: document.querySelector("#issue-scope-disclosure"),
    issueMetadata: document.querySelector("#issue-metadata"),
    issueFingerprint: document.querySelector("#issue-fingerprint"),
    copyIssueFingerprint: document.querySelector("#copy-issue-fingerprint"),
    occurrenceCount: document.querySelector("#occurrence-count"),
    occurrenceList: document.querySelector("#occurrence-list"),
    occurrencesLoading: document.querySelector("#occurrences-loading"),
    occurrencesPagination: document.querySelector("#occurrences-pagination"),
    occurrencesPageStatus: document.querySelector("#occurrences-page-status"),
    occurrencesLoadMore: document.querySelector("#occurrences-load-more"),
    refreshButton: document.querySelector("#refresh-button"),
    sessionPanel: document.querySelector(".session-panel"),
    timelinePanel: document.querySelector(".timeline-panel"),
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
    findingsPagination: document.querySelector("#findings-pagination"),
    findingsPageStatus: document.querySelector("#findings-page-status"),
    findingsLoadMore: document.querySelector("#findings-load-more"),
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

  const focusRegistry = {
    issueCards: new Map(),
    occurrenceActions: new Map(),
    sessionCards: new Map(),
  };
  let searchTimer = 0;
  let issueFilterTimer = 0;
  const mobileQuery = globalThis.matchMedia("(max-width: 680px)");
  bindEvents();
  setActiveView("attention", false);
  refreshAll(false);

  function createIssueBucket(kind) {
    return {
      kind,
      data: [],
      nextCursor: "",
      hasMore: false,
      viewCursor: "",
      analysis: null,
      status: "idle",
      error: null,
      requestGeneration: 0,
    };
  }

  function bindEvents() {
    elements.attentionFilters.addEventListener("submit", (event) => {
      event.preventDefault();
      state.issueFilters.harness = elements.issueFilterHarness.value.trim();
      state.issueFilters.category =
        elements.issueFilterCategory.value.trim().toLowerCase();
      resetAndLoadAttention();
    });
    elements.navAttention.addEventListener("click", () => {
      setActiveView("attention", true);
      if (state.issues.status === "idle") refreshAttention(false);
    });
    elements.navSessions.addEventListener("click", () => {
      setActiveView("sessions", true);
      if (!state.sessions.length) refreshSessions(false);
    });
    elements.refreshButton.addEventListener("click", () => refreshAll(true));
    elements.issuesLoadMore.addEventListener("click", () => {
      loadIssueBucket(state.issues, true);
    });
    elements.evidenceGapsLoadMore.addEventListener("click", () => {
      loadIssueBucket(state.evidenceGaps, true);
    });
    elements.occurrencesLoadMore.addEventListener("click", loadMoreOccurrences);
    elements.issueBackButton.addEventListener("click", closeIssueDetail);
    elements.copyIssueFingerprint.addEventListener("click", () => {
      copyText(
        readText(state.selectedIssue && state.selectedIssue.fingerprint_id),
        elements.copyIssueFingerprint,
      );
    });
    elements.sessionsLoadMore.addEventListener("click", loadMoreSessions);
    elements.eventsLoadMore.addEventListener("click", loadMoreEvents);
    elements.findingsLoadMore.addEventListener("click", loadMoreFindings);
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

    [
      [elements.issueFilterSeverity, "severity"],
      [elements.issueFilterRecurrence, "recurrence"],
      [elements.issueFilterOrigin, "origin"],
      [elements.issueFilterStatus, "analysisStatus"],
    ].forEach(([element, key]) => {
      element.addEventListener("change", () => {
        state.issueFilters[key] = element.value;
        resetAndLoadAttention();
      });
    });
    [elements.issueFilterHarness, elements.issueFilterCategory].forEach(
      (element) => {
        element.addEventListener("input", () => {
          globalThis.clearTimeout(issueFilterTimer);
          issueFilterTimer = globalThis.setTimeout(() => {
            state.issueFilters.harness =
              elements.issueFilterHarness.value.trim();
            state.issueFilters.category =
              elements.issueFilterCategory.value.trim().toLowerCase();
            resetAndLoadAttention();
          }, 250);
        });
      },
    );
    elements.issueFilterExperimental.addEventListener("change", () => {
      state.issueFilters.experimental =
        elements.issueFilterExperimental.checked;
      resetAndLoadAttention();
    });
    elements.clearAttentionFilters.addEventListener("click", () => {
      state.issueFilters = {
        severity: "",
        recurrence: "",
        harness: "",
        category: "",
        origin: "",
        analysisStatus: "",
        experimental: false,
      };
      elements.issueFilterSeverity.value = "";
      elements.issueFilterRecurrence.value = "";
      elements.issueFilterHarness.value = "";
      elements.issueFilterCategory.value = "";
      elements.issueFilterOrigin.value = "";
      elements.issueFilterStatus.value = "";
      elements.issueFilterExperimental.checked = false;
      resetAndLoadAttention();
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
      if (event.key !== "Escape") return;
      if (state.activeView === "sessions" && state.selectedSessionID) {
        closeTimeline();
      } else if (state.activeView === "attention" && state.selectedIssueID) {
        closeIssueDetail();
      }
    });
    const handleMobileChange = () => applyPaneAccessibility();
    if (typeof mobileQuery.addEventListener === "function") {
      mobileQuery.addEventListener("change", handleMobileChange);
    } else if (typeof mobileQuery.addListener === "function") {
      mobileQuery.addListener(handleMobileChange);
    }
  }

  async function refreshAll(preserveSelection) {
    hideError();
    elements.refreshButton.disabled = true;
    try {
      if (state.activeView === "attention") {
        await refreshAttention(preserveSelection);
        return;
      }
      await refreshSessions(preserveSelection);
    } finally {
      elements.refreshButton.disabled = false;
    }
  }

  async function refreshSessions(preserveSelection) {
    const selectedID = preserveSelection ? state.selectedSessionID : "";
    state.sessionLimit = pageLimits.sessions.initial;
    state.sessionNextCursor = "";
    await Promise.allSettled([loadStats(), loadSessions(false)]);
    if (
      selectedID &&
      state.sessions.some((session) => readText(session.session_id) === selectedID)
    ) {
      openSession(selectedID, state.selectedSessionDetail);
    }
  }

  function setActiveView(view, moveFocus) {
    const next = view === "sessions" ? "sessions" : "attention";
    if (next === "sessions" && state.activeView !== "sessions") {
      state.attentionRefreshGeneration += 1;
    }
    if (next === "attention" && state.activeView !== "attention") {
      state.directSessionRequestGeneration += 1;
    }
    state.activeView = next;
    const attentionActive = next === "attention";
    setViewVisibility(elements.attentionView, attentionActive);
    setViewVisibility(elements.sessionsView, !attentionActive);
    setCurrentNavigation(elements.navAttention, attentionActive);
    setCurrentNavigation(elements.navSessions, !attentionActive);
    applyPaneAccessibility();
    if (moveFocus) {
      focusCurrentElement(
        attentionActive ? elements.navAttention : elements.navSessions,
      );
    }
  }

  function setViewVisibility(element, visible) {
    element.hidden = !visible;
    element.inert = !visible;
    if (visible) {
      element.removeAttribute("aria-hidden");
    } else {
      element.setAttribute("aria-hidden", "true");
    }
  }

  function setCurrentNavigation(button, current) {
    if (current) {
      button.setAttribute("aria-current", "page");
    } else {
      button.removeAttribute("aria-current");
    }
  }

  function applyPaneAccessibility() {
    const mobile = mobileQuery.matches;
    const issueOpen = Boolean(state.selectedIssueID);
    const sessionOpen = Boolean(state.selectedSessionID);
    setObscuredPane(
      elements.attentionListPane,
      state.activeView === "attention" && mobile && issueOpen,
    );
    setObscuredPane(
      elements.attentionDetailPane,
      state.activeView === "attention" && mobile && !issueOpen,
    );
    setObscuredPane(
      elements.sessionPanel,
      state.activeView === "sessions" && mobile && sessionOpen,
    );
    setObscuredPane(
      elements.timelinePanel,
      state.activeView === "sessions" && mobile && !sessionOpen,
    );
  }

  function setObscuredPane(element, obscured) {
    element.inert = obscured;
    if (obscured) {
      element.setAttribute("aria-hidden", "true");
    } else {
      element.removeAttribute("aria-hidden");
    }
  }

  function issueFocusKey(kind, issueID) {
    return `${kind === "evidence_gap" ? "evidence_gap" : "issue"}\u0000${issueID}`;
  }

  function occurrenceFocusKey(issueID, occurrence) {
    const occurrenceID = readText(occurrence && occurrence.occurrence_id);
    const sessionID = readText(occurrence && occurrence.session_id);
    return `${issueID}\u0000${occurrenceID || sessionID}`;
  }

  function clearIssueFocusRegistry(kind) {
    const prefix = `${kind === "evidence_gap" ? "evidence_gap" : "issue"}\u0000`;
    Array.from(focusRegistry.issueCards.keys()).forEach((key) => {
      if (key.startsWith(prefix)) focusRegistry.issueCards.delete(key);
    });
  }

  function resolveFocusReference(reference) {
    if (!reference) return null;
    if (reference.type === "issue") {
      return focusRegistry.issueCards.get(reference.key) || null;
    }
    if (reference.type === "occurrence") {
      return focusRegistry.occurrenceActions.get(reference.key) || null;
    }
    if (reference.type === "session") {
      return focusRegistry.sessionCards.get(reference.sessionID) || null;
    }
    return null;
  }

  function canReceiveFocus(element) {
    if (
      !element ||
      !element.isConnected ||
      typeof element.focus !== "function" ||
      element.closest("[hidden]") ||
      element.closest('[aria-hidden="true"]')
    ) {
      return false;
    }
    let current = element;
    while (current) {
      if (current.inert === true) return false;
      current = current.parentElement;
    }
    return true;
  }

  function focusCurrentElement(element) {
    if (!canReceiveFocus(element)) return false;
    element.focus();
    return true;
  }

  function restoreLogicalFocus(reference, fallback) {
    return (
      focusCurrentElement(resolveFocusReference(reference)) ||
      focusCurrentElement(fallback)
    );
  }

  async function refreshAttention(preserveSelection, suppressFailureNotice = false) {
    const refreshGeneration = ++state.attentionRefreshGeneration;
    const selectedID = preserveSelection ? state.selectedIssueID : "";
    const selectedKind = preserveSelection ? state.selectedIssueKind : "";
    const selectedBucket =
      selectedKind === "evidence_gap" ? state.evidenceGaps : state.issues;
    const selectedPageBudget = Math.max(
      1,
      Math.ceil(selectedBucket.data.length / pageLimits.issues.page),
    );
    if (selectedID) {
      closeIssueDetail(false);
      showAttentionNotice(
        "Refreshing Attention; selected detail is hidden until current data confirms it.",
        "pending",
      );
    } else if (!preserveSelection) {
      closeIssueDetail(false);
    }
    resetIssueBucket(state.issues);
    resetIssueBucket(state.evidenceGaps);
    const [issuesReady, gapsReady] = await Promise.all([
      loadIssueBucket(state.issues, false),
      loadIssueBucket(state.evidenceGaps, false),
    ]);
    if (refreshGeneration !== state.attentionRefreshGeneration) return false;
    if (!issuesReady || !gapsReady) {
      if (!suppressFailureNotice && !state.attentionExpiryRefresh) {
        showAttentionNotice(
          selectedID
            ? "Attention refresh failed. Selected detail remains closed to avoid showing stale data."
            : "Attention refresh failed. The issue lists may be incomplete; retry before relying on them.",
          "error",
        );
      }
      return false;
    }
    if (!selectedID) return true;

    const bucket =
      selectedKind === "evidence_gap" ? state.evidenceGaps : state.issues;
    const chainReady = await loadIssuePagesForSelection(
      bucket,
      selectedID,
      selectedPageBudget,
    );
    if (refreshGeneration !== state.attentionRefreshGeneration) return false;
    if (!chainReady) {
      if (!suppressFailureNotice && !state.attentionExpiryRefresh) {
        showAttentionNotice(
          "Attention refresh failed while confirming the selected signal. Its detail remains closed.",
          "error",
        );
      }
      return false;
    }
    const summary = bucket.data.find(
      (issue) => readText(issue.issue_id) === selectedID,
    );
    if (!summary) {
      showAttentionNotice(
        bucket.hasMore
          ? "Attention refreshed. The selected signal was not confirmed in the refreshed loaded results; load more to find it."
          : "Attention refreshed. The selected signal is no longer visible under the current filters.",
        "status",
        7000,
      );
      return true;
    }
    const detailReady = await selectIssue(summary, selectedKind, false);
    if (refreshGeneration !== state.attentionRefreshGeneration) return false;
    if (!detailReady) {
      closeIssueDetail(false);
      if (!suppressFailureNotice && !state.attentionExpiryRefresh) {
        showAttentionNotice(
          "Attention lists refreshed, but the selected detail could not be reloaded and remains closed.",
          "error",
        );
      }
      return false;
    }
    showAttentionNotice(
      "Attention refreshed; selected detail was reloaded from the current view.",
      "success",
      4000,
    );
    return true;
  }

  async function loadIssuePagesForSelection(bucket, issueID, pageBudget) {
    for (let page = 1; page < pageBudget; page += 1) {
      if (
        bucket.data.some((issue) => readText(issue.issue_id) === issueID) ||
        !bucket.hasMore
      ) {
        break;
      }
      const pageReady = await loadIssueBucket(bucket, true);
      if (!pageReady) return false;
    }
    return true;
  }

  function resetAndLoadAttention() {
    state.attentionRefreshGeneration += 1;
    closeIssueDetail(false);
    hideAttentionNotice();
    resetIssueBucket(state.issues);
    resetIssueBucket(state.evidenceGaps);
    renderAttentionFilters();
    void Promise.all([
      loadIssueBucket(state.issues, false),
      loadIssueBucket(state.evidenceGaps, false),
    ]);
  }

  function resetIssueBucket(bucket) {
    bucket.requestGeneration += 1;
    bucket.data = [];
    bucket.nextCursor = "";
    bucket.hasMore = false;
    bucket.viewCursor = "";
    bucket.analysis = null;
    bucket.status = "idle";
    bucket.error = null;
  }

  async function loadIssueBucket(bucket, append) {
    const generation = ++bucket.requestGeneration;
    const cursor = append ? bucket.nextCursor : "";
    bucket.status = append ? "loading-more" : "loading";
    bucket.error = null;
    renderIssueBucket(bucket);
    try {
      const response = await apiGet(buildIssuePath(bucket.kind, cursor));
      if (generation !== bucket.requestGeneration) return false;
      const page = Array.isArray(response.data) ? response.data : [];
      bucket.data =
        append && cursor
          ? deduplicateByID(bucket.data.concat(page), "issue_id")
          : deduplicateByID(page, "issue_id");
      bucket.nextCursor = readCursor(response.next_cursor);
      bucket.hasMore =
        response.has_more === true || Boolean(bucket.nextCursor);
      const viewCursor = readCursor(response.view_cursor);
      if (viewCursor) bucket.viewCursor = viewCursor;
      bucket.analysis = isRecord(response.analysis) ? response.analysis : null;
      bucket.status = "ready";
      renderIssueBucket(bucket);
      renderAnalysisCoverage();
      return true;
    } catch (error) {
      if (generation !== bucket.requestGeneration) return false;
      if (isCursorExpired(error)) {
        await refreshAttentionAfterExpiry();
        return false;
      }
      bucket.status = "error";
      bucket.error = error;
      renderIssueBucket(bucket);
      renderAnalysisCoverage();
      showError(
        bucket.kind === "evidence_gap"
          ? "Unable to load evidence gaps"
          : "Unable to load Attention",
        error,
      );
      return false;
    }
  }

  function buildIssuePath(kind, cursor) {
    const parameters = new URLSearchParams({
      limit: String(pageLimits.issues.page),
      attention_kind: kind,
      experimental: state.issueFilters.experimental ? "include" : "stable",
    });
    if (cursor) parameters.set("cursor", cursor);
    if (state.issueFilters.severity) {
      parameters.set("severity", state.issueFilters.severity);
    }
    if (state.issueFilters.recurrence) {
      parameters.set("recurrence", state.issueFilters.recurrence);
    }
    if (state.issueFilters.harness) {
      parameters.set("harness", state.issueFilters.harness);
    }
    if (state.issueFilters.category) {
      parameters.set("category", state.issueFilters.category);
    }
    if (state.issueFilters.origin) {
      parameters.set("origin", state.issueFilters.origin);
    }
    if (state.issueFilters.analysisStatus) {
      parameters.set("analysis_status", state.issueFilters.analysisStatus);
    }
    return `/v1/issues?${parameters.toString()}`;
  }

  function renderIssueBucket(bucket) {
    const isGap = bucket.kind === "evidence_gap";
    const list = isGap ? elements.evidenceGapList : elements.issueList;
    const loading = isGap
      ? elements.evidenceGapsLoading
      : elements.issuesLoading;
    const empty = isGap ? elements.evidenceGapsEmpty : elements.issuesEmpty;
    const count = isGap ? elements.evidenceGapCount : elements.issueCount;
    const pagination = isGap
      ? elements.evidenceGapsPagination
      : elements.issuesPagination;
    const pageStatus = isGap
      ? elements.evidenceGapsPageStatus
      : elements.issuesPageStatus;
    const loadMore = isGap
      ? elements.evidenceGapsLoadMore
      : elements.issuesLoadMore;
    clearIssueFocusRegistry(bucket.kind);
    const fragment = document.createDocumentFragment();
    bucket.data.forEach((issue) => {
      fragment.append(createIssueCard(issue, bucket.kind));
    });
    list.replaceChildren(fragment);
    loading.hidden = bucket.status !== "loading";
    empty.hidden =
      bucket.data.length !== 0 ||
      bucket.status === "loading" ||
      bucket.status === "idle";
    count.textContent = bucket.hasMore
      ? `${bucket.data.length}+`
      : String(bucket.data.length);
    pagination.hidden = !bucket.hasMore;
    loadMore.disabled = bucket.status === "loading-more";
    pageStatus.textContent = bucket.hasMore
      ? `Showing ${bucket.data.length}; more are available.`
      : `Showing ${bucket.data.length} returned ${
          isGap ? "evidence gaps" : "issues"
        }.`;
    if (!empty.hidden) renderIssueEmptyState(bucket);
    renderAttentionTotals();
    renderAttentionFilters();
  }

  function renderIssueEmptyState(bucket) {
    const isGap = bucket.kind === "evidence_gap";
    const title = isGap
      ? elements.evidenceGapsEmptyTitle
      : elements.issuesEmptyTitle;
    const detail = isGap
      ? elements.evidenceGapsEmptyDetail
      : elements.issuesEmptyDetail;
    if (bucket.status === "error") {
      title.textContent = isGap
        ? "Evidence gaps unavailable"
        : "Issues unavailable";
      detail.textContent = "The Local read failed. Existing session data remains available.";
      return;
    }
    if (attentionFiltersActive()) {
      title.textContent = isGap
        ? "No evidence gaps match these filters"
        : "No issues match these filters";
      detail.textContent =
        "Analysis coverage is shown above; this is not a global safety claim.";
      return;
    }
    const analysis = bucket.analysis || state.issues.analysis;
    if (analysis && analysis.complete === true) {
      title.textContent = isGap
        ? "No evidence gaps reported by configured detectors"
        : "No issues reported by configured detectors";
      detail.textContent = isGap
        ? "Supported completed analysis did not report a verification evidence gap."
        : "Configured deterministic detectors reported no stable issues.";
      return;
    }
    title.textContent = isGap
      ? "No evidence gaps are available from completed analysis"
      : "No issues are available from completed analysis";
    detail.textContent = incompleteCoverageText(analysis);
  }

  function renderAnalysisCoverage() {
    const analysis = state.issues.analysis || state.evidenceGaps.analysis;
    elements.coverageCurrent.textContent = analysis
      ? formatNumber(analysis.current_sessions)
      : "—";
    elements.coveragePending.textContent = analysis
      ? formatNumber(analysis.pending_sessions)
      : "—";
    elements.coverageFailed.textContent = analysis
      ? formatNumber(analysis.failed_sessions)
      : "—";
    elements.coverageTruncated.textContent = analysis
      ? formatNumber(analysis.truncated_sessions)
      : "—";
    elements.coverageCompleteness.textContent =
      analysis && analysis.complete === true ? "Complete" : "Incomplete";
    const unscoped = analysis ? toFiniteNumber(analysis.unscoped_sessions) : 0;
    elements.coverageUnscoped.textContent = unscoped
      ? `${formatNumber(unscoped)} fully analyzed ${
          unscoped === 1 ? "session is" : "sessions are"
        } unscoped and cannot establish cross-session recurrence.`
      : "Scoped analysis can establish exact recurrence where evidence permits.";
    elements.coverageThrough.textContent = parseDate(
      analysis && analysis.analysis_through,
    )
      ? `Latest completed analysis · ${formatRelativeTime(
          analysis.analysis_through,
        )}`
      : "Latest completed analysis unavailable";
  }

  function renderAttentionTotals() {
    const total = state.issues.data.length;
    elements.attentionCount.textContent = state.issues.hasMore
      ? `${total}+`
      : String(total);
    elements.attentionNavCount.hidden = total === 0;
    elements.attentionNavCount.textContent = state.issues.hasMore
      ? `${total}+`
      : String(total);
  }

  function renderAttentionFilters() {
    const experimental = state.issueFilters.experimental;
    const active = attentionFiltersActive();
    elements.attentionFilterNote.textContent = experimental
      ? "Experimental signals are included and labeled."
      : active
        ? "Stable signals matching the selected filters."
        : "Showing stable signals.";
    elements.clearAttentionFilters.disabled = !active;
    elements.issueFilterDisclosure.textContent = occurrenceFiltersActive()
      ? "Filter selected each issue through a matching occurrence; totals include all exact occurrences visible in this snapshot."
      : "";
  }

  function attentionFiltersActive() {
    return (
      state.issueFilters.experimental ||
      [
        "severity",
        "recurrence",
        "harness",
        "category",
        "origin",
        "analysisStatus",
      ].some((key) => Boolean(state.issueFilters[key]))
    );
  }

  function occurrenceFiltersActive() {
    return Boolean(
      state.issueFilters.harness || state.issueFilters.analysisStatus,
    );
  }

  function createIssueCard(issue, kind) {
    const issueID = readText(issue.issue_id);
    const article = createElement("article", "issue-card");
    const button = createElement("button", "issue-card-main");
    button.type = "button";
    button.setAttribute("aria-pressed", String(
      issueID === state.selectedIssueID,
    ));
    button.addEventListener("click", () => {
      void selectIssue(issue, kind, true);
    });
    if (issueID) {
      focusRegistry.issueCards.set(issueFocusKey(kind, issueID), button);
    }
    const top = createElement("span", "issue-card-top");
    const severity = createElement(
      "span",
      "severity-badge",
      readableLabel(issue.severity, "Reported"),
    );
    severity.dataset.tone = severityTone(issue.severity);
    const title = createElement("span", "issue-card-title");
    const catalog = issueCatalogEntry(issue.title_code);
    title.append(
      createElement("strong", "", catalog.title),
      createElement(
        "small",
        "",
        issueRecurrenceLabel(issue),
      ),
    );
    top.append(severity, title);
    const status = normalizeAnalysisStatus(issue.analysis_status);
    if (status !== "current") {
      top.append(
        createElement("span", "analysis-badge", analysisStatusLabel(status)),
      );
    }
    const meta = createElement("span", "issue-card-meta");
    meta.append(
      createElement(
        "span",
        "",
        `${formatNumber(issue.occurrence_count)} ${
          toFiniteNumber(issue.occurrence_count) === 1
            ? "occurrence"
            : "occurrences"
        }`,
      ),
      createElement("span", "", issueHarnessLabel(issue.harnesses)),
      createElement(
        "time",
        "",
        formatRelativeTime(issue.last_observed_at),
      ),
    );
    button.append(top, meta);
    const caveat = issueCaveat(issue);
    if (caveat) button.append(createElement("span", "issue-card-caveat", caveat));
    if (catalog.experimental || issue.experimental === true) {
      button.append(
        createElement("span", "experimental-label", "Experimental signal"),
      );
    }
    if (catalog.title === "Detected issue") {
      button.append(
        createElement(
          "span",
          "issue-code",
          safeCatalogCode(issue.category),
        ),
      );
    }
    article.append(button);
    return article;
  }

  function selectIssue(issue, kind, moveFocus) {
    const issueID = readText(issue.issue_id);
    if (!issueID) return Promise.resolve(false);
    if (moveFocus) state.attentionRefreshGeneration += 1;
    state.selectedIssueID = issueID;
    state.selectedIssueKind = kind === "evidence_gap" ? "evidence_gap" : "issue";
    state.selectedIssue = issue;
    state.issueReturnFocus = {
      type: "issue",
      key: issueFocusKey(state.selectedIssueKind, issueID),
    };
    state.occurrences = [];
    state.occurrenceNextCursor = "";
    state.occurrenceHasMore = false;
    state.occurrenceStatus = "loading";
    elements.attentionWelcome.hidden = true;
    elements.issueDetail.hidden = false;
    document.body.classList.add("is-attention-detail-open");
    renderIssueBucket(state.issues);
    renderIssueBucket(state.evidenceGaps);
    renderIssueDetail();
    applyPaneAccessibility();
    if (moveFocus) focusCurrentElement(elements.issueDetailHeading);
    const bucket =
      state.selectedIssueKind === "evidence_gap"
        ? state.evidenceGaps
        : state.issues;
    return loadIssueDetail(false, bucket.viewCursor);
  }

  async function loadIssueDetail(append, viewCursor) {
    const issueID = state.selectedIssueID;
    if (!issueID) return false;
    const generation = ++state.occurrenceRequestGeneration;
    state.occurrenceStatus = append ? "loading-more" : "loading";
    renderIssueDetail();
    const parameters = new URLSearchParams({
      limit: String(pageLimits.occurrences.page),
    });
    if (append && state.occurrenceNextCursor) {
      parameters.set("cursor", state.occurrenceNextCursor);
    } else if (viewCursor) {
      parameters.set("view_cursor", viewCursor);
    }
    try {
      const response = await apiGet(
        `/v1/issues/${encodeURIComponent(issueID)}/occurrences?${parameters.toString()}`,
      );
      if (
        generation !== state.occurrenceRequestGeneration ||
        issueID !== state.selectedIssueID
      ) {
        return false;
      }
      const payload = isRecord(response.data) ? response.data : {};
      const issue = firstRecord(payload.issue, response.issue);
      const page = Array.isArray(payload.occurrences)
        ? payload.occurrences
        : Array.isArray(response.occurrences)
          ? response.occurrences
          : [];
      if (issue) state.selectedIssue = issue;
      state.occurrences =
        append && state.occurrenceNextCursor
          ? deduplicateByID(
              state.occurrences.concat(page),
              "occurrence_id",
            )
          : deduplicateByID(page, "occurrence_id");
      state.occurrenceNextCursor = readCursor(response.next_cursor);
      state.occurrenceHasMore =
        response.has_more === true || Boolean(state.occurrenceNextCursor);
      state.occurrenceStatus = "ready";
      renderIssueDetail();
      return true;
    } catch (error) {
      if (
        generation !== state.occurrenceRequestGeneration ||
        issueID !== state.selectedIssueID
      ) {
        return false;
      }
      if (isCursorExpired(error)) {
        await refreshAttentionAfterExpiry();
        return false;
      }
      state.occurrenceStatus = "error";
      renderIssueDetail();
      showError("Unable to load matching sessions", error);
      return false;
    }
  }

  function loadMoreOccurrences() {
    if (
      !state.selectedIssueID ||
      !state.occurrenceHasMore ||
      !state.occurrenceNextCursor
    ) {
      return;
    }
    void loadIssueDetail(true, "");
  }

  function renderIssueDetail() {
    const issue = state.selectedIssue || {};
    const catalog = issueCatalogEntry(issue.title_code);
    const kind =
      state.selectedIssueKind === "evidence_gap" ? "Evidence gap" : "Issue";
    elements.issueDetailKind.textContent = kind;
    elements.issueDetailHeading.textContent = catalog.title;
    elements.issueExplanation.textContent = catalog.explanation;
    const status = normalizeAnalysisStatus(issue.analysis_status);
    elements.issueAnalysisQualifier.textContent =
      analysisQualifiers[status] || "";
    elements.issueScopeDisclosure.textContent = issueScopeDisclosure(issue);
    const badges = [
      createToneBadge(issue.severity),
      createElement("span", "meta-badge", issueRecurrenceLabel(issue)),
      createElement("span", "meta-badge", analysisStatusLabel(status)),
    ];
    if (catalog.experimental || issue.experimental === true) {
      badges.push(createElement("span", "experimental-label", "Experimental"));
    }
    elements.issueDetailBadges.replaceChildren(...badges);
    elements.issueFingerprint.textContent =
      readText(issue.fingerprint_id) || "Unavailable";
    elements.copyIssueFingerprint.disabled = !readText(issue.fingerprint_id);
    renderIssueMetadata(issue);
    renderOccurrences();
  }

  function renderIssueMetadata(issue) {
    const metadata = [
      ["Observed", retainedInterval(issue)],
      ["Confidence", readableLabel(issue.confidence, "Unavailable")],
      ["Scope", readableLabel(issue.scope_quality, "Unavailable")],
      ["Origin", readableLabel(issue.origin, "Unavailable")],
      ["Detector", detectorVersionLabel(issue)],
      [
        "Evidence",
        evidenceCompletenessLabel(issue.evidence_complete),
      ],
    ];
    const fragment = document.createDocumentFragment();
    metadata.forEach(([label, value]) => {
      const row = createElement("div");
      row.append(createElement("dt", "", label), createElement("dd", "", value));
      fragment.append(row);
    });
    elements.issueMetadata.replaceChildren(fragment);
  }

  function renderOccurrences() {
    focusRegistry.occurrenceActions.clear();
    const fragment = document.createDocumentFragment();
    state.occurrences.forEach((occurrence) => {
      fragment.append(createOccurrenceRow(occurrence));
    });
    elements.occurrenceList.replaceChildren(fragment);
    elements.occurrencesLoading.hidden =
      state.occurrenceStatus !== "loading";
    elements.occurrenceCount.textContent = state.occurrenceHasMore
      ? `${state.occurrences.length}+`
      : String(state.occurrences.length);
    elements.occurrencesPagination.hidden = !state.occurrenceHasMore;
    elements.occurrencesLoadMore.disabled =
      state.occurrenceStatus === "loading-more";
    elements.occurrencesPageStatus.textContent = state.occurrenceHasMore
      ? `Showing ${state.occurrences.length}; more exact matches are available.`
      : `Showing ${state.occurrences.length} exact matching ${
          state.occurrences.length === 1 ? "session" : "sessions"
        }.`;
    if (
      state.occurrenceStatus === "ready" &&
      state.occurrences.length === 0
    ) {
      elements.occurrenceList.append(
        createElement(
          "p",
          "overview-empty",
          "No retained matching-session occurrence was returned.",
        ),
      );
    }
  }

  function createOccurrenceRow(occurrence) {
    const focusKey = occurrenceFocusKey(state.selectedIssueID, occurrence);
    const article = createElement("article", "occurrence-card");
    const header = createElement("div", "occurrence-header");
    const identity = createElement("div");
    identity.append(
      createElement(
        "strong",
        "",
        `${displayHarness(occurrence.harness)} session`,
      ),
      createElement("code", "", compactID(occurrence.session_id)),
    );
    const status = createElement(
      "span",
      "analysis-badge",
      analysisStatusLabel(occurrence.analysis_status),
    );
    header.append(identity, status);
    const metadata = createElement("p", "occurrence-meta");
    metadata.textContent = [
      retainedInterval(occurrence),
      readableLabel(occurrence.origin, "Unknown origin"),
      `${occurrenceEventIDs(occurrence).length} cited ${
        occurrenceEventIDs(occurrence).length === 1 ? "event" : "events"
      }`,
      occurrence.evidence_complete === false
        ? "Evidence incomplete"
        : occurrence.evidence_complete === true
          ? "Evidence complete"
          : "Evidence completeness unavailable",
    ].join(" · ");
    const actions = createElement("div", "occurrence-actions");
    const inspect = createElement(
      "button",
      "secondary-button",
      "Inspect session evidence",
    );
    inspect.type = "button";
    inspect.setAttribute("aria-expanded", "false");
    const open = createElement("button", "primary-button", "Open full session");
    open.type = "button";
    if (focusKey) focusRegistry.occurrenceActions.set(focusKey, open);
    const evidence = createElement("div", "occurrence-evidence");
    evidence.hidden = true;
    inspect.addEventListener("click", () => {
      void loadOccurrenceEvidence(occurrence, evidence, inspect);
    });
    open.addEventListener("click", () => {
      state.sessionReturnFocus = { type: "occurrence", key: focusKey };
      state.sessionReturnView = "attention";
      openSession(readText(occurrence.session_id), null);
    });
    actions.append(inspect, open);
    article.append(header, metadata, actions, evidence);
    return article;
  }

  async function loadOccurrenceEvidence(occurrence, container, button) {
    if (container.dataset.loaded === "true") {
      container.hidden = !container.hidden;
      button.setAttribute("aria-expanded", String(!container.hidden));
      return;
    }
    const sessionID = readText(occurrence.session_id);
    const eventIDs = occurrenceEventIDs(occurrence).slice(0, 50);
    container.hidden = false;
    button.disabled = true;
    button.setAttribute("aria-expanded", "true");
    container.replaceChildren(
      createElement("p", "overview-empty", "Loading cited events…"),
    );
    if (!sessionID || eventIDs.length === 0) {
      container.replaceChildren(
        createElement(
          "p",
          "overview-empty",
          "No retained cited event IDs were supplied for this occurrence.",
        ),
      );
      container.dataset.loaded = "true";
      button.disabled = false;
      return;
    }
    const parameters = new URLSearchParams();
    eventIDs.forEach((eventID) => parameters.append("event_id", eventID));
    try {
      const response = await apiGet(
        `/v1/sessions/${encodeURIComponent(sessionID)}/events/lookup?${parameters.toString()}`,
      );
      renderOccurrenceEvidence(container, response, eventIDs.length);
      container.dataset.loaded = "true";
    } catch (error) {
      container.replaceChildren(
        createElement(
          "p",
          "overview-empty",
          error instanceof Error
            ? error.message
            : "Cited events could not be loaded.",
        ),
      );
    } finally {
      button.disabled = false;
    }
  }

  function renderOccurrenceEvidence(container, response, requestedFallback) {
    const events = Array.isArray(response.data) ? response.data : [];
    const requested = toFiniteNumber(response.requested_count) || requestedFallback;
    const found = toFiniteNumber(response.found_count) || events.length;
    const missing = Number.isFinite(Number(response.missing_count))
      ? toFiniteNumber(response.missing_count)
      : Math.max(0, requested - found);
    const fragment = document.createDocumentFragment();
    fragment.append(
      createElement(
        "p",
        "evidence-lookup-summary",
        `${found} of ${requested} cited events retained; ${missing} missing.`,
      ),
    );
    events.forEach((event) => fragment.append(createLookupEvent(event)));
    if (!events.length) {
      fragment.append(
        createElement(
          "p",
          "overview-empty",
          "No requested cited event remains in this retained session.",
        ),
      );
    }
    container.replaceChildren(fragment);
  }

  function createLookupEvent(event) {
    const observation = isRecord(event.observation) ? event.observation : {};
    const row = createElement("article", "lookup-event");
    row.append(
      createElement(
        "strong",
        "",
        eventDescriptor(observation).label,
      ),
      createElement(
        "time",
        "",
        formatFullDate(parseDate(event.occurred_at)) || "Time unavailable",
      ),
      createElement(
        "p",
        "",
        eventDescriptor(observation).detail ||
          readText(observation.type) ||
          "Retained event",
      ),
    );
    return row;
  }

  function closeIssueDetail(restoreFocus = true) {
    const returnFocus = state.issueReturnFocus;
    state.occurrenceRequestGeneration += 1;
    state.selectedIssueID = "";
    state.selectedIssueKind = "";
    state.selectedIssue = null;
    state.occurrences = [];
    state.occurrenceNextCursor = "";
    state.occurrenceHasMore = false;
    state.occurrenceStatus = "idle";
    state.issueReturnFocus = null;
    document.body.classList.remove("is-attention-detail-open");
    elements.issueDetail.hidden = true;
    elements.attentionWelcome.hidden = false;
    renderIssueBucket(state.issues);
    renderIssueBucket(state.evidenceGaps);
    applyPaneAccessibility();
    if (restoreFocus) {
      restoreLogicalFocus(returnFocus, elements.navAttention);
    }
  }

  async function refreshAttentionAfterExpiry() {
    if (state.attentionExpiryRefresh) return;
    state.attentionExpiryRefresh = true;
    closeIssueDetail(false);
    showAttentionNotice(
      "The issue view changed. Refreshing both Attention lists from a current snapshot…",
      "pending",
    );
    try {
      const refreshed = await refreshAttention(false, true);
      if (refreshed) {
        showAttentionNotice(
          "Attention refreshed from a current snapshot.",
          "success",
          4000,
        );
      } else {
        showAttentionNotice(
          "Attention refresh failed. Retry before relying on the issue lists.",
          "error",
        );
      }
    } finally {
      state.attentionExpiryRefresh = false;
    }
  }

  function showAttentionNotice(message, tone = "status", hideAfter = 0) {
    const safeTone = ["status", "pending", "success", "error"].includes(tone)
      ? tone
      : "status";
    globalThis.clearTimeout(state.refreshNoticeTimer);
    elements.attentionRefreshNotice.textContent = message;
    elements.attentionRefreshNotice.dataset.tone = safeTone;
    elements.attentionRefreshNotice.setAttribute(
      "role",
      safeTone === "error" ? "alert" : "status",
    );
    elements.attentionRefreshNotice.setAttribute(
      "aria-live",
      safeTone === "error" ? "assertive" : "polite",
    );
    elements.attentionRefreshNotice.hidden = false;
    if (hideAfter > 0) {
      state.refreshNoticeTimer = globalThis.setTimeout(
        hideAttentionNotice,
        hideAfter,
      );
    }
  }

  function hideAttentionNotice() {
    globalThis.clearTimeout(state.refreshNoticeTimer);
    elements.attentionRefreshNotice.hidden = true;
    elements.attentionRefreshNotice.textContent = "";
    elements.attentionRefreshNotice.dataset.tone = "status";
    elements.attentionRefreshNotice.setAttribute("aria-live", "polite");
  }

  function issueCatalogEntry(titleCode) {
    const code = readText(titleCode);
    return (
      issueCatalog[code] || {
        title: "Detected issue",
        explanation:
          "A configured deterministic detector reported retained evidence.",
      }
    );
  }

  function issueRecurrenceLabel(issue) {
    const sessions = Number(issue && issue.session_count);
    if (!Number.isFinite(sessions) || sessions < 1) {
      return "Session count unavailable";
    }
    return sessions > 1
      ? `Exact match across ${formatNumber(sessions)} sessions`
      : "Observed in one session";
  }

  function issueHarnessLabel(values) {
    const harnesses = Array.isArray(values)
      ? values.map(displayHarness).filter(Boolean)
      : [];
    return harnesses.length ? harnesses.join(", ") : "Harness unavailable";
  }

  function issueCaveat(issue) {
    if (issue.evidence_complete === false) return "Evidence is incomplete.";
    if (issue.evidence_complete !== true) {
      return "Evidence completeness is unavailable.";
    }
    const quality = readText(issue.scope_quality).toLowerCase();
    if (quality === "unscoped") {
      return "Unscoped evidence cannot establish cross-session recurrence.";
    }
    if (quality === "conflict") {
      return "Conflicting scope evidence prevents recurrence claims.";
    }
    if (issue.retained_history_only === true) {
      return "Based on retained Local history only.";
    }
    return "";
  }

  function issueScopeDisclosure(issue) {
    const caveat = issueCaveat(issue);
    return caveat
      ? caveat
      : "This explanation is a fixed detector definition, not a generated diagnosis.";
  }

  function createToneBadge(value) {
    const badge = createElement(
      "span",
      "severity-badge",
      readableLabel(value, "Reported"),
    );
    badge.dataset.tone = severityTone(value);
    return badge;
  }

  function severityTone(value) {
    const severity = readText(value).toLocaleLowerCase();
    return ["critical", "high", "medium", "low", "info"].includes(severity)
      ? severity
      : "reported";
  }

  function normalizeAnalysisStatus(value) {
    const status = readText(value).toLowerCase();
    return ["current", "pending", "failed", "truncated"].includes(status)
      ? status
      : "unknown";
  }

  function analysisStatusLabel(value) {
    const status = normalizeAnalysisStatus(value);
    if (status === "truncated") return "Partial";
    if (status === "unknown") return "Analysis status unavailable";
    return readableLabel(status);
  }

  function safeCatalogCode(value) {
    const code = readText(value).toLowerCase();
    return /^[a-z0-9_]{1,64}$/.test(code) ? code : "unknown_category";
  }

  function evidenceCompletenessLabel(value) {
    if (value === true) return "Complete";
    if (value === false) return "Incomplete";
    return "Unavailable";
  }

  function retainedInterval(value) {
    const first = parseDate(value && value.first_observed_at);
    const last = parseDate(value && value.last_observed_at);
    if (first && last) {
      return `${formatFullDate(first)} – ${formatFullDate(last)}`;
    }
    return first || last
      ? formatFullDate(first || last)
      : "Retained interval unavailable";
  }

  function detectorVersionLabel(issue) {
    const detector = readText(issue.detector_id) || "Configured detector";
    const version = readText(issue.detector_version);
    return version ? `${detector} · v${version}` : detector;
  }

  function occurrenceEventIDs(occurrence) {
    const evidence = isRecord(occurrence.evidence) ? occurrence.evidence : {};
    const values = Array.isArray(evidence.cited_event_ids)
      ? evidence.cited_event_ids
      : [];
    return values.map(readText).filter(Boolean);
  }

  function incompleteCoverageText(analysis) {
    if (!analysis) return "Analysis coverage is unavailable.";
    return [
      `${formatNumber(analysis.pending_sessions)} pending`,
      `${formatNumber(analysis.failed_sessions)} failed`,
      `${formatNumber(analysis.truncated_sessions)} partial`,
    ].join(" · ");
  }

  function isCursorExpired(error) {
    return (
      error instanceof LocalAPIError &&
      (error.status === 410 || error.problemType === "belay.local/cursor-expired")
    );
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
    focusRegistry.sessionCards.clear();
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
    if (sessionID) focusRegistry.sessionCards.set(sessionID, button);
    button.addEventListener("click", () => {
      state.sessionReturnFocus = { type: "session", sessionID };
      state.sessionReturnView = "sessions";
      selectSession(sessionID);
    });
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
    beginSessionSelection(sessionID, session);
    void loadTimeline(sessionID, false);
    void loadSessionDetail(sessionID);
    void loadFindings(sessionID);
  }

  function openSession(sessionID, optionalKnownSummary) {
    if (!sessionID) return;
    const known =
      optionalKnownSummary ||
      state.sessions.find(
        (candidate) => readText(candidate.session_id) === sessionID,
      );
    setActiveView("sessions", false);
    if (!known) {
      const generation = ++state.directSessionRequestGeneration;
      void loadDirectSession(sessionID, generation);
      return;
    }
    beginSessionSelection(sessionID, known);
    void loadTimeline(sessionID, false);
    void loadSessionDetail(sessionID);
    void loadFindings(sessionID);
  }

  function beginSessionSelection(sessionID, session) {
    state.selectedSessionID = sessionID;
    state.selectedEventTotal = toFiniteNumber(session.event_count);
    state.selectedSessionDetail = session;
    state.selectedOverview = null;
    state.events = [];
    state.findings = [];
    state.findingNextCursor = "";
    state.findingHasMore = false;
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
    elements.backButton.setAttribute(
      "aria-label",
      state.sessionReturnView === "attention"
        ? "Back to issue detail"
        : "Back to sessions",
    );
    document.body.classList.add("is-timeline-open");
    applyPaneAccessibility();
    focusCurrentElement(elements.selectedHarness);
  }

  async function loadSessionDetail(sessionID) {
    try {
      const response = await apiGet(
        `/v1/sessions/${encodeURIComponent(sessionID)}`,
      );
      if (state.selectedSessionID !== sessionID) return;
      const detail = isRecord(response.data) ? response.data : {};
      state.selectedSessionDetail = detail;
      state.selectedEventTotal = toFiniteNumber(detail.event_count);
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

  async function loadDirectSession(sessionID, generation) {
    hideError();
    try {
      const response = await apiGet(
        `/v1/sessions/${encodeURIComponent(sessionID)}`,
      );
      if (
        generation !== state.directSessionRequestGeneration ||
        state.activeView !== "sessions"
      ) {
        return;
      }
      const detail = isRecord(response.data) ? response.data : null;
      if (!detail) throw new Error("Local session detail was unavailable.");
      beginSessionSelection(sessionID, detail);
      state.selectedSessionDetail = detail;
      state.selectedEventTotal = toFiniteNumber(detail.event_count);
      state.selectedOverview = firstRecord(
        response.overview,
        detail.overview,
        response.data_overview,
      );
      state.overviewStatus = state.selectedOverview ? "complete" : "partial";
      renderSessionHeader(detail);
      renderSessionOverview();
      void loadTimeline(sessionID, false);
      void loadFindings(sessionID);
    } catch (error) {
      if (generation !== state.directSessionRequestGeneration) return;
      const returnView = state.sessionReturnView;
      const returnFocus = state.sessionReturnFocus;
      state.sessionReturnView = "";
      state.sessionReturnFocus = null;
      if (returnView === "attention") {
        setActiveView("attention", false);
        restoreLogicalFocus(
          returnFocus,
          state.selectedIssueID
            ? elements.issueDetailHeading
            : elements.navAttention,
        );
      } else {
        applyPaneAccessibility();
        restoreLogicalFocus(returnFocus, elements.navSessions);
      }
      showError("Unable to open selected session", error);
    }
  }

  async function loadFindings(sessionID) {
    const append = arguments.length > 1 && arguments[1] === true;
    state.findingsStatus = "loading";
    state.findingsMayHaveMore = append
      ? state.findingHasMore
      : true;
    renderFindingList();
    try {
      const cursor = append ? state.findingNextCursor : "";
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
      state.findings =
        append && cursor
          ? deduplicateByID(state.findings.concat(page), "finding_id")
          : deduplicateByID(page, "finding_id");
      state.findingNextCursor = readCursor(response.next_cursor);
      state.findingHasMore =
        response.has_more === true || Boolean(state.findingNextCursor);
      if (state.findingHasMore && !state.findingNextCursor) {
        throw new Error("Local findings response omitted its continuation cursor.");
      }
      state.findingsStatus = "ready";
      state.findingsMayHaveMore = state.findingHasMore;
      renderSessionOverview();
      renderEvents();
    } catch {
      if (state.selectedSessionID !== sessionID) return;
      state.findingsStatus = "error";
      state.findingsMayHaveMore = state.findingHasMore;
      renderSessionOverview();
      renderEvents();
    }
  }

  function loadMoreFindings() {
    if (
      !state.selectedSessionID ||
      !state.findingHasMore ||
      !state.findingNextCursor
    ) {
      return;
    }
    void loadFindings(state.selectedSessionID, true);
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
        badge.dataset.tone = severityTone(severity);
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
            state.findingsStatus === "loading"
              ? "Loading one bounded findings page…"
              : "More findings are available through explicit pagination.",
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
    renderFindingPagination();
  }

  function renderFindingPagination() {
    elements.findingsPagination.hidden = !state.findingHasMore;
    elements.findingsLoadMore.disabled =
      state.findingsStatus === "loading";
    elements.findingsPageStatus.textContent = state.findingHasMore
      ? `Loaded ${state.findings.length} findings; more are available.`
      : `Loaded ${state.findings.length} returned findings.`;
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
    const returnView = state.sessionReturnView;
    const returnFocus = state.sessionReturnFocus;
    state.selectedSessionID = "";
    state.selectedEventTotal = 0;
    state.selectedSessionDetail = null;
    state.selectedOverview = null;
    state.events = [];
    state.findings = [];
    state.findingNextCursor = "";
    state.findingHasMore = false;
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
    elements.findingsPagination.hidden = true;
    renderSessions();
    if (returnView === "attention") {
      setActiveView("attention", false);
      restoreLogicalFocus(
        returnFocus,
        state.selectedIssueID
          ? elements.issueDetailHeading
          : elements.navAttention,
      );
    } else {
      applyPaneAccessibility();
      restoreLogicalFocus(returnFocus, elements.navSessions);
    }
    state.sessionReturnFocus = null;
    state.sessionReturnView = "";
  }

  function retryLastAction() {
    if (state.activeView === "attention") {
      refreshAttention(false);
      return;
    }
    if (state.lastAction === "timeline" && state.selectedSessionID) {
      selectSession(state.selectedSessionID);
      return;
    }
    refreshAll(false);
  }

  class LocalAPIError extends Error {
    constructor(message, status, problemType) {
      super(message);
      this.name = "LocalAPIError";
      this.status = status;
      this.problemType = problemType;
    }
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
      throw new LocalAPIError(
        detail,
        response.status,
        isRecord(body) ? readText(body.type) : "",
      );
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
    return Number.isFinite(date.getTime()) && date.getUTCFullYear() > 1
      ? date
      : null;
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
    if (!(date instanceof Date) || !Number.isFinite(date.getTime())) return "";
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
