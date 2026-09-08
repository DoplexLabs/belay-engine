(() => {
  "use strict";

  const pageLimits = Object.freeze({
    sessions: { initial: 40, step: 40, maximum: 100 },
    events: { initial: 100, step: 100, maximum: 500 },
    findings: { page: 20 },
    issues: { page: 20 },
    familyMembers: { page: 20 },
    occurrences: { page: 20 },
    fixMonitoring: { page: 20 },
    fixMonitoringDetail: { page: 20 },
    fixRecurrences: { page: 20 },
  });
  const mutationRequestDeadlineMilliseconds = 15_000;
  const explicitOutcomes = new Set(["succeeded", "failed", "interrupted"]);
  const issueCatalog = Object.freeze({
    "issue.explicit_command_failure": Object.freeze({
      title: "Command failed",
      explanation: "The source explicitly reported a failed command result.",
      action: "Inspect failed command evidence",
    }),
    "issue.repeated_command_attempts": Object.freeze({
      title: "Command repeatedly attempted",
      explanation:
        "The same private command signature was observed multiple times in one bounded interval.",
      action: "Inspect matching sessions",
      experimental: true,
    }),
    "issue.explicit_permission_denial": Object.freeze({
      title: "Permission denied",
      explanation: "The source explicitly reported a denied permission event.",
      action: "Inspect permission evidence",
    }),
    "issue.verification_not_observed": Object.freeze({
      title: "Verification evidence not observed",
      explanation:
        "A supported live session ended without the required verification evidence.",
      action: "Inspect verification evidence",
      evidenceGap: true,
    }),
    "issue.unresolved_verification_failure_at_completion": Object.freeze({
      title: "Verification still failed at session end",
      explanation:
        "A verification command explicitly failed and no later successful verification was observed before session end.",
      action: "Inspect verification evidence",
    }),
    "issue.numbat_finding": Object.freeze({
      title: "Upstream Numbat finding",
      explanation: "A configured Numbat rule reported retained evidence.",
      caveat:
        "Belay does not interpret this source rule and does not infer cause, impact, or remediation from its identifier.",
      action: "Inspect cited evidence",
    }),
  });
  const sourceSignalCatalog = Object.freeze({
    "numbat/tamper.guardrails_off": Object.freeze({
      title: "Agent safety confirmations may be disabled",
      explanation:
        "Numbat reported retained configuration evidence associated with disabled agent guardrails.",
      caveat:
        "This does not prove malicious tampering, identify who changed the configuration, or establish that an unsafe action occurred.",
      action: "Review cited configuration evidence",
    }),
  });
  const analysisQualifiers = Object.freeze({
    pending: "Prior retained result while reanalysis is pending.",
    failed: "Prior retained result; the latest analysis failed.",
    truncated: "Partial analysis; additional signals may be absent.",
    unknown:
      "Analysis status is unavailable; result freshness and completeness are uncertain.",
  });
  const fixMonitoringCatalog = Object.freeze({
    matching_evidence_observed: Object.freeze({
      title: "Exact matching evidence was observed after this attempt.",
      detail:
        "This does not establish causality or whether the attempted change worked.",
      tone: "attention",
    }),
    monitoring_incomplete: Object.freeze({
      title: "Monitoring is incomplete.",
      detail:
        "Some later Local activity is pending, failed, truncated, or only partially analyzed.",
      tone: "pending",
    }),
    awaiting_later_evidence: Object.freeze({
      title: "Awaiting later evidence.",
      detail: "No comparable completed Local activity is available yet.",
      tone: "neutral",
    }),
    no_later_match_observed: Object.freeze({
      title:
        "No later exact match was observed in retained, completed Local analysis.",
      detail: "This does not verify resolution.",
      tone: "neutral",
    }),
    comparison_unavailable: Object.freeze({
      title: "Comparison unavailable.",
      detail:
        "The stored baseline cannot be compared under current exact-match semantics.",
      tone: "neutral",
    }),
    retracted: Object.freeze({
      title: "Retracted declaration — excluded from active monitoring.",
      detail: "",
      tone: "neutral",
    }),
    unknown: Object.freeze({
      title: "Monitoring status unavailable.",
      detail: "",
      tone: "neutral",
    }),
  });
  const canonicalUUIDv7Pattern =
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  const fixChangeCatalog = Object.freeze([
    Object.freeze({
      value: "code_change",
      label: "Code change",
      description: "Source or test code changed.",
    }),
    Object.freeze({
      value: "configuration_change",
      label: "Configuration change",
      description: "Project/application configuration changed.",
    }),
    Object.freeze({
      value: "dependency_change",
      label: "Dependency change",
      description: "Dependency version or lock state changed.",
    }),
    Object.freeze({
      value: "permission_change",
      label: "Permission change",
      description: "Access or permission configuration changed.",
    }),
    Object.freeze({
      value: "environment_change",
      label: "Environment change",
      description: "Local runtime, toolchain, or environment changed.",
    }),
    Object.freeze({
      value: "agent_instruction",
      label: "Agent instruction",
      description: "User-level agent instruction changed.",
    }),
    Object.freeze({
      value: "project_rule",
      label: "Project rule",
      description: "Repository/project agent rule changed.",
    }),
    Object.freeze({
      value: "monitor_hook",
      label: "Monitor hook",
      description: "Agent monitoring hook/configuration changed.",
    }),
    Object.freeze({
      value: "other",
      label: "Other",
      description: "A deliberate category outside the fixed catalog.",
    }),
  ]);
  const fixRetractionCatalog = Object.freeze([
    Object.freeze({
      value: "recorded_by_mistake",
      label: "Recorded by mistake",
    }),
    Object.freeze({
      value: "superseded",
      label: "Superseded by another declaration",
    }),
    Object.freeze({
      value: "other",
      label: "Other",
    }),
  ]);
  const fixEligibilityMessages = Object.freeze({
    analysis_not_current:
      "Recording is unavailable until issue analysis is current.",
    experimental_signal:
      "Experimental signals cannot anchor fix-attempt declarations.",
    evidence_gap:
      "Evidence gaps cannot anchor fix-attempt declarations.",
    scope_unavailable:
      "This issue lacks a compatible private scope for future exact recurrence measurement.",
  });
  const config = globalThis.BELAY_LOCAL_CONFIG || {};
  const state = {
    token: resolveToken(config),
    apiBase: normalizeApiBase(config.apiBase),
    activeView: "attention",
    issues: createAttentionFamilyBucket(),
    evidenceGaps: createIssueBucket("evidence_gap"),
    issueFilters: {
      severity: "",
      harness: "",
      origin: "",
      analysisStatus: "",
      experimental: false,
    },
    selectedFamily: null,
    selectedFamilyViewCursor: "",
    familyMembers: [],
    familyMemberNextCursor: "",
    familyMemberHasMore: false,
    familyMemberStatus: "idle",
    familyMemberError: "",
    familyMemberRequestGeneration: 0,
    familyReturnFocus: null,
    fixMonitoring: createFixMonitoringBucket(),
    fixMonitoringFilters: {
      state: "",
      changeKind: "",
      severity: "",
      harness: "",
      issueID: "",
      recordedAfter: "",
      includeRetracted: false,
    },
    selectedIssueID: "",
    selectedIssueKind: "",
    selectedIssueSource: "",
    selectedIssueFamilyID: "",
    selectedIssueEvidenceContext: "",
    selectedDrivingAnnotationID: "",
    selectedIssue: null,
    selectedIssueCatalog: null,
    selectedGlobalAnalysisCoverage: null,
    selectedIssueViewCursor: "",
    occurrences: [],
    occurrenceNextCursor: "",
    occurrenceHasMore: false,
    occurrenceStatus: "idle",
    occurrenceRequestGeneration: 0,
    issueEvidencePreview: createIssueEvidencePreview(),
    issueEvidencePreviewRequestGeneration: 0,
    issueEvidencePreviewController: null,
    fixEligibility: null,
    fixEligibilityStatus: "idle",
    fixEligibilityRequestGeneration: 0,
    fixHistory: [],
    fixHistoryNextCursor: "",
    fixHistoryHasMore: false,
    fixHistoryStatus: "idle",
    fixHistoryRequestGeneration: 0,
    fixHistoryViewCursor: "",
    fixHistoryCurrentIssueAvailable: null,
    fixHistoryStale: false,
    fixHistoryError: null,
    fixEvidenceEvaluatedAt: "",
    fixActionMessage: "",
    fixActionTone: "status",
    activeModal: "",
    modalSubmitting: false,
    dialogReturnFocus: null,
    activeRetractionAnnotationID: "",
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
    appShell: document.querySelector("#app-shell"),
    navAttention: document.querySelector("#nav-attention"),
    navSessions: document.querySelector("#nav-sessions"),
    attentionNavCount: document.querySelector("#attention-nav-count"),
    attentionView: document.querySelector("#attention-view"),
    sessionsView: document.querySelector("#sessions-view"),
    attentionListPane: document.querySelector("#attention-list-pane"),
    attentionDetailPane: document.querySelector("#attention-detail-pane"),
    attentionScroll: document.querySelector(".attention-scroll"),
    stableIssuesSection: document.querySelector("#stable-issues-section"),
    evidenceGapsSection: document.querySelector("#evidence-gaps-section"),
    fixMonitoringSection: document.querySelector("#fix-monitoring-section"),
    coveragePanel: document.querySelector("#coverage-panel"),
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
    issueFilterHarness: document.querySelector("#issue-filter-harness"),
    issueFilterOrigin: document.querySelector("#issue-filter-origin"),
    issueFilterStatus: document.querySelector("#issue-filter-status"),
    issueFilterExperimental: document.querySelector(
      "#issue-filter-experimental",
    ),
    clearAttentionFilters: document.querySelector("#clear-attention-filters"),
    attentionFilterNote: document.querySelector("#attention-filter-note"),
    issueFilterDisclosure: document.querySelector("#issue-filter-disclosure"),
    fixMonitoringFilters: document.querySelector("#fix-monitoring-filters"),
    fixMonitoringFilterState: document.querySelector(
      "#fix-monitoring-filter-state",
    ),
    fixMonitoringFilterChangeKind: document.querySelector(
      "#fix-monitoring-filter-change-kind",
    ),
    fixMonitoringFilterSeverity: document.querySelector(
      "#fix-monitoring-filter-severity",
    ),
    fixMonitoringFilterHarness: document.querySelector(
      "#fix-monitoring-filter-harness",
    ),
    fixMonitoringFilterIssueID: document.querySelector(
      "#fix-monitoring-filter-issue-id",
    ),
    fixMonitoringFilterRecordedAfter: document.querySelector(
      "#fix-monitoring-filter-recorded-after",
    ),
    fixMonitoringFilterRetracted: document.querySelector(
      "#fix-monitoring-filter-retracted",
    ),
    clearFixMonitoringFilters: document.querySelector(
      "#clear-fix-monitoring-filters",
    ),
    fixMonitoringFilterNote: document.querySelector(
      "#fix-monitoring-filter-note",
    ),
    fixMonitoringStatus: document.querySelector("#fix-monitoring-status"),
    fixMonitoringCount: document.querySelector("#fix-monitoring-count"),
    fixMonitoringList: document.querySelector("#fix-monitoring-list"),
    fixMonitoringLoading: document.querySelector("#fix-monitoring-loading"),
    fixMonitoringEmpty: document.querySelector("#fix-monitoring-empty"),
    fixMonitoringEmptyTitle: document.querySelector(
      "#fix-monitoring-empty-title",
    ),
    fixMonitoringEmptyDetail: document.querySelector(
      "#fix-monitoring-empty-detail",
    ),
    fixMonitoringPagination: document.querySelector(
      "#fix-monitoring-pagination",
    ),
    fixMonitoringPageStatus: document.querySelector(
      "#fix-monitoring-page-status",
    ),
    fixMonitoringLoadMore: document.querySelector(
      "#fix-monitoring-load-more",
    ),
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
    familyDetail: document.querySelector("#family-detail"),
    familyBackButton: document.querySelector("#family-back-button"),
    familyDetailHeading: document.querySelector("#family-detail-heading"),
    familyDetailBadges: document.querySelector("#family-detail-badges"),
    familyAnalysisQualifier: document.querySelector(
      "#family-analysis-qualifier",
    ),
    familyObservation: document.querySelector("#family-observation"),
    familyCaveat: document.querySelector("#family-caveat"),
    familyNextAction: document.querySelector("#family-next-action"),
    familySummary: document.querySelector("#family-summary"),
    familyMemberCount: document.querySelector("#family-member-count"),
    familyMemberList: document.querySelector("#family-member-list"),
    familyMembersLoading: document.querySelector("#family-members-loading"),
    familyMembersEmpty: document.querySelector("#family-members-empty"),
    familyMembersPagination: document.querySelector(
      "#family-members-pagination",
    ),
    familyMembersPageStatus: document.querySelector(
      "#family-members-page-status",
    ),
    familyMembersLoadMore: document.querySelector(
      "#family-members-load-more",
    ),
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
    issueNextAction: document.querySelector("#issue-next-action"),
    issueExplanationSection: document.querySelector(
      "#issue-explanation-section",
    ),
    issueEvidencePreview: document.querySelector("#issue-evidence-preview"),
    issueEvidencePreviewCount: document.querySelector(
      "#issue-evidence-preview-count",
    ),
    issueEvidencePreviewDisclosure: document.querySelector(
      "#issue-evidence-preview-disclosure",
    ),
    issueEvidencePreviewList: document.querySelector(
      "#issue-evidence-preview-list",
    ),
    issueEvidencePreviewAll: document.querySelector(
      "#issue-evidence-preview-all",
    ),
    issueMetadata: document.querySelector("#issue-metadata"),
    fingerprintPanel: document.querySelector("#fingerprint-panel"),
    issueFingerprint: document.querySelector("#issue-fingerprint"),
    copyIssueFingerprint: document.querySelector("#copy-issue-fingerprint"),
    recordFixAttempt: document.querySelector("#record-fix-attempt"),
    fixAttemptsSection: document.querySelector("#fix-attempts-section"),
    fixEligibilityStatus: document.querySelector("#fix-eligibility-status"),
    fixMonitoringDetailStatus: document.querySelector(
      "#fix-monitoring-detail-status",
    ),
    fixActionStatus: document.querySelector("#fix-action-status"),
    fixHistoryList: document.querySelector("#fix-history-list"),
    fixHistoryLoading: document.querySelector("#fix-history-loading"),
    fixHistoryEmpty: document.querySelector("#fix-history-empty"),
    fixEvidenceEvaluated: document.querySelector("#fix-evidence-evaluated"),
    fixHistoryPagination: document.querySelector("#fix-history-pagination"),
    fixHistoryPageStatus: document.querySelector("#fix-history-page-status"),
    fixHistoryLoadMore: document.querySelector("#fix-history-load-more"),
    occurrenceCount: document.querySelector("#occurrence-count"),
    occurrenceList: document.querySelector("#occurrence-list"),
    occurrencesLoading: document.querySelector("#occurrences-loading"),
    occurrencesPagination: document.querySelector("#occurrences-pagination"),
    occurrencesPageStatus: document.querySelector("#occurrences-page-status"),
    occurrencesLoadMore: document.querySelector("#occurrences-load-more"),
    matchingSection: document.querySelector("#matching-section"),
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
    needsAttentionSummary: document.querySelector("#needs-attention-summary"),
    observedWorkSummary: document.querySelector("#observed-work-summary"),
    sessionHighlights: document.querySelector("#session-highlights"),
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
    fixAttemptModalLayer: document.querySelector("#fix-attempt-modal-layer"),
    fixAttemptDialog: document.querySelector("#fix-attempt-dialog"),
    fixAttemptClose: document.querySelector("#fix-attempt-close"),
    fixAttemptCancel: document.querySelector("#fix-attempt-cancel"),
    fixAttemptConfirm: document.querySelector("#fix-attempt-confirm"),
    fixAttemptAlert: document.querySelector("#fix-attempt-alert"),
    fixCategoryFieldset: document.querySelector("#fix-category-fieldset"),
    fixCategoryOptions: document.querySelector("#fix-category-options"),
    fixDraftRecovery: document.querySelector("#fix-draft-recovery"),
    abandonFixDraft: document.querySelector("#abandon-fix-draft"),
    fixRetractionModalLayer: document.querySelector(
      "#fix-retraction-modal-layer",
    ),
    fixRetractionDialog: document.querySelector("#fix-retraction-dialog"),
    fixRetractionClose: document.querySelector("#fix-retraction-close"),
    fixRetractionCancel: document.querySelector("#fix-retraction-cancel"),
    fixRetractionConfirm: document.querySelector("#fix-retraction-confirm"),
    fixRetractionAlert: document.querySelector("#fix-retraction-alert"),
    fixRetractionFieldset: document.querySelector(
      "#fix-retraction-fieldset",
    ),
    fixRetractionOptions: document.querySelector(
      "#fix-retraction-options",
    ),
    fixRetractionRecovery: document.querySelector(
      "#fix-retraction-recovery",
    ),
    abandonFixRetraction: document.querySelector(
      "#abandon-fix-retraction",
    ),
  };

  const focusRegistry = {
    monitoringCards: new Map(),
    issueCards: new Map(),
    familyCards: new Map(),
    familyMembers: new Map(),
    occurrenceActions: new Map(),
    sessionCards: new Map(),
    fixTriggers: new Map(),
    fixHistoryRows: new Map(),
    fixRetractionTriggers: new Map(),
    fixObservationRows: new Map(),
  };
  const fixDrafts = new Map();
  const fixRetractionDrafts = new Map();
  const fixObservationPages = new Map();
  let searchTimer = 0;
  let issueFilterTimer = 0;
  const mobileQuery = globalThis.matchMedia("(max-width: 680px)");
  prepareValueFirstAttentionLayout();
  renderFixDialogChoices();
  renderFixMonitoringFilters();
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
      selection: null,
      status: "idle",
      error: null,
      requestGeneration: 0,
    };
  }

  function createAttentionFamilyBucket() {
    return {
      kind: "family",
      data: [],
      nextCursor: "",
      hasMore: false,
      analysis: null,
      selection: null,
      status: "idle",
      error: null,
      requestGeneration: 0,
    };
  }

  function createFixMonitoringBucket() {
    return {
      data: [],
      nextCursor: "",
      hasMore: false,
      viewCursor: "",
      evidenceEvaluatedAt: "",
      status: "idle",
      error: null,
      requestGeneration: 0,
    };
  }

  function createIssueEvidencePreview() {
    return {
      issueID: "",
      occurrenceID: "",
      sessionID: "",
      status: "idle",
      events: [],
      requestedCount: 0,
      foundCount: 0,
      missingCount: 0,
      missingEventIDs: [],
      availableEventIDs: [],
      expanded: false,
      error: "",
    };
  }

  function prepareValueFirstAttentionLayout() {
    const analysisDisclosure = createElement(
      "details",
      "attention-disclosure",
    );
    analysisDisclosure.id = "analysis-disclosure";
    analysisDisclosure.append(
      createElement("summary", "", "Analysis status and coverage"),
      elements.coveragePanel,
    );
    const filterDisclosure = createElement(
      "details",
      "attention-disclosure",
    );
    filterDisclosure.id = "attention-filter-disclosure";
    filterDisclosure.append(
      createElement("summary", "", "Filter Attention"),
      elements.attentionFilters,
    );
    elements.attentionScroll.replaceChildren(
      elements.stableIssuesSection,
      elements.evidenceGapsSection,
      elements.fixMonitoringSection,
      analysisDisclosure,
      filterDisclosure,
    );
  }

  function bindEvents() {
    elements.attentionFilters.addEventListener("submit", (event) => {
      event.preventDefault();
      state.issueFilters.harness = elements.issueFilterHarness.value.trim();
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
    elements.familyMembersLoadMore.addEventListener("click", () => {
      void loadAttentionFamilyDetail(true);
    });
    elements.evidenceGapsLoadMore.addEventListener("click", () => {
      loadIssueBucket(state.evidenceGaps, true);
    });
    elements.fixMonitoringLoadMore.addEventListener("click", () => {
      void loadFixMonitoring(true);
    });
    elements.fixMonitoringFilters.addEventListener("submit", (event) => {
      event.preventDefault();
      syncFixMonitoringFilters();
      resetAndLoadFixMonitoring();
    });
    [
      [elements.fixMonitoringFilterState, "state"],
      [elements.fixMonitoringFilterChangeKind, "changeKind"],
      [elements.fixMonitoringFilterSeverity, "severity"],
    ].forEach(([element, key]) => {
      element.addEventListener("change", () => {
        state.fixMonitoringFilters[key] = element.value;
        if (key === "state" && element.value === "retracted") {
          state.fixMonitoringFilters.includeRetracted = true;
          elements.fixMonitoringFilterRetracted.checked = true;
        }
        resetAndLoadFixMonitoring();
      });
    });
    elements.fixMonitoringFilterRetracted.addEventListener("change", () => {
      state.fixMonitoringFilters.includeRetracted =
        elements.fixMonitoringFilterRetracted.checked;
      resetAndLoadFixMonitoring();
    });
    [
      elements.fixMonitoringFilterHarness,
      elements.fixMonitoringFilterIssueID,
      elements.fixMonitoringFilterRecordedAfter,
    ].forEach((element) => {
      element.addEventListener("change", () => {
        syncFixMonitoringFilters();
        resetAndLoadFixMonitoring();
      });
    });
    elements.clearFixMonitoringFilters.addEventListener("click", () => {
      state.fixMonitoringFilters = {
        state: "",
        changeKind: "",
        severity: "",
        harness: "",
        issueID: "",
        recordedAfter: "",
        includeRetracted: false,
      };
      renderFixMonitoringFilters();
      resetAndLoadFixMonitoring();
    });
    elements.occurrencesLoadMore.addEventListener("click", loadMoreOccurrences);
    elements.issueEvidencePreviewAll.addEventListener("click", () => {
      const preview = state.issueEvidencePreview;
      const occurrence = state.occurrences.find(
        (value) =>
          readText(value.occurrence_id) === readText(preview.occurrenceID),
      );
      if (occurrence) void loadIssueEvidencePreview(occurrence, true);
    });
    elements.recordFixAttempt.addEventListener("click", openFixAttemptDialog);
    elements.fixHistoryLoadMore.addEventListener(
      "click",
      loadMoreFixMonitoringDetail,
    );
    elements.issueBackButton.addEventListener("click", closeIssueDetail);
    elements.familyBackButton.addEventListener("click", closeFamilyDetail);
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
    elements.fixAttemptClose.addEventListener("click", () => {
      closeFixAttemptDialog(true);
    });
    elements.fixAttemptCancel.addEventListener("click", () => {
      closeFixAttemptDialog(true);
    });
    elements.fixAttemptConfirm.addEventListener("click", () => {
      void submitFixAttempt();
    });
    elements.abandonFixDraft.addEventListener("click", abandonFixDraft);
    elements.fixCategoryOptions.addEventListener("change", (event) => {
      updateFixDraftChoice(event.target);
    });
    elements.fixRetractionClose.addEventListener("click", () => {
      closeFixRetractionDialog(true);
    });
    elements.fixRetractionCancel.addEventListener("click", () => {
      closeFixRetractionDialog(true);
    });
    elements.fixRetractionConfirm.addEventListener("click", () => {
      void submitFixRetraction();
    });
    elements.abandonFixRetraction.addEventListener(
      "click",
      abandonFixRetraction,
    );
    elements.fixRetractionOptions.addEventListener("change", (event) => {
      updateFixRetractionChoice(event.target);
    });
    elements.fixAttemptModalLayer.addEventListener(
      "keydown",
      handleModalKeydown,
    );
    elements.fixRetractionModalLayer.addEventListener(
      "keydown",
      handleModalKeydown,
    );

    [
      [elements.issueFilterSeverity, "severity"],
      [elements.issueFilterOrigin, "origin"],
      [elements.issueFilterStatus, "analysisStatus"],
    ].forEach(([element, key]) => {
      element.addEventListener("change", () => {
        state.issueFilters[key] = element.value;
        resetAndLoadAttention();
      });
    });
    elements.issueFilterHarness.addEventListener("input", () => {
      globalThis.clearTimeout(issueFilterTimer);
      issueFilterTimer = globalThis.setTimeout(() => {
        state.issueFilters.harness = elements.issueFilterHarness.value.trim();
        resetAndLoadAttention();
      }, 250);
    });
    elements.issueFilterExperimental.addEventListener("change", () => {
      state.issueFilters.experimental =
        elements.issueFilterExperimental.checked;
      resetAndLoadAttention();
    });
    elements.clearAttentionFilters.addEventListener("click", () => {
      state.issueFilters = {
        severity: "",
        harness: "",
        origin: "",
        analysisStatus: "",
        experimental: false,
      };
      elements.issueFilterSeverity.value = "";
      elements.issueFilterHarness.value = "";
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
      if (state.activeModal) return;
      if (state.activeView === "sessions" && state.selectedSessionID) {
        closeTimeline();
      } else if (state.activeView === "attention" && state.selectedIssueID) {
        closeIssueDetail();
      } else if (state.activeView === "attention" && state.selectedFamily) {
        closeFamilyDetail();
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
    const issueOpen = Boolean(state.selectedIssueID || state.selectedFamily);
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

  function familyFocusKey(familyID) {
    return readText(familyID);
  }

  function familyMemberFocusKey(familyID, issueID) {
    return `${readText(familyID)}\u0000${readText(issueID)}`;
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
    if (reference.type === "monitoring") {
      return focusRegistry.monitoringCards.get(reference.key) || null;
    }
    if (reference.type === "issue") {
      return focusRegistry.issueCards.get(reference.key) || null;
    }
    if (reference.type === "family") {
      return focusRegistry.familyCards.get(reference.familyID) || null;
    }
    if (reference.type === "family-member") {
      return focusRegistry.familyMembers.get(reference.key) || null;
    }
    if (reference.type === "occurrence") {
      return focusRegistry.occurrenceActions.get(reference.key) || null;
    }
    if (reference.type === "session") {
      return focusRegistry.sessionCards.get(reference.sessionID) || null;
    }
    if (reference.type === "fix-trigger") {
      return focusRegistry.fixTriggers.get(reference.issueID) || null;
    }
    if (reference.type === "fix-history") {
      return focusRegistry.fixHistoryRows.get(reference.annotationID) || null;
    }
    if (reference.type === "fix-observation") {
      return (
        focusRegistry.fixObservationRows.get(reference.recurrenceID) || null
      );
    }
    if (reference.type === "fix-retraction") {
      return (
        focusRegistry.fixRetractionTriggers.get(reference.annotationID) || null
      );
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
    const selectedSource = preserveSelection ? state.selectedIssueSource : "";
    const selectedFamilyID =
      preserveSelection && state.selectedFamily
        ? readText(state.selectedFamily.family_id)
        : preserveSelection
          ? readText(state.selectedIssueFamilyID)
          : "";
    const selectedFamilyMemberPageBudget = Math.max(
      1,
      Math.ceil(
        state.familyMembers.length / pageLimits.familyMembers.page,
      ),
    );
    const selectedBucket =
      selectedKind === "evidence_gap" ? state.evidenceGaps : state.issues;
    const selectedPageBudget = Math.max(
      1,
      Math.ceil(selectedBucket.data.length / pageLimits.issues.page),
    );
    if (
      (selectedID || selectedFamilyID) &&
      selectedSource !== "monitoring"
    ) {
      closeIssueDetail(false, true);
      clearAttentionFamilyDetailState();
      showAttentionNotice(
        "Refreshing Attention; selected detail is hidden until current data confirms it.",
        "pending",
      );
    } else if (!preserveSelection) {
      closeIssueDetail(false, true);
      clearAttentionFamilyDetailState();
    }
    resetIssueBucket(state.issues);
    resetIssueBucket(state.evidenceGaps);
    resetFixMonitoringBucket();
    const [issuesReady, gapsReady, monitoringReady] = await Promise.all([
      loadIssueBucket(state.issues, false),
      loadIssueBucket(state.evidenceGaps, false),
      loadFixMonitoring(false),
    ]);
    if (refreshGeneration !== state.attentionRefreshGeneration) return false;
    if (selectedID && selectedSource === "monitoring") {
      if (!monitoringReady) {
        state.fixHistoryStale = true;
        state.fixHistoryError =
          "Monitoring refresh failed. Previously loaded attempt detail may be stale.";
        renderFixAttempts();
        return false;
      }
      const summary = state.fixMonitoring.data.find(
        (entry) => readText(entry.issue_id) === selectedID,
      );
      if (!summary) {
        closeIssueDetail(false);
        showAttentionNotice(
          state.fixMonitoring.hasMore
            ? "Post-attempt monitoring refreshed. The selected item is outside the loaded results."
            : "Post-attempt monitoring refreshed. The selected item is no longer visible under the current filters.",
          "status",
          7000,
        );
        return issuesReady && gapsReady;
      }
      const detailReady = await refreshSelectedMonitoringIssue(summary);
      if (refreshGeneration !== state.attentionRefreshGeneration) return false;
      if (!detailReady) {
        state.fixHistoryStale = true;
        state.fixHistoryError =
          "Monitoring list refreshed, but attempt detail could not be refreshed.";
        renderFixAttempts();
        return false;
      }
      showAttentionNotice(
        "Attention refreshed; post-attempt detail was reloaded from the current view.",
        "success",
        4000,
      );
      return issuesReady && gapsReady;
    }
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
    if (!selectedID && !selectedFamilyID) return true;

    if (selectedKind === "evidence_gap") {
      const chainReady = await loadIssuePagesForSelection(
        state.evidenceGaps,
        selectedID,
        selectedPageBudget,
      );
      if (!chainReady) return false;
      const summary = state.evidenceGaps.data.find(
        (issue) => readText(issue.issue_id) === selectedID,
      );
      return summary ? selectIssue(summary, selectedKind, false) : true;
    }

    const chainReady = await loadFamilyPagesForSelection(
      selectedFamilyID,
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
    const summary = state.issues.data.find(
      (family) =>
        (selectedFamilyID &&
          readText(family.family_id) === selectedFamilyID) ||
        (!selectedFamilyID &&
          readText(family.kind) === "exact_issue" &&
          readText(family.representative_issue_id) === selectedID),
    );
    if (!summary) {
      showAttentionNotice(
        state.issues.hasMore
          ? "Attention refreshed. The selected signal was not confirmed in the refreshed loaded results; load more to find it."
          : "Attention refreshed. The selected signal is no longer visible under the current filters.",
        "status",
        7000,
      );
      return true;
    }
    let detailReady = false;
    if (readText(summary.kind) === "mapped_upstream") {
      detailReady = await selectAttentionFamily(summary, false);
      if (detailReady && selectedID) {
        const memberChainReady = await loadFamilyMemberPagesForSelection(
          selectedID,
          selectedFamilyMemberPageBudget,
        );
        if (!memberChainReady) {
          closeFamilyDetail(false);
          detailReady = false;
        }
        const member = state.familyMembers.find(
          (value) => readText(value.issue && value.issue.issue_id) === selectedID,
        );
        if (memberChainReady && !member) {
          const moreMembersRemain = state.familyMemberHasMore;
          closeFamilyDetail(false);
          showAttentionNotice(
            moreMembersRemain
              ? "Attention refreshed, but the selected affected record was not found within the previously loaded bounded pages. Reopen the family and load more to select it again."
              : "Attention refreshed, but the selected affected record is no longer visible in this family.",
            "status",
            8000,
          );
          return true;
        }
        if (memberChainReady && member) {
          const returnToFamily =
            selectedSource === "family" &&
            toFiniteNumber(summary.supporting_issue_count) > 1;
          const returnFocus = returnToFamily
            ? {
                type: "family-member",
                key: familyMemberFocusKey(
                  readText(summary.family_id),
                  selectedID,
                ),
              }
            : {
                type: "family",
                familyID: readText(summary.family_id),
              };
          if (!returnToFamily) closeFamilyDetail(false);
          detailReady = await selectIssue(member.issue, "issue", false, {
            viewCursor: readCursor(member.view_cursor),
            catalog: member.catalog,
            source: returnToFamily ? "family" : "attention",
            familyID: readText(summary.family_id),
            evidenceContext: attentionFamilyEvidenceContext(summary),
            returnFocus,
          });
        }
      }
    } else {
      detailReady = await selectIssue(
        exactFamilyRepresentative(summary),
        "issue",
        false,
        {
          viewCursor: readCursor(summary.view_cursor),
          catalog: summary.catalog,
          familyID: readText(summary.family_id),
          evidenceContext: attentionFamilyEvidenceContext(summary),
          returnFocus: {
            type: "family",
            familyID: readText(summary.family_id),
          },
        },
      );
    }
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

  async function loadFamilyPagesForSelection(familyID, issueID, pageBudget) {
    for (let page = 1; page < pageBudget; page += 1) {
      if (
        state.issues.data.some(
          (family) =>
            (familyID && readText(family.family_id) === familyID) ||
            (!familyID &&
              readText(family.kind) === "exact_issue" &&
              readText(family.representative_issue_id) === issueID),
        ) ||
        !state.issues.hasMore
      ) {
        break;
      }
      const pageReady = await loadIssueBucket(state.issues, true);
      if (!pageReady) return false;
    }
    return true;
  }

  async function loadFamilyMemberPagesForSelection(issueID, pageBudget) {
    for (let page = 1; page < pageBudget; page += 1) {
      if (
        state.familyMembers.some(
          (member) =>
            readText(member.issue && member.issue.issue_id) === issueID,
        ) ||
        !state.familyMemberHasMore
      ) {
        break;
      }
      const pageReady = await loadAttentionFamilyDetail(true);
      if (!pageReady) return false;
    }
    return true;
  }

  function resetAndLoadAttention() {
    state.attentionRefreshGeneration += 1;
    closeIssueDetail(false, true);
    clearAttentionFamilyDetailState();
    hideAttentionNotice();
    resetIssueBucket(state.issues);
    resetIssueBucket(state.evidenceGaps);
    resetFixMonitoringBucket();
    renderAttentionFilters();
    void Promise.all([
      loadIssueBucket(state.issues, false),
      loadIssueBucket(state.evidenceGaps, false),
      loadFixMonitoring(false),
    ]);
  }

  function resetIssueBucket(bucket) {
    bucket.requestGeneration += 1;
    bucket.data = [];
    bucket.nextCursor = "";
    bucket.hasMore = false;
    bucket.viewCursor = "";
    bucket.analysis = null;
    bucket.selection = null;
    bucket.status = "idle";
    bucket.error = null;
  }

  function resetFixMonitoringBucket(render = true) {
    state.fixMonitoring.requestGeneration += 1;
    state.fixMonitoring.data = [];
    state.fixMonitoring.nextCursor = "";
    state.fixMonitoring.hasMore = false;
    state.fixMonitoring.viewCursor = "";
    state.fixMonitoring.evidenceEvaluatedAt = "";
    state.fixMonitoring.status = "idle";
    state.fixMonitoring.error = null;
    focusRegistry.monitoringCards.clear();
    if (render) renderFixMonitoring();
  }

  function syncFixMonitoringFilters() {
    state.fixMonitoringFilters.harness =
      elements.fixMonitoringFilterHarness.value.trim();
    state.fixMonitoringFilters.issueID =
      elements.fixMonitoringFilterIssueID.value.trim();
    const recordedAfter = elements.fixMonitoringFilterRecordedAfter.value;
    if (!recordedAfter) {
      state.fixMonitoringFilters.recordedAfter = "";
      return;
    }
    const date = new Date(recordedAfter);
    state.fixMonitoringFilters.recordedAfter = Number.isFinite(date.getTime())
      ? date.toISOString()
      : "";
  }

  function renderFixMonitoringFilters() {
    const filters = state.fixMonitoringFilters;
    elements.fixMonitoringFilterState.value = filters.state;
    elements.fixMonitoringFilterChangeKind.value = filters.changeKind;
    elements.fixMonitoringFilterSeverity.value = filters.severity;
    elements.fixMonitoringFilterHarness.value = filters.harness;
    elements.fixMonitoringFilterIssueID.value = filters.issueID;
    elements.fixMonitoringFilterRetracted.checked = filters.includeRetracted;
    if (!filters.recordedAfter) {
      elements.fixMonitoringFilterRecordedAfter.value = "";
    }
    elements.fixMonitoringFilterNote.textContent = filters.includeRetracted
      ? "Including all-retracted history."
      : "Showing active attempts.";
  }

  function resetAndLoadFixMonitoring() {
    if (state.selectedIssueSource === "monitoring") {
      closeIssueDetail(false);
      showAttentionNotice(
        "Post-attempt filters changed. The prior detail was closed until a current result is selected.",
        "status",
        5000,
      );
    }
    resetFixMonitoringBucket();
    renderFixMonitoringFilters();
    void loadFixMonitoring(false);
  }

  function buildFixMonitoringPath(cursor) {
    const parameters = new URLSearchParams();
    if (cursor) {
      parameters.set("cursor", cursor);
      return `/v1/fix-monitoring?${parameters.toString()}`;
    }
    const filters = state.fixMonitoringFilters;
    parameters.set("limit", String(pageLimits.fixMonitoring.page));
    if (filters.state) parameters.set("state", filters.state);
    if (filters.changeKind) {
      parameters.set("change_kind", filters.changeKind);
    }
    if (filters.severity) parameters.set("severity", filters.severity);
    if (filters.harness) parameters.set("harness", filters.harness);
    if (filters.issueID) parameters.set("issue_id", filters.issueID);
    if (filters.recordedAfter) {
      parameters.set("recorded_after", filters.recordedAfter);
    }
    if (filters.includeRetracted) {
      parameters.set("include_retracted", "true");
    }
    return `/v1/fix-monitoring?${parameters.toString()}`;
  }

  async function loadFixMonitoring(append) {
    const bucket = state.fixMonitoring;
    if (append && (!bucket.hasMore || !bucket.nextCursor)) return false;
    const generation = ++bucket.requestGeneration;
    const cursor = append ? bucket.nextCursor : "";
    bucket.status = append ? "loading-more" : "loading";
    bucket.error = null;
    renderFixMonitoring();
    try {
      const response = await apiGet(buildFixMonitoringPath(cursor));
      requireFixMonitoringSchema(response);
      if (generation !== bucket.requestGeneration) return false;
      const page = Array.isArray(response.data)
        ? response.data.filter(
            (entry) =>
              isRecord(entry) &&
              readText(entry.issue_id) &&
              readText(entry.annotation_id),
          )
        : [];
      const data =
        append && cursor
          ? deduplicateByID(bucket.data.concat(page), "issue_id")
          : deduplicateByID(page, "issue_id");
      const nextCursor = readCursor(response.next_cursor);
      if (response.has_more === true && !nextCursor) {
        throw new Error(
          "Local API returned monitoring pagination without a cursor.",
        );
      }
      const viewCursor = readCursor(response.monitoring_view_cursor);
      if (!viewCursor) {
        throw new Error(
          "Local API returned monitoring results without a view cursor.",
        );
      }
      bucket.data = data;
      bucket.nextCursor = nextCursor;
      bucket.hasMore = Boolean(nextCursor);
      bucket.viewCursor = viewCursor;
      bucket.evidenceEvaluatedAt = readText(response.evidence_evaluated_at);
      bucket.status = "ready";
      renderFixMonitoring();
      return true;
    } catch (error) {
      if (generation !== bucket.requestGeneration) return false;
      if (isCursorExpired(error) && append) {
        const closedSelectedMonitoringDetail =
          state.selectedIssueSource === "monitoring";
        if (closedSelectedMonitoringDetail) {
          state.attentionRefreshGeneration += 1;
          closeIssueDetail(false);
        }
        resetFixMonitoringBucket();
        const refreshed = await loadFixMonitoring(false);
        bucket.error = refreshed
          ? closedSelectedMonitoringDetail
            ? "The prior monitoring view expired. After attempts was refreshed from a new snapshot, and the connected detail was closed."
            : "The prior monitoring view expired. After attempts was refreshed from a new snapshot."
          : "This monitoring view expired and could not be refreshed.";
        renderFixMonitoring();
        return refreshed;
      }
      bucket.status = "error";
      bucket.error = fixMonitoringErrorMessage(error);
      renderFixMonitoring();
      return false;
    }
  }

  function fixMonitoringErrorMessage(error) {
    if (error instanceof LocalAPIError && error.status === 503) {
      if (
        error.problemType ===
        "belay.local/monitoring-catchup-in-progress"
      ) {
        return "Post-attempt monitoring is catching up. Issues, evidence gaps, sessions, and fix actions remain available.";
      }
      if (
        error.problemType === "belay.local/monitoring-catchup-failed"
      ) {
        return "Post-attempt monitoring catch-up needs a Local retry. Other Local views remain available.";
      }
    }
    if (isCursorExpired(error)) {
      return "This monitoring view expired. Refresh After attempts.";
    }
    return "Post-attempt monitoring could not be loaded. Other Local views remain available.";
  }

  function renderFixMonitoring() {
    const bucket = state.fixMonitoring;
    focusRegistry.monitoringCards.clear();
    const fragment = document.createDocumentFragment();
    bucket.data.forEach((summary) => {
      fragment.append(createFixMonitoringCard(summary));
    });
    elements.fixMonitoringList.replaceChildren(fragment);
    elements.fixMonitoringCount.textContent =
      bucket.status === "ready" ? formatNumber(bucket.data.length) : "—";
    elements.fixMonitoringLoading.hidden = bucket.status !== "loading";
    elements.fixMonitoringEmpty.hidden =
      bucket.status !== "ready" || bucket.data.length !== 0;
    elements.fixMonitoringEmptyTitle.textContent =
      state.fixMonitoringFilters.includeRetracted
        ? "No monitored attempt history matches these filters"
        : "No active monitored attempts match these filters";
    elements.fixMonitoringEmptyDetail.textContent =
      "Recording an attempt is optional. Monitoring begins only after a retained declaration.";
    elements.fixMonitoringPagination.hidden = !bucket.hasMore;
    elements.fixMonitoringLoadMore.disabled =
      bucket.status === "loading-more";
    elements.fixMonitoringPageStatus.textContent = bucket.hasMore
      ? `Showing ${bucket.data.length}; more monitored issues are available.`
      : `Showing ${bucket.data.length} monitored ${
          bucket.data.length === 1 ? "issue" : "issues"
        }.`;
    elements.fixMonitoringStatus.hidden = !bucket.error;
    elements.fixMonitoringStatus.textContent = bucket.error || "";
    elements.fixMonitoringStatus.dataset.tone =
      bucket.status === "error" ? "error" : "status";
    elements.fixMonitoringSection.hidden =
      bucket.status === "ready" &&
      bucket.data.length === 0 &&
      !fixMonitoringFiltersActive();
  }

  function fixMonitoringFiltersActive() {
    const filters = state.fixMonitoringFilters;
    return Boolean(
      filters.state ||
        filters.changeKind ||
        filters.severity ||
        filters.harness ||
        filters.issueID ||
        filters.recordedAfter ||
        filters.includeRetracted,
    );
  }

  function createFixMonitoringCard(summary) {
    const issueID = readText(summary.issue_id);
    const annotationID = readText(summary.annotation_id);
    const article = createElement("article", "issue-card monitoring-card");
    const button = createElement("button", "issue-card-main");
    button.type = "button";
    const stateEntry = fixMonitoringStateEntry(
      summary.fix_recurrence_state,
    );
    button.dataset.monitoringTone = stateEntry.tone;
    const catalog = monitoringSubjectCatalog(summary);
    const header = createElement("span", "issue-card-top");
    header.append(
      createElement("strong", "", catalog.title),
      createToneBadge(summary.severity),
    );
    const category = fixChangeCatalogEntry(summary.change_kind);
    const metadata = createElement("span", "issue-card-meta");
    const activeAttempts = toOptionalCount(summary.active_attempt_count);
    const observedAttempts = toOptionalCount(
      summary.observed_attempt_count,
    );
    const sameSession = toOptionalCount(
      summary.same_anchor_session_observation_count,
    );
    const otherSessions = toOptionalCount(
      summary.other_session_observation_count,
    );
    metadata.append(
      createElement("span", "", category.label),
      createElement(
        "span",
        "",
        `${
          activeAttempts === null
            ? "Active attempt count unavailable"
            : `${formatNumber(activeAttempts)} active`
        } · ${
          observedAttempts === null
            ? "Observed-attempt count unavailable"
            : `${formatNumber(observedAttempts)} with matching evidence`
        }`,
      ),
      createElement(
        "span",
        "",
        `${
          sameSession === null
            ? "Original-session count unavailable"
            : `${formatNumber(sameSession)} original-session`
        } · ${
          otherSessions === null
            ? "Other-session count unavailable"
            : `${formatNumber(otherSessions)} other-session`
        }`,
      ),
      createElement(
        "span",
        "",
        formatRelativeTime(parseDate(summary.recorded_at)),
      ),
    );
    button.append(
      header,
      createElement("span", "monitoring-state-title", stateEntry.title),
    );
    if (stateEntry.detail) {
      button.append(
        createElement("span", "issue-card-caveat", stateEntry.detail),
      );
    }
    button.append(metadata);
    if (toFiniteNumber(summary.same_anchor_session_observation_count) > 0) {
      button.append(
        createElement(
          "span",
          "issue-card-caveat",
          "This may be continuation within the original session.",
        ),
      );
    }
    if (
      summary.analysis_complete === false &&
      normalizeFixRecurrenceState(summary.fix_recurrence_state) ===
        "matching_evidence_observed"
    ) {
      button.append(
        createElement(
          "span",
          "issue-card-caveat",
          "Additional matching evidence may exist because monitoring coverage is incomplete.",
        ),
      );
    }
    if (
      normalizeFixRecurrenceState(summary.fix_recurrence_state) ===
        "retracted" &&
      toOptionalCount(summary.historical_matching_evidence_count) > 0
    ) {
      button.append(
        createElement(
          "span",
          "issue-card-caveat",
          "Matching evidence was recorded before this declaration was retracted.",
        ),
      );
    }
    const focusKey = `${issueID}\u0000${annotationID}`;
    focusRegistry.monitoringCards.set(focusKey, button);
    const selected =
      state.selectedIssueSource === "monitoring" &&
      state.selectedIssueID === issueID &&
      state.selectedDrivingAnnotationID === annotationID;
    article.classList.toggle("is-selected", selected);
    button.setAttribute("aria-pressed", String(selected));
    button.setAttribute("aria-current", selected ? "true" : "false");
    button.addEventListener("click", () => {
      void selectMonitoringIssue(summary, true);
    });
    article.append(button);
    return article;
  }

  function selectMonitoringIssue(summary, moveFocus) {
    const issueID = readText(summary && summary.issue_id);
    const annotationID = readText(summary && summary.annotation_id);
    if (!issueID || !annotationID) return Promise.resolve(false);
    const monitoringViewCursor = readCursor(
      state.fixMonitoring.viewCursor,
    );
    if (!monitoringViewCursor) {
      state.fixMonitoring.status = "error";
      state.fixMonitoring.error =
        "This monitoring snapshot is unavailable. Refresh After attempts before opening it.";
      renderFixMonitoring();
      return Promise.resolve(false);
    }
    if (moveFocus) state.attentionRefreshGeneration += 1;
    state.selectedIssueID = issueID;
    state.selectedIssueKind = "issue";
    state.selectedIssueSource = "monitoring";
    state.selectedIssueFamilyID = "";
    state.selectedIssueEvidenceContext = "";
    state.selectedDrivingAnnotationID = annotationID;
    state.selectedIssue = monitoringSummaryIssue(summary);
    state.issueReturnFocus = {
      type: "monitoring",
      key: `${issueID}\u0000${annotationID}`,
    };
    state.occurrences = [];
    state.occurrenceNextCursor = "";
    state.occurrenceHasMore = false;
    state.occurrenceStatus = "idle";
    resetIssueEvidencePreview();
    resetFixIssueState();
    state.fixEligibilityStatus = "loading";
    elements.attentionWelcome.hidden = true;
    elements.issueDetail.hidden = false;
    document.body.classList.add("is-attention-detail-open");
    renderIssueBucket(state.issues);
    renderIssueBucket(state.evidenceGaps);
    renderFixMonitoring();
    renderIssueDetail();
    applyPaneAccessibility();
    if (moveFocus) focusCurrentElement(elements.issueDetailHeading);
    return loadFixMonitoringDetail(
      issueID,
      false,
      monitoringViewCursor,
      false,
      moveFocus ? annotationID : "",
    );
  }

  function refreshSelectedMonitoringIssue(summary) {
    const issueID = readText(summary && summary.issue_id);
    const annotationID = readText(summary && summary.annotation_id);
    if (
      !issueID ||
      !annotationID ||
      issueID !== state.selectedIssueID ||
      state.selectedIssueSource !== "monitoring"
    ) {
      return Promise.resolve(false);
    }
    const monitoringViewCursor = readCursor(
      state.fixMonitoring.viewCursor,
    );
    if (!monitoringViewCursor) {
      state.fixHistoryStale = true;
      state.fixHistoryError =
        "The monitoring list snapshot is unavailable. Connected attempt detail was preserved; refresh After attempts before reloading it.";
      renderFixAttempts();
      return Promise.resolve(false);
    }
    state.selectedDrivingAnnotationID = annotationID;
    state.issueReturnFocus = {
      type: "monitoring",
      key: `${issueID}\u0000${annotationID}`,
    };
    renderFixMonitoring();
    return loadFixMonitoringDetail(
      issueID,
      false,
      monitoringViewCursor,
      true,
      annotationID,
    );
  }

  function monitoringSummaryIssue(summary) {
    return {
      issue_id: readText(summary.issue_id),
      title_code: readText(summary.title_code),
      severity: readText(summary.severity),
      analysis_status: "",
      evidence_complete: null,
      retained_history_only: true,
    };
  }

  async function loadIssueBucket(bucket, append) {
    if (bucket.kind === "family") {
      return loadAttentionFamilies(bucket, append);
    }
    const generation = ++bucket.requestGeneration;
    const cursor = append ? bucket.nextCursor : "";
    bucket.status = append ? "loading-more" : "loading";
    bucket.error = null;
    renderIssueBucket(bucket);
    try {
      const response = await apiGet(buildIssuePath(bucket.kind, cursor));
      if (generation !== bucket.requestGeneration) return false;
      const page = Array.isArray(response.data) ? response.data : [];
      const pagination = requireCursorPage(
        response,
        cursor,
        bucket.kind === "evidence_gap" ? "evidence-gap" : "issue-list",
      );
      const viewCursor = readCursor(response.view_cursor);
      if (!viewCursor) {
        throw new Error(
          "Local API returned issue results without a view cursor.",
        );
      }
      if (cursor && viewCursor !== bucket.viewCursor) {
        throw new Error(
          "Local API changed the issue-list view cursor during continuation.",
        );
      }
      const selection = readIssueSelection(response.selection, bucket.kind);
      bucket.data =
        append && cursor
          ? deduplicateByID(bucket.data.concat(page), "issue_id")
          : deduplicateByID(page, "issue_id");
      bucket.nextCursor = pagination.nextCursor;
      bucket.hasMore = pagination.hasMore;
      bucket.viewCursor = viewCursor;
      bucket.analysis = isRecord(response.analysis) ? response.analysis : null;
      bucket.selection = selection;
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

  async function loadAttentionFamilies(bucket, append) {
    const generation = ++bucket.requestGeneration;
    const cursor = append ? bucket.nextCursor : "";
    bucket.status = append ? "loading-more" : "loading";
    bucket.error = null;
    renderIssueBucket(bucket);
    try {
      const response = await apiGet(buildAttentionFamilyPath(cursor));
      if (generation !== bucket.requestGeneration) return false;
      const page = Array.isArray(response.data) ? response.data : [];
      const pagination = requireCursorPage(
        response,
        cursor,
        "attention-family-list",
      );
      const selection = readIssueSelection(response.selection, "issue");
      page.forEach(requireAttentionFamilySummary);
      bucket.data =
        append && cursor
          ? deduplicateByID(bucket.data.concat(page), "family_id")
          : deduplicateByID(page, "family_id");
      bucket.nextCursor = pagination.nextCursor;
      bucket.hasMore = pagination.hasMore;
      bucket.analysis = readGlobalAnalysisCoverage(
        response.global_analysis_coverage,
        readGlobalAnalysisCoverage(response.analysis, bucket.analysis),
      );
      bucket.selection = selection;
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
      showError("Unable to group Attention signals", error);
      return false;
    }
  }

  function buildAttentionFamilyPath(cursor) {
    if (cursor) {
      return `/v1/attention-families?${new URLSearchParams({ cursor }).toString()}`;
    }
    const parameters = new URLSearchParams({
      limit: String(pageLimits.issues.page),
      attention_kind: "issue",
      experimental: state.issueFilters.experimental ? "include" : "stable",
    });
    if (state.issueFilters.severity) {
      parameters.set("severity", state.issueFilters.severity);
    }
    if (state.issueFilters.harness) {
      parameters.set("harness", state.issueFilters.harness);
    }
    if (state.issueFilters.origin) {
      parameters.set("origin", state.issueFilters.origin);
    }
    if (state.issueFilters.analysisStatus) {
      parameters.set("analysis_status", state.issueFilters.analysisStatus);
    }
    return `/v1/attention-families?${parameters.toString()}`;
  }

  function buildIssuePath(kind, cursor) {
    if (cursor) {
      return `/v1/issues?${new URLSearchParams({ cursor }).toString()}`;
    }
    const parameters = new URLSearchParams({
      limit: String(pageLimits.issues.page),
      attention_kind: kind,
      experimental: state.issueFilters.experimental ? "include" : "stable",
    });
    if (state.issueFilters.severity) {
      parameters.set("severity", state.issueFilters.severity);
    }
    if (state.issueFilters.harness) {
      parameters.set("harness", state.issueFilters.harness);
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
    if (bucket.kind === "family") {
      renderAttentionFamilyBucket(bucket);
      return;
    }
    const selection = bucket.selection || expectedIssueSelection(bucket.kind);
    const isGap = selection.attention_kind === "evidence_gap";
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

  function renderAttentionFamilyBucket(bucket) {
    focusRegistry.familyCards.clear();
    const fragment = document.createDocumentFragment();
    bucket.data.forEach((family) => {
      fragment.append(createAttentionFamilyCard(family));
    });
    elements.issueList.replaceChildren(fragment);
    elements.issuesLoading.hidden = bucket.status !== "loading";
    elements.issuesEmpty.hidden =
      bucket.data.length !== 0 ||
      bucket.status === "loading" ||
      bucket.status === "idle";
    elements.issueCount.textContent = bucket.hasMore
      ? `${bucket.data.length}+`
      : String(bucket.data.length);
    elements.issuesPagination.hidden = !bucket.hasMore;
    elements.issuesLoadMore.disabled = bucket.status === "loading-more";
    elements.issuesPageStatus.textContent = bucket.hasMore
      ? `Showing ${bucket.data.length} signal families; more are available.`
      : `Showing ${bucket.data.length} supported signal ${
          bucket.data.length === 1 ? "family" : "families"
        }.`;
    if (!elements.issuesEmpty.hidden) renderIssueEmptyState(bucket);
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
        : "Attention grouping is unavailable";
      detail.textContent = isGap
        ? "The Local read failed. Existing session data remains available."
        : "Sessions and exact retained data remain available.";
      return;
    }
    if (attentionFiltersActive()) {
      title.textContent = isGap
        ? "No evidence gaps match these filters"
        : "No supported signals match these filters";
      detail.textContent =
        "Analysis coverage is shown above; this is not a global safety claim.";
      return;
    }
    const analysis = bucket.analysis || state.issues.analysis;
    if (analysis && analysis.complete === true) {
      title.textContent = isGap
        ? "No evidence gaps reported by configured detectors"
        : "No supported signals were reported in completed retained analysis";
      detail.textContent = isGap
        ? "Supported completed analysis did not report a verification evidence gap."
        : "The fixed family catalog admitted no stable Attention signals.";
      return;
    }
    title.textContent = isGap
      ? "No evidence gaps are available from completed analysis"
      : "No supported signals are available from completed analysis";
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
    const selection =
      state.issues.selection || expectedIssueSelection("issue");
    const experimental =
      selection.includes_experimental === true ||
      state.issueFilters.experimental;
    const active = attentionFiltersActive();
    elements.attentionFilterNote.textContent = experimental
      ? "Experimental signals are included and labeled."
      : active
        ? "Stable signals matching the selected filters."
        : "Showing stable signals.";
    elements.clearAttentionFilters.disabled = !active;
    elements.issueFilterDisclosure.textContent = occurrenceFiltersActive()
      ? "Displayed family and evidence-gap counts match the active filters. Analysis coverage remains global for the frozen retained snapshot."
      : "";
  }

  function attentionFiltersActive() {
    return (
      state.issueFilters.experimental ||
      [
        "severity",
        "harness",
        "origin",
        "analysisStatus",
      ].some((key) => Boolean(state.issueFilters[key]))
    );
  }

  function occurrenceFiltersActive() {
    return Boolean(
      state.issueFilters.harness ||
        state.issueFilters.analysisStatus ||
        state.issueFilters.severity ||
        state.issueFilters.origin,
    );
  }

  function requireAttentionFamilySummary(family) {
    if (
      !isRecord(family) ||
      !readText(family.family_id) ||
      !["exact_issue", "mapped_upstream"].includes(readText(family.kind)) ||
      !readText(family.representative_issue_id) ||
      !readCursor(family.view_cursor) ||
      !isRecord(family.catalog)
    ) {
      throw new Error("Local API returned an invalid Attention family.");
    }
    return family;
  }

  function createAttentionFamilyCard(family) {
    const familyID = readText(family.family_id);
    const kind = readText(family.kind);
    const catalog = isRecord(family.catalog) ? family.catalog : {};
    const article = createElement("article", "issue-card family-card");
    const button = createElement("button", "issue-card-main");
    button.type = "button";
    button.setAttribute(
      "aria-pressed",
      String(
        Boolean(state.selectedFamily) &&
          readText(state.selectedFamily.family_id) === familyID,
      ),
    );
    button.addEventListener("click", () => {
      if (kind === "exact_issue") {
        const issue = exactFamilyRepresentative(family);
        void selectIssue(issue, "issue", true, {
          viewCursor: readCursor(family.view_cursor),
          catalog,
          familyID,
          returnFocus: {
            type: "family",
            familyID,
          },
        });
        return;
      }
      if (toFiniteNumber(family.supporting_issue_count) === 1) {
        void selectSingleMemberAttentionFamily(family, true);
        return;
      }
      void selectAttentionFamily(family, true);
    });
    if (familyID) focusRegistry.familyCards.set(familyFocusKey(familyID), button);

    const top = createElement("span", "issue-card-top");
    const severity = createElement(
      "span",
      "severity-badge",
      readableLabel(family.severity, "Reported"),
    );
    severity.dataset.tone = severityTone(family.severity);
    const title = createElement("span", "issue-card-title");
    title.append(
      createElement(
        "strong",
        "",
        readText(catalog.display_title) || "Supported Attention signal",
      ),
      createElement("small", "", attentionFamilyObservationSummary(family)),
    );
    top.append(severity, title);
    const status = normalizeAnalysisStatus(family.analysis_status);
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
        `${formatNumber(family.occurrence_count)} retained ${
          toFiniteNumber(family.occurrence_count) === 1
            ? "observation"
            : "observations"
        }`,
      ),
      createElement("span", "", issueHarnessLabel(family.harnesses)),
      createElement("time", "", formatRelativeTime(family.last_observed_at)),
    );
    button.append(top, meta);
    if (kind === "mapped_upstream") {
      button.append(
        createElement(
          "span",
          "issue-card-caveat family-disclosure",
          "Grouped by one known signal type. Retained records may come from unrelated sessions or projects and do not establish recurrence or one cause.",
        ),
      );
    }
    button.append(
      createElement(
        "span",
        "issue-card-action",
        kind === "mapped_upstream"
          ? "Review affected sessions and cited configuration evidence"
          : issueNextActionLabel(catalog.next_evidence_action),
      ),
    );
    if (family.experimental === true) {
      button.append(
        createElement("span", "experimental-label", "Experimental signal"),
      );
    }
    article.append(button);
    return article;
  }

  function attentionFamilyObservationSummary(family) {
    const sessions = Number(family && family.session_count);
    const sessionText =
      Number.isFinite(sessions) && sessions > 0
        ? `Observed in ${formatNumber(sessions)} retained ${
            sessions === 1 ? "session" : "sessions"
          }`
        : "Retained session count unavailable";
    return `${sessionText} · ${issueHarnessLabel(
      family && family.harnesses,
    )} · last observed ${formatRelativeTime(family && family.last_observed_at)}`;
  }

  function exactFamilyRepresentative(family) {
    return {
      issue_id: readText(family.representative_issue_id),
      title_code: "",
      severity: readText(family.severity),
      confidence: readText(family.confidence),
      first_observed_at: family.first_observed_at,
      last_observed_at: family.last_observed_at,
      occurrence_count: family.occurrence_count,
      session_count: family.session_count,
      harnesses: Array.isArray(family.harnesses) ? family.harnesses : [],
      analysis_status: readText(family.analysis_status),
      evidence_complete: family.evidence_complete,
      retained_history_only: family.retained_history_only === true,
      experimental: family.experimental === true,
    };
  }

  async function selectAttentionFamily(
    family,
    moveFocus,
    revealDetail = true,
  ) {
    const familyID = readText(family && family.family_id);
    const viewCursor = readCursor(family && family.view_cursor);
    if (
      !familyID ||
      readText(family && family.kind) !== "mapped_upstream" ||
      !viewCursor
    ) {
      return false;
    }
    if (moveFocus) state.attentionRefreshGeneration += 1;
    state.selectedFamily = family;
    state.selectedFamilyViewCursor = viewCursor;
    state.familyMembers = [];
    state.familyMemberNextCursor = "";
    state.familyMemberHasMore = false;
    state.familyMemberStatus = "loading";
    state.familyMemberError = "";
    state.familyReturnFocus = { type: "family", familyID };
    focusRegistry.familyMembers.clear();
    elements.attentionWelcome.hidden = revealDetail;
    elements.issueDetail.hidden = true;
    elements.familyDetail.hidden = !revealDetail;
    document.body.classList.toggle(
      "is-attention-detail-open",
      revealDetail,
    );
    renderIssueBucket(state.issues);
    renderAttentionFamilyDetail();
    applyPaneAccessibility();
    if (moveFocus && revealDetail) {
      focusCurrentElement(elements.familyDetailHeading);
    }
    return loadAttentionFamilyDetail(false);
  }

  async function selectSingleMemberAttentionFamily(family, moveFocus) {
    const ready = await selectAttentionFamily(family, false, false);
    if (!ready) {
      closeFamilyDetail(false);
      showAttentionNotice(
        "The affected retained record could not be opened. Refresh Attention and try again.",
        "error",
      );
      return false;
    }
    if (
      state.familyMembers.length !== 1 ||
      state.familyMemberHasMore
    ) {
      elements.attentionWelcome.hidden = true;
      elements.familyDetail.hidden = false;
      document.body.classList.add("is-attention-detail-open");
      renderAttentionFamilyDetail();
      applyPaneAccessibility();
      if (moveFocus) focusCurrentElement(elements.familyDetailHeading);
      return true;
    }
    const member = state.familyMembers[0];
    const familyID = readText(family && family.family_id);
    const evidenceContext = attentionFamilyEvidenceContext(family);
    closeFamilyDetail(false);
    return selectIssue(member.issue, "issue", moveFocus, {
      viewCursor: readCursor(member.view_cursor),
      catalog: member.catalog,
      familyID,
      evidenceContext,
      returnFocus: { type: "family", familyID },
    });
  }

  function attentionFamilyEvidenceContext(family) {
    const catalog = isRecord(family && family.catalog) ? family.catalog : {};
    return readText(catalog.mapping_key) ===
      "attention.agent_guardrails_configuration"
      ? "mapped-guardrail"
      : "";
  }

  function issueEvidenceContext(issue, catalog, previous = "") {
    if (previous === "mapped-guardrail") return previous;
    const sourceSignalCode =
      safeSourceSignalCode(catalog && catalog.source_signal_code) ||
      safeSourceSignalCode(issue && issue.source_signal_code);
    return sourceSignalCode === "tamper.guardrails_off"
      ? "mapped-guardrail"
      : "";
  }

  async function loadAttentionFamilyDetail(append) {
    const family = state.selectedFamily;
    const familyID = readText(family && family.family_id);
    if (!familyID) return false;
    const generation = ++state.familyMemberRequestGeneration;
    const cursor = append ? state.familyMemberNextCursor : "";
    state.familyMemberStatus = append ? "loading-more" : "loading";
    state.familyMemberError = "";
    if (!append) {
      state.familyMembers = [];
      state.familyMemberNextCursor = "";
      state.familyMemberHasMore = false;
    }
    renderAttentionFamilyDetail();
    const parameters = cursor
      ? new URLSearchParams({ cursor })
      : new URLSearchParams({
          limit: String(pageLimits.familyMembers.page),
          view_cursor: state.selectedFamilyViewCursor,
        });
    try {
      const response = await apiGet(
        `/v1/attention-families/${encodeURIComponent(
          familyID,
        )}?${parameters.toString()}`,
      );
      if (
        generation !== state.familyMemberRequestGeneration ||
        familyID !==
          readText(state.selectedFamily && state.selectedFamily.family_id)
      ) {
        return false;
      }
      const payload = isRecord(response.data) ? response.data : {};
      const responseFamily = requireAttentionFamilySummary(payload.family);
      if (
        readText(responseFamily.family_id) !== familyID ||
        readText(responseFamily.kind) !== "mapped_upstream"
      ) {
        throw new Error(
          "Local API returned inconsistent Attention family detail.",
        );
      }
      const responseViewCursor = readCursor(response.view_cursor);
      if (
        !responseViewCursor ||
        responseViewCursor !== state.selectedFamilyViewCursor
      ) {
        throw new Error(
          "Local API changed the Attention family view cursor.",
        );
      }
      const members = Array.isArray(payload.members) ? payload.members : [];
      members.forEach(requireAttentionFamilyMember);
      const pagination = requireCursorPage(
        response,
        cursor,
        "attention-family-members",
      );
      state.selectedFamily = responseFamily;
      state.familyMembers =
        append && cursor
          ? deduplicateFamilyMembers(state.familyMembers.concat(members))
          : deduplicateFamilyMembers(members);
      state.familyMemberNextCursor = pagination.nextCursor;
      state.familyMemberHasMore = pagination.hasMore;
      state.familyMemberStatus = "ready";
      state.issues.analysis = readGlobalAnalysisCoverage(
        response.global_analysis_coverage,
        state.issues.analysis,
      );
      renderAttentionFamilyDetail();
      renderAnalysisCoverage();
      return true;
    } catch (error) {
      if (
        generation !== state.familyMemberRequestGeneration ||
        familyID !==
          readText(state.selectedFamily && state.selectedFamily.family_id)
      ) {
        return false;
      }
      if (isCursorExpired(error)) {
        clearAttentionFamilyDetailState();
        await refreshAttentionAfterExpiry();
        return false;
      }
      state.familyMemberStatus = "error";
      state.familyMemberError =
        error instanceof Error
          ? error.message
          : "Affected retained records could not be loaded.";
      renderAttentionFamilyDetail();
      return false;
    }
  }

  function requireAttentionFamilyMember(member) {
    if (
      !isRecord(member) ||
      !isRecord(member.issue) ||
      !readText(member.issue.issue_id) ||
      !isRecord(member.catalog) ||
      !readCursor(member.view_cursor)
    ) {
      throw new Error("Local API returned an invalid Attention family member.");
    }
    return member;
  }

  function deduplicateFamilyMembers(values) {
    const seen = new Set();
    return values.filter((member) => {
      const issueID = readText(member && member.issue && member.issue.issue_id);
      if (!issueID || seen.has(issueID)) return false;
      seen.add(issueID);
      return true;
    });
  }

  function renderAttentionFamilyDetail() {
    const family = state.selectedFamily;
    if (!family) return;
    const catalog = isRecord(family.catalog) ? family.catalog : {};
    const status = normalizeAnalysisStatus(family.analysis_status);
    elements.familyDetailHeading.textContent =
      readText(catalog.display_title) || "Supported Attention signal";
    elements.familyObservation.textContent =
      readText(catalog.observation_statement) ||
      "A supported deterministic signal was reported in retained Local analysis.";
    elements.familyCaveat.textContent = [
      readText(catalog.caveat),
      "Grouped by one known signal type. Members may come from unrelated sessions or projects and do not establish recurrence or one cause.",
    ]
      .filter(Boolean)
      .join(" ");
    elements.familyNextAction.textContent =
      "Next: choose one affected retained record to inspect its cited evidence.";
    elements.familyAnalysisQualifier.textContent =
      analysisQualifiers[status] || "";
    elements.familyDetailBadges.replaceChildren(
      createToneBadge(family.severity),
      createElement("span", "meta-badge", analysisStatusLabel(status)),
    );
    elements.familySummary.textContent = attentionFamilyDetailSummary(family);
    focusRegistry.familyMembers.clear();
    const fragment = document.createDocumentFragment();
    state.familyMembers.forEach((member) => {
      fragment.append(createAttentionFamilyMember(member));
    });
    if (state.familyMemberStatus === "error") {
      fragment.append(
        createElement(
          "p",
          "family-member-error",
          state.familyMemberError ||
            "Affected retained records could not be loaded. Other Attention data remains available.",
        ),
      );
    }
    elements.familyMemberList.replaceChildren(fragment);
    elements.familyMembersLoading.hidden =
      state.familyMemberStatus !== "loading";
    elements.familyMembersEmpty.hidden =
      state.familyMemberStatus !== "ready" ||
      state.familyMembers.length !== 0;
    elements.familyMemberCount.textContent = state.familyMemberHasMore
      ? `${state.familyMembers.length}+`
      : String(state.familyMembers.length);
    elements.familyMembersPagination.hidden = !state.familyMemberHasMore;
    elements.familyMembersLoadMore.disabled =
      state.familyMemberStatus === "loading-more";
    elements.familyMembersPageStatus.textContent = state.familyMemberHasMore
      ? `Showing ${state.familyMembers.length} affected retained records; more are available.`
      : `Showing ${state.familyMembers.length} affected retained ${
          state.familyMembers.length === 1 ? "record" : "records"
        }.`;
  }

  function attentionFamilyDetailSummary(family) {
    const scope = isRecord(family.scope) ? family.scope : {};
    const scoped =
      toFiniteNumber(scope.resolved) + toFiniteNumber(scope.lexical);
    const uncertain =
      toFiniteNumber(scope.unscoped) + toFiniteNumber(scope.conflict);
    const parts = [
      attentionFamilyObservationSummary(family),
      `${formatNumber(family.supporting_issue_count)} exact retained ${
        toFiniteNumber(family.supporting_issue_count) === 1
          ? "record"
          : "records"
      }`,
    ];
    if (scoped > 0) parts.push(`${formatNumber(scoped)} with usable scope`);
    if (uncertain > 0) {
      parts.push(
        `${formatNumber(uncertain)} with unavailable or conflicting scope`,
      );
    }
    return parts.join(" · ");
  }

  function createAttentionFamilyMember(member) {
    const issue = member.issue;
    const familyID = readText(
      state.selectedFamily && state.selectedFamily.family_id,
    );
    const issueID = readText(issue.issue_id);
    const key = familyMemberFocusKey(familyID, issueID);
    const button = createElement("button", "family-member-card");
    button.type = "button";
    button.append(
      createElement(
        "strong",
        "",
        `${issueHarnessLabel(issue.harnesses)} · observed ${formatRelativeTime(
          issue.last_observed_at,
        )}`,
      ),
      createElement(
        "span",
        "",
        `${formatNumber(issue.occurrence_count)} retained ${
          toFiniteNumber(issue.occurrence_count) === 1
            ? "observation"
            : "observations"
        } · ${issueRecurrenceLabel(issue)}`,
      ),
      createElement(
        "span",
        "family-member-state",
        [
          analysisStatusLabel(normalizeAnalysisStatus(issue.analysis_status)),
          evidenceCompletenessLabel(issue.evidence_complete),
          familyScopeReliability(issue.scope_quality),
        ].join(" · "),
      ),
      createElement("span", "issue-card-action", "Inspect cited evidence"),
    );
    button.addEventListener("click", () => {
      void selectIssue(issue, "issue", true, {
        viewCursor: readCursor(member.view_cursor),
        catalog: member.catalog,
        source: "family",
        familyID,
        evidenceContext: attentionFamilyEvidenceContext(
          state.selectedFamily,
        ),
        returnFocus: { type: "family-member", key },
      });
    });
    focusRegistry.familyMembers.set(key, button);
    return button;
  }

  function familyScopeReliability(value) {
    switch (readText(value).toLowerCase()) {
      case "resolved":
      case "lexical":
        return "Scope available for this exact record";
      case "conflict":
        return "Scope evidence conflicts";
      case "unscoped":
        return "Scope unavailable";
      default:
        return "Scope reliability unavailable";
    }
  }

  function createIssueCard(issue, kind) {
    const issueID = readText(issue.issue_id);
    const article = createElement("article", "issue-card");
    const button = createElement("button", "issue-card-main");
    button.type = "button";
    button.setAttribute(
      "aria-pressed",
      String(
        state.selectedIssueSource === "attention" &&
          issueID === state.selectedIssueID &&
          state.selectedIssueKind === kind,
      ),
    );
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
    const catalog = issueDisplayCatalog(issue);
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
    button.append(
      createElement(
        "span",
        "issue-card-action",
        catalog.action || "Inspect retained evidence",
      ),
    );
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

  function selectIssue(issue, kind, moveFocus, options = {}) {
    const issueID = readText(issue.issue_id);
    if (!issueID) return Promise.resolve(false);
    if (moveFocus) state.attentionRefreshGeneration += 1;
    state.selectedIssueID = issueID;
    state.selectedIssueKind = kind === "evidence_gap" ? "evidence_gap" : "issue";
    state.selectedIssueSource =
      options.source === "family" ? "family" : "attention";
    state.selectedIssueFamilyID = readText(options.familyID);
    state.selectedIssueEvidenceContext = readText(options.evidenceContext);
    state.selectedDrivingAnnotationID = "";
    state.selectedIssue = issue;
    state.selectedIssueCatalog = isRecord(options.catalog)
      ? options.catalog
      : null;
    state.selectedGlobalAnalysisCoverage = null;
    state.selectedIssueViewCursor = "";
    state.issueReturnFocus = options.returnFocus || {
      type: "issue",
      key: issueFocusKey(state.selectedIssueKind, issueID),
    };
    state.occurrences = [];
    state.occurrenceNextCursor = "";
    state.occurrenceHasMore = false;
    state.occurrenceStatus = "loading";
    resetIssueEvidencePreview();
    resetFixIssueState();
    elements.attentionWelcome.hidden = true;
    elements.familyDetail.hidden = true;
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
    const viewCursor =
      readCursor(options.viewCursor) || readCursor(bucket.viewCursor);
    void loadFixEligibility(issueID, viewCursor);
    void loadFixMonitoringDetail(issueID, false, "", false, "");
    return loadIssueDetail(false, viewCursor);
  }

  async function loadIssueDetail(append, viewCursor) {
    const issueID = state.selectedIssueID;
    if (!issueID) return false;
    const generation = ++state.occurrenceRequestGeneration;
    state.occurrenceStatus = append ? "loading-more" : "loading";
    renderIssueDetail();
    const cursor = append ? state.occurrenceNextCursor : "";
    const parameters = cursor
      ? new URLSearchParams({ cursor })
      : new URLSearchParams({
          limit: String(pageLimits.occurrences.page),
        });
    if (!cursor && viewCursor) {
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
      if (!issue || readText(issue.issue_id) !== issueID) {
        throw new Error("Local API returned inconsistent issue detail.");
      }
      const pagination = requireCursorPage(
        response,
        cursor,
        "issue-occurrence",
      );
      const responseViewCursor = readCursor(response.view_cursor);
      if (!responseViewCursor) {
        throw new Error(
          "Local API returned issue detail without a view cursor.",
        );
      }
      if (
        cursor &&
        responseViewCursor !== state.selectedIssueViewCursor
      ) {
        throw new Error(
          "Local API changed the issue-detail view cursor during continuation.",
        );
      }
      const catalog = readIssueCatalog(
        response.catalog,
        issue,
        state.selectedIssueCatalog,
      );
      const evidenceContext = issueEvidenceContext(
        issue,
        catalog,
        state.selectedIssueEvidenceContext,
      );
      const globalCoverage = readGlobalAnalysisCoverage(
        response.global_analysis_coverage,
        state.selectedGlobalAnalysisCoverage ||
          selectedIssueListCoverage(),
      );
      state.selectedIssue = issue;
      state.selectedIssueCatalog = catalog;
      state.selectedIssueEvidenceContext = evidenceContext;
      state.selectedGlobalAnalysisCoverage = globalCoverage;
      state.selectedIssueViewCursor = responseViewCursor;
      state.occurrences =
        append && cursor
          ? deduplicateByID(
              state.occurrences.concat(page),
              "occurrence_id",
            )
          : deduplicateByID(page, "occurrence_id");
      state.occurrenceNextCursor = pagination.nextCursor;
      state.occurrenceHasMore = pagination.hasMore;
      state.occurrenceStatus = "ready";
      renderIssueDetail();
      if (!append && state.occurrences.length > 0) {
        void loadIssueEvidencePreview(state.occurrences[0], false);
      }
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

  function resetIssueEvidencePreview() {
    state.issueEvidencePreviewRequestGeneration += 1;
    if (state.issueEvidencePreviewController) {
      state.issueEvidencePreviewController.abort();
      state.issueEvidencePreviewController = null;
    }
    state.issueEvidencePreview = createIssueEvidencePreview();
    renderIssueEvidencePreview();
  }

  async function loadIssueEvidencePreview(occurrence, expanded) {
    const issueID = state.selectedIssueID;
    const sessionID = readText(occurrence && occurrence.session_id);
    const occurrenceID = readText(occurrence && occurrence.occurrence_id);
    const availableEventIDs = occurrenceEventIDs(occurrence);
    const eventIDs = availableEventIDs.slice(
      0,
      expanded ? pageLimits.events.maximum / 10 : 3,
    );
    if (state.issueEvidencePreviewController) {
      state.issueEvidencePreviewController.abort();
      state.issueEvidencePreviewController = null;
    }
    const generation = ++state.issueEvidencePreviewRequestGeneration;
    state.issueEvidencePreview = {
      ...createIssueEvidencePreview(),
      issueID,
      occurrenceID,
      sessionID,
      status: eventIDs.length ? "loading" : "ready",
      requestedCount: eventIDs.length,
      availableEventIDs,
      expanded,
    };
    renderIssueEvidencePreview();
    if (!issueID || !sessionID || eventIDs.length === 0) return;

    const controller = new AbortController();
    state.issueEvidencePreviewController = controller;
    const parameters = new URLSearchParams();
    eventIDs.forEach((eventID) => parameters.append("event_id", eventID));
    try {
      const response = await apiGet(
        `/v1/sessions/${encodeURIComponent(sessionID)}/events/lookup?${parameters.toString()}`,
        controller.signal,
      );
      if (
        generation !== state.issueEvidencePreviewRequestGeneration ||
        issueID !== state.selectedIssueID ||
        occurrenceID !== state.issueEvidencePreview.occurrenceID ||
        controller !== state.issueEvidencePreviewController
      ) {
        return;
      }
      const events = Array.isArray(response.data) ? response.data : [];
      state.issueEvidencePreview = {
        ...state.issueEvidencePreview,
        status: "ready",
        events,
        requestedCount:
          toFiniteNumber(response.requested_count) || eventIDs.length,
        foundCount: toFiniteNumber(response.found_count) || events.length,
        missingCount: Number.isFinite(Number(response.missing_count))
          ? toFiniteNumber(response.missing_count)
          : Math.max(0, eventIDs.length - events.length),
        missingEventIDs: Array.isArray(response.missing_event_ids)
          ? response.missing_event_ids.map(readText).filter(Boolean)
          : [],
      };
      renderIssueEvidencePreview();
    } catch (error) {
      if (
        generation !== state.issueEvidencePreviewRequestGeneration ||
        issueID !== state.selectedIssueID ||
        controller.signal.aborted
      ) {
        return;
      }
      if (isCursorExpired(error)) {
        resetIssueEvidencePreview();
        await refreshAttentionAfterExpiry();
        return;
      }
      state.issueEvidencePreview.status = "error";
      state.issueEvidencePreview.error =
        error instanceof Error
          ? error.message
          : "Cited evidence could not be loaded.";
      renderIssueEvidencePreview();
    } finally {
      if (state.issueEvidencePreviewController === controller) {
        state.issueEvidencePreviewController = null;
      }
    }
  }

  function renderIssueEvidencePreview() {
    const preview = state.issueEvidencePreview;
    if (!elements.issueEvidencePreviewList) return;
    const fragment = document.createDocumentFragment();
    if (preview.status === "loading") {
      fragment.append(
        createElement("p", "overview-empty", "Loading cited events…"),
      );
    } else if (preview.status === "error") {
      fragment.append(
        createElement(
          "p",
          "overview-empty",
          preview.error || "Cited evidence could not be loaded.",
        ),
      );
    } else if (preview.status === "ready" && preview.events.length) {
      preview.events.forEach((event) => {
        fragment.append(
          createLookupEvent(event, state.selectedIssueEvidenceContext),
        );
      });
    } else if (preview.status === "ready") {
      fragment.append(
        createElement(
          "p",
          "overview-empty",
          preview.requestedCount
            ? "None of the requested cited events remains in this retained session."
            : "This occurrence did not supply a retained cited event ID.",
        ),
      );
    }
    elements.issueEvidencePreviewList.replaceChildren(fragment);
    elements.issueEvidencePreviewCount.textContent =
      preview.status === "ready"
        ? `${preview.foundCount}/${preview.requestedCount}`
        : "—";
    const retentionText =
      "Event hydration uses current Local retention, not the frozen issue snapshot.";
    elements.issueEvidencePreviewDisclosure.textContent =
      preview.status === "ready"
        ? `${preview.foundCount} of ${preview.requestedCount} requested cited events retained; ${preview.missingCount} missing. ${retentionText}`
        : retentionText;
    elements.issueEvidencePreviewAll.hidden =
      preview.status === "idle" ||
      preview.status === "loading" ||
      preview.expanded ||
      preview.availableEventIDs.length <= 3;
  }

  function renderIssueDetail() {
    const issue = state.selectedIssue || {};
    const catalog =
      state.selectedIssueSource === "attention" ||
      state.selectedIssueSource === "family"
        ? issueDetailCatalog(issue, state.selectedIssueCatalog)
        : monitoringSubjectCatalog(issue);
    const historyOnly =
      state.fixHistoryCurrentIssueAvailable === false;
    const currentProjectionPending =
      state.selectedIssueSource === "monitoring" &&
      state.fixHistoryCurrentIssueAvailable === null;
    const kind =
      historyOnly
        ? "Retained attempt history"
        : currentProjectionPending
          ? "Post-attempt monitoring"
        : state.selectedIssueKind === "evidence_gap"
          ? "Evidence gap"
          : "Issue";
    elements.issueDetailKind.textContent = kind;
    elements.issueDetailHeading.textContent = catalog.title;
    elements.issueExplanation.textContent = catalog.explanation;
    elements.issueNextAction.textContent = historyOnly
      ? "Inspect retained attempt history."
      : currentProjectionPending
        ? "Wait for the current issue projection before inspecting matching evidence."
        : issueNextActionLabel(catalog.nextEvidenceAction);
    const status = normalizeAnalysisStatus(issue.analysis_status);
    const coverageQualifier = globalCoverageQualifier(
      state.selectedGlobalAnalysisCoverage,
    );
    const analysisQualifier = analysisQualifiers[status] || "";
    elements.issueAnalysisQualifier.textContent =
      historyOnly
        ? "The current issue projection is unavailable. Durable attempt and observation history remains available."
        : currentProjectionPending
          ? "Checking whether a current issue projection remains available."
          : [analysisQualifier, coverageQualifier].filter(Boolean).join(" ");
    elements.issueScopeDisclosure.textContent = historyOnly
      ? "Current fix eligibility and matching sessions are unavailable in history-only detail."
      : currentProjectionPending
        ? "Attempt history is loading from the selected monitoring snapshot."
        : issueScopeDisclosure(issue, catalog.caveat);
    const badges = [
      createToneBadge(issue.severity),
    ];
    if (!historyOnly && !currentProjectionPending) {
      badges.push(
        createElement("span", "meta-badge", issueRecurrenceLabel(issue)),
        createElement("span", "meta-badge", analysisStatusLabel(status)),
      );
    } else {
      badges.push(createElement("span", "meta-badge", "History only"));
    }
    if (catalog.experimental || issue.experimental === true) {
      badges.push(createElement("span", "experimental-label", "Experimental"));
    }
    elements.issueDetailBadges.replaceChildren(...badges);
    elements.issueFingerprint.textContent =
      readText(issue.fingerprint_id) || "Unavailable";
    elements.copyIssueFingerprint.disabled = !readText(issue.fingerprint_id);
    elements.fingerprintPanel.hidden = historyOnly || currentProjectionPending;
    elements.matchingSection.hidden = historyOnly || currentProjectionPending;
    elements.issueEvidencePreview.hidden =
      historyOnly || currentProjectionPending;
    renderIssueMetadata(issue);
    renderIssueEvidencePreview();
    renderFixAttempts();
    if (!historyOnly && !currentProjectionPending) renderOccurrences();
  }

  function monitoringSubjectCatalog(value) {
    const subject = isRecord(value && value.subject) ? value.subject : value;
    const titleCode = readText(subject && subject.title_code);
    return issueCatalogEntry(
      titleCode && !titleCode.startsWith("issue.")
        ? `issue.${titleCode}`
        : titleCode,
    );
  }

  function issueDetailCatalog(issue, metadata) {
    const display = monitoringSubjectCatalog(issue);
    const catalog = metadata || fallbackIssueCatalog(issue);
    return {
      ...display,
      title: readText(catalog.display_title) || display.title,
      explanation:
        readText(catalog.observation_statement) || display.explanation,
      caveat: readText(catalog.caveat),
      nextEvidenceAction:
        readText(catalog.next_evidence_action) || "inspect_cited_events",
    };
  }

  function issueNextActionLabel(action) {
    const labels = {
      inspect_cited_events: "Next: inspect the cited retained events.",
      inspect_matching_sessions:
        "Next: compare the exact matching sessions.",
      inspect_verification_events:
        "Next: inspect the retained verification evidence.",
      review_agent_permissions:
        "Next: review the cited configuration evidence and the agent's permission settings.",
    };
    return labels[action] || labels.inspect_cited_events;
  }

  function monitoringAttemptSubjectIssue(attempt) {
    if (!isRecord(attempt) || !isRecord(attempt.subject)) return null;
    return {
      issue_id: readText(attempt.issue_id),
      title_code: readText(attempt.subject.title_code),
      severity: readText(attempt.subject.severity),
      confidence: readText(attempt.subject.confidence),
      origin: readText(attempt.subject.origin),
      detector_id: readText(attempt.subject.detector_id),
      detector_version: readText(attempt.subject.detector_version),
      fingerprint_version: readText(attempt.subject.fingerprint_version),
      retained_history_only: true,
    };
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
    const sourceSignalCode = safeSourceSignalCode(issue.source_signal_code);
    if (sourceSignalCode) {
      metadata.push(["Source rule ID", sourceSignalCode]);
    }
    const fragment = document.createDocumentFragment();
    metadata.forEach(([label, value]) => {
      const row = createElement("div");
      row.append(createElement("dt", "", label), createElement("dd", "", value));
      fragment.append(row);
    });
    elements.issueMetadata.replaceChildren(fragment);
  }

  function resetFixIssueState() {
    state.fixEligibilityRequestGeneration += 1;
    state.fixHistoryRequestGeneration += 1;
    state.fixEligibility = null;
    state.fixEligibilityStatus = "idle";
    state.fixHistory = [];
    state.fixHistoryNextCursor = "";
    state.fixHistoryHasMore = false;
    state.fixHistoryStatus = "idle";
    state.fixHistoryViewCursor = "";
    state.fixHistoryCurrentIssueAvailable = null;
    state.fixHistoryStale = false;
    state.fixHistoryError = null;
    state.fixEvidenceEvaluatedAt = "";
    state.fixActionMessage = "";
    state.fixActionTone = "status";
    focusRegistry.fixTriggers.clear();
    focusRegistry.fixHistoryRows.clear();
    focusRegistry.fixRetractionTriggers.clear();
    focusRegistry.fixObservationRows.clear();
    fixObservationPages.forEach((page) => {
      page.requestGeneration += 1;
    });
    fixObservationPages.clear();
  }

  async function loadFixEligibility(issueID, viewCursor) {
    const generation = ++state.fixEligibilityRequestGeneration;
    state.fixEligibility = null;
    state.fixEligibilityStatus = "loading";
    renderFixAttempts();
    if (!viewCursor) {
      state.fixEligibilityStatus = "error";
      renderFixAttempts();
      return false;
    }
    const parameters = new URLSearchParams({ view_cursor: viewCursor });
    try {
      const response = await apiGet(
        `/v1/issues/${encodeURIComponent(issueID)}/fix-eligibility?${parameters.toString()}`,
      );
      requireFixSchema(response);
      if (
        generation !== state.fixEligibilityRequestGeneration ||
        issueID !== state.selectedIssueID
      ) {
        return false;
      }
      const eligibility = isRecord(response.data) ? response.data : {};
      state.fixEligibility = {
        eligible: eligibility.eligible === true,
        reason: readText(eligibility.reason),
        actionToken: readText(eligibility.action_token),
        expiresAt: readText(eligibility.expires_at),
        changeCatalogVersion: readText(
          eligibility.change_catalog_version,
        ),
      };
      state.fixEligibilityStatus = "ready";
      renderFixAttempts();
      return true;
    } catch (error) {
      if (
        generation !== state.fixEligibilityRequestGeneration ||
        issueID !== state.selectedIssueID
      ) {
        return false;
      }
      if (isCursorExpired(error)) {
        await refreshAttentionAfterExpiry();
        return false;
      }
      state.fixEligibility = null;
      state.fixEligibilityStatus = "error";
      renderFixAttempts();
      return false;
    }
  }

  async function loadMonitoringIssueEligibility(issueID) {
    const loadedBucket = state.issues.data.some(
      (entry) => readText(entry.issue_id) === issueID,
    )
      ? state.issues
      : state.evidenceGaps.data.some(
            (entry) => readText(entry.issue_id) === issueID,
          )
        ? state.evidenceGaps
        : null;
    const loadedViewCursor = readCursor(
      loadedBucket && loadedBucket.viewCursor,
    );
    if (loadedViewCursor) {
      return loadFixEligibility(issueID, loadedViewCursor);
    }

    const generation = ++state.fixEligibilityRequestGeneration;
    state.fixEligibility = null;
    state.fixEligibilityStatus = "loading";
    renderFixAttempts();
    const parameters = new URLSearchParams({ limit: "1" });
    try {
      const response = await apiGet(
        `/v1/issues/${encodeURIComponent(issueID)}/occurrences?${parameters.toString()}`,
      );
      if (
        generation !== state.fixEligibilityRequestGeneration ||
        issueID !== state.selectedIssueID ||
        state.selectedIssueSource !== "monitoring"
      ) {
        return false;
      }
      if (
        !isRecord(response) ||
        response.schema_version !== "belay.read.v1" ||
        !isRecord(response.data) ||
        !isRecord(response.data.issue)
      ) {
        throw new Error("Local API returned an invalid exact issue result.");
      }
      const issue = response.data.issue;
      const viewCursor = readCursor(response.view_cursor);
      if (readText(issue.issue_id) !== issueID || !viewCursor) {
        throw new Error(
          "Local API did not return a current exact issue snapshot.",
        );
      }
      state.selectedIssue = issue;
      renderIssueDetail();
      return loadFixEligibility(issueID, viewCursor);
    } catch (error) {
      if (
        generation !== state.fixEligibilityRequestGeneration ||
        issueID !== state.selectedIssueID ||
        state.selectedIssueSource !== "monitoring"
      ) {
        return false;
      }
      state.fixEligibility = null;
      state.fixEligibilityStatus = "error";
      renderFixAttempts();
      return false;
    }
  }

  async function loadFixMonitoringDetail(
    issueID,
    append,
    viewCursor,
    preserveOnFailure,
    focusAnnotationID,
  ) {
    const generation = ++state.fixHistoryRequestGeneration;
    const cursor = append ? state.fixHistoryNextCursor : "";
    state.fixHistoryStatus = append ? "loading-more" : "loading";
    state.fixHistoryError = null;
    state.fixHistoryStale = false;
    renderFixAttempts();
    const parameters = new URLSearchParams({
      limit: String(pageLimits.fixMonitoringDetail.page),
    });
    if (cursor) {
      parameters.delete("limit");
      parameters.set("cursor", cursor);
    } else if (viewCursor) {
      parameters.set("view_cursor", viewCursor);
    }
    try {
      const response = await apiGet(
        `/v1/issues/${encodeURIComponent(issueID)}/fix-monitoring?${parameters.toString()}`,
      );
      requireFixMonitoringSchema(response);
      if (
        generation !== state.fixHistoryRequestGeneration ||
        issueID !== state.selectedIssueID
      ) {
        return false;
      }
      const evaluatedAt = readText(response.evidence_evaluated_at);
      const currentIssueAvailable =
        response.current_issue_available === true;
      const currentIssue = response.current_issue;
      if (
        (currentIssueAvailable && !isRecord(currentIssue)) ||
        (!currentIssueAvailable && currentIssue !== null) ||
        (currentIssueAvailable &&
          readText(currentIssue.issue_id) !== issueID)
      ) {
        throw new Error(
          "Local API returned an invalid current-issue monitoring state.",
        );
      }
      const page = Array.isArray(response.data)
        ? response.data
            .filter(
              (annotation) =>
                isRecord(annotation) &&
                readText(annotation.annotation_id) &&
                readText(annotation.issue_id) === issueID,
            )
            .map((annotation) => ({
              ...annotation,
              browser_evidence_evaluated_at: evaluatedAt,
            }))
        : [];
      const history =
        append && cursor
          ? deduplicateByID(
              state.fixHistory.concat(page),
              "annotation_id",
            )
          : deduplicateByID(page, "annotation_id");
      const nextCursor = readCursor(response.next_cursor);
      if (append && nextCursor && nextCursor === cursor) {
        throw new Error(
          "Local API returned a non-advancing attempt cursor.",
        );
      }
      if (response.has_more === true && !nextCursor) {
        throw new Error(
          "Local API returned attempt pagination without a cursor.",
        );
      }
      const monitoringViewCursor = readCursor(
        response.monitoring_view_cursor,
      );
      if (!monitoringViewCursor) {
        throw new Error(
          "Local API returned attempt monitoring without a view cursor.",
        );
      }
      state.fixHistory = history;
      state.fixHistoryNextCursor = nextCursor;
      state.fixHistoryHasMore = Boolean(state.fixHistoryNextCursor);
      state.fixHistoryViewCursor = monitoringViewCursor;
      state.fixHistoryCurrentIssueAvailable = currentIssueAvailable;
      state.fixEvidenceEvaluatedAt = evaluatedAt;
      state.fixHistoryStatus = "ready";
      state.fixHistoryError = null;
      state.fixHistoryStale = false;
      if (!append) {
        if (
          currentIssueAvailable &&
          state.selectedIssueSource === "monitoring"
        ) {
          state.selectedIssue = currentIssue;
          state.occurrenceStatus = "loading";
          void loadIssueDetail(false, "");
          void loadMonitoringIssueEligibility(issueID);
        } else if (!currentIssueAvailable) {
          if (state.selectedIssueSource === "monitoring") {
            state.selectedIssue =
              monitoringAttemptSubjectIssue(
                state.fixHistory.find(
                  (attempt) =>
                    readText(attempt.annotation_id) ===
                    state.selectedDrivingAnnotationID,
                ) || state.fixHistory[0],
              ) ||
              state.selectedIssue;
          }
          state.occurrenceRequestGeneration += 1;
          state.occurrenceStatus = "unavailable";
          state.fixEligibilityRequestGeneration += 1;
          state.fixEligibility = null;
          state.fixEligibilityStatus = "unavailable";
        }
      }
      renderFixAttempts();
      renderIssueDetail();
      if (
        focusAnnotationID &&
        !state.fixHistory.some(
          (attempt) =>
            readText(attempt.annotation_id) === focusAnnotationID,
        ) &&
        state.fixHistoryHasMore &&
        state.fixHistoryNextCursor
      ) {
        return loadFixMonitoringDetail(
          issueID,
          true,
          "",
          true,
          focusAnnotationID,
        );
      }
      if (focusAnnotationID) {
        const drivingAttempt =
          focusRegistry.fixHistoryRows.get(focusAnnotationID) || null;
        focusCurrentElement(drivingAttempt) ||
          focusCurrentElement(elements.issueDetailHeading);
      }
      return true;
    } catch (error) {
      if (
        generation !== state.fixHistoryRequestGeneration ||
        issueID !== state.selectedIssueID
      ) {
        return false;
      }
      if (isCursorExpired(error)) {
        return recoverExpiredFixMonitoringDetail(issueID, append);
      }
      if (preserveOnFailure && state.fixHistory.length) {
        state.fixHistoryStatus = "ready";
        state.fixHistoryStale = true;
      } else {
        state.fixHistoryStatus = "error";
      }
      state.fixHistoryError = fixMonitoringErrorMessage(error);
      renderFixAttempts();
      return false;
    }
  }

  async function recoverExpiredFixMonitoringDetail(issueID, append) {
    if (issueID !== state.selectedIssueID) return false;
    const expiredSurface = append
      ? "Attempt pagination"
      : "Attempt detail";
    state.attentionRefreshGeneration += 1;
    resetFixMonitoringBucket(false);
    closeIssueDetail(false);
    showAttentionNotice(
      `${expiredSurface} expired. Connected attempt and observation detail was cleared before refreshing After attempts…`,
      "pending",
    );
    const refreshed = await loadFixMonitoring(false);
    showAttentionNotice(
      refreshed
        ? `${expiredSurface} expired. After attempts was refreshed from a current snapshot; reopen the issue to inspect current evidence.`
        : `${expiredSurface} expired. Connected detail remains closed, and After attempts could not be refreshed.`,
      refreshed ? "status" : "error",
      refreshed ? 7000 : 0,
    );
    return false;
  }

  function loadMoreFixMonitoringDetail() {
    if (
      !state.selectedIssueID ||
      !state.fixHistoryHasMore ||
      !state.fixHistoryNextCursor
    ) {
      return;
    }
    void loadFixMonitoringDetail(
      state.selectedIssueID,
      true,
      "",
      true,
      "",
    );
  }

  function renderFixAttempts() {
    const issueID = state.selectedIssueID;
    if (!issueID) return;
    focusRegistry.fixTriggers.set(issueID, elements.recordFixAttempt);
    const eligibility = state.fixEligibility || {};
    const retainedDraft = fixDrafts.get(issueID);
    const canResume =
      Boolean(retainedDraft) &&
      retainedDraft.unresolved === true &&
      Boolean(retainedDraft.idempotencyKey) &&
      Boolean(retainedDraft.actionToken);
    const monitoringCurrentIssueUnavailable =
      state.fixHistoryCurrentIssueAvailable === false ||
      (state.selectedIssueSource === "monitoring" &&
        state.fixHistoryCurrentIssueAvailable === null);
    const catalogReady =
      eligibility.changeCatalogVersion === "fix-change.v1";
    const canRecord =
      !monitoringCurrentIssueUnavailable &&
      (canResume ||
        (state.fixEligibilityStatus === "ready" &&
          eligibility.eligible === true &&
          Boolean(eligibility.actionToken) &&
          catalogReady));
    const hasDurableHistory =
      state.fixHistory.length > 0 ||
      (state.selectedIssueSource === "monitoring" &&
        Boolean(state.selectedDrivingAnnotationID));
    const showFixSection = canResume || canRecord || hasDurableHistory;
    elements.fixAttemptsSection.hidden = !showFixSection;
    const serverReportedIneligible =
      state.fixEligibilityStatus === "ready" &&
      eligibility.eligible !== true &&
      !canResume;
    elements.recordFixAttempt.hidden =
      !canRecord ||
      monitoringCurrentIssueUnavailable ||
      serverReportedIneligible;
    elements.recordFixAttempt.disabled = !canRecord;
    elements.recordFixAttempt.textContent = canResume
      ? "Resume fix attempt"
      : "Record fix attempt";
    if (canResume && monitoringCurrentIssueUnavailable) {
      elements.fixEligibilityStatus.textContent =
        "An unresolved submission is retained, but it cannot resume without a current issue projection and current eligibility.";
    } else if (canResume) {
      elements.fixEligibilityStatus.textContent =
        "An unresolved submission is retained. Resume it with the same private retry key or explicitly abandon it.";
    } else if (state.fixEligibilityStatus === "loading") {
      elements.fixEligibilityStatus.textContent =
        "Checking whether this current issue can anchor a declaration…";
    } else if (state.fixEligibilityStatus === "unavailable") {
      elements.fixEligibilityStatus.textContent =
        "Current eligibility is unavailable because this is retained attempt history without a current issue projection.";
    } else if (state.fixEligibilityStatus === "error") {
      elements.fixEligibilityStatus.textContent =
        "Eligibility could not be confirmed. Matching-session evidence remains available.";
    } else if (canRecord) {
      const expiresAt = parseDate(eligibility.expiresAt);
      elements.fixEligibilityStatus.textContent = expiresAt
        ? `Eligible for an optional declaration. Confirmation expires ${formatRelativeTime(expiresAt)}.`
        : "Eligible for an optional declaration.";
    } else if (state.fixEligibilityStatus === "ready") {
      elements.fixEligibilityStatus.textContent = catalogReady
        ? fixEligibilityMessages[eligibility.reason] ||
          "This issue is not eligible for a fix-attempt declaration."
        : "The required change-category catalog is unavailable.";
    } else {
      elements.fixEligibilityStatus.textContent =
        "Eligibility has not been checked.";
    }
    elements.fixMonitoringDetailStatus.hidden =
      !state.fixHistoryError && !state.fixHistoryStale;
    elements.fixMonitoringDetailStatus.textContent =
      state.fixHistoryError ||
      (state.fixHistoryStale
        ? "Attempt monitoring may be stale. Other issue detail remains available."
        : "");
    elements.fixMonitoringDetailStatus.dataset.tone =
      state.fixHistoryError ? "error" : "status";
    renderFixActionStatus();
    renderFixHistory();
  }

  function renderFixActionStatus() {
    const safeTone = ["status", "success", "error", "pending"].includes(
      state.fixActionTone,
    )
      ? state.fixActionTone
      : "status";
    elements.fixActionStatus.hidden = !state.fixActionMessage;
    elements.fixActionStatus.textContent = state.fixActionMessage;
    elements.fixActionStatus.dataset.tone = safeTone;
    elements.fixActionStatus.setAttribute(
      "role",
      safeTone === "error" ? "alert" : "status",
    );
    elements.fixActionStatus.setAttribute(
      "aria-live",
      safeTone === "error" ? "assertive" : "polite",
    );
  }

  function renderFixHistory() {
    focusRegistry.fixHistoryRows.clear();
    focusRegistry.fixRetractionTriggers.clear();
    focusRegistry.fixObservationRows.clear();
    const fragment = document.createDocumentFragment();
    state.fixHistory.forEach((annotation) => {
      fragment.append(createFixHistoryRow(annotation));
    });
    elements.fixHistoryList.replaceChildren(fragment);
    elements.fixHistoryLoading.hidden =
      state.fixHistoryStatus !== "loading";
    elements.fixHistoryEmpty.hidden =
      state.fixHistoryStatus !== "ready" || state.fixHistory.length !== 0;
    if (state.fixHistoryStatus === "error" && state.fixHistory.length === 0) {
      elements.fixHistoryList.append(
        createElement(
          "p",
          "overview-empty",
          "Attempt monitoring could not be loaded. Matching sessions and fix actions remain available.",
        ),
      );
    }
    elements.fixHistoryPagination.hidden = !state.fixHistoryHasMore;
    elements.fixHistoryLoadMore.disabled =
      state.fixHistoryStatus === "loading-more";
    elements.fixHistoryPageStatus.textContent = state.fixHistoryHasMore
      ? `Showing ${state.fixHistory.length}; more monitored attempts are available.`
      : `Showing ${state.fixHistory.length} monitored ${
          state.fixHistory.length === 1 ? "attempt" : "attempts"
        }.`;
    const evaluatedAt = parseDate(state.fixEvidenceEvaluatedAt);
    elements.fixEvidenceEvaluated.textContent = evaluatedAt
      ? `Monitoring evidence retention evaluated ${formatRelativeTime(evaluatedAt)}. Payload-free monitoring metadata and positive observations may remain after cited evidence is pruned.`
      : state.fixHistory.length
        ? "Monitoring evidence retention evaluation time is unavailable. Payload-free monitoring metadata and positive observations may remain after cited evidence is pruned."
        : "";
  }

  function createFixHistoryRow(annotation) {
    const annotationID = readText(annotation && annotation.annotation_id);
    const row = createElement("article", "fix-history-card");
    row.tabIndex = -1;
    if (annotationID) {
      focusRegistry.fixHistoryRows.set(annotationID, row);
    }
    const category = fixChangeCatalogEntry(annotation.change_kind);
    const annotationState = normalizeFixAnnotationState(annotation.state);
    const monitoringState = normalizeFixRecurrenceState(
      annotation.fix_recurrence_state,
    );
    const monitoringEntry = fixMonitoringStateEntry(monitoringState);
    const header = createElement("div", "fix-history-header");
    const title = createElement("div");
    title.append(
      createElement("strong", "", category.label),
      createElement(
        "small",
        "",
        monitoringSubjectCatalog(annotation).title,
      ),
    );
    const badges = createElement("div", "fix-history-badges");
    const stateBadge = createElement(
      "span",
      "fix-state-badge",
      annotationState === "retracted"
        ? "Retracted declaration"
        : annotationState === "active"
          ? "Active declaration"
          : "Declaration state unavailable",
    );
    stateBadge.dataset.tone = annotationState;
    const evidenceStatus = normalizeFixEvidenceStatus(
      annotation.anchor_evidence_currently_retained,
    );
    const evidenceBadge = createElement(
      "span",
      "fix-evidence-badge",
      fixEvidenceStatusLabel(evidenceStatus),
    );
    evidenceBadge.dataset.tone = evidenceStatus;
    badges.append(stateBadge, evidenceBadge);
    header.append(title, badges);
    const status = createElement("div", "fix-monitoring-state");
    status.dataset.tone = monitoringEntry.tone;
    status.append(
      createElement("strong", "", monitoringEntry.title),
      createElement("p", "", monitoringEntry.detail),
    );
    const metadata = createElement("dl", "fix-history-metadata");
    appendFixMetadata(
      metadata,
      "Recorded",
      formatFullDate(parseDate(annotation.recorded_at)) || "Time unavailable",
    );
    appendFixMetadata(
      metadata,
      "Monitoring began",
      formatFullDate(parseDate(annotation.monitor_from)) || "Time unavailable",
    );
    appendFixMetadata(
      metadata,
      "Latest matching evidence",
      formatFullDate(parseDate(annotation.last_recurrence_observed_at)) ||
        "No matching-evidence time available",
    );
    appendFixMetadata(
      metadata,
      "Anchor evidence",
      fixEvidenceStatusExplanation(evidenceStatus),
    );
    appendOptionalFixCount(
      metadata,
      "Matching observations",
      annotation.fix_recurrence_count,
      annotation.count_is_lower_bound === true,
    );
    appendOptionalFixCount(
      metadata,
      "Original session",
      annotation.same_anchor_session_observation_count,
      false,
    );
    appendOptionalFixCount(
      metadata,
      "Other sessions",
      annotation.other_session_observation_count,
      false,
    );
    appendMonitoringCoverage(metadata, annotation.coverage);
    appendRecurrenceEvidence(metadata, annotation.recurrence_evidence);
    if (annotationState === "retracted") {
      appendFixMetadata(
        metadata,
        "Retraction",
        `${fixRetractionReasonLabel(annotation.retraction_reason)} · ${
          formatFullDate(parseDate(annotation.retracted_at)) ||
          "Time unavailable"
        }`,
      );
    }
    row.append(
      header,
      createElement("p", "fix-history-description", category.description),
      status,
      metadata,
    );
    if (
      toOptionalCount(annotation.same_anchor_session_observation_count) > 0
    ) {
      row.append(
        createElement(
          "p",
          "fix-monitoring-caveat",
          "This may be continuation within the original session.",
        ),
      );
    }
    if (
      monitoringState === "matching_evidence_observed" &&
      annotation.analysis_complete === false
    ) {
      row.append(
        createElement(
          "p",
          "fix-monitoring-caveat",
          "The matching-evidence count is a lower bound because monitoring coverage is incomplete.",
        ),
      );
    }
    if (
      annotationState === "active" &&
      annotation.future_comparison_available === false
    ) {
      row.append(
        createElement(
          "p",
          "fix-monitoring-caveat",
          futureComparisonUnavailableText(
            annotation.future_comparison_unavailable_reason,
          ),
        ),
      );
    }
    if (
      annotationState === "retracted" &&
      toOptionalCount(annotation.historical_matching_evidence_count) > 0
    ) {
      row.append(
        createElement(
          "p",
          "fix-monitoring-caveat",
          "Matching evidence was recorded before this declaration was retracted.",
        ),
      );
    }
    const actions = createElement("div", "fix-history-actions");
    const observationCount = toOptionalCount(
      annotation.fix_recurrence_count,
    );
    const observationCursor = readCursor(annotation.observation_view_cursor);
    if (
      observationCount !== null &&
      observationCount > 0 &&
      observationCursor &&
      annotationID
    ) {
      const observations = createElement(
        "button",
        "secondary-button",
        "Show observations",
      );
      observations.type = "button";
      observations.setAttribute(
        "aria-expanded",
        String(Boolean(fixObservationPages.get(annotationID)?.expanded)),
      );
      observations.textContent = fixObservationPages.get(annotationID)?.expanded
        ? "Hide observations"
        : "Show observations";
      observations.addEventListener("click", () => {
        toggleFixRecurrenceObservations(annotation, observations, row);
      });
      actions.append(observations);
    }
    if (
      annotationState === "active" &&
      monitoringState !== "unknown" &&
      annotationID
    ) {
      const retract = createElement(
        "button",
        "secondary-button",
        "Retract",
      );
      retract.type = "button";
      retract.addEventListener("click", () => {
        openFixRetractionDialog(annotationID);
      });
      focusRegistry.fixRetractionTriggers.set(annotationID, retract);
      actions.append(retract);
    }
    if (actions.childElementCount) row.append(actions);
    if (annotationState === "retracted") {
      row.append(
        createElement(
          "p",
          "fix-retraction-note",
          "Preserved in history and excluded from active monitoring.",
        ),
      );
    } else if (annotationState === "unknown") {
      row.append(
        createElement(
          "p",
          "fix-retraction-note",
          "Declaration state is unavailable. Retraction is disabled until Local reports an exact active state.",
        ),
      );
    }
    renderFixObservationSection(annotation, row);
    return row;
  }

  function appendOptionalFixCount(list, label, value, lowerBound) {
    const count = toOptionalCount(value);
    appendFixMetadata(
      list,
      label,
      count === null
        ? "Unavailable"
        : `${lowerBound ? "At least " : ""}${formatNumber(count)}`,
    );
  }

  function appendMonitoringCoverage(list, value) {
    if (!isRecord(value)) {
      appendFixMetadata(list, "Monitoring coverage", "Unavailable");
      return;
    }
    const counts = [
      ["current", toOptionalCount(value.comparable_current)],
      ["pending", toOptionalCount(value.comparable_pending)],
      ["failed", toOptionalCount(value.comparable_failed)],
      ["partial", toOptionalCount(value.comparable_truncated)],
    ];
    if (counts.some((entry) => entry[1] === null)) {
      appendFixMetadata(list, "Monitoring coverage", "Unavailable");
      return;
    }
    appendFixMetadata(
      list,
      "Monitoring coverage",
      counts
        .map(([label, count]) => `${formatNumber(count)} ${label}`)
        .join(" · "),
    );
    appendFixMetadata(
      list,
      "Analysis through",
      formatFullDate(parseDate(value.analysis_through)) ||
        "Comparable analysis time unavailable",
    );
  }

  function appendRecurrenceEvidence(list, value) {
    if (!isRecord(value)) {
      appendFixMetadata(list, "Observation evidence", "Unavailable");
      return;
    }
    const counts = [
      ["retained", toOptionalCount(value.available)],
      ["partial", toOptionalCount(value.partial)],
      ["pruned", toOptionalCount(value.pruned)],
      ["unknown", toOptionalCount(value.unknown)],
    ];
    if (counts.some((entry) => entry[1] === null)) {
      appendFixMetadata(list, "Observation evidence", "Unavailable");
      return;
    }
    appendFixMetadata(
      list,
      "Observation evidence",
      counts
        .map(([label, count]) => `${formatNumber(count)} ${label}`)
        .join(" · "),
    );
  }

  function toggleFixRecurrenceObservations(annotation, button) {
    const annotationID = readText(annotation.annotation_id);
    if (!annotationID) return;
    const page = getFixObservationPage(annotationID);
    if (page.expanded) {
      page.expanded = false;
      renderFixHistory();
      restoreLogicalFocus(
        { type: "fix-history", annotationID },
        button,
      );
      return;
    }
    page.expanded = true;
    if (page.status === "idle") {
      void loadFixRecurrences(annotation, false);
    } else {
      renderFixHistory();
    }
  }

  function getFixObservationPage(annotationID) {
    let page = fixObservationPages.get(annotationID);
    if (!page) {
      page = {
        data: [],
        nextCursor: "",
        hasMore: false,
        status: "idle",
        error: "",
        expired: false,
        expanded: false,
        requestGeneration: 0,
      };
      fixObservationPages.set(annotationID, page);
    }
    return page;
  }

  async function loadFixRecurrences(annotation, append) {
    const issueID = state.selectedIssueID;
    const annotationID = readText(annotation.annotation_id);
    const viewCursor = readCursor(annotation.observation_view_cursor);
    if (!issueID || !annotationID) return false;
    const pageState = getFixObservationPage(annotationID);
    if (
      append &&
      (!pageState.hasMore || !pageState.nextCursor)
    ) {
      return false;
    }
    const generation = ++pageState.requestGeneration;
    pageState.status = append ? "loading-more" : "loading";
    pageState.error = "";
    pageState.expired = false;
    renderFixHistory();
    const parameters = new URLSearchParams();
    if (append) {
      parameters.set("cursor", pageState.nextCursor);
    } else {
      if (!viewCursor) {
        pageState.status = "error";
        pageState.error =
          "Observation history is unavailable for this attempt snapshot.";
        renderFixHistory();
        return false;
      }
      parameters.set("observation_view_cursor", viewCursor);
      parameters.set("limit", String(pageLimits.fixRecurrences.page));
    }
    try {
      const response = await apiGet(
        `/v1/issues/${encodeURIComponent(issueID)}/fixes/${encodeURIComponent(annotationID)}/recurrences?${parameters.toString()}`,
      );
      requireFixMonitoringSchema(response);
      if (
        generation !== pageState.requestGeneration ||
        issueID !== state.selectedIssueID
      ) {
        return false;
      }
      if (
        readText(response.issue_id) !== issueID ||
        readText(response.annotation_id) !== annotationID
      ) {
        throw new Error("Local API returned mismatched observation history.");
      }
      const rows = Array.isArray(response.data)
        ? response.data.filter(
            (entry) =>
              isRecord(entry) &&
              readText(entry.recurrence_id) &&
              readText(entry.session_id),
          )
        : [];
      pageState.data =
        append && pageState.nextCursor
          ? deduplicateByID(
              pageState.data.concat(rows),
              "recurrence_id",
            )
          : deduplicateByID(rows, "recurrence_id");
      pageState.nextCursor = readCursor(response.next_cursor);
      if (response.has_more === true && !pageState.nextCursor) {
        throw new Error(
          "Local API returned observation pagination without a cursor.",
        );
      }
      pageState.hasMore = Boolean(pageState.nextCursor);
      pageState.status = "ready";
      pageState.error = "";
      pageState.expired = false;
      renderFixHistory();
      return true;
    } catch (error) {
      if (
        generation !== pageState.requestGeneration ||
        issueID !== state.selectedIssueID
      ) {
        return false;
      }
      pageState.status = "error";
      pageState.expired = isCursorExpired(error);
      pageState.error = pageState.expired
        ? "This observation view expired. Reload attempt monitoring to inspect current retained evidence."
        : "Observation history could not be loaded. Attempt monitoring remains available.";
      renderFixHistory();
      return false;
    }
  }

  function renderFixObservationSection(annotation, row) {
    const annotationID = readText(annotation.annotation_id);
    const page = fixObservationPages.get(annotationID);
    if (!page || !page.expanded) return;
    const section = createElement("section", "fix-observation-section");
    section.setAttribute("aria-label", "Matching observations");
    if (page.status === "loading" && !page.data.length) {
      section.append(
        createElement("p", "overview-empty", "Loading observations…"),
      );
    }
    if (page.error) {
      const error = createElement(
        "p",
        "monitoring-status",
        page.error,
      );
      error.dataset.tone = "error";
      error.setAttribute("role", "status");
      section.append(error);
      const retry = createElement(
        "button",
        "secondary-button",
        page.expired
          ? "Reload attempt monitoring"
          : "Retry observations",
      );
      retry.type = "button";
      retry.addEventListener("click", () => {
        if (page.expired) {
          fixObservationPages.delete(annotationID);
          void loadFixMonitoringDetail(
            state.selectedIssueID,
            false,
            "",
            true,
            annotationID,
          );
          return;
        }
        void loadFixRecurrences(annotation, false);
      });
      section.append(retry);
    }
    page.data.forEach((observation) => {
      section.append(createFixObservationRow(observation));
    });
    if (page.status === "ready" && !page.data.length) {
      section.append(
        createElement(
          "p",
          "overview-empty",
          "No bounded observation rows were returned for this attempt snapshot.",
        ),
      );
    }
    if (page.hasMore) {
      const more = createElement(
        "button",
        "secondary-button",
        "Load more observations",
      );
      more.type = "button";
      more.disabled = page.status === "loading-more";
      more.addEventListener("click", () => {
        void loadFixRecurrences(annotation, true);
      });
      section.append(more);
    }
    row.append(section);
  }

  function createFixObservationRow(observation) {
    const recurrenceID = readText(observation.recurrence_id);
    const sessionID = readText(observation.session_id);
    const article = createElement("article", "fix-observation-card");
    article.tabIndex = -1;
    if (recurrenceID) {
      focusRegistry.fixObservationRows.set(recurrenceID, article);
    }
    const header = createElement("div", "fix-observation-header");
    header.append(
      createElement("strong", "", "Exact matching observation"),
      createElement(
        "span",
        "fix-evidence-badge",
        fixEvidenceStatusLabel(
          normalizeFixEvidenceStatus(
            observation.evidence_currently_retained,
          ),
        ),
      ),
    );
    const metadata = createElement("dl", "fix-history-metadata");
    appendFixMetadata(
      metadata,
      "First matching evidence",
      formatFullDate(parseDate(observation.first_qualifying_event_at)) ||
        "Time unavailable",
    );
    appendFixMetadata(
      metadata,
      "Last matching evidence",
      formatFullDate(parseDate(observation.last_qualifying_event_at)) ||
        "Time unavailable",
    );
    appendFixMetadata(
      metadata,
      "Session",
      compactID(sessionID) || "Unavailable",
    );
    appendFixMetadata(
      metadata,
      "Detector",
      detectorVersionLabel(observation),
    );
    appendFixMetadata(
      metadata,
      "Detection evidence",
      evidenceCompletenessLabel(observation.evidence_complete),
    );
    appendFixMetadata(
      metadata,
      "Observation recorded",
      formatFullDate(parseDate(observation.observed_at)) ||
        "Time unavailable",
    );
    appendObservationEvidenceMetadata(metadata, observation);
    article.append(header, metadata);
    if (observation.same_session_as_anchor === true) {
      article.append(
        createElement(
          "p",
          "fix-monitoring-caveat",
          "This may be continuation within the original session.",
        ),
      );
    }
    const retainedEventIDs = recurrenceEventIDs(observation);
    if (sessionID) {
      const actions = createElement("div", "fix-history-actions");
      let evidence = null;
      if (retainedEventIDs.length) {
        const inspect = createElement(
          "button",
          "secondary-button",
          "Inspect retained evidence",
        );
        inspect.type = "button";
        inspect.setAttribute("aria-expanded", "false");
        evidence = createElement("div", "occurrence-evidence");
        evidence.hidden = true;
        inspect.addEventListener("click", () => {
          void loadRecurrenceEvidence(
            sessionID,
            retainedEventIDs,
            evidence,
            inspect,
          );
        });
        actions.append(inspect);
      }
      const open = createElement(
        "button",
        "primary-button",
        "Open full session",
      );
      open.type = "button";
      if (recurrenceID) {
        focusRegistry.fixObservationRows.set(recurrenceID, open);
      }
      open.addEventListener("click", () => {
        state.sessionReturnFocus = {
          type: "fix-observation",
          recurrenceID,
        };
        state.sessionReturnView = "attention";
        openSession(sessionID, null);
      });
      actions.append(open);
      article.append(actions);
      if (evidence) article.append(evidence);
    }
    return article;
  }

  function appendObservationEvidenceMetadata(metadata, observation) {
    const status = normalizeFixEvidenceStatus(
      observation.evidence_currently_retained,
    );
    if (status === "unknown") {
      appendFixMetadata(
        metadata,
        "Evidence retention",
        "Evidence status unknown; retained and missing counts are unavailable.",
      );
      return;
    }
    const retained = toOptionalCount(observation.retained_event_count);
    const missing = toOptionalCount(observation.missing_event_count);
    const total = toOptionalCount(observation.qualifying_citation_count);
    const truncation =
      observation.evidence_truncated === true
        ? " · returned IDs are truncated to 50"
        : "";
    appendFixMetadata(
      metadata,
      "Evidence retention",
      retained === null || missing === null || total === null
        ? fixEvidenceStatusLabel(status)
        : `${formatNumber(retained)} retained · ${formatNumber(
            missing,
          )} missing · ${formatNumber(total)} cited${truncation}`,
    );
  }

  function recurrenceEventIDs(observation) {
    if (
      normalizeFixEvidenceStatus(
        observation.evidence_currently_retained,
      ) === "unknown"
    ) {
      return [];
    }
    const values = Array.isArray(observation.retained_event_ids)
      ? observation.retained_event_ids
      : [];
    return values
      .map(readText)
      .filter((value) => canonicalUUIDv7Pattern.test(value))
      .slice(0, 50);
  }

  async function loadRecurrenceEvidence(
    sessionID,
    eventIDs,
    container,
    button,
  ) {
    if (container.dataset.loaded === "true") {
      container.hidden = !container.hidden;
      button.setAttribute("aria-expanded", String(!container.hidden));
      return;
    }
    container.hidden = false;
    button.disabled = true;
    button.setAttribute("aria-expanded", "true");
    container.replaceChildren(
      createElement("p", "overview-empty", "Loading retained cited events…"),
    );
    const parameters = new URLSearchParams();
    eventIDs.slice(0, 50).forEach((eventID) => {
      parameters.append("event_id", eventID);
    });
    try {
      const response = await apiGet(
        `/v1/sessions/${encodeURIComponent(sessionID)}/events/lookup?${parameters.toString()}`,
      );
      renderOccurrenceEvidence(container, response, eventIDs.length);
      container.dataset.loaded = "true";
    } catch {
      container.replaceChildren(
        createElement(
          "p",
          "overview-empty",
          "Retained cited events could not be loaded. The observation remains recorded.",
        ),
      );
    } finally {
      button.disabled = false;
    }
  }

  function appendFixMetadata(list, label, value) {
    const item = createElement("div");
    item.append(
      createElement("dt", "", label),
      createElement("dd", "", value),
    );
    list.append(item);
  }

  function fixChangeCatalogEntry(value) {
    return (
      fixChangeCatalog.find((entry) => entry.value === readText(value)) || {
        value: "",
        label: "Category unavailable",
        description:
          "The recorded category is outside this browser catalog version.",
      }
    );
  }

  function normalizeFixAnnotationState(value) {
    const stateValue = readText(value);
    return ["active", "retracted"].includes(stateValue)
      ? stateValue
      : "unknown";
  }

  function normalizeFixRecurrenceState(value) {
    const recurrenceState = readText(value);
    return Object.prototype.hasOwnProperty.call(
      fixMonitoringCatalog,
      recurrenceState,
    )
      ? recurrenceState
      : "unknown";
  }

  function fixMonitoringStateEntry(value) {
    return fixMonitoringCatalog[normalizeFixRecurrenceState(value)];
  }

  function normalizeFixEvidenceStatus(value) {
    const status = readText(value);
    return ["available", "partial", "pruned", "unknown"].includes(status)
      ? status
      : "unknown";
  }

  function futureComparisonUnavailableText(value) {
    const messages = {
      scope_unavailable:
        "Future comparison is unavailable because an exact private scope was not retained.",
      source_positive_only:
        "Future comparison can report later positive matches, but this source cannot support a no-match conclusion.",
      capability_unavailable:
        "Future comparison capability is unavailable for this detector snapshot.",
      baseline_time_unavailable:
        "Future comparison is unavailable because the exact baseline time was not retained.",
      fingerprint_version_unsupported:
        "Future comparison is unavailable for this fingerprint version.",
    };
    return (
      messages[readText(value)] ||
      "Future comparison capability is unavailable for this attempt."
    );
  }

  function fixEvidenceStatusLabel(status) {
    const labels = {
      available: "Evidence retained",
      partial: "Evidence partly retained",
      pruned: "Evidence pruned",
      unknown: "Evidence status unknown",
    };
    return labels[status] || labels.unknown;
  }

  function fixEvidenceStatusExplanation(status) {
    const explanations = {
      available: "All cited anchor events are currently retained.",
      partial: "Some cited anchor events are currently retained.",
      pruned: "No cited anchor events are currently retained.",
      unknown: "Anchor evidence retention could not be determined.",
    };
    return explanations[status] || explanations.unknown;
  }

  function fixEvidenceHistoryText(annotation, status) {
    const explanation = fixEvidenceStatusExplanation(status);
    const evaluatedAt = parseDate(
      annotation && annotation.browser_evidence_evaluated_at,
    );
    return evaluatedAt
      ? `${explanation} Evaluated ${formatFullDate(evaluatedAt)}.`
      : `${explanation} Evaluation time unavailable.`;
  }

  function fixRetractionReasonLabel(value) {
    const entry = fixRetractionCatalog.find(
      (candidate) => candidate.value === readText(value),
    );
    return entry ? entry.label : "Reason unavailable";
  }

  function renderFixDialogChoices() {
    const categoryFragment = document.createDocumentFragment();
    fixChangeCatalog.forEach((entry) => {
      categoryFragment.append(
        createDialogChoice(
          "fix-change-kind",
          `fix-change-${entry.value}`,
          entry.value,
          entry.label,
          entry.description,
        ),
      );
    });
    elements.fixCategoryOptions.replaceChildren(categoryFragment);
    const retractionFragment = document.createDocumentFragment();
    fixRetractionCatalog.forEach((entry) => {
      retractionFragment.append(
        createDialogChoice(
          "fix-retraction-reason",
          `fix-retraction-${entry.value}`,
          entry.value,
          entry.label,
          "",
        ),
      );
    });
    elements.fixRetractionOptions.replaceChildren(retractionFragment);
  }

  function createDialogChoice(name, id, value, label, description) {
    const wrapper = createElement("label", "choice-option");
    const input = createElement("input");
    input.type = "radio";
    input.name = name;
    input.id = id;
    input.value = value;
    const copy = createElement("span");
    copy.append(createElement("strong", "", label));
    if (description) copy.append(createElement("small", "", description));
    wrapper.append(input, copy);
    return wrapper;
  }

  function openFixAttemptDialog() {
    const issueID = state.selectedIssueID;
    const eligibility = state.fixEligibility;
    const retainedDraft = fixDrafts.get(issueID);
    const canResume =
      Boolean(retainedDraft) &&
      retainedDraft.unresolved === true &&
      Boolean(retainedDraft.idempotencyKey) &&
      Boolean(retainedDraft.actionToken);
    if (
      !issueID ||
      (!canResume &&
        (state.fixEligibilityStatus !== "ready" ||
          !eligibility ||
          eligibility.eligible !== true ||
          !eligibility.actionToken ||
          eligibility.changeCatalogVersion !== "fix-change.v1"))
    ) {
      return;
    }
    state.dialogReturnFocus = { type: "fix-trigger", issueID };
    state.activeModal = "fix-attempt";
    hideModalAlert(elements.fixAttemptAlert);
    syncFixDraftDialog();
    const draft = getFixDraft(issueID);
    const selected = elements.fixCategoryOptions.querySelector(
      'input[name="fix-change-kind"]:checked',
    );
    const first = elements.fixCategoryOptions.querySelector(
      'input[name="fix-change-kind"]',
    );
    openModalLayer(
      elements.fixAttemptModalLayer,
      elements.fixAttemptDialog,
      draft.attempted ? elements.fixAttemptConfirm : selected || first,
    );
  }

  function closeFixAttemptDialog(restoreFocus) {
    if (
      restoreFocus &&
      state.activeModal === "fix-attempt" &&
      state.modalSubmitting
    ) {
      return;
    }
    closeModalLayer(
      "fix-attempt",
      elements.fixAttemptModalLayer,
      restoreFocus,
    );
    hideModalAlert(elements.fixAttemptAlert);
  }

  function getFixDraft(issueID) {
    let draft = fixDrafts.get(issueID);
    if (!draft) {
      draft = {
        changeKind: "",
        idempotencyKey: "",
        actionToken: "",
        attempted: false,
        unresolved: false,
        pending: false,
      };
      fixDrafts.set(issueID, draft);
    }
    return draft;
  }

  function syncFixDraftDialog() {
    const draft = getFixDraft(state.selectedIssueID);
    const locked = draft.attempted || draft.pending;
    elements.fixCategoryOptions
      .querySelectorAll('input[name="fix-change-kind"]')
      .forEach((input) => {
        input.checked = input.value === draft.changeKind;
        input.disabled = locked;
      });
    elements.fixDraftRecovery.hidden = !draft.unresolved;
    elements.fixAttemptClose.disabled = draft.pending;
    elements.fixAttemptCancel.disabled = draft.pending;
    elements.abandonFixDraft.disabled = draft.pending;
    elements.fixAttemptConfirm.disabled =
      draft.pending || !fixChangeCatalogEntry(draft.changeKind).value;
    elements.fixAttemptConfirm.textContent = draft.pending
      ? "Recording…"
      : draft.attempted
        ? "Retry same declaration"
        : "Record declaration";
  }

  function updateFixDraftChoice(target) {
    if (
      !(target instanceof HTMLInputElement) ||
      target.name !== "fix-change-kind"
    ) {
      return;
    }
    const draft = getFixDraft(state.selectedIssueID);
    if (draft.attempted || draft.pending) {
      syncFixDraftDialog();
      return;
    }
    const entry = fixChangeCatalogEntry(target.value);
    draft.changeKind = entry.value;
    hideModalAlert(elements.fixAttemptAlert);
    syncFixDraftDialog();
  }

  function abandonFixDraft() {
    const draft = getFixDraft(state.selectedIssueID);
    if (draft.pending) return;
    draft.idempotencyKey = "";
    draft.actionToken = "";
    draft.attempted = false;
    draft.unresolved = false;
    syncFixDraftDialog();
    showModalAlert(
      elements.fixAttemptAlert,
      "Unresolved submission abandoned. Confirming again will create a new private retry key.",
      false,
    );
  }

  async function submitFixAttempt() {
    const issueID = state.selectedIssueID;
    const eligibility = state.fixEligibility;
    const draft = getFixDraft(issueID);
    if (!fixChangeCatalogEntry(draft.changeKind).value) {
      showModalAlert(
        elements.fixAttemptAlert,
        "Select one primary change category before recording.",
        false,
      );
      focusCurrentElement(
        elements.fixCategoryOptions.querySelector(
          'input[name="fix-change-kind"]',
        ),
      );
      return;
    }
    if (draft.pending) {
      return;
    }
    if (!draft.idempotencyKey) {
      if (
        !eligibility ||
        eligibility.eligible !== true ||
        !eligibility.actionToken ||
        eligibility.changeCatalogVersion !== "fix-change.v1"
      ) {
        showModalAlert(
          elements.fixAttemptAlert,
          "Current eligibility could not be confirmed. Nothing was recorded.",
          true,
        );
        return;
      }
      draft.idempotencyKey = createUUIDv4();
      if (!draft.idempotencyKey) {
        showModalAlert(
          elements.fixAttemptAlert,
          "A secure UUIDv4 retry key could not be created. Nothing was recorded.",
          true,
        );
        return;
      }
      draft.actionToken = eligibility.actionToken;
    }
    draft.attempted = true;
    draft.unresolved = true;
    draft.pending = true;
    state.modalSubmitting = true;
    hideModalAlert(elements.fixAttemptAlert);
    syncFixDraftDialog();
    try {
      const response = await apiMutation(
        `/v1/issues/${encodeURIComponent(issueID)}/fixes`,
        {
          action_token: draft.actionToken,
          change_kind: draft.changeKind,
        },
        draft.idempotencyKey,
        "record-fix-attempt.v1",
      );
      requireFixSchema(response);
      const annotation = isRecord(response.data) ? response.data : null;
      const annotationID = readText(
        annotation && annotation.annotation_id,
      );
      if (
        !annotationID ||
        readText(annotation.issue_id) !== issueID ||
        readText(annotation.change_kind) !== draft.changeKind ||
        readText(annotation.change_catalog_version) !== "fix-change.v1"
      ) {
        throw new Error("Local API returned an invalid fix-attempt response.");
      }
      const replayed = response.replayed === true;
      fixDrafts.delete(issueID);
      state.fixActionMessage = replayed
        ? "Previously recorded attempt restored; no duplicate created."
        : "Fix attempt declaration recorded · Not verified by Belay.";
      state.fixActionTone = "success";
      draft.pending = false;
      state.modalSubmitting = false;
      closeFixAttemptDialog(false);
      await reloadFixMonitoringAndFocus(issueID, annotationID);
    } catch (error) {
      draft.pending = false;
      state.modalSubmitting = false;
      if (isCursorExpired(error)) {
        draft.idempotencyKey = "";
        draft.actionToken = "";
        draft.attempted = false;
        draft.unresolved = false;
        state.fixEligibility = null;
        state.fixEligibilityStatus = "idle";
        closeFixAttemptDialog(false);
        showAttentionNotice(
          "The fix-attempt confirmation expired. Your category was preserved, but nothing was resubmitted.",
          "status",
          7000,
        );
        await refreshAttentionAfterExpiry();
        return;
      }
      syncFixDraftDialog();
      showModalAlert(
        elements.fixAttemptAlert,
        fixMutationErrorMessage(error, "record"),
        true,
      );
    }
  }

  function openFixRetractionDialog(annotationID) {
    const annotation = state.fixHistory.find(
      (entry) => readText(entry && entry.annotation_id) === annotationID,
    );
    if (
      !state.selectedIssueID ||
      !annotationID ||
      normalizeFixAnnotationState(annotation && annotation.state) !== "active" ||
      normalizeFixRecurrenceState(
        annotation && annotation.fix_recurrence_state,
      ) === "unknown"
    ) {
      return;
    }
    state.activeRetractionAnnotationID = annotationID;
    state.dialogReturnFocus = {
      type: "fix-retraction",
      annotationID,
    };
    state.activeModal = "fix-retraction";
    hideModalAlert(elements.fixRetractionAlert);
    syncFixRetractionDialog();
    const draft = getFixRetractionDraft(
      state.selectedIssueID,
      annotationID,
    );
    const selected = elements.fixRetractionOptions.querySelector(
      'input[name="fix-retraction-reason"]:checked',
    );
    const first = elements.fixRetractionOptions.querySelector(
      'input[name="fix-retraction-reason"]',
    );
    openModalLayer(
      elements.fixRetractionModalLayer,
      elements.fixRetractionDialog,
      draft.attempted ? elements.fixRetractionConfirm : selected || first,
    );
  }

  function closeFixRetractionDialog(restoreFocus) {
    if (
      restoreFocus &&
      state.activeModal === "fix-retraction" &&
      state.modalSubmitting
    ) {
      return;
    }
    closeModalLayer(
      "fix-retraction",
      elements.fixRetractionModalLayer,
      restoreFocus,
    );
    hideModalAlert(elements.fixRetractionAlert);
    state.activeRetractionAnnotationID = "";
  }

  function fixRetractionDraftKey(issueID, annotationID) {
    return `${issueID}\u0000${annotationID}`;
  }

  function getFixRetractionDraft(issueID, annotationID) {
    const key = fixRetractionDraftKey(issueID, annotationID);
    let draft = fixRetractionDrafts.get(key);
    if (!draft) {
      draft = {
        reason: "",
        idempotencyKey: "",
        attempted: false,
        unresolved: false,
        pending: false,
      };
      fixRetractionDrafts.set(key, draft);
    }
    return draft;
  }

  function syncFixRetractionDialog() {
    const draft = getFixRetractionDraft(
      state.selectedIssueID,
      state.activeRetractionAnnotationID,
    );
    const locked = draft.attempted || draft.pending;
    elements.fixRetractionOptions
      .querySelectorAll('input[name="fix-retraction-reason"]')
      .forEach((input) => {
        input.checked = input.value === draft.reason;
        input.disabled = locked;
      });
    elements.fixRetractionRecovery.hidden = !draft.unresolved;
    elements.fixRetractionClose.disabled = draft.pending;
    elements.fixRetractionCancel.disabled = draft.pending;
    elements.abandonFixRetraction.disabled = draft.pending;
    elements.fixRetractionConfirm.disabled =
      draft.pending || !fixRetractionReasonEntry(draft.reason);
    elements.fixRetractionConfirm.textContent = draft.pending
      ? "Retracting…"
      : draft.attempted
        ? "Retry same retraction"
        : "Retract declaration";
  }

  function updateFixRetractionChoice(target) {
    if (
      !(target instanceof HTMLInputElement) ||
      target.name !== "fix-retraction-reason"
    ) {
      return;
    }
    const draft = getFixRetractionDraft(
      state.selectedIssueID,
      state.activeRetractionAnnotationID,
    );
    if (draft.attempted || draft.pending) {
      syncFixRetractionDialog();
      return;
    }
    const entry = fixRetractionReasonEntry(target.value);
    draft.reason = entry ? entry.value : "";
    hideModalAlert(elements.fixRetractionAlert);
    syncFixRetractionDialog();
  }

  function abandonFixRetraction() {
    const draft = getFixRetractionDraft(
      state.selectedIssueID,
      state.activeRetractionAnnotationID,
    );
    if (draft.pending) return;
    draft.idempotencyKey = "";
    draft.attempted = false;
    draft.unresolved = false;
    syncFixRetractionDialog();
    showModalAlert(
      elements.fixRetractionAlert,
      "Unresolved retraction abandoned. Confirming again will create a new private retry key.",
      false,
    );
  }

  function fixRetractionReasonEntry(value) {
    return (
      fixRetractionCatalog.find(
        (entry) => entry.value === readText(value),
      ) || null
    );
  }

  async function submitFixRetraction() {
    const issueID = state.selectedIssueID;
    const annotationID = state.activeRetractionAnnotationID;
    const draft = getFixRetractionDraft(issueID, annotationID);
    if (!fixRetractionReasonEntry(draft.reason)) {
      showModalAlert(
        elements.fixRetractionAlert,
        "Select one retraction reason before confirming.",
        false,
      );
      focusCurrentElement(
        elements.fixRetractionOptions.querySelector(
          'input[name="fix-retraction-reason"]',
        ),
      );
      return;
    }
    if (!issueID || !annotationID || draft.pending) return;
    if (!draft.idempotencyKey) {
      draft.idempotencyKey = createUUIDv4();
      if (!draft.idempotencyKey) {
        showModalAlert(
          elements.fixRetractionAlert,
          "A secure UUIDv4 retry key could not be created. Nothing was retracted.",
          true,
        );
        return;
      }
    }
    draft.attempted = true;
    draft.unresolved = true;
    draft.pending = true;
    state.modalSubmitting = true;
    hideModalAlert(elements.fixRetractionAlert);
    syncFixRetractionDialog();
    try {
      const response = await apiMutation(
        `/v1/issues/${encodeURIComponent(issueID)}/fixes/${encodeURIComponent(annotationID)}/retractions`,
        { reason: draft.reason },
        draft.idempotencyKey,
        "retract-fix-attempt.v1",
      );
      requireFixSchema(response);
      if (
        !isRecord(response.data) ||
        readText(response.data.annotation_id) !== annotationID ||
        readText(response.data.issue_id) !== issueID
      ) {
        throw new Error("Local API returned an invalid retraction response.");
      }
      const replayed = response.replayed === true;
      fixRetractionDrafts.delete(
        fixRetractionDraftKey(issueID, annotationID),
      );
      state.fixActionMessage = replayed
        ? "Previously recorded retraction restored; no duplicate created."
        : "Retraction declaration recorded. The original attempt remains in history.";
      state.fixActionTone = "success";
      draft.pending = false;
      state.modalSubmitting = false;
      closeFixRetractionDialog(false);
      await reloadFixMonitoringAndFocus(issueID, annotationID);
    } catch (error) {
      draft.pending = false;
      state.modalSubmitting = false;
      syncFixRetractionDialog();
      showModalAlert(
        elements.fixRetractionAlert,
        fixMutationErrorMessage(error, "retract"),
        true,
      );
    }
  }

  async function reloadFixMonitoringAndFocus(issueID, annotationID) {
    invalidateFixMonitoringSnapshotsAfterMutation();
    void loadFixMonitoring(false);
    const loaded = await loadFixMonitoringDetail(
      issueID,
      false,
      "",
      true,
      annotationID,
    );
    if (
      !loaded ||
      issueID !== state.selectedIssueID ||
      !focusCurrentElement(focusRegistry.fixHistoryRows.get(annotationID))
    ) {
      focusCurrentElement(elements.fixActionStatus);
    }
  }

  function invalidateFixMonitoringSnapshotsAfterMutation() {
    state.fixMonitoring.requestGeneration += 1;
    state.fixMonitoring.data = [];
    state.fixMonitoring.nextCursor = "";
    state.fixMonitoring.hasMore = false;
    state.fixMonitoring.viewCursor = "";
    state.fixMonitoring.evidenceEvaluatedAt = "";
    state.fixMonitoring.status = "idle";
    state.fixMonitoring.error = null;
    focusRegistry.monitoringCards.clear();

    state.fixHistoryRequestGeneration += 1;
    state.fixHistoryNextCursor = "";
    state.fixHistoryHasMore = false;
    state.fixHistoryViewCursor = "";
    state.fixHistory = state.fixHistory.map((annotation) => ({
      ...annotation,
      observation_view_cursor: "",
    }));
    fixObservationPages.forEach((page) => {
      page.requestGeneration += 1;
    });
    fixObservationPages.clear();
    focusRegistry.fixObservationRows.clear();
    renderFixMonitoring();
    renderFixAttempts();
  }

  function fixMutationErrorMessage(error, action) {
    const verb = action === "retract" ? "retraction" : "declaration";
    if (error instanceof LocalMutationTimeoutError) {
      return `The ${verb} timed out before Local confirmed it. Retry uses the same private key; do not start another submission.`;
    }
    if (!(error instanceof LocalAPIError)) {
      return `The ${verb} was not confirmed. Retry uses the same private key; do not start another submission.`;
    }
    if (error.status === 403) {
      return "Belay rejected this browser write. Reload Local and retry from the same listener page.";
    }
    if (error.status === 409) {
      if (error.problemType === "belay.local/already-retracted") {
        return "This declaration was already retracted by another request. Reload history before continuing.";
      }
      if (error.problemType === "belay.local/idempotency-conflict") {
        return `This ${verb} conflicts with its retained retry key. Abandon the unresolved submission before choosing different input.`;
      }
      if (error.problemType === "belay.local/ineligible-fix-annotation") {
        return "The issue is no longer eligible for this declaration. Refresh Attention before continuing.";
      }
      return `The ${verb} conflicted with current Local state. Its retry key remains retained.`;
    }
    if (error.status === 400) {
      return `The ${verb} request was rejected. Review the selected category or reason before retrying.`;
    }
    if (error.status >= 500) {
      return `Local could not confirm the ${verb}. Retry uses the same private key.`;
    }
    return `The ${verb} was not confirmed. Its retry key remains retained.`;
  }

  function requireFixSchema(response) {
    if (!isRecord(response) || response.schema_version !== "belay.fix.v1") {
      throw new Error("Local API returned an unsupported fix schema.");
    }
  }

  function requireFixMonitoringSchema(response) {
    if (
      !isRecord(response) ||
      response.schema_version !== "belay.fix-monitoring.v1"
    ) {
      throw new Error(
        "Local API returned an unsupported fix-monitoring schema.",
      );
    }
  }

  function showFixActionStatus(message, tone) {
    state.fixActionMessage = message;
    state.fixActionTone = tone;
    renderFixActionStatus();
  }

  function showModalAlert(element, message, moveFocus) {
    element.textContent = message;
    element.hidden = false;
    if (moveFocus) focusCurrentElement(element);
  }

  function hideModalAlert(element) {
    element.hidden = true;
    element.textContent = "";
  }

  function openModalLayer(layer, dialog, initialFocus) {
    elements.appShell.inert = true;
    elements.appShell.setAttribute("aria-hidden", "true");
    layer.hidden = false;
    document.body.classList.add("is-modal-open");
    focusCurrentElement(initialFocus) || focusCurrentElement(dialog);
  }

  function closeModalLayer(kind, layer, restoreFocus) {
    if (state.activeModal !== kind && layer.hidden) return;
    const returnFocus = state.dialogReturnFocus;
    layer.hidden = true;
    if (state.activeModal === kind) state.activeModal = "";
    if (!state.activeModal) {
      elements.appShell.inert = false;
      elements.appShell.removeAttribute("aria-hidden");
      document.body.classList.remove("is-modal-open");
      applyPaneAccessibility();
    }
    state.dialogReturnFocus = null;
    if (restoreFocus) {
      restoreLogicalFocus(returnFocus, elements.issueDetailHeading);
    }
  }

  function handleModalKeydown(event) {
    if (!state.activeModal) return;
    if (event.key === "Escape") {
      if (state.modalSubmitting) return;
      event.preventDefault();
      if (state.activeModal === "fix-attempt") {
        closeFixAttemptDialog(true);
      } else {
        closeFixRetractionDialog(true);
      }
      return;
    }
    if (event.key !== "Tab") return;
    const dialog =
      state.activeModal === "fix-attempt"
        ? elements.fixAttemptDialog
        : elements.fixRetractionDialog;
    const focusable = Array.from(
      dialog.querySelectorAll(
        'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    ).filter(canReceiveFocus);
    if (!focusable.length) {
      event.preventDefault();
      focusCurrentElement(dialog);
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      focusCurrentElement(last);
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      focusCurrentElement(first);
    }
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
    events.forEach((event) =>
      fragment.append(
        createLookupEvent(event, state.selectedIssueEvidenceContext),
      ),
    );
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

  function createLookupEvent(event, evidenceContext = "") {
    const observation = isRecord(event.observation) ? event.observation : {};
    const descriptor = eventDescriptor(observation, evidenceContext);
    const row = createElement("article", "lookup-event");
    row.append(
      createElement(
        "strong",
        "",
        descriptor.label,
      ),
      createElement(
        "time",
        "",
        formatFullDate(parseDate(event.occurred_at)) || "Time unavailable",
      ),
      createElement(
        "p",
        "",
        descriptor.detail ||
          readText(observation.type) ||
          "Retained event",
      ),
    );
    return row;
  }

  function closeIssueDetail(restoreFocus = true, forceList = false) {
    const returnFocus = state.issueReturnFocus;
    const returnToFamily =
      !forceList &&
      state.selectedIssueSource === "family" &&
      Boolean(state.selectedFamily);
    state.occurrenceRequestGeneration += 1;
    state.fixEligibilityRequestGeneration += 1;
    state.fixHistoryRequestGeneration += 1;
    closeFixAttemptDialog(false);
    closeFixRetractionDialog(false);
    state.selectedIssueID = "";
    state.selectedIssueKind = "";
    state.selectedIssueSource = "";
    state.selectedIssueFamilyID = "";
    state.selectedIssueEvidenceContext = "";
    state.selectedDrivingAnnotationID = "";
    state.selectedIssue = null;
    state.selectedIssueCatalog = null;
    state.selectedGlobalAnalysisCoverage = null;
    state.selectedIssueViewCursor = "";
    state.occurrences = [];
    state.occurrenceNextCursor = "";
    state.occurrenceHasMore = false;
    state.occurrenceStatus = "idle";
    resetIssueEvidencePreview();
    resetFixIssueState();
    state.issueReturnFocus = null;
    elements.issueDetail.hidden = true;
    elements.familyDetail.hidden = !returnToFamily;
    elements.attentionWelcome.hidden = returnToFamily || Boolean(state.selectedFamily);
    document.body.classList.toggle(
      "is-attention-detail-open",
      returnToFamily || Boolean(state.selectedFamily),
    );
    renderIssueBucket(state.issues);
    renderIssueBucket(state.evidenceGaps);
    renderFixMonitoring();
    if (returnToFamily) renderAttentionFamilyDetail();
    applyPaneAccessibility();
    if (restoreFocus) {
      restoreLogicalFocus(
        returnFocus,
        returnToFamily ? elements.familyDetailHeading : elements.navAttention,
      );
    }
  }

  function closeFamilyDetail(restoreFocus = true) {
    const returnFocus = state.familyReturnFocus;
    if (state.selectedIssueID) closeIssueDetail(false, true);
    clearAttentionFamilyDetailState();
    elements.familyDetail.hidden = true;
    elements.issueDetail.hidden = true;
    elements.attentionWelcome.hidden = false;
    document.body.classList.remove("is-attention-detail-open");
    renderIssueBucket(state.issues);
    applyPaneAccessibility();
    if (restoreFocus) {
      restoreLogicalFocus(returnFocus, elements.navAttention);
    }
  }

  function clearAttentionFamilyDetailState() {
    state.familyMemberRequestGeneration += 1;
    state.selectedFamily = null;
    state.selectedFamilyViewCursor = "";
    state.familyMembers = [];
    state.familyMemberNextCursor = "";
    state.familyMemberHasMore = false;
    state.familyMemberStatus = "idle";
    state.familyMemberError = "";
    state.familyReturnFocus = null;
    focusRegistry.familyMembers.clear();
    elements.familyMemberList.replaceChildren();
    elements.familyMembersLoading.hidden = true;
    elements.familyMembersEmpty.hidden = true;
    elements.familyMembersPagination.hidden = true;
    elements.familyMembersPageStatus.textContent = "";
  }

  async function refreshAttentionAfterExpiry() {
    if (state.attentionExpiryRefresh) return;
    state.attentionExpiryRefresh = true;
    clearExpiredIssueSnapshotState();
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

  function clearExpiredIssueSnapshotState() {
    resetIssueBucket(state.issues);
    resetIssueBucket(state.evidenceGaps);
    resetFixMonitoringBucket(false);
    clearExpiredFixDraftState();
    closeIssueDetail(false, true);
    clearAttentionFamilyDetailState();
    elements.familyDetail.hidden = true;
    elements.attentionWelcome.hidden = false;
    document.body.classList.remove("is-attention-detail-open");
    applyPaneAccessibility();
  }

  function clearExpiredFixDraftState() {
    fixDrafts.forEach((draft) => {
      draft.idempotencyKey = "";
      draft.actionToken = "";
      draft.attempted = false;
      draft.unresolved = false;
      draft.pending = false;
    });
    fixRetractionDrafts.forEach((draft) => {
      draft.idempotencyKey = "";
      draft.attempted = false;
      draft.unresolved = false;
      draft.pending = false;
    });
    state.modalSubmitting = false;
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
        action: "Inspect retained evidence",
      }
    );
  }

  function issueDisplayCatalog(issue) {
    const base = issueCatalogEntry(issue && issue.title_code);
    if (
      readText(issue && issue.title_code) !== "issue.numbat_finding" ||
      readText(issue && issue.origin).toLowerCase() !== "numbat"
    ) {
      return base;
    }
    const sourceSignalCode = safeSourceSignalCode(
      issue && issue.source_signal_code,
    );
    return (
      sourceSignalCatalog[`numbat/${sourceSignalCode}`] ||
      issueCatalog["issue.numbat_finding"]
    );
  }

  function safeSourceSignalCode(value) {
    const code = readText(value);
    return /^[a-z0-9][a-z0-9_.-]{0,63}$/.test(code) ? code : "";
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

  function issueScopeDisclosure(issue, catalogCaveat = "") {
    const statements = [
      readText(catalogCaveat),
      issueCaveat(issue),
      "This explanation is fixed catalog content, not a generated diagnosis or remediation recommendation.",
    ].filter(Boolean);
    return Array.from(new Set(statements)).join(" ");
  }

  function expectedIssueSelection(kind) {
    const attentionKind = kind === "evidence_gap" ? "evidence_gap" : "issue";
    const experimental = state.issueFilters.experimental
      ? "include"
      : "stable";
    return {
      attention_kind: attentionKind,
      experimental,
      includes_evidence_gaps: attentionKind === "evidence_gap",
      includes_experimental: experimental !== "stable",
    };
  }

  function readIssueSelection(value, kind) {
    const expected = expectedIssueSelection(kind);
    if (!isRecord(value)) return expected;
    const selection = {
      attention_kind: readText(value.attention_kind).toLowerCase(),
      experimental: readText(value.experimental).toLowerCase(),
      includes_evidence_gaps: value.includes_evidence_gaps === true,
      includes_experimental: value.includes_experimental === true,
    };
    if (
      selection.attention_kind !== expected.attention_kind ||
      selection.experimental !== expected.experimental ||
      selection.includes_evidence_gaps !==
        expected.includes_evidence_gaps ||
      selection.includes_experimental !==
        expected.includes_experimental
    ) {
      throw new Error(
        "Local API returned issue results for a different normalized selection.",
      );
    }
    return selection;
  }

  function requireCursorPage(response, currentCursor, label) {
    const nextCursor = readCursor(response && response.next_cursor);
    const hasMore = response && response.has_more === true;
    if (hasMore !== Boolean(nextCursor)) {
      throw new Error(
        `Local API returned inconsistent ${label} pagination metadata.`,
      );
    }
    if (currentCursor && nextCursor === currentCursor) {
      throw new Error(
        `Local API returned a non-advancing ${label} cursor.`,
      );
    }
    return { nextCursor, hasMore };
  }

  function readIssueCatalog(value, issue, previous) {
    if (!isRecord(value)) {
      return previous || fallbackIssueCatalog(issue);
    }
    const catalog = {
      catalog_version: readText(value.catalog_version),
      catalog_status: readText(value.catalog_status).toLowerCase(),
      title_code: readText(value.title_code),
      display_title: readText(value.display_title),
      observation_statement: readText(value.observation_statement),
      caveat: readText(value.caveat),
      next_evidence_action: readText(value.next_evidence_action),
      source_signal_code: safeSourceSignalCode(value.source_signal_code) || null,
      source_signal_catalog_version: readText(
        value.source_signal_catalog_version,
      ),
      source_signal_catalog_status: readText(
        value.source_signal_catalog_status,
      ).toLowerCase(),
    };
    const issueTitleCode = readText(issue && issue.title_code);
    const actions = new Set([
      "inspect_cited_events",
      "inspect_matching_sessions",
      "inspect_verification_events",
      "review_agent_permissions",
    ]);
    if (
      catalog.catalog_version !== "belay.issue-explanations.v1" ||
      !["known", "unknown"].includes(catalog.catalog_status) ||
      !catalog.title_code ||
      catalog.title_code !== issueTitleCode ||
      !catalog.display_title ||
      !catalog.observation_statement ||
      !catalog.caveat ||
      !actions.has(catalog.next_evidence_action) ||
      catalog.source_signal_catalog_version !== "belay.source-signals.v1" ||
      !["known", "unknown", "not_applicable"].includes(
        catalog.source_signal_catalog_status,
      ) ||
      (catalog.source_signal_catalog_status === "known" &&
        !catalog.source_signal_code)
    ) {
      throw new Error("Local API returned invalid issue catalog metadata.");
    }
    return catalog;
  }

  function fallbackIssueCatalog(issue) {
    const titleCode = readText(issue && issue.title_code);
    const display = issueDisplayCatalog(issue);
    const sourceSignalCode = safeSourceSignalCode(
      issue && issue.source_signal_code,
    );
    return {
      catalog_version: "browser-fallback",
      catalog_status:
        display.title === "Detected issue" ? "unknown" : "known",
      title_code: titleCode,
      display_title: display.title,
      observation_statement: display.explanation,
      caveat: display.caveat || "",
      next_evidence_action: "inspect_cited_events",
      source_signal_code: sourceSignalCode || null,
      source_signal_catalog_version: "belay.source-signals.v1",
      source_signal_catalog_status:
        titleCode === "issue.numbat_finding"
          ? sourceSignalCode === "tamper.guardrails_off"
            ? "known"
            : "unknown"
          : "not_applicable",
    };
  }

  function readGlobalAnalysisCoverage(value, fallback) {
    return isRecord(value)
      ? value
      : isRecord(fallback)
        ? fallback
        : null;
  }

  function selectedIssueListCoverage() {
    const bucket =
      state.selectedIssueKind === "evidence_gap"
        ? state.evidenceGaps
        : state.issues;
    return bucket.analysis;
  }

  function globalCoverageQualifier(coverage) {
    if (!isRecord(coverage) || coverage.complete === true) return "";
    return `Global retained-session analysis is incomplete: ${incompleteCoverageText(
      coverage,
    )}.`;
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
    renderSessionStory(overview);
    renderFindingList();
  }

  function renderSessionStory(overview) {
    const attention = [];
    if (overview.failures > 0) {
      attention.push(
        `${formatNumber(overview.failures)} explicit ${
          overview.failures === 1 ? "failure" : "failures"
        }`,
      );
    }
    const deniedPermissions = state.events.filter(isDeniedPermissionEvent).length;
    if (deniedPermissions > 0) {
      attention.push(
        `${formatNumber(deniedPermissions)} denied ${
          deniedPermissions === 1 ? "permission" : "permissions"
        } in loaded events`,
      );
    }
    if (overview.findings > 0) {
      attention.push(
        `${formatNumber(overview.findings)} configured-rule ${
          overview.findings === 1 ? "finding" : "findings"
        }`,
      );
    }
    const sessionOutcome = normalizeOutcome(
      state.selectedSessionDetail && state.selectedSessionDetail.outcome,
    );
    if (sessionOutcome === "interrupted") {
      attention.push("The source explicitly reported an interrupted session");
    }
    elements.needsAttentionSummary.replaceChildren(
      createStoryList(
        attention,
        "No explicit failure, denied permission, interruption, or configured-rule finding is shown in the available overview.",
      ),
    );

    const work = [
      ["command", overview.commands],
      ["tool", overview.tools],
      ["file operation", overview.files],
      ["network indicator", overview.network],
      ["permission event", overview.permissions],
    ]
      .filter(([, count]) => count > 0)
      .map(
        ([label, count]) =>
          `${formatNumber(count)} ${label}${count === 1 ? "" : "s"}`,
      );
    elements.observedWorkSummary.replaceChildren(
      createStoryList(work, "No supported work metadata was reported."),
    );

    const highlights = selectSessionHighlights(state.events);
    const fragment = document.createDocumentFragment();
    highlights.forEach((event) => {
      const observation = isRecord(event.observation) ? event.observation : {};
      const descriptor = eventDescriptor(observation);
      const item = createElement("li", "session-highlight");
      const heading = createElement("div");
      heading.append(
        createElement("strong", "", descriptor.label),
        createElement(
          "time",
          "",
          formatClockTime(parseDate(event.occurred_at)) || "Time unavailable",
        ),
      );
      item.append(heading);
      if (descriptor.detail) {
        item.append(createElement("p", "", descriptor.detail));
      }
      fragment.append(item);
    });
    if (!highlights.length) {
      fragment.append(
        createElement(
          "li",
          "overview-empty",
          state.events.length
            ? "No launch-priority highlight was present in the loaded events."
            : "Timeline events are still loading.",
        ),
      );
    }
    elements.sessionHighlights.replaceChildren(fragment);
  }

  function createStoryList(values, emptyMessage) {
    if (!values.length) {
      return createElement("p", "overview-empty", emptyMessage);
    }
    const list = createElement("ul", "story-list");
    values.forEach((value) => list.append(createElement("li", "", value)));
    return list;
  }

  function selectSessionHighlights(events) {
    const cited = citedFindingMap();
    const candidates = events
      .map((event, index) => ({
        event,
        index,
        priority: sessionHighlightPriority(event, cited),
      }))
      .filter((candidate) => candidate.priority < 100);
    candidates.sort(
      (left, right) =>
        left.priority - right.priority || left.index - right.index,
    );
    return candidates
      .slice(0, 5)
      .sort((left, right) => left.index - right.index)
      .map((candidate) => candidate.event);
  }

  function sessionHighlightPriority(event, cited) {
    const observation = isRecord(event.observation) ? event.observation : {};
    const type = readText(observation.type).toLowerCase();
    const outcome = normalizeOutcome(observation.outcome);
    if (["failed", "interrupted"].includes(outcome)) return 0;
    if (isDeniedPermissionEvent(event)) return 1;
    if (cited.has(readText(event.event_id))) return 2;
    if (type === "command.result") return 3;
    if (type === "command.exec") return 4;
    if (["file.write", "file.create", "file.delete"].includes(type)) return 5;
    if (type === "tool.call") return 6;
    return 100;
  }

  function isDeniedPermissionEvent(event) {
    const observation = isRecord(event.observation) ? event.observation : {};
    const type = readText(observation.type).toLowerCase();
    if (!type.startsWith("permission.")) return false;
    if (normalizeOutcome(observation.outcome) === "failed") return true;
    const details = isRecord(observation.details) ? observation.details : {};
    return [details.decision, details.approval_decision]
      .map((value) => readText(value).toLowerCase())
      .some((value) => ["denied", "rejected"].includes(value));
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
        const display = findingDisplayCatalog(finding);
        const heading = createElement("div");
        const badge = createElement("span", "finding-badge", readableLabel(severity));
        badge.dataset.tone = severityTone(severity);
        heading.append(
          badge,
          createElement(
            "strong",
            "",
            display.title,
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
        const sourceRuleID = safeSourceSignalCode(finding.rule_id);
        if (sourceRuleID) {
          const details = createElement("details", "evidence-details");
          details.append(
            createElement("summary", "", "Technical details"),
            createElement("p", "", `Source rule ID · ${sourceRuleID}`),
          );
          item.append(details);
        }
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

  function findingDisplayCatalog(finding) {
    const sourceSignalCode = safeSourceSignalCode(finding && finding.rule_id);
    return (
      sourceSignalCatalog[`numbat/${sourceSignalCode}`] || {
        title: "Configured Numbat rule finding",
      }
    );
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
        const display = findingDisplayCatalog(finding);
        findingRefs.append(
          createElement(
            "span",
            "",
            `Cited by ${display.title}`,
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
    const coverage = isRecord(event.coverage) ? event.coverage : {};
    const redaction = isRecord(event.redaction) ? event.redaction : {};
    const historical = isRecord(event.historical) ? event.historical : {};
    const resource = isRecord(observation.resource) ? observation.resource : {};
    const descriptor = eventDescriptor(observation);
    const details = createElement("details", "evidence-details");
    details.append(createElement("summary", "", "Evidence"));
    const list = createElement("dl");
    appendEvidence(list, "Event ID", event.event_id);
    appendEvidence(list, "Occurred", formatFullDate(parseDate(event.occurred_at)));
    appendEvidence(list, "Action", descriptor.label);
    appendEvidence(list, "Event type", observation.type);
    appendEvidence(list, "Outcome", observation.outcome);
    appendEvidence(list, "Safe summary", observation.summary);
    appendEvidence(list, "Resource kind", resource.kind);
    appendEvidence(list, "Resource name", resource.name);
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
    if (observation.duration_ms !== undefined && observation.duration_ms !== null) {
      appendEvidence(list, "Duration (ms)", observation.duration_ms);
    }
    appendEvidence(list, "Fields removed", redaction.fields_removed);
    appendEvidence(list, "Secrets removed", redaction.secrets_removed);
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

  function eventDescriptor(observation, evidenceContext = "") {
    const type = readText(observation.type).toLocaleLowerCase();
    const resource = isRecord(observation.resource) ? observation.resource : {};
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
      "config.agent": "Agent configuration observed",
      "network.indicator": "Observed network target",
      "prompt.user": "User input lifecycle",
      "message.assistant": "Assistant lifecycle",
      "reasoning.start": "Reasoning lifecycle started",
      "reasoning.end": "Reasoning lifecycle ended",
    };
    const mappedGuardrail =
      type === "config.agent" && evidenceContext === "mapped-guardrail";
    const label = mappedGuardrail
      ? "Agent guardrail configuration observed"
      : labels[type] || readableLabel(observation.action, "Activity");
    const detail =
      mappedGuardrail
        ? "This configuration event supported the safety-confirmation signal. Belay does not retain the configuration value or body."
        : type === "config.agent"
          ? resourceName || "Configuration metadata was reported by the source."
        : type.startsWith("command.")
          ? summary || resourceName
          : resourceName || summary;
    return { label, detail };
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

  class LocalMutationTimeoutError extends Error {
    constructor() {
      super(
        "The Local mutation deadline elapsed before a response was confirmed.",
      );
      this.name = "LocalMutationTimeoutError";
    }
  }

  async function apiGet(path, signal) {
    return apiRequest(path, "GET", null, null, null, signal);
  }

  async function apiMutation(path, body, idempotencyKey, intent) {
    if (!isCanonicalUUIDv4(idempotencyKey)) {
      throw new Error("A canonical UUIDv4 retry key is required.");
    }
    if (
      !["record-fix-attempt.v1", "retract-fix-attempt.v1"].includes(intent)
    ) {
      throw new Error("The Local write intent is invalid.");
    }
    const controller = new AbortController();
    let deadlineReached = false;
    const deadline = globalThis.setTimeout(() => {
      deadlineReached = true;
      controller.abort();
    }, mutationRequestDeadlineMilliseconds);
    try {
      return await apiRequest(
        path,
        "POST",
        body,
        idempotencyKey,
        intent,
        controller.signal,
      );
    } catch (error) {
      if (deadlineReached || controller.signal.aborted) {
        throw new LocalMutationTimeoutError();
      }
      throw error;
    } finally {
      globalThis.clearTimeout(deadline);
    }
  }

  async function apiRequest(
    path,
    method,
    body,
    idempotencyKey,
    intent,
    signal,
  ) {
    if (!state.token) {
      throw new Error(
        "No launch token was provided. Open the URL supplied by Belay Local.",
      );
    }
    const headers = {
      Accept: "application/json",
      Authorization: `Bearer ${state.token}`,
    };
    const request = {
      method,
      cache: "no-store",
      credentials: "omit",
      headers,
    };
    if (signal) request.signal = signal;
    if (method === "POST") {
      headers["Content-Type"] = "application/json";
      headers["Idempotency-Key"] = idempotencyKey;
      headers["X-Belay-Intent"] = intent;
      request.body = JSON.stringify(body);
    }
    const response = await fetch(`${state.apiBase}${path}`, {
      ...request,
    });
    throwIfMutationAborted(signal);
    const responseBody = await readResponseBody(response);
    throwIfMutationAborted(signal);
    if (!response.ok) {
      const detail =
        isRecord(responseBody) && readText(responseBody.detail)
          ? readText(responseBody.detail)
          : `Local API returned ${response.status}.`;
      throw new LocalAPIError(
        detail,
        response.status,
        isRecord(responseBody) ? readText(responseBody.type) : "",
      );
    }
    if (!isRecord(responseBody)) {
      throw new Error("Local API returned an invalid response.");
    }
    return responseBody;
  }

  function throwIfMutationAborted(signal) {
    if (signal && signal.aborted) {
      throw new LocalMutationTimeoutError();
    }
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

  function createUUIDv4() {
    if (
      !globalThis.crypto ||
      typeof globalThis.crypto.randomUUID !== "function"
    ) {
      return "";
    }
    const value = globalThis.crypto.randomUUID();
    return isCanonicalUUIDv4(value) ? value : "";
  }

  function isCanonicalUUIDv4(value) {
    return /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      readText(value),
    );
  }

  function toFiniteNumber(value) {
    const number = Number(value);
    return Number.isFinite(number) && number >= 0 ? number : 0;
  }

  function toOptionalCount(value) {
    if (
      value === null ||
      value === undefined ||
      value === "" ||
      typeof value === "boolean"
    ) {
      return null;
    }
    const number = Number(value);
    return Number.isInteger(number) && number >= 0 ? number : null;
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
