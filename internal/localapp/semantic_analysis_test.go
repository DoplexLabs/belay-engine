package localapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/issueintel"
	"github.com/DoplexLabs/belay-engine/internal/transcript"
)

type semanticAnalysisTestStore struct {
	projects []issueintel.Project
	inputs   map[string]issueintel.SemanticInput
	existing map[string]issueintel.InsightRecord
	stored   []issueintel.InsightRecord
}

func (s *semanticAnalysisTestStore) ListInsightProjects(
	context.Context,
	int,
) ([]issueintel.Project, error) {
	return append([]issueintel.Project(nil), s.projects...), nil
}

func (s *semanticAnalysisTestStore) LoadSemanticInput(
	_ context.Context,
	project issueintel.Project,
) (issueintel.SemanticInput, error) {
	return s.inputs[project.Identity], nil
}

func (s *semanticAnalysisTestStore) ReplaceProjectInsight(
	_ context.Context,
	record issueintel.InsightRecord,
) error {
	s.stored = append(s.stored, record)
	return nil
}

func (s *semanticAnalysisTestStore) GetProjectInsight(
	_ context.Context,
	projectIdentity string,
) (issueintel.InsightRecord, error) {
	record, ok := s.existing[projectIdentity]
	if !ok {
		return issueintel.InsightRecord{}, sql.ErrNoRows
	}
	return record, nil
}

func TestAnalyzeSemanticProjectsSendsOnlyCandidatesAndExcerpts(t *testing.T) {
	input := semanticTestInput()
	store := &semanticAnalysisTestStore{
		projects: []issueintel.Project{input.Project},
		inputs: map[string]issueintel.SemanticInput{
			input.Project.Identity: input,
		},
		existing: make(map[string]issueintel.InsightRecord),
	}
	runner := func(
		_ context.Context,
		harness SemanticHarness,
		prompt []byte,
		schema []byte,
	) (SemanticHarnessResult, error) {
		if harness != SemanticHarnessClaude ||
			strings.Contains(string(prompt), input.Project.Path) ||
			strings.Contains(string(prompt), input.Issues[0].Fingerprint) ||
			!strings.Contains(string(prompt), "verbatim failure") ||
			!json.Valid(schema) {
			t.Fatalf("unexpected prompt/schema: %s / %s", prompt, schema)
		}
		return SemanticHarnessResult{
			Model: "claude-test",
			Result: issueintel.InsightResult{
				Clusters: []issueintel.InsightCluster{{
					CandidateIDs: []string{"candidate-1"},
					Topic:        "Editing workflow",
					RuleText:     "Inspect the whole file before editing it.",
					TargetFile:   "CLAUDE.md",
					Confidence:   0.9,
				}},
				Fixes: []issueintel.InsightFix{{
					IssueID:    "csi_test",
					RuleText:   "Stop after two identical failures and diagnose the cause.",
					TargetFile: "AGENTS.md",
					Confidence: 0.95,
				}},
			},
		}, nil
	}
	report, err := AnalyzeSemanticProjects(
		context.Background(),
		store,
		SemanticHarnessClaude,
		runner,
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Projects != 1 ||
		report.Clusters != 1 ||
		report.Fixes != 1 ||
		len(store.stored) != 1 ||
		store.stored[0].PromptVersion != InsightPromptVersion ||
		store.stored[0].InputHash == "" ||
		store.stored[0].Model != "claude-test" {
		t.Fatalf("semantic report/store = %+v/%+v", report, store.stored)
	}
}

func TestAnalyzeSemanticProjectsSkipsMatchingProvenance(t *testing.T) {
	input := semanticTestInput()
	_, _, inputHash, err := semanticPrompt(input)
	if err != nil {
		t.Fatal(err)
	}
	store := &semanticAnalysisTestStore{
		projects: []issueintel.Project{input.Project},
		inputs: map[string]issueintel.SemanticInput{
			input.Project.Identity: input,
		},
		existing: map[string]issueintel.InsightRecord{
			input.Project.Identity: {
				Harness:       string(SemanticHarnessCodex),
				PromptVersion: InsightPromptVersion,
				InputHash:     inputHash,
			},
		},
	}
	report, err := AnalyzeSemanticProjects(
		context.Background(),
		store,
		SemanticHarnessCodex,
		func(
			context.Context,
			SemanticHarness,
			[]byte,
			[]byte,
		) (SemanticHarnessResult, error) {
			t.Fatal("matching insight invoked harness")
			return SemanticHarnessResult{}, nil
		},
	)
	if err != nil || report.Skipped != 1 || len(store.stored) != 0 {
		t.Fatalf("skip report/store/error = %+v/%+v/%v", report, store.stored, err)
	}
}

func TestAnalyzeSemanticProjectsRecordsUnknownModelWithoutGuessing(t *testing.T) {
	input := semanticTestInput()
	store := &semanticAnalysisTestStore{
		projects: []issueintel.Project{input.Project},
		inputs: map[string]issueintel.SemanticInput{
			input.Project.Identity: input,
		},
		existing: make(map[string]issueintel.InsightRecord),
	}
	_, err := AnalyzeSemanticProjects(
		context.Background(),
		store,
		SemanticHarnessCodex,
		func(
			context.Context,
			SemanticHarness,
			[]byte,
			[]byte,
		) (SemanticHarnessResult, error) {
			return SemanticHarnessResult{
				Result: issueintel.InsightResult{
					Fixes: []issueintel.InsightFix{{
						IssueID:    "csi_test",
						RuleText:   "Run tests before claiming completion.",
						TargetFile: "AGENTS.md",
						Confidence: 0.8,
					}},
				},
			}, nil
		},
	)
	if err != nil || len(store.stored) != 1 ||
		store.stored[0].Model != "unknown" {
		t.Fatalf("stored insight/error = %+v/%v", store.stored, err)
	}
}

func TestValidateSemanticResultRejectsMissingAndMultilineRules(t *testing.T) {
	input := semanticTestInput()
	if err := validateSemanticResult(
		input,
		issueintel.InsightResult{},
	); err == nil {
		t.Fatal("missing issue fix was accepted")
	}
	if err := validateSemanticResult(
		input,
		issueintel.InsightResult{
			Fixes: []issueintel.InsightFix{{
				IssueID:    "csi_test",
				RuleText:   "first line\nsecond line",
				TargetFile: "AGENTS.md",
				Confidence: 0.9,
			}},
		},
	); err == nil {
		t.Fatal("multiline rule was accepted")
	}
}

func TestDecodeClaudeSemanticStructuredOutput(t *testing.T) {
	body := []byte(`{
		"model":"claude-test",
		"structured_output":{
			"clusters":[],
			"fixes":[{
				"issue_id":"csi_test",
				"rule_text":"Run tests before claiming completion.",
				"target_file":"CLAUDE.md",
				"confidence":0.8
			}]
		}
	}`)
	result, err := decodeClaudeSemanticResult(body)
	if err != nil || result.Model != "claude-test" ||
		len(result.Result.Fixes) != 1 {
		t.Fatalf("decoded Claude result/error = %+v/%v", result, err)
	}
}

func semanticTestInput() issueintel.SemanticInput {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	project := issueintel.Project{
		Identity: "git@example.test:team/project.git",
		Path:     "/private/project/path",
	}
	return issueintel.SemanticInput{
		Project: project,
		Issues: []issueintel.Issue{{
			IssueID:     "csi_test",
			DetectorID:  issueintel.DetectorRetryLoop,
			Fingerprint: "PRIVATE_FINGERPRINT",
			Headline:    "Tests failed three times.",
			Excerpts: []issueintel.Excerpt{
				{
					Citation: issueintel.Citation{
						SessionKey: "ses_a",
						TurnIndex:  1,
						OccurredAt: now,
					},
					Role: transcript.RoleToolCall,
					Text: "go test ./...",
				},
				{
					Citation: issueintel.Citation{
						SessionKey: "ses_a",
						TurnIndex:  2,
						OccurredAt: now,
					},
					Role: transcript.RoleToolResult,
					Text: "verbatim failure",
				},
			},
			SuggestedFix: issueintel.SuggestedFix{
				Kind:       "harness_rule",
				TargetFile: "AGENTS.md",
				Rationale:  "Stop repeated retries.",
			},
		}},
		CorrectionCandidates: []issueintel.CorrectionCandidate{{
			CandidateID: "candidate-1",
			Project:     project,
			Citation: issueintel.Citation{
				SessionKey: "ses_a",
				TurnIndex:  3,
				OccurredAt: now,
			},
			Text:       "No, inspect the whole file first.",
			Marker:     "no",
			OccurredAt: now,
		}},
	}
}
