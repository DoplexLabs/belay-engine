package detection

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
)

func TestBuiltinCatalogIsFixed(t *testing.T) {
	entries := DefaultCatalog().Entries()
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.DetectorID)
		if entry.DetectorVersion != builtinVersion ||
			entry.FingerprintVersion != "1" ||
			entry.Category == "" ||
			entry.TitleCode == "" ||
			entry.SuggestedActionType == "" {
			t.Fatalf("incomplete catalog entry: %+v", entry)
		}
	}
	want := []string{
		"explicit_command_failure",
		"repeated_command_attempts",
		"explicit_permission_denial",
		"verification_not_observed",
		"unresolved_verification_failure_at_completion",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detector order = %v, want %v", got, want)
	}
	if !entries[1].Experimental {
		t.Fatal("repeated command detector must remain experimental")
	}
	for index, entry := range entries {
		if index != 1 && entry.Experimental {
			t.Fatalf("%s unexpectedly experimental", entry.DetectorID)
		}
	}
}

func TestExplicitCommandFailureThresholdPairingAndUnknowns(t *testing.T) {
	tests := []struct {
		name       string
		events     []model.Event
		enrich     func(*SessionInput)
		wantMatch  bool
		severity   string
		confidence string
		citations  int
	}{
		{
			name: "single explicit failure",
			events: []model.Event{
				withOutcome(testEvent("result-1", 1, "command.result"), "failed", nil),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "result-1", "sig-a", CommandClassOther)
			},
			wantMatch:  true,
			severity:   SeverityLow,
			confidence: ConfidenceLow,
			citations:  1,
		},
		{
			name: "three failures increase severity",
			events: []model.Event{
				withOutcome(testEvent("result-1", 1, "command.result"), "failed", nil),
				withOutcome(testEvent("result-2", 2, "command.result"), "failed", nil),
				withOutcome(testEvent("result-3", 3, "command.result"), "failed", nil),
			},
			enrich: func(input *SessionInput) {
				for index := 1; index <= 3; index++ {
					enrichCommand(input, fmt.Sprintf("result-%d", index), "sig-a", CommandClassOther)
				}
			},
			wantMatch:  true,
			severity:   SeverityMedium,
			confidence: ConfidenceLow,
			citations:  3,
		},
		{
			name: "exec and result with tool ID count once",
			events: []model.Event{
				withToolCall(testEvent("exec", 1, "command.exec"), "call-1"),
				withToolCall(
					withOutcome(testEvent("result", 2, "command.result"), "failed", intPointer(1)),
					"call-1",
				),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "exec", "sig-a", CommandClassOther)
				enrichCommand(input, "result", "sig-a", CommandClassOther)
			},
			wantMatch:  true,
			severity:   SeverityLow,
			confidence: ConfidenceHigh,
			citations:  2,
		},
		{
			name: "adjacent exec and result without tool ID count once",
			events: []model.Event{
				testEvent("exec", 1, "command.exec"),
				withOutcome(testEvent("result", 2, "command.result"), "failed", nil),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "exec", "sig-a", CommandClassOther)
				enrichCommand(input, "result", "sig-a", CommandClassOther)
			},
			wantMatch:  true,
			severity:   SeverityLow,
			confidence: ConfidenceHigh,
			citations:  2,
		},
		{
			name: "unknown outcome does not fire",
			events: []model.Event{
				testEvent("result", 1, "command.result"),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "result", "sig-a", CommandClassOther)
			},
		},
		{
			name: "conflicting exit and outcome remains unknown",
			events: []model.Event{
				withOutcome(testEvent("result", 1, "command.result"), "succeeded", intPointer(1)),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "result", "sig-a", CommandClassOther)
			},
		},
		{
			name: "contradictory paired observations remain unknown",
			events: []model.Event{
				withToolCall(
					withOutcome(testEvent("exec", 1, "command.exec"), "failed", nil),
					"call-1",
				),
				withToolCall(
					withOutcome(testEvent("result", 2, "command.result"), "succeeded", nil),
					"call-1",
				),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "exec", "sig-a", CommandClassOther)
				enrichCommand(input, "result", "sig-a", CommandClassOther)
			},
		},
		{
			name: "internally contradictory paired observation remains unknown",
			events: []model.Event{
				withToolCall(
					withOutcome(testEvent("exec", 1, "command.exec"), "succeeded", intPointer(1)),
					"call-1",
				),
				withToolCall(
					withOutcome(testEvent("result", 2, "command.result"), "failed", nil),
					"call-1",
				),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "exec", "sig-a", CommandClassOther)
				enrichCommand(input, "result", "sig-a", CommandClassOther)
			},
		},
		{
			name: "missing signature does not fire",
			events: []model.Event{
				withOutcome(testEvent("result", 1, "command.result"), "failed", nil),
			},
			enrich: func(*SessionInput) {},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := testInput(test.events...)
			test.enrich(&input)
			result := DefaultCatalog().Run(context.Background(), input)
			requireCurrent(t, result)
			match, ok := findMatch(t, result, "explicit_command_failure")
			if ok != test.wantMatch {
				t.Fatalf("match present = %v, want %v; matches = %+v", ok, test.wantMatch, result.Matches)
			}
			if !ok {
				return
			}
			if match.Severity != test.severity ||
				match.Confidence != test.confidence ||
				len(match.CitedEventIDs) != test.citations {
				t.Fatalf("unexpected match: %+v", match)
			}
		})
	}
}

func TestRepeatedCommandAttemptsThresholdAndConfidence(t *testing.T) {
	for _, test := range []struct {
		name           string
		attempts       int
		paired         bool
		wantMatch      bool
		wantConfidence string
	}{
		{name: "below threshold", attempts: 3},
		{name: "four unpaired", attempts: 4, wantMatch: true, wantConfidence: ConfidenceLow},
		{name: "four paired", attempts: 4, paired: true, wantMatch: true, wantConfidence: ConfidenceMedium},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []model.Event
			input := testInput()
			for index := 0; index < test.attempts; index++ {
				if test.paired {
					execID := fmt.Sprintf("exec-%d", index)
					resultID := fmt.Sprintf("result-%d", index)
					toolID := fmt.Sprintf("call-%d", index)
					events = append(
						events,
						withToolCall(testEvent(execID, int64(index*2+1), "command.exec"), toolID),
						withToolCall(testEvent(resultID, int64(index*2+2), "command.result"), toolID),
					)
					enrichCommand(&input, execID, "sig-repeat", CommandClassOther)
					enrichCommand(&input, resultID, "sig-repeat", CommandClassOther)
				} else {
					id := fmt.Sprintf("exec-%d", index)
					events = append(events, testEvent(id, int64(index+1), "command.exec"))
					enrichCommand(&input, id, "sig-repeat", CommandClassOther)
				}
			}
			input.Events = events
			result := DefaultCatalog().Run(context.Background(), input)
			requireCurrent(t, result)
			match, ok := findMatch(t, result, "repeated_command_attempts")
			if ok != test.wantMatch {
				t.Fatalf("match present = %v, want %v; matches = %+v", ok, test.wantMatch, result.Matches)
			}
			if ok && (match.Severity != SeverityInfo ||
				match.Confidence != test.wantConfidence ||
				!match.Experimental) {
				t.Fatalf("unexpected match: %+v", match)
			}
		})
	}
}

func TestRepeatedCommandAttemptsRequireSameSignature(t *testing.T) {
	var events []model.Event
	input := testInput()
	for index := 0; index < 6; index++ {
		id := fmt.Sprintf("exec-%d", index)
		events = append(events, testEvent(id, int64(index+1), "command.exec"))
		enrichCommand(&input, id, fmt.Sprintf("sig-%d", index), CommandClassOther)
	}
	input.Events = events
	result := DefaultCatalog().Run(context.Background(), input)
	requireCurrent(t, result)
	if _, ok := findMatch(t, result, "repeated_command_attempts"); ok {
		t.Fatalf("distinct signatures produced repetition: %+v", result.Matches)
	}
}

func TestReusedToolCallIDsDoNotInflateAttemptThresholds(t *testing.T) {
	input := testInput()
	for index := 0; index < 3; index++ {
		execID := fmt.Sprintf("exec-%d", index)
		resultID := fmt.Sprintf("result-%d", index)
		input.Events = append(
			input.Events,
			withToolCall(testEvent(execID, int64(index+1), "command.exec"), "reused-call"),
			withToolCall(
				withOutcome(
					testEvent(resultID, int64(index+4), "command.result"),
					"failed",
					nil,
				),
				"reused-call",
			),
		)
		enrichCommand(&input, execID, "sig-repeat", CommandClassOther)
		enrichCommand(&input, resultID, "sig-repeat", CommandClassOther)
	}

	result := DefaultCatalog().Run(context.Background(), input)
	requireCurrent(t, result)
	if _, ok := findMatch(t, result, "repeated_command_attempts"); ok {
		t.Fatalf("ambiguous results inflated repetition threshold: %+v", result.Matches)
	}
	failure, ok := findMatch(t, result, "explicit_command_failure")
	if !ok ||
		failure.Severity != SeverityLow ||
		failure.Confidence != ConfidenceLow {
		t.Fatalf("ambiguous failures inflated D1 severity: %+v", result.Matches)
	}
}

func TestResultWithoutCommandInheritsOnlyFromTrustedPair(t *testing.T) {
	tests := []struct {
		name       string
		agent      string
		toolCallID string
		wantTrust  commandAttemptTrust
	}{
		{
			name:       "Codex tool-call pair",
			agent:      "codex",
			toolCallID: "call_codex_123",
			wantTrust:  trustToolCallPair,
		},
		{
			name:      "Claude adjacent no-ID pair",
			agent:     "claude-code",
			wantTrust: trustAdjacentPair,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exec := testEvent("exec", 1, "command.exec")
			exec.Source.Agent = test.agent
			resultEvent := withOutcome(
				testEvent("result", 2, "command.result"),
				"failed",
				intPointer(1),
			)
			resultEvent.Source.Agent = test.agent
			if test.toolCallID != "" {
				exec = withToolCall(exec, test.toolCallID)
				resultEvent = withToolCall(resultEvent, test.toolCallID)
			}
			terminal := testEvent("terminal", 3, "session.end")
			terminal.Source.Agent = test.agent
			terminal.Coverage.Depth = "lifecycle"

			input := testInput(exec, resultEvent, terminal)
			enrichCommand(&input, "exec", "sig-test", CommandClassTest)
			input.Enrichments["result"] = EventEnrichment{
				CommandClass: CommandClassOther,
				Version:      "1",
			}
			session, err := prepareSession(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			attempts, err := commandAttempts(context.Background(), session)
			if err != nil {
				t.Fatal(err)
			}
			if len(attempts) != 1 ||
				attempts[0].signature != "sig-test" ||
				attempts[0].class != CommandClassTest ||
				attempts[0].trust != test.wantTrust ||
				len(attempts[0].events) != 2 {
				t.Fatalf("inherited attempt = %+v", attempts)
			}

			catalogResult := DefaultCatalog().Run(context.Background(), input)
			requireCurrent(t, catalogResult)
			failure, ok := findMatch(
				t,
				catalogResult,
				"explicit_command_failure",
			)
			if !ok || failure.Confidence != ConfidenceHigh {
				t.Fatalf("paired D1 = %+v, present = %v", failure, ok)
			}
			if _, ok := findMatch(
				t,
				catalogResult,
				"unresolved_verification_failure_at_completion",
			); !ok {
				t.Fatalf("paired result did not produce D5: %+v", catalogResult.Matches)
			}
		})
	}
}

func TestUnpairedResultsCannotProduceTrustedClaims(t *testing.T) {
	tests := []struct {
		name   string
		input  SessionInput
		wantD1 bool
	}{
		{
			name: "result without identity",
			input: testInput(
				withOutcome(
					testEvent("result", 1, "command.result"),
					"failed",
					intPointer(1),
				),
				testEvent("terminal", 2, "session.end"),
			),
		},
		{
			name: "standalone signed result",
			input: func() SessionInput {
				input := testInput(
					withOutcome(
						testEvent("result", 1, "command.result"),
						"failed",
						intPointer(1),
					),
					testEvent("terminal", 2, "session.end"),
				)
				enrichCommand(&input, "result", "sig-test", CommandClassTest)
				return input
			}(),
			wantD1: true,
		},
		{
			name: "reused tool-call ID",
			input: func() SessionInput {
				firstExec := withToolCall(
					testEvent("exec-1", 1, "command.exec"),
					"reused",
				)
				secondExec := withToolCall(
					testEvent("exec-2", 2, "command.exec"),
					"reused",
				)
				resultEvent := withToolCall(
					withOutcome(
						testEvent("result", 3, "command.result"),
						"failed",
						intPointer(1),
					),
					"reused",
				)
				input := testInput(
					firstExec,
					secondExec,
					resultEvent,
					testEvent("terminal", 4, "session.end"),
				)
				enrichCommand(&input, "exec-1", "sig-test", CommandClassTest)
				enrichCommand(&input, "exec-2", "sig-test", CommandClassTest)
				return input
			}(),
		},
		{
			name: "non-adjacent no-ID result",
			input: func() SessionInput {
				exec := testEvent("exec", 1, "command.exec")
				unrelated := testEvent("unrelated", 2, "file.read")
				resultEvent := withOutcome(
					testEvent("result", 3, "command.result"),
					"failed",
					intPointer(1),
				)
				input := testInput(
					exec,
					unrelated,
					resultEvent,
					testEvent("terminal", 4, "session.end"),
				)
				enrichCommand(&input, "exec", "sig-test", CommandClassTest)
				return input
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := DefaultCatalog().Run(context.Background(), test.input)
			requireCurrent(t, result)
			d1, ok := findMatch(t, result, "explicit_command_failure")
			if ok != test.wantD1 {
				t.Fatalf("D1 present = %v, want %v; %+v", ok, test.wantD1, result.Matches)
			}
			if ok && d1.Confidence == ConfidenceHigh {
				t.Fatalf("unpaired result produced high-confidence D1: %+v", d1)
			}
			if _, ok := findMatch(
				t,
				result,
				"unresolved_verification_failure_at_completion",
			); ok {
				t.Fatalf("unpaired result produced D5: %+v", result.Matches)
			}
		})
	}
}

func TestExplicitPermissionDenial(t *testing.T) {
	known := testEvent("known", 1, "permission.denied")
	unknown := testEvent("unknown", 2, "permission.denied")
	input := testInput(known, unknown)
	input.Enrichments["known"] = EventEnrichment{PermissionClass: "filesystem.write"}

	result := DefaultCatalog().Run(context.Background(), input)
	requireCurrent(t, result)
	var matches []Match
	for _, match := range result.Matches {
		if match.DetectorID == "explicit_permission_denial" {
			matches = append(matches, match)
		}
	}
	if len(matches) != 2 {
		t.Fatalf("permission matches = %+v", matches)
	}
	confidenceByClass := make(map[string]string)
	for _, match := range matches {
		for _, dimension := range match.Fingerprint {
			if dimension.Name == "permission_class" {
				confidenceByClass[dimension.Value] = match.Confidence
			}
		}
	}
	if confidenceByClass["filesystem.write"] != ConfidenceHigh ||
		confidenceByClass["permission.unknown"] != ConfidenceMedium {
		t.Fatalf("confidence by class = %v", confidenceByClass)
	}
}

func TestVerificationGapExactCompatibilityMatrix(t *testing.T) {
	base := func(agent string) SessionInput {
		command := testEvent("command", 1, "command.exec")
		command.Source.Agent = agent
		mutation := testEvent("mutation", 2, "file.write")
		mutation.Source.Agent = agent
		terminal := testEvent("terminal", 3, "session.end")
		terminal.Source.Agent = agent
		terminal.Coverage.Depth = "lifecycle"
		input := testInput(command, mutation, terminal)
		enrichCommand(&input, "command", "sig-other", CommandClassOther)
		return input
	}

	for _, agent := range []string{"codex", "claude-code"} {
		t.Run("enabled "+agent, func(t *testing.T) {
			result := DefaultCatalog().Run(context.Background(), base(agent))
			requireCurrent(t, result)
			match, ok := findMatch(t, result, "verification_not_observed")
			if !ok || match.Severity != SeverityInfo || match.Confidence != ConfidenceMedium {
				t.Fatalf("D4 match = %+v, present = %v", match, ok)
			}
		})
	}

	tests := []struct {
		name   string
		mutate func(*SessionInput)
	}{
		{
			name: "unsupported harness",
			mutate: func(input *SessionInput) {
				for index := range input.Events {
					input.Events[index].Source.Agent = "other"
				}
			},
		},
		{
			name: "historical",
			mutate: func(input *SessionInput) {
				for index := range input.Events {
					input.Events[index].Historical.IsHistorical = true
					input.Events[index].Source.Kind = "artifact"
				}
			},
		},
		{
			name: "mixed history",
			mutate: func(input *SessionInput) {
				input.Events[0].Historical.IsHistorical = true
				input.Events[0].Source.Kind = "artifact"
			},
		},
		{
			name: "mixed harness",
			mutate: func(input *SessionInput) {
				input.Events[0].Source.Agent = "claude-code"
			},
		},
		{
			name: "missing command coverage",
			mutate: func(input *SessionInput) {
				input.Events = input.Events[1:]
			},
		},
		{
			name: "missing mutation coverage",
			mutate: func(input *SessionInput) {
				input.Events[1].Coverage.Depth = "lifecycle"
			},
		},
		{
			name: "missing terminal coverage",
			mutate: func(input *SessionInput) {
				input.Events[2].Coverage.Depth = "tool_call"
			},
		},
		{
			name: "terminal before mutation",
			mutate: func(input *SessionInput) {
				input.Events[1].Source.Sequence = 4
			},
		},
	}
	for _, test := range tests {
		t.Run("disabled "+test.name, func(t *testing.T) {
			input := base("codex")
			test.mutate(&input)
			result := DefaultCatalog().Run(context.Background(), input)
			requireCurrent(t, result)
			if _, ok := findMatch(t, result, "verification_not_observed"); ok {
				t.Fatalf("D4 fired outside matrix: %+v", result.Matches)
			}
		})
	}
}

func TestVerificationAfterFinalMutationSuppressesGap(t *testing.T) {
	for _, eventType := range []string{"command.exec", "command.result"} {
		t.Run(eventType, func(t *testing.T) {
			commandCoverage := testEvent("command-coverage", 1, "command.exec")
			mutation := testEvent("mutation", 2, "file.write")
			verification := testEvent("verification", 3, eventType)
			terminal := testEvent("terminal", 4, "session.end")
			terminal.Coverage.Depth = "lifecycle"
			input := testInput(commandCoverage, mutation, verification, terminal)
			enrichCommand(&input, "command-coverage", "sig-other", CommandClassOther)
			enrichCommand(&input, "verification", "sig-test", CommandClassTest)
			result := DefaultCatalog().Run(context.Background(), input)
			requireCurrent(t, result)
			if _, ok := findMatch(t, result, "verification_not_observed"); ok {
				t.Fatalf("observed verification did not suppress D4: %+v", result.Matches)
			}
		})
	}
}

func TestVerificationGapDisabledWhenCoalescedEvidenceIncludesHistory(t *testing.T) {
	commandHook := testEvent("command-hook", 1, "command.exec")
	commandArtifact := commandHook
	commandArtifact.EventID = "command-artifact"
	commandArtifact.Source.Kind = "artifact"
	commandArtifact.Source.Sequence = 2
	commandArtifact.Historical.IsHistorical = true
	commandArtifact.Coverage.Depth = "artifact"
	mutation := testEvent("mutation", 3, "file.write")
	terminal := testEvent("terminal", 4, "session.end")
	terminal.Coverage.Depth = "lifecycle"
	input := testInput(commandHook, commandArtifact, mutation, terminal)
	enrichCommand(&input, "command-hook", "sig-other", CommandClassOther)
	enrichCommand(&input, "command-artifact", "sig-other", CommandClassOther)

	session, err := prepareSession(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.events) != 3 || !session.events[0].historical || !session.events[0].live {
		t.Fatalf("coalesced provenance not retained: %+v", session.events)
	}
	result := DefaultCatalog().Run(context.Background(), input)
	requireCurrent(t, result)
	if _, ok := findMatch(t, result, "verification_not_observed"); ok {
		t.Fatalf("D4 fired for mixed historical/live evidence: %+v", result.Matches)
	}
}

func TestUnresolvedVerificationFailureAtCompletion(t *testing.T) {
	tests := []struct {
		name      string
		events    []model.Event
		enrich    func(*SessionInput)
		wantMatch bool
	}{
		{
			name: "failed verification followed by terminal",
			events: []model.Event{
				withToolCall(testEvent("exec", 1, "command.exec"), "call-1"),
				withToolCall(
					withOutcome(testEvent("failed", 2, "command.result"), "failed", nil),
					"call-1",
				),
				func() model.Event {
					event := testEvent("terminal", 3, "session.end")
					event.Coverage.Depth = "lifecycle"
					return event
				}(),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "exec", "sig-test", CommandClassTest)
			},
			wantMatch: true,
		},
		{
			name: "later same signature success resolves",
			events: []model.Event{
				withToolCall(testEvent("exec-fail", 1, "command.exec"), "call-1"),
				withToolCall(
					withOutcome(testEvent("failed", 2, "command.result"), "failed", nil),
					"call-1",
				),
				withToolCall(testEvent("exec-pass", 3, "command.exec"), "call-2"),
				withToolCall(
					withOutcome(testEvent("passed", 4, "command.result"), "succeeded", nil),
					"call-2",
				),
				testEvent("terminal", 5, "session.end"),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "exec-fail", "sig-test", CommandClassTest)
				enrichCommand(input, "exec-pass", "sig-test", CommandClassTest)
			},
		},
		{
			name: "different signature success does not resolve",
			events: []model.Event{
				withToolCall(testEvent("exec-fail", 1, "command.exec"), "call-1"),
				withToolCall(
					withOutcome(testEvent("failed", 2, "command.result"), "failed", nil),
					"call-1",
				),
				withToolCall(testEvent("exec-pass", 3, "command.exec"), "call-2"),
				withToolCall(
					withOutcome(testEvent("passed", 4, "command.result"), "succeeded", nil),
					"call-2",
				),
				testEvent("terminal", 5, "session.end"),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "exec-fail", "sig-test-a", CommandClassTest)
				enrichCommand(input, "exec-pass", "sig-test-b", CommandClassTest)
			},
			wantMatch: true,
		},
		{
			name: "no terminal",
			events: []model.Event{
				withOutcome(testEvent("failed", 1, "command.result"), "failed", nil),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "failed", "sig-test", CommandClassTest)
			},
		},
		{
			name: "non verification failure",
			events: []model.Event{
				withOutcome(testEvent("failed", 1, "command.result"), "failed", nil),
				testEvent("terminal", 2, "session.end"),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "failed", "sig-search", CommandClassOther)
			},
		},
		{
			name: "unknown result",
			events: []model.Event{
				testEvent("unknown", 1, "command.result"),
				testEvent("terminal", 2, "session.end"),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "unknown", "sig-test", CommandClassTest)
			},
		},
		{
			name: "conflicting paired command classes",
			events: []model.Event{
				withToolCall(testEvent("exec", 1, "command.exec"), "call-1"),
				withToolCall(
					withOutcome(testEvent("result", 2, "command.result"), "failed", nil),
					"call-1",
				),
				testEvent("terminal", 3, "session.end"),
			},
			enrich: func(input *SessionInput) {
				enrichCommand(input, "exec", "sig-test", CommandClassTest)
				enrichCommand(input, "result", "sig-test", CommandClassOther)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := testInput(test.events...)
			test.enrich(&input)
			result := DefaultCatalog().Run(context.Background(), input)
			requireCurrent(t, result)
			match, ok := findMatch(t, result, "unresolved_verification_failure_at_completion")
			if ok != test.wantMatch {
				t.Fatalf("match present = %v, want %v; matches = %+v", ok, test.wantMatch, result.Matches)
			}
			if ok && (match.Severity != SeverityHigh || match.Confidence != ConfidenceHigh) {
				t.Fatalf("unexpected D5 match: %+v", match)
			}
		})
	}
}

func TestUnresolvedVerificationRequiresResultBeforeTerminal(t *testing.T) {
	exec := withToolCall(testEvent("exec", 1, "command.exec"), "call-1")
	terminal := testEvent("terminal", 2, "session.end")
	terminal.Coverage.Depth = "lifecycle"
	resultEvent := withToolCall(
		withOutcome(testEvent("result", 3, "command.result"), "failed", intPointer(1)),
		"call-1",
	)
	input := testInput(exec, terminal, resultEvent)
	enrichCommand(&input, "exec", "sig-test", CommandClassTest)

	result := DefaultCatalog().Run(context.Background(), input)
	requireCurrent(t, result)
	if _, ok := findMatch(t, result, "explicit_command_failure"); !ok {
		t.Fatalf("paired failure itself was not detected: %+v", result.Matches)
	}
	if _, ok := findMatch(
		t,
		result,
		"unresolved_verification_failure_at_completion",
	); ok {
		t.Fatalf("result after terminal produced D5: %+v", result.Matches)
	}
}

func TestNegativeCorpusDoesNotOverclaim(t *testing.T) {
	tests := []struct {
		name      string
		input     SessionInput
		forbidden []string
	}{
		{
			name: "TDD failure followed by pass",
			input: func() SessionInput {
				input := testInput(
					withOutcome(testEvent("fail", 1, "command.result"), "failed", nil),
					withOutcome(testEvent("pass", 2, "command.result"), "succeeded", nil),
					testEvent("terminal", 3, "session.end"),
				)
				enrichCommand(&input, "fail", "sig-test", CommandClassTest)
				enrichCommand(&input, "pass", "sig-test", CommandClassTest)
				return input
			}(),
			forbidden: []string{"unresolved_verification_failure_at_completion"},
		},
		{
			name: "search miss is not unresolved verification",
			input: func() SessionInput {
				input := testInput(
					withOutcome(testEvent("grep", 1, "command.result"), "failed", intPointer(1)),
					testEvent("terminal", 2, "session.end"),
				)
				enrichCommand(&input, "grep", "sig-grep", CommandClassOther)
				return input
			}(),
			forbidden: []string{"unresolved_verification_failure_at_completion"},
		},
		{
			name: "file mutation without complete matrix is not evidence gap",
			input: testInput(
				testEvent("mutation", 1, "file.write"),
				testEvent("terminal", 2, "session.end"),
			),
			forbidden: []string{"verification_not_observed"},
		},
		{
			name: "deliberate permission denial is only attention signal",
			input: testInput(
				testEvent("denied", 1, "permission.denied"),
			),
			forbidden: []string{
				"explicit_command_failure",
				"verification_not_observed",
				"unresolved_verification_failure_at_completion",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := DefaultCatalog().Run(context.Background(), test.input)
			requireCurrent(t, result)
			for _, detectorID := range test.forbidden {
				if _, ok := findMatch(t, result, detectorID); ok {
					t.Fatalf("%s overclaimed: %+v", detectorID, result.Matches)
				}
			}
		})
	}
}
