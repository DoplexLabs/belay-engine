// Package candidatecompiler deterministically compiles inert experience
// candidates from retained trajectory evidence. It does not generate semantic
// guidance or grant instruction authority.
package candidatecompiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DoplexLabs/belay-engine/internal/detection/transcriptissues"
	"github.com/DoplexLabs/belay-engine/internal/experience"
	"github.com/DoplexLabs/belay-engine/internal/issueintel"
	"github.com/DoplexLabs/belay-engine/internal/trajectory"
	"github.com/DoplexLabs/belay-engine/internal/transcript"
)

const (
	ExtractorVersion         = "belay.experience-candidate.det.v3"
	maxCandidateEvidenceRefs = 32
	maxCandidateOutcomeRefs  = 16
	maxCandidateExcerptBytes = 16 * 1024
	commandScrubbingVersion  = "belay.redaction.v1"
	pendingSemanticGuidance  = "Pending semantic compilation; do not apply as agent guidance."
	pendingSemanticRationale = "Deterministic evidence establishes a candidate family only; semantic guidance has not been generated."
	pendingSemanticScope     = "Applicability has not yet been semantically compiled beyond the cited project evidence."
	observationOnlyVerifier  = "Retain the cited observations for later semantic compilation and user review."
)

type Input struct {
	ProjectIdentity string
	Turns           []transcript.Turn
	Edges           []trajectory.Edge
	Outcomes        []trajectory.Outcome
	ProjectConfigs  map[string]issueintel.ProjectConfig
}

type Result struct {
	Candidates []experience.Candidate
}

type turnKey struct {
	sessionKey string
	turnIndex  int64
}

type compiler struct {
	projectIdentity string
	turns           map[turnKey]transcript.Turn
	ambiguousTurns  map[turnKey]bool
	edges           []trajectory.Edge
	outcomes        []trajectory.Outcome
	projectConfigs  map[string]issueintel.ProjectConfig
	candidates      map[string]experience.Candidate
}

// Compile selects only the three high-confidence deterministic candidate
// families defined by C4-A.
func Compile(input Input) (Result, error) {
	projectIdentity := strings.TrimSpace(input.ProjectIdentity)
	if projectIdentity == "" {
		return Result{}, errors.New("candidate compilation requires a project identity")
	}
	value := compiler{
		projectIdentity: projectIdentity,
		turns:           make(map[turnKey]transcript.Turn),
		ambiguousTurns:  make(map[turnKey]bool),
		projectConfigs:  input.ProjectConfigs,
		candidates:      make(map[string]experience.Candidate),
	}
	for _, turn := range input.Turns {
		key := turnKey{sessionKey: turn.SessionKey, turnIndex: turn.TurnIndex}
		if _, exists := value.turns[key]; exists {
			value.ambiguousTurns[key] = true
			delete(value.turns, key)
			continue
		}
		if !value.ambiguousTurns[key] {
			value.turns[key] = turn
		}
	}
	for _, edge := range input.Edges {
		if edge.ProjectIdentity != projectIdentity {
			continue
		}
		if err := edge.Validate(); err != nil {
			return Result{}, fmt.Errorf("validate candidate trajectory edge: %w", err)
		}
		value.edges = append(value.edges, edge)
	}
	for _, outcome := range input.Outcomes {
		if outcome.ProjectIdentity != projectIdentity {
			continue
		}
		if err := outcome.Validate(); err != nil {
			return Result{}, fmt.Errorf("validate candidate outcome: %w", err)
		}
		value.outcomes = append(value.outcomes, outcome)
	}
	sort.Slice(value.edges, func(i, j int) bool {
		if !value.edges[i].OccurredAt.Equal(value.edges[j].OccurredAt) {
			return value.edges[i].OccurredAt.Before(value.edges[j].OccurredAt)
		}
		return value.edges[i].EdgeID < value.edges[j].EdgeID
	})
	sort.Slice(value.outcomes, func(i, j int) bool {
		if !value.outcomes[i].OccurredAt.Equal(value.outcomes[j].OccurredAt) {
			return value.outcomes[i].OccurredAt.Before(value.outcomes[j].OccurredAt)
		}
		return value.outcomes[i].OutcomeID < value.outcomes[j].OutcomeID
	})

	if err := value.compileCorrections(); err != nil {
		return Result{}, err
	}
	if err := value.compileSuccessfulProcedures(); err != nil {
		return Result{}, err
	}
	if err := value.compileFailedApproaches(); err != nil {
		return Result{}, err
	}

	result := Result{Candidates: make([]experience.Candidate, 0, len(value.candidates))}
	for _, candidate := range value.candidates {
		result.Candidates = append(result.Candidates, candidate)
	}
	sort.Slice(result.Candidates, func(i, j int) bool {
		return result.Candidates[i].CandidateID < result.Candidates[j].CandidateID
	})
	return result, nil
}

func (value *compiler) compileCorrections() error {
	for _, outcome := range value.outcomes {
		if !qualifyingOutcome(
			outcome,
			trajectory.OutcomeCorrection,
			trajectory.ResultObserved,
		) {
			continue
		}
		behavior, behaviorRef, user, userRef, ok := value.correctionTurns(outcome)
		if !ok {
			continue
		}
		userText := strings.TrimSpace(user.Payload.Text)
		if strings.TrimSpace(user.Payload.ParentToolUseID) != "" ||
			transcriptissues.IsMachineGeneratedEnvelope(userText) ||
			transcriptissues.HighConfidenceCorrectionMarker(userText) == "" ||
			len(userText) > maxCandidateExcerptBytes {
			continue
		}
		if !value.hasExactInferenceEdge(
			outcome,
			trajectory.RelationRespondsTo,
			userRef,
			behaviorRef,
		) || !value.hasExactInferenceEdge(
			outcome,
			trajectory.RelationCorrects,
			userRef,
			behaviorRef,
		) {
			continue
		}
		evidence, ok := value.evidenceSet(
			[]trajectory.NodeRef{behaviorRef, userRef},
			[]trajectory.Outcome{outcome},
		)
		if !ok {
			continue
		}
		candidate, err := newCandidate(
			experience.CandidateCorrection,
			value.projectIdentity,
			"The cited user turn explicitly corrected the immediately preceding agent behavior.",
			userText,
			evidence,
			[]string{outcome.OutcomeID},
			pendingProposal(
				experience.ExperiencePreference,
				value.projectIdentity,
				observationVerifier(),
			),
			maxTime(outcome.OccurredAt, behavior.OccurredAt, user.OccurredAt),
		)
		if err != nil {
			return fmt.Errorf("compile correction candidate: %w", err)
		}
		value.candidates[candidate.CandidateID] = candidate
	}
	return nil
}

func (value *compiler) compileSuccessfulProcedures() error {
	for _, outcome := range value.outcomes {
		if !qualifyingOutcome(
			outcome,
			trajectory.OutcomeVerificationPass,
			trajectory.ResultSucceeded,
		) {
			continue
		}
		call, callRef, result, resultRef, ok := value.commandOutcomeTurns(outcome, 0)
		if !ok {
			continue
		}
		commandClass, rawCommand, recognized :=
			transcriptissues.RetainedVerificationCommand(
				call,
				value.projectConfigs[outcome.SessionKey],
			)
		if !recognized {
			continue
		}
		verifies := value.matchingVerificationEdges(outcome, callRef)
		if len(verifies) == 0 {
			continue
		}
		sourceRefs := []trajectory.NodeRef{callRef, resultRef}
		generatedAt := maxTime(outcome.OccurredAt, call.OccurredAt, result.OccurredAt)
		for _, edge := range verifies {
			sourceRefs = append(sourceRefs, edge.SourceRefs...)
			generatedAt = maxTime(generatedAt, edge.OccurredAt)
		}
		evidence, ok := value.evidenceSet(sourceRefs, []trajectory.Outcome{outcome})
		if !ok {
			continue
		}
		candidate, err := newCandidate(
			experience.CandidateSuccessfulProcedure,
			value.projectIdentity,
			"The cited verification command succeeded after the cited file mutations.",
			"",
			evidence,
			[]string{outcome.OutcomeID},
			pendingProposal(
				experience.ExperienceProcedure,
				value.projectIdentity,
				experience.Verifier{
					Kind: experience.VerifierCommandSucceeded,
					Command: &experience.CommandVerifierSpec{
						Command:          rawCommand,
						CommandClass:     commandClass,
						ScrubbingVersion: commandScrubbingVersion,
					},
				},
			),
			generatedAt,
		)
		if err != nil {
			return fmt.Errorf("compile successful-procedure candidate: %w", err)
		}
		value.candidates[candidate.CandidateID] = candidate
	}
	return nil
}

func (value *compiler) compileFailedApproaches() error {
	for _, repair := range value.outcomes {
		if !qualifyingOutcome(
			repair,
			trajectory.OutcomeRepair,
			trajectory.ResultSucceeded,
		) {
			continue
		}
		for _, edge := range value.matchingRepairEdges(repair) {
			if len(edge.SourceRefs) != 4 ||
				!containsNode(edge.SourceRefs, edge.From) ||
				!containsNode(edge.SourceRefs, edge.To) {
				continue
			}
			failureCall, ok := value.turn(edge.To)
			if !ok || failureCall.Role != transcript.RoleToolCall {
				continue
			}
			successCall, ok := value.turn(edge.From)
			if !ok || successCall.Role != transcript.RoleToolCall {
				continue
			}
			failureResult, failureResultRef, ok := value.explicitResult(
				edge.SourceRefs,
				failureCall,
				func(exitCode int) bool { return exitCode != 0 },
			)
			if !ok {
				continue
			}
			successResult, _, ok := value.explicitResult(
				edge.SourceRefs,
				successCall,
				func(exitCode int) bool { return exitCode == 0 },
			)
			if !ok || !failureCall.OccurredAt.Before(successCall.OccurredAt) {
				continue
			}
			failureSignature, failureCommand := transcriptissues.NormalizedCommandSignature(failureCall)
			successSignature, successCommand := transcriptissues.NormalizedCommandSignature(successCall)
			failureFamily, failureFamilyOK := transcriptissues.CommandRepairFamily(failureCall)
			successFamily, successFamilyOK := transcriptissues.CommandRepairFamily(successCall)
			if !failureCommand || !successCommand ||
				!failureFamilyOK || !successFamilyOK ||
				failureFamily != successFamily ||
				failureSignature == successSignature {
				continue
			}
			if !sameNodeSet(edge.SourceRefs, repair.SourceRefs) {
				continue
			}
			outcomes := []trajectory.Outcome{repair}
			for _, candidate := range value.outcomes {
				if qualifyingOutcome(
					candidate,
					trajectory.OutcomeVerificationFail,
					trajectory.ResultFailed,
				) && candidate.SessionKey == repair.SessionKey &&
					containsNode(candidate.SourceRefs, edge.To) &&
					containsNode(candidate.SourceRefs, failureResultRef) {
					outcomes = append(outcomes, candidate)
					break
				}
			}
			evidence, ok := value.evidenceSet(edge.SourceRefs, outcomes)
			if !ok {
				continue
			}
			outcomeRefs := make([]string, 0, len(outcomes))
			for _, outcome := range outcomes {
				outcomeRefs = append(outcomeRefs, outcome.OutcomeID)
			}
			candidate, err := newCandidate(
				experience.CandidateFailedApproach,
				value.projectIdentity,
				"An explicit command failure was followed by a different successful command that repaired it.",
				"",
				evidence,
				outcomeRefs,
				pendingProposal(
					experience.ExperienceWarning,
					value.projectIdentity,
					observationVerifier(),
				),
				maxTime(
					repair.OccurredAt,
					edge.OccurredAt,
					failureCall.OccurredAt,
					failureResult.OccurredAt,
					successCall.OccurredAt,
					successResult.OccurredAt,
				),
			)
			if err != nil {
				return fmt.Errorf("compile failed-approach candidate: %w", err)
			}
			value.candidates[candidate.CandidateID] = candidate
		}
	}
	return nil
}

func (value *compiler) correctionTurns(
	outcome trajectory.Outcome,
) (
	transcript.Turn,
	trajectory.NodeRef,
	transcript.Turn,
	trajectory.NodeRef,
	bool,
) {
	if len(outcome.SourceRefs) != 2 {
		return transcript.Turn{}, trajectory.NodeRef{}, transcript.Turn{}, trajectory.NodeRef{}, false
	}
	var behavior transcript.Turn
	var behaviorRef trajectory.NodeRef
	var user transcript.Turn
	var userRef trajectory.NodeRef
	for _, ref := range outcome.SourceRefs {
		turn, ok := value.turn(ref)
		if !ok || turn.SessionKey != outcome.SessionKey {
			return transcript.Turn{}, trajectory.NodeRef{}, transcript.Turn{}, trajectory.NodeRef{}, false
		}
		switch turn.Role {
		case transcript.RoleUser:
			if userRef.Kind != "" {
				return transcript.Turn{}, trajectory.NodeRef{}, transcript.Turn{}, trajectory.NodeRef{}, false
			}
			user, userRef = turn, ref
		case transcript.RoleAssistant, transcript.RoleToolCall, transcript.RoleToolResult:
			if behaviorRef.Kind != "" {
				return transcript.Turn{}, trajectory.NodeRef{}, transcript.Turn{}, trajectory.NodeRef{}, false
			}
			behavior, behaviorRef = turn, ref
		default:
			return transcript.Turn{}, trajectory.NodeRef{}, transcript.Turn{}, trajectory.NodeRef{}, false
		}
	}
	return behavior, behaviorRef, user, userRef, behaviorRef.Kind != "" && userRef.Kind != ""
}

func (value *compiler) commandOutcomeTurns(
	outcome trajectory.Outcome,
	wantExitCode int,
) (
	transcript.Turn,
	trajectory.NodeRef,
	transcript.Turn,
	trajectory.NodeRef,
	bool,
) {
	if len(outcome.SourceRefs) != 2 {
		return transcript.Turn{}, trajectory.NodeRef{}, transcript.Turn{}, trajectory.NodeRef{}, false
	}
	var call transcript.Turn
	var callRef trajectory.NodeRef
	var result transcript.Turn
	var resultRef trajectory.NodeRef
	for _, ref := range outcome.SourceRefs {
		turn, ok := value.turn(ref)
		if !ok || turn.SessionKey != outcome.SessionKey {
			return transcript.Turn{}, trajectory.NodeRef{}, transcript.Turn{}, trajectory.NodeRef{}, false
		}
		switch turn.Role {
		case transcript.RoleToolCall:
			call, callRef = turn, ref
		case transcript.RoleToolResult:
			result, resultRef = turn, ref
		}
	}
	failed, known := transcriptissues.ExplicitToolResultFailed(result)
	if callRef.Kind == "" || resultRef.Kind == "" ||
		call.Payload.ToolCallID == "" ||
		call.Payload.ToolCallID != result.Payload.ToolCallID ||
		!known ||
		(failed && wantExitCode == 0) ||
		(!failed && wantExitCode != 0) {
		return transcript.Turn{}, trajectory.NodeRef{}, transcript.Turn{}, trajectory.NodeRef{}, false
	}
	return call, callRef, result, resultRef, true
}

func (value *compiler) explicitResult(
	refs []trajectory.NodeRef,
	call transcript.Turn,
	exitMatches func(int) bool,
) (transcript.Turn, trajectory.NodeRef, bool) {
	var match transcript.Turn
	var matchRef trajectory.NodeRef
	count := 0
	for _, ref := range refs {
		turn, ok := value.turn(ref)
		failed, known := transcriptissues.ExplicitToolResultFailed(turn)
		exitCode := 0
		if failed {
			exitCode = 1
		}
		if !ok || turn.Role != transcript.RoleToolResult ||
			call.Payload.ToolCallID == "" ||
			turn.Payload.ToolCallID != call.Payload.ToolCallID ||
			!known ||
			!exitMatches(exitCode) {
			continue
		}
		match, matchRef = turn, ref
		count++
	}
	return match, matchRef, count == 1
}

func (value *compiler) matchingVerificationEdges(
	outcome trajectory.Outcome,
	callRef trajectory.NodeRef,
) []trajectory.Edge {
	result := make([]trajectory.Edge, 0)
	for _, edge := range value.edges {
		if edge.ProjectIdentity != outcome.ProjectIdentity ||
			edge.SessionKey != outcome.SessionKey ||
			edge.Relation != trajectory.RelationVerifies ||
			edge.EvidenceClass != trajectory.EvidenceDeterministicInference ||
			edge.Confidence != trajectory.ConfidenceHigh ||
			edge.DerivationVersion != outcome.DerivationVersion ||
			!sameNode(edge.From, callRef) ||
			(edge.To.Kind != trajectory.NodeCanonicalEvent &&
				edge.To.Kind != trajectory.NodeTranscriptTurn) ||
			!containsNode(edge.SourceRefs, callRef) ||
			!containsNode(edge.SourceRefs, edge.To) {
			continue
		}
		mutationFound := false
		for _, ref := range edge.SourceRefs {
			turn, ok := value.turn(ref)
			if ok && turn.Role == transcript.RoleToolCall &&
				len(transcriptissues.ExtractEditedFiles(turn)) > 0 {
				mutationFound = true
				break
			}
		}
		if mutationFound {
			result = append(result, edge)
		}
	}
	return result
}

func (value *compiler) matchingRepairEdges(
	outcome trajectory.Outcome,
) []trajectory.Edge {
	result := make([]trajectory.Edge, 0)
	for _, edge := range value.edges {
		if edge.ProjectIdentity == outcome.ProjectIdentity &&
			edge.SessionKey == outcome.SessionKey &&
			edge.Relation == trajectory.RelationRepairs &&
			edge.EvidenceClass == trajectory.EvidenceDeterministicInference &&
			edge.Confidence == trajectory.ConfidenceHigh &&
			edge.DerivationVersion == outcome.DerivationVersion &&
			edge.From.Kind == trajectory.NodeTranscriptTurn &&
			edge.To.Kind == trajectory.NodeTranscriptTurn {
			result = append(result, edge)
		}
	}
	return result
}

func (value *compiler) hasExactInferenceEdge(
	outcome trajectory.Outcome,
	relation trajectory.EdgeRelation,
	from trajectory.NodeRef,
	to trajectory.NodeRef,
) bool {
	for _, edge := range value.edges {
		if edge.ProjectIdentity == outcome.ProjectIdentity &&
			edge.SessionKey == outcome.SessionKey &&
			edge.Relation == relation &&
			edge.EvidenceClass == trajectory.EvidenceDeterministicInference &&
			edge.Confidence == trajectory.ConfidenceHigh &&
			edge.DerivationVersion == outcome.DerivationVersion &&
			sameNode(edge.From, from) &&
			sameNode(edge.To, to) &&
			containsNode(edge.SourceRefs, from) &&
			containsNode(edge.SourceRefs, to) {
			return true
		}
	}
	return false
}

func (value *compiler) evidenceSet(
	refs []trajectory.NodeRef,
	outcomes []trajectory.Outcome,
) (experience.EvidenceSet, bool) {
	evidence := make([]experience.EvidenceRef, 0, len(refs)+len(outcomes))
	seen := make(map[string]bool)
	appendRef := func(ref experience.EvidenceRef) {
		if len(evidence) >= maxCandidateEvidenceRefs {
			return
		}
		encoded, _ := json.Marshal(ref)
		key := string(encoded)
		if !seen[key] {
			seen[key] = true
			evidence = append(evidence, ref)
		}
	}
	for _, outcome := range outcomes {
		occurredAt := outcome.OccurredAt.UTC().Round(0)
		appendRef(experience.EvidenceRef{
			Kind:       experience.EvidenceOutcomeObservation,
			OutcomeID:  outcome.OutcomeID,
			OccurredAt: &occurredAt,
		})
	}
	for _, ref := range refs {
		converted, ok := value.evidenceRef(ref)
		if !ok {
			return experience.EvidenceSet{}, false
		}
		appendRef(converted)
	}
	if len(evidence) == 0 {
		return experience.EvidenceSet{}, false
	}
	sort.Slice(evidence, func(i, j int) bool {
		left, _ := json.Marshal(evidence[i])
		right, _ := json.Marshal(evidence[j])
		return string(left) < string(right)
	})
	result := experience.EvidenceSet{
		Availability: experience.EvidenceAvailable,
		Refs:         evidence,
	}
	result.EvidenceSetID = result.DeterministicID()
	return result, result.Validate() == nil
}

func (value *compiler) evidenceRef(
	ref trajectory.NodeRef,
) (experience.EvidenceRef, bool) {
	switch ref.Kind {
	case trajectory.NodeTranscriptTurn:
		turn, ok := value.turn(ref)
		if !ok {
			return experience.EvidenceRef{}, false
		}
		occurredAt := turn.OccurredAt.UTC().Round(0)
		turnIndex := turn.TurnIndex
		return experience.EvidenceRef{
			Kind:       experience.EvidenceTranscriptTurn,
			SessionKey: turn.SessionKey,
			TurnIndex:  &turnIndex,
			OccurredAt: &occurredAt,
			Excerpt:    turnExcerpt(turn),
		}, true
	case trajectory.NodeCanonicalEvent:
		return experience.EvidenceRef{
			Kind:    experience.EvidenceCanonicalEvent,
			EventID: ref.EventID,
		}, true
	case trajectory.NodeOutcome:
		return experience.EvidenceRef{
			Kind:      experience.EvidenceOutcomeObservation,
			OutcomeID: ref.OutcomeID,
		}, true
	default:
		return experience.EvidenceRef{}, false
	}
}

func (value *compiler) turn(ref trajectory.NodeRef) (transcript.Turn, bool) {
	if ref.Kind != trajectory.NodeTranscriptTurn ||
		ref.TurnIndex == nil ||
		value.ambiguousTurns[turnKey{
			sessionKey: ref.SessionKey,
			turnIndex:  *ref.TurnIndex,
		}] {
		return transcript.Turn{}, false
	}
	turn, ok := value.turns[turnKey{
		sessionKey: ref.SessionKey,
		turnIndex:  *ref.TurnIndex,
	}]
	return turn, ok
}

func newCandidate(
	family experience.CandidateFamily,
	projectIdentity string,
	observedBehavior string,
	userFeedback string,
	evidence experience.EvidenceSet,
	outcomeRefs []string,
	proposal experience.ExperienceProposal,
	generatedAt time.Time,
) (experience.Candidate, error) {
	outcomeRefs = normalizedStrings(outcomeRefs, maxCandidateOutcomeRefs)
	generatedAt = generatedAt.UTC().Round(0)
	if generatedAt.IsZero() {
		return experience.Candidate{}, errors.New("candidate evidence has no generation time")
	}
	inputHash := sha256JSON(struct {
		ExtractorVersion string
		Family           experience.CandidateFamily
		ProjectIdentity  string
		Evidence         experience.EvidenceSet
		OutcomeRefs      []string
	}{
		ExtractorVersion: ExtractorVersion,
		Family:           family,
		ProjectIdentity:  projectIdentity,
		Evidence:         evidence,
		OutcomeRefs:      outcomeRefs,
	})
	candidate := experience.Candidate{
		SchemaVersion:    experience.CandidateSchemaVersion,
		Family:           family,
		ProjectIdentity:  projectIdentity,
		ObservedBehavior: observedBehavior,
		UserFeedback:     userFeedback,
		Evidence:         evidence,
		OutcomeRefs:      outcomeRefs,
		Proposal:         proposal,
		Provenance: experience.Provenance{
			ExtractorVersion: ExtractorVersion,
			InputHash:        inputHash,
			GeneratedAt:      generatedAt,
		},
		Authority:      experience.AuthorityNone,
		LifecycleState: experience.LifecycleCandidate,
		CreatedAt:      generatedAt,
	}
	candidate.CandidateID = candidate.DeterministicID()
	if err := candidate.Validate(); err != nil {
		return experience.Candidate{}, err
	}
	return candidate, nil
}

func pendingProposal(
	experienceType experience.ExperienceType,
	projectIdentity string,
	verifier experience.Verifier,
) experience.ExperienceProposal {
	return experience.ExperienceProposal{
		Type: experienceType,
		Scope: experience.Scope{
			Kind:            experience.ScopeProject,
			ProjectIdentity: projectIdentity,
		},
		Applicability: experience.Applicability{
			SemanticDescription: pendingSemanticScope,
		},
		Guidance: experience.Guidance{
			Instruction:          pendingSemanticGuidance,
			Rationale:            pendingSemanticRationale,
			InterventionStrength: experience.InterventionObserve,
		},
		Verifier:                   verifier,
		SemanticCompilationPending: true,
	}
}

func observationVerifier() experience.Verifier {
	return experience.Verifier{
		Kind: experience.VerifierObservationOnly,
		ObservationOnly: &experience.ObservationOnlySpec{
			Explanation: observationOnlyVerifier,
		},
	}
}

func qualifyingOutcome(
	outcome trajectory.Outcome,
	kind trajectory.OutcomeKind,
	result trajectory.OutcomeResult,
) bool {
	return outcome.Kind == kind &&
		outcome.Result == result &&
		outcome.EvidenceClass == trajectory.EvidenceDeterministicInference &&
		outcome.Confidence == trajectory.ConfidenceHigh
}

func sameNode(left, right trajectory.NodeRef) bool {
	if left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case trajectory.NodeTranscriptTurn:
		return left.SessionKey == right.SessionKey &&
			left.TurnIndex != nil &&
			right.TurnIndex != nil &&
			*left.TurnIndex == *right.TurnIndex
	case trajectory.NodeCanonicalEvent:
		return left.EventID == right.EventID
	case trajectory.NodeOutcome:
		return left.OutcomeID == right.OutcomeID
	case trajectory.NodeExperience:
		return left.ExperienceID == right.ExperienceID &&
			left.ExperienceVersion == right.ExperienceVersion
	case trajectory.NodeApplication:
		return left.ApplicationID == right.ApplicationID
	case trajectory.NodeGitObject:
		return left.GitObject == right.GitObject
	default:
		return false
	}
}

func containsNode(refs []trajectory.NodeRef, want trajectory.NodeRef) bool {
	for _, ref := range refs {
		if sameNode(ref, want) {
			return true
		}
	}
	return false
}

func sameNodeSet(left, right []trajectory.NodeRef) bool {
	if len(left) != len(right) {
		return false
	}
	for _, ref := range left {
		if !containsNode(right, ref) {
			return false
		}
	}
	for _, ref := range right {
		if !containsNode(left, ref) {
			return false
		}
	}
	return true
}

func turnExcerpt(turn transcript.Turn) string {
	for _, value := range []string{
		turn.Payload.Text,
		turn.Payload.RawCommand,
		turn.Payload.ToolResult,
		string(turn.Payload.ToolInput),
	} {
		value = strings.TrimSpace(value)
		if value != "" {
			return truncateUTF8(value, maxCandidateExcerptBytes)
		}
	}
	return ""
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func normalizedStrings(values []string, limit int) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func sha256JSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func maxTime(values ...time.Time) time.Time {
	var result time.Time
	for _, value := range values {
		if value.After(result) {
			result = value
		}
	}
	return result
}
