package localhttp

import (
	"strings"
	"testing"
)

func TestDeveloperBriefBrowserShellAndDefaultView(t *testing.T) {
	index := readBrowserAsset(t, "assets/index.html")

	for _, required := range []string{
		`id="nav-brief"`,
		`id="nav-brief"` + "\n" + `            type="button"` + "\n" + `            aria-current="page"`,
		`id="brief-view"`,
		`id="brief-heading" tabindex="-1">Your last 24 hours`,
		`id="brief-action-list"`,
		`id="brief-recent-list"`,
		`id="brief-agent-summary"`,
		`id="brief-coverage"`,
		`id="attention-view"` + "\n" + `          hidden`,
		`id="sessions-view" hidden`,
		`id="session-diagnosis"`,
		`id="session-diagnosis-heading" tabindex="-1"`,
	} {
		if !strings.Contains(index, required) {
			t.Errorf("Developer Brief shell is missing %q", required)
		}
	}

	diagnosis := strings.Index(index, `id="session-diagnosis"`)
	overview := strings.Index(index, `id="session-overview"`)
	if diagnosis < 0 || overview < 0 || diagnosis > overview {
		t.Fatal("session diagnosis must render before the existing overview and timeline")
	}
}

func TestDeveloperBriefBrowserContractAndBounds(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	index := readBrowserAsset(t, "assets/index.html")

	for _, required := range []string{
		`activeView: "brief"`,
		`setActiveView("brief", false);`,
		`await apiGet("/v1/developer-brief")`,
		`"belay.developer-brief.v1"`,
		`brief.action_cards.slice(0, 5)`,
		`brief.recent_work.slice(0, 8)`,
		`"No recorded agent activity in the last 24 hours"`,
		`"No reviewed action was identified in the evaluated activity"`,
		`"This is not a claim that all activity was successful or problem-free."`,
		`"Brief is limited. Review the coverage notes before relying on it."`,
		`kind === "open_attention_family"`,
		`kind === "open_issue"`,
		`kind === "open_session"`,
		`state.sessionReturnView =`,
		`? "brief"`,
		`: "sessions";`,
		`state.briefSelectionID = "";`,
		`await loadDeveloperBrief();`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("Developer Brief browser contract is missing %q", required)
		}
	}
	if !strings.Contains(index, `>What to review now</h2>`) {
		t.Error("Developer Brief is missing the primary review section")
	}

	if strings.Contains(app, "brief.action_cards.sort(") ||
		strings.Contains(app, "brief.recent_work.sort(") {
		t.Fatal("browser must preserve the server-ranked Brief order")
	}
}

func TestSessionDiagnosisBrowserContract(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")

	for _, required := range []string{
		`state.selectedDiagnosis = isRecord(response.diagnosis)`,
		`"belay.session-diagnosis.v1"`,
		`diagnosis.action_cards.slice(0, 5)`,
		`elements.sessionDiagnosisTitle.textContent =`,
		`readText(diagnosis.summary.title)`,
		`elements.sessionDiagnosisDetail.textContent =`,
		`readText(diagnosis.summary.detail)`,
		`focusRegistry.diagnosisActions.set(cardID, button);`,
		`returnFocus.type === "diagnosis-action"`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("session diagnosis browser contract is missing %q", required)
		}
	}
}

func TestDeveloperBriefAccessibilityAndSafeRendering(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	styles := readBrowserAsset(t, "assets/styles.css")

	for _, required := range []string{
		`setViewVisibility(elements.briefView, briefActive);`,
		`setCurrentNavigation(elements.navBrief, briefActive);`,
		`focusRegistry.briefActions.get(reference.key)`,
		`focusRegistry.briefSessions.get(reference.key)`,
		`focusRegistry.diagnosisActions.get(reference.key)`,
		`elements.briefStatus.textContent =`,
		`"brief-observation"`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("Developer Brief accessibility contract is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"innerHTML",
		"outerHTML",
		"insertAdjacentHTML",
		"document.write",
	} {
		if strings.Contains(app, forbidden) {
			t.Errorf("browser asset contains unsafe rendering primitive %q", forbidden)
		}
	}
	for _, required := range []string{
		`.brief-workspace {`,
		`overflow-y: auto;`,
		`.brief-session-button {`,
		`min-height: 44px;`,
		`.session-diagnosis {`,
		`@media (max-width: 680px)`,
		`.session-diagnosis-actions {`,
	} {
		if !strings.Contains(styles, required) {
			t.Errorf("Developer Brief responsive styles are missing %q", required)
		}
	}
}
