package localhttp

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestAttentionBrowserShellContract(t *testing.T) {
	index := readBrowserAsset(t, "assets/index.html")

	for _, required := range []string{
		`<nav class="primary-nav" aria-label="Belay Local views">`,
		`id="nav-attention"`,
		`aria-current="page"`,
		`id="nav-sessions"`,
		`id="attention-view"`,
		`id="sessions-view" hidden`,
		`id="issue-list"`,
		`id="issues-pagination"`,
		`id="evidence-gap-list"`,
		`id="evidence-gaps-pagination"`,
		`id="issue-detail-heading" tabindex="-1"`,
		`id="occurrence-list"`,
		`id="occurrences-pagination"`,
		`id="findings-pagination"`,
		`id="findings-load-more"`,
		`id="attention-refresh-notice"`,
		`aria-live="polite"`,
		`Include experimental signals`,
		`Precision is still being validated.`,
	} {
		if !strings.Contains(index, required) {
			t.Errorf("Attention browser shell is missing %q", required)
		}
	}

	idPattern := regexp.MustCompile(`\sid="([^"]+)"`)
	seen := make(map[string]struct{})
	for _, match := range idPattern.FindAllStringSubmatch(index, -1) {
		if _, duplicate := seen[match[1]]; duplicate {
			t.Errorf("browser shell contains duplicate id %q", match[1])
		}
		seen[match[1]] = struct{}{}
	}
}

func TestAttentionBrowserFrozenContract(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")

	catalog := map[string][]string{
		"issue.explicit_command_failure": {
			"Command failed",
			"The source explicitly reported a failed command result.",
		},
		"issue.repeated_command_attempts": {
			"Command repeatedly attempted",
			"The same private command signature was observed multiple times in one bounded interval.",
		},
		"issue.explicit_permission_denial": {
			"Permission denied",
			"The source explicitly reported a denied permission event.",
		},
		"issue.verification_not_observed": {
			"Verification evidence not observed",
			"A supported live session ended without the required verification evidence.",
		},
		"issue.unresolved_verification_failure_at_completion": {
			"Verification still failed at session end",
			"A verification command explicitly failed and no later successful verification was observed before session end.",
		},
		"issue.numbat_finding": {
			"Upstream Numbat finding",
			"A configured Numbat rule reported retained evidence.",
		},
	}
	for code, values := range catalog {
		if !strings.Contains(app, `"`+code+`"`) {
			t.Errorf("fixed issue catalog is missing %q", code)
		}
		for _, value := range values {
			if !strings.Contains(app, value) {
				t.Errorf("fixed issue catalog entry %q is missing %q", code, value)
			}
		}
	}

	for _, required := range []string{
		`issues: createAttentionFamilyBucket()`,
		`evidenceGaps: createIssueBucket("evidence_gap")`,
		"`/v1/attention-families?${new URLSearchParams({ cursor }).toString()}`",
		"`/v1/attention-families/${encodeURIComponent(",
		`view_cursor: state.selectedFamilyViewCursor`,
		`readText(family.kind) === "exact_issue"`,
		`kind === "mapped_upstream"`,
		`attention_kind: kind`,
		`experimental: state.issueFilters.experimental ? "include" : "stable"`,
		`bucket.viewCursor = viewCursor;`,
		`parameters.set("view_cursor", viewCursor)`,
		"`/v1/issues/${encodeURIComponent(issueID)}/occurrences?${parameters.toString()}`",
		`eventIDs.forEach((eventID) => parameters.append("event_id", eventID));`,
		"`/v1/sessions/${encodeURIComponent(sessionID)}/events/lookup?${parameters.toString()}`",
		`occurrenceEventIDs(occurrence).slice(0, 50)`,
		`error.status === 410`,
		`error.problemType === "belay.local/cursor-expired"`,
		`title: "Detected issue"`,
		`return /^[a-z0-9_]{1,64}$/.test(code)`,
		`return "Evidence completeness is unavailable.";`,
		`: "unknown";`,
		`pending: "Prior retained result while reanalysis is pending."`,
		`failed: "Prior retained result; the latest analysis failed."`,
		`truncated: "Partial analysis; additional signals may be absent."`,
		`"Analysis status is unavailable; result freshness and completeness are uncertain."`,
		`return "Analysis status unavailable";`,
		`return "Session count unavailable";`,
		`"No supported signals match these filters"`,
		`"No supported signals were reported in completed retained analysis"`,
		`"No supported signals are available from completed analysis"`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("Attention browser data contract is missing %q", required)
		}
	}
}

func TestAttentionBrowserAccessibilityAndSafeRendering(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	styles := readBrowserAsset(t, "assets/styles.css")

	for _, required := range []string{
		`element.inert = !visible;`,
		`element.setAttribute("aria-hidden", "true");`,
		`const focusRegistry = {`,
		`focusRegistry.issueCards.get(reference.key)`,
		`focusRegistry.occurrenceActions.get(reference.key)`,
		`focusRegistry.sessionCards.get(reference.sessionID)`,
		`clearIssueFocusRegistry(bucket.kind);`,
		`focusRegistry.occurrenceActions.clear();`,
		`focusRegistry.sessionCards.clear();`,
		`state.issueReturnFocus = {`,
		`key: issueFocusKey(state.selectedIssueKind, issueID),`,
		`state.sessionReturnFocus = { type: "session", sessionID };`,
		`state.sessionReturnFocus = { type: "occurrence", key: focusKey };`,
		`!element.isConnected`,
		`element.closest("[hidden]")`,
		`element.closest('[aria-hidden="true"]')`,
		`if (current.inert === true) return false;`,
		`focusCurrentElement(resolveFocusReference(reference))`,
		`restoreLogicalFocus(returnFocus, elements.navAttention);`,
		`inspect.setAttribute("aria-expanded", "false");`,
		`severity.dataset.tone = severityTone(issue.severity);`,
		`["critical", "high", "medium", "low", "info"].includes(severity)`,
		`["current", "pending", "failed", "truncated"].includes(status)`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("Attention accessibility/safety contract is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"issueOriginButton",
		"sessionOriginButton",
		"An immutable upstream Numbat rule emitted a finding.",
	} {
		if strings.Contains(app, forbidden) {
			t.Errorf("browser asset retains stale contract %q", forbidden)
		}
	}
	if got := strings.Count(app, ".focus();"); got != 1 ||
		!strings.Contains(app, "element.focus();") {
		t.Errorf("focus must be centralized behind current-DOM checks; source has %d direct focus calls", got)
	}
	for _, forbidden := range []string{
		"innerHTML",
		"outerHTML",
		"insertAdjacentHTML",
		"document.write",
		"eval(",
	} {
		if strings.Contains(app, forbidden) {
			t.Errorf("browser asset contains unsafe rendering primitive %q", forbidden)
		}
	}
	for _, required := range []string{
		`.primary-nav-button[aria-current="page"]`,
		`body.is-attention-detail-open .attention-detail-panel`,
		`@media (prefers-reduced-motion: reduce)`,
		`@media (max-height: 700px)`,
		`height: 100dvh;`,
		`overscroll-behavior: contain;`,
		`min-height: 44px;`,
	} {
		if !strings.Contains(styles, required) {
			t.Errorf("Attention styles are missing %q", required)
		}
	}
	for _, block := range []struct {
		start string
		end   string
	}{
		{".primary-nav-button {", ".primary-nav-button:hover {"},
		{".issue-card-main {", `.issue-card-main[aria-pressed="true"] {`},
		{".fingerprint-panel button,", ".primary-button {"},
		{".pagination-bar button {", ".pagination-bar button:hover {"},
	} {
		section := browserSourceBlock(t, styles, block.start, block.end)
		if !strings.Contains(section, "min-height: 44px;") {
			t.Errorf("primary touch target %q is smaller than 44px", block.start)
		}
	}
}

func TestAttentionRefreshClosesStaleDetailAndConfirmsCurrentChain(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	refresh := browserSourceBlock(
		t,
		app,
		"  async function refreshAttention(preserveSelection, suppressFailureNotice = false) {",
		"  async function loadIssuePagesForSelection(",
	)

	for _, required := range []string{
		`closeIssueDetail(false);`,
		`const [issuesReady, gapsReady, monitoringReady] = await Promise.all([`,
		`const refreshGeneration = ++state.attentionRefreshGeneration;`,
		`if (refreshGeneration !== state.attentionRefreshGeneration) return false;`,
		`if (!issuesReady || !gapsReady) {`,
		`const chainReady = await loadIssuePagesForSelection(`,
		`const summary = bucket.data.find(`,
		`if (!summary) {`,
		`const detailReady = await selectIssue(summary, selectedKind, false);`,
		`if (!detailReady) {`,
		`selected detail is hidden until current data confirms it.`,
	} {
		if !strings.Contains(refresh, required) {
			t.Errorf("current-chain refresh contract is missing %q", required)
		}
	}
	if strings.Contains(refresh, "Promise.allSettled") {
		t.Error("Attention refresh cannot treat failed required reads as success")
	}
	closeIndex := strings.Index(refresh, "closeIssueDetail(false);")
	readIndex := strings.Index(refresh, "await Promise.all([")
	selectIndex := strings.Index(refresh, "await selectIssue(")
	missingIndex := strings.Index(refresh, "if (!summary) {")
	if closeIndex < 0 || readIndex < 0 || closeIndex > readIndex {
		t.Error("selected detail is not closed before fresh list reads")
	}
	if missingIndex < 0 || selectIndex < 0 || missingIndex > selectIndex {
		t.Error("selected detail can be retained without confirming list visibility")
	}
}

func TestCursorRefreshNoticeWaitsForRequiredReads(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	refresh := browserSourceBlock(
		t,
		app,
		"  async function refreshAttentionAfterExpiry() {",
		"  function showAttentionNotice(",
	)

	for _, required := range []string{
		`"The issue view changed. Refreshing both Attention lists from a current snapshot…"`,
		`const refreshed = await refreshAttention(false, true);`,
		`if (refreshed) {`,
		`"Attention refreshed from a current snapshot."`,
		`"Attention refresh failed. Retry before relying on the issue lists."`,
	} {
		if !strings.Contains(refresh, required) {
			t.Errorf("cursor refresh status contract is missing %q", required)
		}
	}
	awaitIndex := strings.Index(refresh, "await refreshAttention(false, true)")
	successIndex := strings.Index(refresh, `"Attention refreshed from a current snapshot."`)
	if awaitIndex < 0 || successIndex < 0 || successIndex < awaitIndex {
		t.Error("cursor refresh reports success before required reads complete")
	}
}

func TestAttentionBrowserUsesCursorOnlyContinuationAndAdditiveMetadata(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	listPath := browserSourceBlock(
		t,
		app,
		"  function buildIssuePath(kind, cursor) {",
		"  function renderIssueBucket(bucket) {",
	)
	detail := browserSourceBlock(
		t,
		app,
		"  async function loadIssueDetail(append, viewCursor) {",
		"  function loadMoreOccurrences() {",
	)

	for _, required := range []string{
		`if (cursor) {`,
		"return `/v1/issues?${new URLSearchParams({ cursor }).toString()}`;",
		`const parameters = new URLSearchParams({`,
		`attention_kind: kind,`,
		`experimental: state.issueFilters.experimental ? "include" : "stable",`,
	} {
		if !strings.Contains(listPath, required) {
			t.Errorf("issue-list cursor-v2 request contract is missing %q", required)
		}
	}
	cursorBranch := listPath[:strings.Index(listPath, "    const parameters =")]
	for _, forbidden := range []string{"limit:", "attention_kind:", "experimental:"} {
		if strings.Contains(cursorBranch, forbidden) {
			t.Errorf("issue-list continuation still sends %q", forbidden)
		}
	}

	for _, required := range []string{
		`const parameters = cursor`,
		`? new URLSearchParams({ cursor })`,
		`: new URLSearchParams({`,
		`limit: String(pageLimits.occurrences.page),`,
		`const pagination = requireCursorPage(`,
		`const responseViewCursor = readCursor(response.view_cursor);`,
		`response.catalog`,
		`response.global_analysis_coverage`,
	} {
		if !strings.Contains(detail, required) {
			t.Errorf("issue-detail cursor-v2/additive contract is missing %q", required)
		}
	}
	if strings.Contains(detail, `parameters.set("cursor"`) {
		t.Error("issue occurrence continuation mutates a fresh-request parameter set")
	}

	for _, required := range []string{
		`bucket.selection = selection;`,
		`function readIssueSelection(value, kind)`,
		`function readIssueCatalog(value, issue, previous)`,
		`function readGlobalAnalysisCoverage(value, fallback)`,
		`function globalCoverageQualifier(coverage)`,
		`"belay.issue-explanations.v1"`,
		`"belay.source-signals.v1"`,
		`"review_agent_permissions"`,
		`"inspect_cited_events"`,
		`"inspect_matching_sessions"`,
		`"inspect_verification_events"`,
		`Local API returned issue results without a view cursor.`,
		`Local API returned issue detail without a view cursor.`,
		`hasMore !== Boolean(nextCursor)`,
		`currentCursor && nextCursor === currentCursor`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("browser metadata/cursor consistency contract is missing %q", required)
		}
	}
}

func TestAttentionFamilyBrowserListDetailAndExactChildContract(t *testing.T) {
	index := readBrowserAsset(t, "assets/index.html")
	app := readBrowserAsset(t, "assets/app.js")
	styles := readBrowserAsset(t, "assets/styles.css")

	for _, forbidden := range []string{
		`id="issue-filter-recurrence"`,
		`id="issue-filter-category"`,
		`Session spread`,
		`state.issueFilters.recurrence`,
		`state.issueFilters.category`,
		`parameters.set("recurrence"`,
		`parameters.set("category"`,
	} {
		if strings.Contains(index, forbidden) || strings.Contains(app, forbidden) {
			t.Errorf("default Attention retains removed filter contract %q", forbidden)
		}
	}

	for _, required := range []string{
		`function buildAttentionFamilyPath(cursor)`,
		"return `/v1/attention-families?${new URLSearchParams({ cursor }).toString()}`;",
		`view_cursor: state.selectedFamilyViewCursor`,
		`? new URLSearchParams({ cursor })`,
		`function selectAttentionFamily(family, moveFocus)`,
		`readText(family.kind) === "exact_issue"`,
		`kind === "mapped_upstream"`,
		`source: "family"`,
		`returnFocus: { type: "family-member", key }`,
		`Grouped by one known signal type.`,
		`do not establish recurrence or one cause`,
		`clearAttentionFamilyDetailState();`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("Attention family browser contract is missing %q", required)
		}
	}
	for _, required := range []string{
		`id="family-detail" hidden`,
		`id="family-detail-heading" tabindex="-1"`,
		`id="family-member-list"`,
		`id="family-members-load-more"`,
	} {
		if !strings.Contains(index, required) {
			t.Errorf("Attention family shell is missing %q", required)
		}
	}
	for _, required := range []string{
		`.family-detail`,
		`.family-member-card`,
		`min-height: 44px;`,
		`overflow-wrap: anywhere;`,
	} {
		if !strings.Contains(styles, required) {
			t.Errorf("Attention family styles are missing %q", required)
		}
	}
}

func TestValueFirstAttentionAndSessionContracts(t *testing.T) {
	index := readBrowserAsset(t, "assets/index.html")
	app := readBrowserAsset(t, "assets/app.js")
	styles := readBrowserAsset(t, "assets/styles.css")

	for _, required := range []string{
		`id="stable-issues-section"`,
		`id="evidence-gaps-section"`,
		`id="fix-monitoring-section"`,
		`id="issue-evidence-preview"`,
		`id="issue-evidence-preview-all"`,
		`id="issue-technical-details"`,
		`<summary>Technical details</summary>`,
		`id="needs-attention-summary"`,
		`id="observed-work-summary"`,
		`id="session-highlights"`,
		`Highlights from the currently loaded retained events.`,
	} {
		if !strings.Contains(index, required) {
			t.Errorf("value-first browser shell is missing %q", required)
		}
	}

	layout := browserSourceBlock(
		t,
		app,
		"  function prepareValueFirstAttentionLayout() {",
		"  function bindEvents() {",
	)
	for _, required := range []string{
		`elements.stableIssuesSection,`,
		`elements.evidenceGapsSection,`,
		`elements.fixMonitoringSection,`,
		`analysisDisclosure,`,
		`filterDisclosure,`,
	} {
		if !strings.Contains(layout, required) {
			t.Errorf("first-viewport Attention order is missing %q", required)
		}
	}
	if issues, gaps, monitoring := strings.Index(layout, "elements.stableIssuesSection"), strings.Index(layout, "elements.evidenceGapsSection"), strings.Index(layout, "elements.fixMonitoringSection"); issues < 0 || gaps < issues || monitoring < gaps {
		t.Error("Attention sections are not ordered Issues, Evidence gaps, After attempts")
	}

	preview := browserSourceBlock(
		t,
		app,
		"  async function loadIssueEvidencePreview(",
		"  function renderIssueEvidencePreview() {",
	)
	for _, required := range []string{
		`expanded ? pageLimits.events.maximum / 10 : 3`,
		`const generation = ++state.issueEvidencePreviewRequestGeneration;`,
		`generation !== state.issueEvidencePreviewRequestGeneration`,
		`issueID !== state.selectedIssueID`,
		`occurrenceID !== state.issueEvidencePreview.occurrenceID`,
		`eventIDs.forEach((eventID) => parameters.append("event_id", eventID));`,
	} {
		if !strings.Contains(preview, required) {
			t.Errorf("bounded cancellable evidence preview is missing %q", required)
		}
	}
	for _, required := range []string{
		`state.issueEvidencePreviewRequestGeneration += 1;`,
		`state.issueEvidencePreview = createIssueEvidencePreview();`,
		`resetIssueEvidencePreview();`,
		`Event hydration uses current Local retention, not the frozen issue snapshot.`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("evidence preview reset/disclosure is missing %q", required)
		}
	}

	for _, required := range []string{
		`elements.recordFixAttempt.hidden =`,
		`serverReportedIneligible`,
		`renderFixHistory();`,
		`selectSessionHighlights(state.events)`,
		`.slice(0, 5)`,
		`left.priority - right.priority || left.index - right.index`,
		`.sort((left, right) => left.index - right.index)`,
		`type.startsWith("command.")`,
		`? summary || resourceName`,
		`: resourceName || summary`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("value-first behavior is missing %q", required)
		}
	}
	if strings.Contains(
		browserSourceBlock(
			t,
			app,
			"  function createEventRow(",
			"  function createEvidenceDetails(",
		),
		`"event-type"`,
	) {
		t.Error("raw event type remains in the primary timeline heading")
	}

	for _, required := range []string{
		`.attention-disclosure`,
		`.issue-evidence-preview`,
		`.technical-details`,
		`.session-highlights`,
		`max-height: none;`,
		`.event-scroll,`,
		`overflow: visible;`,
	} {
		if !strings.Contains(styles, required) {
			t.Errorf("value-first accessibility/mobile style is missing %q", required)
		}
	}
}

func TestAttentionEvidencePreview410FullyResetsState(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	reset := browserSourceBlock(
		t,
		app,
		"  function resetIssueEvidencePreview() {",
		"  async function loadIssueEvidencePreview(",
	)
	preview := browserSourceBlock(
		t,
		app,
		"  async function loadIssueEvidencePreview(",
		"  function renderIssueEvidencePreview() {",
	)

	for _, required := range []string{
		`issueEvidencePreviewController: null,`,
		`state.issueEvidencePreviewController.abort();`,
		`state.issueEvidencePreviewController = null;`,
		`state.issueEvidencePreview = createIssueEvidencePreview();`,
		`renderIssueEvidencePreview();`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("evidence-preview reset contract is missing %q", required)
		}
	}
	for _, required := range []string{
		`state.issueEvidencePreviewRequestGeneration += 1;`,
		`state.issueEvidencePreviewController.abort();`,
		`state.issueEvidencePreviewController = null;`,
		`state.issueEvidencePreview = createIssueEvidencePreview();`,
		`renderIssueEvidencePreview();`,
	} {
		if !strings.Contains(reset, required) {
			t.Errorf("evidence-preview reset does not clear %q", required)
		}
	}
	for _, required := range []string{
		`const controller = new AbortController();`,
		`state.issueEvidencePreviewController = controller;`,
		`controller.signal,`,
		`controller !== state.issueEvidencePreviewController`,
		`controller.signal.aborted`,
		`if (isCursorExpired(error)) {`,
		`resetIssueEvidencePreview();`,
		`await refreshAttentionAfterExpiry();`,
	} {
		if !strings.Contains(preview, required) {
			t.Errorf("evidence-preview 410 recovery is missing %q", required)
		}
	}
	expiry := strings.Index(preview, `if (isCursorExpired(error)) {`)
	staleError := strings.Index(
		preview,
		`state.issueEvidencePreview.status = "error";`,
	)
	if expiry < 0 || staleError < 0 || expiry > staleError {
		t.Error("evidence-preview 410 must reset and return before stale error state is rendered")
	}
}

func TestAttentionBrowserRejectsContinuationViewCursorMismatchBeforeMutation(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	list := browserSourceBlock(
		t,
		app,
		"  async function loadIssueBucket(bucket, append) {",
		"  function buildIssuePath(kind, cursor) {",
	)
	detail := browserSourceBlock(
		t,
		app,
		"  async function loadIssueDetail(append, viewCursor) {",
		"  function loadMoreOccurrences() {",
	)

	listCheck := `if (cursor && viewCursor !== bucket.viewCursor) {`
	listError := `Local API changed the issue-list view cursor during continuation.`
	listMutation := `bucket.data =`
	for _, required := range []string{listCheck, listError} {
		if !strings.Contains(list, required) {
			t.Errorf("issue-list continuation mismatch handling is missing %q", required)
		}
	}
	if check, mutation := strings.Index(list, listCheck), strings.Index(list, listMutation); check < 0 || mutation < 0 || check > mutation {
		t.Error("issue-list view cursor mismatch must fail before cached rows are mutated")
	}

	detailCheck := `responseViewCursor !== state.selectedIssueViewCursor`
	detailError := `Local API changed the issue-detail view cursor during continuation.`
	detailMutation := `state.selectedIssue = issue;`
	for _, required := range []string{detailCheck, detailError} {
		if !strings.Contains(detail, required) {
			t.Errorf("issue-detail continuation mismatch handling is missing %q", required)
		}
	}
	if check, mutation := strings.Index(detail, detailCheck), strings.Index(detail, detailMutation); check < 0 || mutation < 0 || check > mutation {
		t.Error("issue-detail view cursor mismatch must fail before detail state is mutated")
	}
}

func TestAttentionBrowserCursorExpiryClearsEveryDependentState(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	refresh := browserSourceBlock(
		t,
		app,
		"  async function refreshAttentionAfterExpiry() {",
		"  function showAttentionNotice(",
	)
	clearState := browserSourceBlock(
		t,
		app,
		"  function clearExpiredIssueSnapshotState() {",
		"  function clearExpiredFixDraftState() {",
	)
	clearDrafts := browserSourceBlock(
		t,
		app,
		"  function clearExpiredFixDraftState() {",
		"  function showAttentionNotice(",
	)
	closeDetail := browserSourceBlock(
		t,
		app,
		"  function closeIssueDetail(",
		"  async function refreshAttentionAfterExpiry()",
	)

	if !strings.Contains(refresh, "clearExpiredIssueSnapshotState();") {
		t.Fatal("410 recovery does not clear dependent state before refresh")
	}
	for _, required := range []string{
		`resetIssueBucket(state.issues);`,
		`resetIssueBucket(state.evidenceGaps);`,
		`resetFixMonitoringBucket(false);`,
		`clearExpiredFixDraftState();`,
		`closeIssueDetail(false, true);`,
		`clearAttentionFamilyDetailState();`,
	} {
		if !strings.Contains(clearState, required) {
			t.Errorf("410 state clearing is missing %q", required)
		}
	}
	for _, required := range []string{
		`draft.idempotencyKey = "";`,
		`draft.actionToken = "";`,
		`draft.attempted = false;`,
		`draft.unresolved = false;`,
		`draft.pending = false;`,
		`state.modalSubmitting = false;`,
	} {
		if !strings.Contains(clearDrafts, required) {
			t.Errorf("410 fix/action-token clearing is missing %q", required)
		}
	}
	for _, required := range []string{
		`state.selectedIssueCatalog = null;`,
		`state.selectedGlobalAnalysisCoverage = null;`,
		`state.selectedIssueViewCursor = "";`,
		`state.occurrenceNextCursor = "";`,
		`resetFixIssueState();`,
	} {
		if !strings.Contains(closeDetail, required) {
			t.Errorf("410 detail clearing is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"submitFixAttempt(",
		"apiMutation(",
		"selectIssue(",
	} {
		if strings.Contains(refresh, forbidden) ||
			strings.Contains(clearState, forbidden) ||
			strings.Contains(clearDrafts, forbidden) {
			t.Errorf("410 recovery must not automatically invoke %q", forbidden)
		}
	}
}

func TestAttentionUnknownCountsAndAnalysisStayUncertain(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	recurrence := browserSourceBlock(
		t,
		app,
		"  function issueRecurrenceLabel(issue) {",
		"  function issueHarnessLabel(",
	)
	analysis := browserSourceBlock(
		t,
		app,
		"  function normalizeAnalysisStatus(value) {",
		"  function safeCatalogCode(",
	)

	for _, required := range []string{
		`const sessions = Number(issue && issue.session_count);`,
		`if (!Number.isFinite(sessions) || sessions < 1) {`,
		`return "Session count unavailable";`,
	} {
		if !strings.Contains(recurrence, required) {
			t.Errorf("uncertain session-count contract is missing %q", required)
		}
	}
	if strings.Index(recurrence, `return "Session count unavailable";`) >
		strings.Index(recurrence, `"Observed in one session"`) {
		t.Error("missing or zero session count can be presented as one session")
	}
	for _, required := range []string{
		`: "unknown";`,
		`if (status === "unknown") return "Analysis status unavailable";`,
	} {
		if !strings.Contains(analysis, required) {
			t.Errorf("unknown analysis status contract is missing %q", required)
		}
	}
	for _, required := range []string{
		`elements.issueAnalysisQualifier.textContent =`,
		`analysisQualifiers[status] || "";`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("issue detail uncertainty qualifier is missing %q", required)
		}
	}
}

func TestAttentionDirectSessionOpeningFetchesBeforeRendering(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	openSession := browserSourceBlock(
		t,
		app,
		"  function openSession(sessionID, optionalKnownSummary) {",
		"  function beginSessionSelection(",
	)
	directLoad := browserSourceBlock(
		t,
		app,
		"  async function loadDirectSession(sessionID, generation) {",
		"  async function loadFindings(",
	)

	for _, required := range []string{
		`if (!known) {`,
		`void loadDirectSession(sessionID, generation);`,
		`beginSessionSelection(sessionID, known);`,
	} {
		if !strings.Contains(openSession, required) {
			t.Errorf("direct session opening is missing %q", required)
		}
	}
	fetchIndex := strings.Index(
		directLoad,
		"`/v1/sessions/${encodeURIComponent(sessionID)}`",
	)
	renderIndex := strings.Index(
		directLoad,
		"beginSessionSelection(sessionID, detail);",
	)
	if fetchIndex < 0 || renderIndex < 0 || fetchIndex > renderIndex {
		t.Error("an unloaded occurrence session is rendered before its detail fetch")
	}
	for _, required := range []string{
		`const returnFocus = state.sessionReturnFocus;`,
		`setActiveView("attention", false);`,
		`restoreLogicalFocus(`,
		`showError("Unable to open selected session", error);`,
	} {
		if !strings.Contains(directLoad, required) {
			t.Errorf("direct session failure handling is missing %q", required)
		}
	}
	activateIndex := strings.Index(directLoad, `setActiveView("attention", false);`)
	restoreIndex := strings.Index(directLoad, "restoreLogicalFocus(")
	if activateIndex < 0 || restoreIndex < 0 || activateIndex > restoreIndex {
		t.Error("direct session failure restores focus before the mobile pane is active")
	}
}

func TestSessionFindingsRemainBoundedAndExplicitlyPaginated(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	findings := browserSourceBlock(
		t,
		app,
		"  async function loadFindings(sessionID) {",
		"  function loadMoreFindings() {",
	)

	if got := strings.Count(findings, "await apiGet("); got != 1 {
		t.Fatalf("session findings page performs %d API requests; want exactly one", got)
	}
	for _, forbidden := range []string{"while (", "do {", "pageLimits.findings.maximum"} {
		if strings.Contains(findings, forbidden) {
			t.Errorf("session findings loader contains unbounded behavior %q", forbidden)
		}
	}
	for _, required := range []string{
		`findings: { page: 20 }`,
		`session_id: sessionID`,
		`parameters.set("cursor", cursor)`,
		`state.findingNextCursor`,
		`state.findingHasMore`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("bounded findings contract is missing %q", required)
		}
	}
}

func readBrowserAsset(t *testing.T, name string) string {
	t.Helper()
	body, err := fs.ReadFile(assetFiles, name)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func browserSourceBlock(t *testing.T, source, start, end string) string {
	t.Helper()
	startIndex := strings.Index(source, start)
	if startIndex < 0 {
		t.Fatalf("browser source is missing section start %q", start)
	}
	endOffset := strings.Index(source[startIndex:], end)
	if endOffset < 0 {
		t.Fatalf("browser source is missing section end %q", end)
	}
	return source[startIndex : startIndex+endOffset]
}
