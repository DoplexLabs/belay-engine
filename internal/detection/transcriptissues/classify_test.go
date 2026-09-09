package transcriptissues

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DoplexLabs/belay-engine/internal/issueintel"
	"github.com/DoplexLabs/belay-engine/internal/transcript"
)

func TestVerificationClassifierUnderstandsPackageScriptsAndProjectTargets(
	t *testing.T,
) {
	tests := []struct {
		command string
		config  issueintel.ProjectConfig
		want    string
	}{
		{command: "pnpm run check", want: commandClassBuild},
		{command: "npm test", want: commandClassTest},
		{command: "python -m pytest", want: commandClassTest},
		{command: "make verify", config: issueintel.ProjectConfig{
			VerificationCommands: []string{"make verify"},
		}, want: commandClassBuild},
		{command: "pnpm run lint", want: commandClassLint},
		{command: "tsc --noEmit", want: commandClassTypecheck},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			if got := classifyCommand(test.command, test.config); got != test.want {
				t.Fatalf("class = %q, want %q", got, test.want)
			}
		})
	}
}

func TestClassifyVerificationCommandRejectsOrdinaryCommands(t *testing.T) {
	if class, ok := ClassifyVerificationCommand(
		"pnpm run check",
		issueintel.ProjectConfig{},
	); !ok || class != commandClassBuild {
		t.Fatalf("verification classification = %q/%t", class, ok)
	}
	if class, ok := ClassifyVerificationCommand(
		"git status --short",
		issueintel.ProjectConfig{},
	); ok || class != "" {
		t.Fatalf("ordinary command classification = %q/%t", class, ok)
	}
}

func TestVerificationClassifierDoesNotScanArgumentsOrPaths(t *testing.T) {
	commands := []string{
		"grep TODO . | grep -v _test.go",
		"grep lint internal/detection/transcriptissues",
		"grep build docs/build-notes.md",
		"rg check internal/checks",
		"cat docs/format/checklist.md",
		"sed -n 1,80p internal/build/check.go",
		"bash ./scripts/run-tests.sh",
		"sh ./scripts/lint.sh",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			if got := classifyCommand(
				command,
				issueintel.ProjectConfig{},
			); got != commandClassOther {
				t.Fatalf("class = %q, want %q", got, commandClassOther)
			}
		})
	}
}

func TestVerificationClassifierRecognizesBoundedExecutableBasenames(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{command: "./scripts/run-tests.sh", want: commandClassTest},
		{command: "./scripts/lint.sh", want: commandClassLint},
		{command: "./scripts/project-build", want: commandClassBuild},
		{command: "./scripts/check", want: commandClassBuild},
		{command: "./scripts/format-check.sh", want: commandClassFormat},
		{command: "./scripts/type-check.sh", want: commandClassTypecheck},
		{command: "./scripts/contest.sh", want: commandClassOther},
		{command: "./scripts/checklist.sh", want: commandClassOther},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			if got := classifyCommand(
				test.command,
				issueintel.ProjectConfig{},
			); got != test.want {
				t.Fatalf("class = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeCommandStripsVolatileArguments(t *testing.T) {
	first := normalizeCommand(
		"go test ./pkg/123 --port 4312 /tmp/run-a deadbeef123",
	)
	second := normalizeCommand(
		"go test ./pkg/456 --port 9921 /private/tmp/run-b cafebabe999",
	)
	if first != second ||
		!strings.Contains(first, "<n>") ||
		!strings.Contains(first, "<port>") ||
		!strings.Contains(first, "<tmp>") ||
		!strings.Contains(first, "<hash>") {
		t.Fatalf("normalized commands = %q / %q", first, second)
	}
}

func TestEditedFilesExtractsStructuredAndPatchPaths(t *testing.T) {
	input, err := json.Marshal(map[string]any{
		"patch":     "*** Update File: internal/a.go\n@@\n",
		"file_path": "internal/b.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	files := editedFiles(transcript.Turn{
		Role:     transcript.RoleToolCall,
		ToolName: "apply_patch",
		Payload:  transcript.Payload{ToolInput: input},
	})
	if len(files) != 2 ||
		files[0] != "internal/a.go" ||
		files[1] != "internal/b.go" {
		t.Fatalf("files = %v", files)
	}
}
