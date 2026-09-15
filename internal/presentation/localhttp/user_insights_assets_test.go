package localhttp

import (
	"strings"
	"testing"
)

func TestHabitsBrowserShellIsSeparateAndHiddenByDefault(t *testing.T) {
	index := readBrowserAsset(t, "assets/index.html")

	for _, required := range []string{
		`id="nav-habits"`,
		`<span id="nav-habits-label">Habits</span>`,
		`id="habits-view"`,
		`id="habits-view"` + "\n" + `          aria-labelledby="habits-heading"` + "\n" + `          hidden`,
		`id="habits-heading" tabindex="-1">How you work with your agent`,
		`id="habits-status"`,
		`id="habits-loading"`,
		`id="habits-error"`,
		`id="habits-retry"`,
		`id="habits-content" hidden`,
		`id="habits-empty" hidden`,
		`id="habits-list"`,
		`id="habits-about" hidden`,
		`id="habits-limitations"`,
		`Nothing here is compared with other people, and nothing here is sent to your agent.`,
	} {
		if !strings.Contains(index, required) {
			t.Errorf("Habits shell is missing %q", required)
		}
	}

	// The existing views keep their default states: Report stays the landing
	// view and the other workspaces keep their own markup untouched.
	for _, preserved := range []string{
		`id="nav-brief"` + "\n" + `            type="button"` + "\n" + `            aria-current="page"`,
		`id="attention-view"` + "\n" + `          hidden`,
		`id="sessions-view" hidden`,
	} {
		if !strings.Contains(index, preserved) {
			t.Errorf("Habits must not change existing view defaults: missing %q", preserved)
		}
	}
	if strings.Index(index, `id="habits-view"`) < strings.Index(index, `id="sessions-view"`) {
		t.Error("Habits view should render after the existing workspaces")
	}
}

func TestHabitsBrowserContractReadsOnlyItsOwnRoute(t *testing.T) {
	app := readBrowserAsset(t, "assets/app.js")
	styles := readBrowserAsset(t, "assets/styles.css")

	for _, required := range []string{
		`navHabits: "Habits"`,
		`habitsStatus: "idle"`,
		`["brief", "attention", "sessions", "habits"].includes(view)`,
		`setViewVisibility(elements.habitsView, habitsActive);`,
		`setCurrentNavigation(elements.navHabits, habitsActive);`,
		`elements.navHabitsLabel.textContent = copy.navHabits;`,
		`await apiGet("/v1/user-insights")`,
		`"belay.user-insights.v1"`,
		`.slice(0, 25)`,
		`session.findings.filter(isRecord).slice(0, 3)`,
		`if (state.activeView === "habits") {`,
		`"How Belay knows"`,
		`"Ready-made opening for your next session"`,
		`"Copy opening"`,
		`readText(finding.evidence_class) === "judgment"`,
		`"Outcome not reported"`,
	} {
		if !strings.Contains(app, required) {
			t.Errorf("Habits browser contract is missing %q", required)
		}
	}
	if strings.Count(app, `navHabits: "Habits"`) != 2 {
		t.Error("Habits label must be defined for both runtime experiences")
	}
	// Habits never writes through the Local API and never reuses the
	// Report, Patterns, or Sessions loaders.
	habitsStart := strings.Index(app, "async function loadUserInsights()")
	habitsEnd := strings.Index(app, "function createElement(tagName, className, text)")
	if habitsStart < 0 || habitsEnd < habitsStart {
		t.Fatal("Habits loader and renderer must be defined together")
	}
	habits := app[habitsStart:habitsEnd]
	for _, forbidden := range []string{
		"apiMutation(",
		"loadDeveloperBrief(",
		"loadCostIssues(",
		"refreshAttention(",
		"refreshSessions(",
		"openSession(",
		"innerHTML",
	} {
		if strings.Contains(habits, forbidden) {
			t.Errorf("Habits code must stay separate from other views: found %q", forbidden)
		}
	}
	for _, required := range []string{
		".habits-card {",
		".habits-finding-keep {",
		".habits-opener {",
		".habits-evidence summary {",
	} {
		if !strings.Contains(styles, required) {
			t.Errorf("Habits styles are missing %q", required)
		}
	}
}
