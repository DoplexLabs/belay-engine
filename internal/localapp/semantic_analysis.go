package localapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/issueintel"
)

const (
	InsightPromptVersion      = "belay.insight-prompt.v1"
	maxSemanticProjects       = 100
	maxSemanticCommandOutput  = 4 << 20
	defaultSemanticRunTimeout = 5 * time.Minute
)

type SemanticHarness string

const (
	SemanticHarnessClaude SemanticHarness = "claude"
	SemanticHarnessCodex  SemanticHarness = "codex"
)

type SemanticInsightStore interface {
	ListInsightProjects(context.Context, int) ([]issueintel.Project, error)
	LoadSemanticInput(
		context.Context,
		issueintel.Project,
	) (issueintel.SemanticInput, error)
	ReplaceProjectInsight(context.Context, issueintel.InsightRecord) error
	GetProjectInsight(
		context.Context,
		string,
	) (issueintel.InsightRecord, error)
}

type SemanticHarnessResult struct {
	Result issueintel.InsightResult
	Model  string
}

type SemanticHarnessRunner func(
	context.Context,
	SemanticHarness,
	[]byte,
	[]byte,
) (SemanticHarnessResult, error)

type SemanticAnalysisReport struct {
	Projects int `json:"projects"`
	Skipped  int `json:"skipped"`
	Clusters int `json:"clusters"`
	Fixes    int `json:"fixes"`
}

func AnalyzeSemanticProjects(
	ctx context.Context,
	store SemanticInsightStore,
	harness SemanticHarness,
	runner SemanticHarnessRunner,
) (SemanticAnalysisReport, error) {
	if store == nil || runner == nil {
		return SemanticAnalysisReport{}, errors.New(
			"semantic analysis requires a store and harness runner",
		)
	}
	if !harness.Valid() {
		return SemanticAnalysisReport{}, errors.New("invalid semantic harness")
	}
	projects, err := store.ListInsightProjects(ctx, maxSemanticProjects)
	if err != nil {
		return SemanticAnalysisReport{}, err
	}
	var report SemanticAnalysisReport
	var analysisErrors []error
	for _, project := range projects {
		if err := ctx.Err(); err != nil {
			return report, errors.Join(append(analysisErrors, err)...)
		}
		input, err := store.LoadSemanticInput(ctx, project)
		if err != nil {
			analysisErrors = append(analysisErrors, err)
			continue
		}
		prompt, schema, inputHash, err := semanticPrompt(input)
		if err != nil {
			analysisErrors = append(analysisErrors, err)
			continue
		}
		existing, existingErr := store.GetProjectInsight(
			ctx,
			project.Identity,
		)
		if existingErr == nil &&
			existing.Harness == string(harness) &&
			existing.PromptVersion == InsightPromptVersion &&
			existing.InputHash == inputHash {
			report.Skipped++
			continue
		}
		result, err := runner(ctx, harness, prompt, schema)
		if err != nil {
			analysisErrors = append(analysisErrors, err)
			continue
		}
		if err := validateSemanticResult(input, result.Result); err != nil {
			analysisErrors = append(analysisErrors, err)
			continue
		}
		generatedAt := time.Now().UTC()
		record := issueintel.InsightRecord{
			InsightID: "ins_" + semanticDigest(
				project.Identity,
				string(harness),
				InsightPromptVersion,
				inputHash,
			),
			Project:       project,
			Harness:       string(harness),
			Model:         semanticModelName(result.Model),
			PromptVersion: InsightPromptVersion,
			InputHash:     inputHash,
			GeneratedAt:   generatedAt,
			Result:        result.Result,
		}
		if err := store.ReplaceProjectInsight(ctx, record); err != nil {
			analysisErrors = append(analysisErrors, err)
			continue
		}
		report.Projects++
		report.Clusters += len(result.Result.Clusters)
		report.Fixes += len(result.Result.Fixes)
	}
	return report, errors.Join(analysisErrors...)
}

func semanticModelName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func (h SemanticHarness) Valid() bool {
	return h == SemanticHarnessClaude || h == SemanticHarnessCodex
}

func InstalledSemanticHarness(preferred string) (SemanticHarness, bool) {
	switch strings.ToLower(strings.TrimSpace(preferred)) {
	case "claude", "claude-code":
		_, err := exec.LookPath("claude")
		return SemanticHarnessClaude, err == nil
	case "codex":
		_, err := exec.LookPath("codex")
		return SemanticHarnessCodex, err == nil
	case "", "auto":
		if _, err := exec.LookPath("claude"); err == nil {
			return SemanticHarnessClaude, true
		}
		if _, err := exec.LookPath("codex"); err == nil {
			return SemanticHarnessCodex, true
		}
	}
	return "", false
}

func RunInstalledSemanticHarness(
	ctx context.Context,
	harness SemanticHarness,
	prompt []byte,
	schema []byte,
) (SemanticHarnessResult, error) {
	if !harness.Valid() {
		return SemanticHarnessResult{}, errors.New("invalid semantic harness")
	}
	runCtx, cancel := context.WithTimeout(ctx, defaultSemanticRunTimeout)
	defer cancel()
	tempDir, err := os.MkdirTemp("", "belay-analyze-*")
	if err != nil {
		return SemanticHarnessResult{}, errors.New(
			"create semantic analysis workspace",
		)
	}
	defer os.RemoveAll(tempDir)
	schemaPath := filepath.Join(tempDir, "schema.json")
	if err := os.WriteFile(schemaPath, schema, 0o600); err != nil {
		return SemanticHarnessResult{}, errors.New(
			"write semantic output schema",
		)
	}
	var name string
	var args []string
	var outputPath string
	switch harness {
	case SemanticHarnessClaude:
		name = "claude"
		args = []string{
			"-p",
			string(prompt),
			"--output-format",
			"json",
			"--json-schema",
			string(schema),
			"--tools",
			"",
			"--no-session-persistence",
		}
	case SemanticHarnessCodex:
		name = "codex"
		outputPath = filepath.Join(tempDir, "result.json")
		args = []string{
			"exec",
			"--json",
			"--ephemeral",
			"--sandbox",
			"read-only",
			"--skip-git-repo-check",
			"--output-schema",
			schemaPath,
			"--output-last-message",
			outputPath,
			string(prompt),
		}
	}
	executable, err := exec.LookPath(name)
	if err != nil {
		return SemanticHarnessResult{}, fmt.Errorf(
			"%s harness is not installed",
			name,
		)
	}
	command := exec.CommandContext(runCtx, executable, args...)
	command.Dir = tempDir
	var stdout, stderr bytes.Buffer
	command.Stdout = &boundedWriter{
		writer: &stdout,
		limit:  maxSemanticCommandOutput,
	}
	command.Stderr = &boundedWriter{
		writer: &stderr,
		limit:  maxSemanticCommandOutput,
	}
	if err := command.Run(); err != nil {
		return SemanticHarnessResult{}, fmt.Errorf(
			"%s semantic analysis failed: %w",
			name,
			err,
		)
	}
	if outputPath != "" {
		body, err := os.ReadFile(outputPath)
		if err != nil {
			return SemanticHarnessResult{}, errors.New(
				"read Codex semantic output",
			)
		}
		return decodeSemanticResult(body, "")
	}
	return decodeClaudeSemanticResult(stdout.Bytes())
}

type semanticPromptPayload struct {
	Issues     []semanticPromptIssue     `json:"issues"`
	Candidates []semanticPromptCandidate `json:"correction_candidates"`
}

type semanticPromptIssue struct {
	IssueID      string                  `json:"issue_id"`
	DetectorID   string                  `json:"detector_id"`
	Headline     string                  `json:"headline"`
	SuggestedFix issueintel.SuggestedFix `json:"suggested_fix"`
	Excerpts     []issueintel.Excerpt    `json:"excerpts"`
}

type semanticPromptCandidate struct {
	CandidateID string              `json:"candidate_id"`
	Text        string              `json:"text"`
	Citation    issueintel.Citation `json:"citation"`
}

func semanticPrompt(
	input issueintel.SemanticInput,
) ([]byte, []byte, string, error) {
	payload := semanticPromptPayload{
		Issues: make([]semanticPromptIssue, 0, len(input.Issues)),
		Candidates: make(
			[]semanticPromptCandidate,
			0,
			len(input.CorrectionCandidates),
		),
	}
	for _, issue := range input.Issues {
		payload.Issues = append(payload.Issues, semanticPromptIssue{
			IssueID:      issue.IssueID,
			DetectorID:   issue.DetectorID,
			Headline:     issue.Headline,
			SuggestedFix: issue.SuggestedFix,
			Excerpts:     issue.Excerpts,
		})
	}
	for _, candidate := range input.CorrectionCandidates {
		payload.Candidates = append(
			payload.Candidates,
			semanticPromptCandidate{
				CandidateID: candidate.CandidateID,
				Text:        candidate.Text,
				Citation:    candidate.Citation,
			},
		)
	}
	sort.Slice(payload.Issues, func(i, j int) bool {
		return payload.Issues[i].IssueID < payload.Issues[j].IssueID
	})
	sort.Slice(payload.Candidates, func(i, j int) bool {
		return payload.Candidates[i].CandidateID <
			payload.Candidates[j].CandidateID
	})
	inputBody, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, "", errors.New("encode semantic candidates")
	}
	schema, err := semanticOutputSchema(payload)
	if err != nil {
		return nil, nil, "", err
	}
	prompt := append([]byte(
		"You are refining deterministic Belay Local findings. "+
			"Do not use tools, inspect files, or add facts. "+
			"Use only the JSON candidates and verbatim excerpts below. "+
			"Cluster correction candidates by durable rule topic. "+
			"Return exactly one concise, single-line fix rule for every issue. "+
			"Target only CLAUDE.md, AGENTS.md, .claude/settings.json, "+
			"or .codex/rules/default.rules. Return only schema-valid JSON.\n\n",
	), inputBody...)
	sum := sha256.Sum256(inputBody)
	return prompt, schema, hex.EncodeToString(sum[:]), nil
}

func semanticOutputSchema(
	payload semanticPromptPayload,
) ([]byte, error) {
	issueIDs := make([]string, 0, len(payload.Issues))
	for _, issue := range payload.Issues {
		issueIDs = append(issueIDs, issue.IssueID)
	}
	candidateIDs := make([]string, 0, len(payload.Candidates))
	for _, candidate := range payload.Candidates {
		candidateIDs = append(candidateIDs, candidate.CandidateID)
	}
	schema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"clusters", "fixes"},
		"properties": map[string]any{
			"clusters": map[string]any{
				"type":     "array",
				"maxItems": len(candidateIDs),
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required": []string{
						"candidate_ids",
						"topic",
						"rule_text",
						"target_file",
						"confidence",
					},
					"properties": map[string]any{
						"candidate_ids": map[string]any{
							"type":        "array",
							"minItems":    1,
							"uniqueItems": true,
							"items": map[string]any{
								"type": "string",
								"enum": candidateIDs,
							},
						},
						"topic":       insightLineSchema(120),
						"rule_text":   insightLineSchema(500),
						"target_file": insightTargetSchema(),
						"confidence": map[string]any{
							"type":    "number",
							"minimum": 0,
							"maximum": 1,
						},
					},
				},
			},
			"fixes": map[string]any{
				"type":     "array",
				"minItems": len(issueIDs),
				"maxItems": len(issueIDs),
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required": []string{
						"issue_id",
						"rule_text",
						"target_file",
						"confidence",
					},
					"properties": map[string]any{
						"issue_id": map[string]any{
							"type": "string",
							"enum": issueIDs,
						},
						"rule_text":   insightLineSchema(500),
						"target_file": insightTargetSchema(),
						"confidence": map[string]any{
							"type":    "number",
							"minimum": 0,
							"maximum": 1,
						},
					},
				},
			},
		},
	}
	body, err := json.Marshal(schema)
	if err != nil {
		return nil, errors.New("encode semantic output schema")
	}
	return body, nil
}

func insightLineSchema(maximum int) map[string]any {
	return map[string]any{
		"type":      "string",
		"minLength": 1,
		"maxLength": maximum,
		"pattern":   `^[^\r\n]+$`,
	}
}

func insightTargetSchema() map[string]any {
	return map[string]any{
		"type": "string",
		"enum": []string{
			"CLAUDE.md",
			"AGENTS.md",
			".claude/settings.json",
			".codex/rules/default.rules",
		},
	}
}

func decodeClaudeSemanticResult(body []byte) (SemanticHarnessResult, error) {
	var envelope struct {
		Model            string          `json:"model"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		Result           json.RawMessage `json:"result"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return SemanticHarnessResult{}, errors.New(
			"decode Claude semantic response",
		)
	}
	if len(envelope.StructuredOutput) > 0 &&
		string(envelope.StructuredOutput) != "null" {
		return decodeSemanticResult(envelope.StructuredOutput, envelope.Model)
	}
	var text string
	if json.Unmarshal(envelope.Result, &text) == nil {
		return decodeSemanticResult([]byte(text), envelope.Model)
	}
	return decodeSemanticResult(envelope.Result, envelope.Model)
}

func decodeSemanticResult(
	body []byte,
	model string,
) (SemanticHarnessResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var result issueintel.InsightResult
	if err := decoder.Decode(&result); err != nil {
		return SemanticHarnessResult{}, errors.New(
			"decode semantic result",
		)
	}
	if decoder.Decode(&struct{}{}) == nil {
		return SemanticHarnessResult{}, errors.New(
			"semantic result contains trailing JSON",
		)
	}
	return SemanticHarnessResult{
		Result: result,
		Model:  strings.TrimSpace(model),
	}, nil
}

func validateSemanticResult(
	input issueintel.SemanticInput,
	result issueintel.InsightResult,
) error {
	issueIDs := make(map[string]bool, len(input.Issues))
	for _, issue := range input.Issues {
		issueIDs[issue.IssueID] = true
	}
	candidateIDs := make(map[string]bool, len(input.CorrectionCandidates))
	for _, candidate := range input.CorrectionCandidates {
		candidateIDs[candidate.CandidateID] = true
	}
	seenIssues := make(map[string]bool, len(result.Fixes))
	for _, fix := range result.Fixes {
		if !issueIDs[fix.IssueID] ||
			seenIssues[fix.IssueID] ||
			!validInsightLine(fix.RuleText, 500) ||
			!validInsightTarget(fix.TargetFile) ||
			fix.Confidence < 0 ||
			fix.Confidence > 1 {
			return errors.New("semantic result contains an invalid fix")
		}
		seenIssues[fix.IssueID] = true
	}
	if len(seenIssues) != len(issueIDs) {
		return errors.New("semantic result omitted an issue fix")
	}
	seenCandidates := make(map[string]bool)
	for _, cluster := range result.Clusters {
		if len(cluster.CandidateIDs) == 0 ||
			!validInsightLine(cluster.Topic, 120) ||
			!validInsightLine(cluster.RuleText, 500) ||
			!validInsightTarget(cluster.TargetFile) ||
			cluster.Confidence < 0 ||
			cluster.Confidence > 1 {
			return errors.New("semantic result contains an invalid cluster")
		}
		for _, candidateID := range cluster.CandidateIDs {
			if !candidateIDs[candidateID] || seenCandidates[candidateID] {
				return errors.New(
					"semantic result contains an invalid cluster candidate",
				)
			}
			seenCandidates[candidateID] = true
		}
	}
	return nil
}

func validInsightLine(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	return value != "" &&
		len(value) <= maximum &&
		!strings.ContainsAny(value, "\r\n")
}

func validInsightTarget(value string) bool {
	switch strings.TrimSpace(value) {
	case "CLAUDE.md", "AGENTS.md", ".claude/settings.json",
		".codex/rules/default.rules":
		return true
	default:
		return false
	}
}

func semanticDigest(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:16])
}

type boundedWriter struct {
	writer *bytes.Buffer
	limit  int
}

func (w *boundedWriter) Write(body []byte) (int, error) {
	if w.writer.Len()+len(body) > w.limit {
		return 0, errors.New("semantic harness output exceeds safety limit")
	}
	return w.writer.Write(body)
}
