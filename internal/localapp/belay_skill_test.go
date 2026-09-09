package localapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DoplexLabs/belay-engine/internal/acquisition/numbat"
)

func TestInstallBelaySkillsUsesHarnessConfigRootsAndIsIdempotent(t *testing.T) {
	claudeRoot := filepath.Join(t.TempDir(), "claude")
	codexRoot := filepath.Join(t.TempDir(), "codex")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeRoot)
	t.Setenv("CODEX_HOME", codexRoot)
	inventory := belaySkillTestInventory(true, true)
	results, err := InstallBelaySkills(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 ||
		results[0].Status != "installed" ||
		results[1].Status != "installed" {
		t.Fatalf("install results = %+v", results)
	}
	for _, root := range []string{claudeRoot, codexRoot} {
		path := filepath.Join(root, "skills", "belay", "SKILL.md")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != belaySkillBody ||
			!strings.Contains(string(body), "Only after explicit approval") ||
			!strings.Contains(string(body), "verification are deferred") ||
			!strings.Contains(string(body), "/belay start --issue <issue_id>") ||
			!strings.Contains(
				string(body),
				"Use this Mission Pack for this session?",
			) ||
			!strings.Contains(
				string(body),
				"Mission Pack mode must not edit any file",
			) ||
			!strings.Contains(
				string(body),
				"`readmodel.rendered_markdown` as the canonical preview",
			) ||
			!strings.Contains(
				string(body),
				"`verification[].command` exactly as returned",
			) ||
			!strings.Contains(
				string(body),
				"Whenever the conversation contains a concrete",
			) ||
			!strings.Contains(
				string(body),
				"active user task, pass a",
			) ||
			!strings.Contains(
				string(body),
				"concise `task_hint` that describes that task "+
					"in at most 280 characters",
			) ||
			!strings.Contains(
				string(body),
				"`/belay start` itself is not a task hint",
			) ||
			!strings.Contains(
				string(body),
				"When no concrete active task",
			) ||
			!strings.Contains(
				string(body),
				"exists, omit `task_hint`; do not invent one",
			) ||
			!strings.Contains(
				string(body),
				"use `harness: claude` in",
			) ||
			!strings.Contains(
				string(body),
				"`harness: codex` in Codex",
			) ||
			!strings.Contains(
				string(body),
				"Never infer or guess another",
			) ||
			!strings.Contains(
				string(body),
				"If `readmodel.status` is `empty`",
			) ||
			!strings.Contains(
				string(body),
				"Do not show project metadata or ask",
			) ||
			!strings.Contains(
				string(body),
				"command in its own fenced code block",
			) ||
			!strings.Contains(
				string(body),
				"Do not invent an operating rule",
			) ||
			!strings.Contains(
				string(body),
				"Never display warnings, coverage, freshness",
			) ||
			!strings.Contains(
				string(body),
				"Belay found no useful",
			) ||
			!strings.Contains(
				string(body),
				"guidance for this session.",
			) {
			t.Fatalf("skill body at %s = %q", path, body)
		}
	}
	results, err = InstallBelaySkills(inventory)
	if err != nil || results[0].Status != "unchanged" ||
		results[1].Status != "unchanged" {
		t.Fatalf("idempotent results/error = %+v/%v", results, err)
	}
}

func TestInstallBelaySkillsSkipsUndetectedHarnesses(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "codex"))
	results, err := InstallBelaySkills(belaySkillTestInventory(false, true))
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != "unavailable" ||
		results[1].Status != "installed" {
		t.Fatalf("results = %+v", results)
	}
}

func TestInstallBelaySkillsDoesNotOverwriteForeignSkillOrFollowSymlink(
	t *testing.T,
) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))
	skillDirectory := filepath.Join(root, "skills", "belay")
	if err := os.MkdirAll(skillDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(skillDirectory, "SKILL.md")
	if err := os.WriteFile(
		target,
		[]byte("---\nname: custom\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	results, err := InstallBelaySkills(belaySkillTestInventory(true, false))
	if err == nil || results[0].Status != "conflict" {
		t.Fatalf("foreign skill results/error = %+v/%v", results, err)
	}
	body, readErr := os.ReadFile(target)
	if readErr != nil || !strings.Contains(string(body), "name: custom") {
		t.Fatalf("foreign skill changed/error = %q/%v", body, readErr)
	}

	symlinkRoot := filepath.Join(t.TempDir(), "codex-link")
	outside := t.TempDir()
	if err := os.Symlink(outside, symlinkRoot); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", symlinkRoot)
	results, err = InstallBelaySkills(belaySkillTestInventory(true, false))
	if err == nil || results[0].Status != "failed" {
		t.Fatalf("symlink results/error = %+v/%v", results, err)
	}
}

func belaySkillTestInventory(
	codex, claude bool,
) numbat.Inventory {
	inventory := numbat.Inventory{
		LaunchTargets: make(map[numbat.Agent]numbat.InventoryRow),
	}
	if codex {
		inventory.LaunchTargets[numbat.AgentCodex] = numbat.InventoryRow{
			Agent:    "codex",
			Present:  true,
			Detected: true,
		}
	}
	if claude {
		inventory.LaunchTargets[numbat.AgentClaude] = numbat.InventoryRow{
			Agent:    "claude",
			Present:  true,
			Detected: true,
		}
	}
	return inventory
}
