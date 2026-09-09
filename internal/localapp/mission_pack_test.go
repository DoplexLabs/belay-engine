package localapp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/issueintel"
	"github.com/DoplexLabs/belay-engine/internal/missionpack"
)

type missionPackTestRepository struct {
	issueProject missionpack.ResolvedProject
	cwdProject   missionpack.ResolvedProject
	evidence     missionpack.EvidenceSnapshot
	selectors    []missionpack.ProjectSelector
	limits       missionpack.Limits
	readCalls    int
}

func (r *missionPackTestRepository) ResolveMissionPackProject(
	_ context.Context,
	selector missionpack.ProjectSelector,
) (missionpack.ResolvedProject, error) {
	r.selectors = append(r.selectors, selector)
	if selector.IssueID != "" {
		return r.issueProject, nil
	}
	return r.cwdProject, nil
}

func (r *missionPackTestRepository) ReadMissionPackEvidence(
	_ context.Context,
	_ string,
	limits missionpack.Limits,
) (missionpack.EvidenceSnapshot, error) {
	r.readCalls++
	r.limits = limits
	return r.evidence, nil
}

func TestMissionPackServiceGeneratesFromBoundedEvidenceAndWorkspace(
	t *testing.T,
) {
	root := t.TempDir()
	runMissionPackTestCommand(t, root, "git", "init", "-b", "main")
	runMissionPackTestCommand(
		t,
		root,
		"git",
		"remote",
		"add",
		"origin",
		"https://user:secret@example.test/team/project.git?token=private",
	)
	if err := os.WriteFile(
		filepath.Join(root, "package.json"),
		[]byte(`{
			"packageManager": "pnpm@9.12.0",
			"scripts": {"check": "tsc --noEmit && eslint ."}
		}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "pnpm-lock.yaml"),
		[]byte("lockfileVersion: '9.0'\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	repository := &missionPackTestRepository{
		cwdProject: missionpack.ResolvedProject{
			Identity:     "https://example.test/team/project.git",
			IdentityKind: "remote",
			Path:         root,
		},
		evidence: missionpack.EvidenceSnapshot{
			SourceState: missionpack.SourceState{
				TranscriptGeneration: 4,
				AnalyzedGeneration:   4,
				AnalysisStatus:       missionpack.AnalysisStatusCurrent,
				DataThrough:          now,
			},
			Sessions: []missionpack.EvidenceSession{{
				SessionKey:  "ses_one",
				Harness:     "codex",
				ProjectPath: root,
				Coverage:    missionpack.TranscriptCoverageComplete,
			}},
			SuccessfulCommands: []missionpack.SuccessfulCommand{{
				Command:     "pnpm run check",
				SucceededAt: now,
				Source: missionpack.SourceRef{
					Kind:       "transcript_turn",
					SessionKey: "ses_one",
				},
			}},
			Coverage: missionpack.Coverage{
				TranscriptStatus:          missionpack.TranscriptCoverageComplete,
				CanonicalContextAvailable: false,
			},
		},
	}
	service, err := NewMissionPackService(repository)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	pack, err := service.Generate(context.Background(), missionpack.Request{
		CWD:     root,
		Intent:  missionpack.IntentImplement,
		Harness: missionpack.HarnessCodex,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(repository.selectors) != 1 ||
		repository.selectors[0].RemoteIdentity !=
			"https://example.test/team/project.git" {
		t.Fatalf("project selectors = %#v", repository.selectors)
	}
	if repository.limits.Sessions != 20 ||
		repository.limits.CommandTurns != 500 ||
		repository.limits.Events != 500 {
		t.Fatalf("evidence limits = %#v", repository.limits)
	}
	if pack.Project.Label != "project" ||
		pack.Project.Branch != "main" ||
		pack.Harness != missionpack.HarnessCodex ||
		len(pack.Verification) != 1 ||
		pack.Verification[0].Command != "pnpm run check" ||
		!pack.Verification[0].Configured ||
		!pack.Verification[0].Observed {
		t.Fatalf("generated pack = %#v", pack)
	}
}

func TestMissionPackServiceRejectsInvalidHarness(t *testing.T) {
	root := t.TempDir()
	runMissionPackTestCommand(t, root, "git", "init", "-b", "main")
	repository := &missionPackTestRepository{
		cwdProject: missionpack.ResolvedProject{
			Identity:     root,
			IdentityKind: "path",
			Path:         root,
		},
	}
	service, err := NewMissionPackService(repository)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Generate(context.Background(), missionpack.Request{
		CWD:     root,
		Intent:  missionpack.IntentImplement,
		Harness: missionpack.Harness("cursor"),
	})
	if err == nil || !strings.Contains(err.Error(), "harness") {
		t.Fatalf("Generate() error = %v", err)
	}
	if repository.readCalls != 0 {
		t.Fatalf("evidence reads = %d, want 0", repository.readCalls)
	}
}

func TestMissionPackServiceRequestTimeoutIsFiveSeconds(t *testing.T) {
	if missionPackServiceRequestTimeout != 5*time.Second {
		t.Fatalf(
			"Mission Pack service timeout = %s, want 5s",
			missionPackServiceRequestTimeout,
		)
	}
}

func TestMissionPackServiceRejectsIssueCWDProjectMismatch(t *testing.T) {
	root := t.TempDir()
	runMissionPackTestCommand(t, root, "git", "init", "-b", "main")
	repository := &missionPackTestRepository{
		issueProject: missionpack.ResolvedProject{
			Identity:     "project-a",
			IdentityKind: "path",
			Path:         root,
		},
		cwdProject: missionpack.ResolvedProject{
			Identity:     "project-b",
			IdentityKind: "path",
			Path:         root,
		},
	}
	service, err := NewMissionPackService(repository)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Generate(context.Background(), missionpack.Request{
		CWD:     root,
		IssueID: "issue_one",
		Intent:  missionpack.IntentDebug,
	})
	if !errors.Is(err, missionpack.ErrProjectMismatch) {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestMissionPackServiceRejectsChangedRemoteForIssueOnly(t *testing.T) {
	root := t.TempDir()
	runMissionPackTestCommand(t, root, "git", "init", "-b", "main")
	runMissionPackTestCommand(
		t,
		root,
		"git",
		"remote",
		"add",
		"origin",
		"https://example.test/team/current.git",
	)
	repository := &missionPackTestRepository{
		issueProject: missionpack.ResolvedProject{
			Identity:     "https://example.test/team/previous.git",
			IdentityKind: "remote",
			Path:         root,
		},
	}
	service, err := NewMissionPackService(repository)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Generate(context.Background(), missionpack.Request{
		IssueID: "issue_previous",
		Intent:  missionpack.IntentDebug,
	})
	if !errors.Is(err, missionpack.ErrProjectNotFound) {
		t.Fatalf("Generate() error = %v", err)
	}
	if repository.readCalls != 0 {
		t.Fatalf("evidence reads = %d, want 0", repository.readCalls)
	}
}

func TestObservedMissionPackCommandsRequireRecognizedSuccess(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	values := observedMissionPackCommands(
		[]missionpack.SuccessfulCommand{
			{Command: "go test ./...", SucceededAt: now},
			{Command: "git status --short", SucceededAt: now},
		},
		issueintel.ProjectConfig{},
	)
	if len(values) != 1 || values[0].Command != "go test ./..." {
		t.Fatalf("observed commands = %#v", values)
	}
}

func TestObservedMissionPackCommandsRejectShellPrograms(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	values := observedMissionPackCommands(
		[]missionpack.SuccessfulCommand{
			{
				Command: "go test -count=1 ./cmd/belay " +
					"./internal/presentation/localhttp 2>&1 | tail -30",
				SucceededAt: now,
			},
			{
				Command: "gofmt -d internal/pipeline/edge_acceptance_test.go; " +
					"git diff --check; git status --short; " +
					"wc -l internal/pipeline/edge_acceptance_test.go",
				SucceededAt: now,
			},
			{
				Command: "gofmt -w internal/pipeline/edge_acceptance_test.go && " +
					"git diff --check -- internal/pipeline/edge_acceptance_test.go",
				SucceededAt: now,
			},
			{Command: "go test ./...", SucceededAt: now},
			{Command: "make test", SucceededAt: now},
			{Command: "pnpm run check", SucceededAt: now},
			{Command: "./scripts/verify", SucceededAt: now},
		},
		issueintel.ProjectConfig{
			VerificationCommands: []string{"./scripts/verify"},
		},
	)
	got := make(map[string]bool, len(values))
	for _, value := range values {
		got[value.Command] = true
	}
	want := []string{
		"go test ./...",
		"make test",
		"pnpm run check",
		"./scripts/verify",
	}
	if len(got) != len(want) {
		t.Fatalf("observed commands = %#v", values)
	}
	for _, command := range want {
		if !got[command] {
			t.Fatalf("observed commands missing %q: %#v", command, values)
		}
	}
}

func runMissionPackTestCommand(
	t *testing.T,
	workdir string,
	name string,
	args ...string,
) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = workdir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
}
