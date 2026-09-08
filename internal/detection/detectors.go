package detection

import (
	"context"
	"sort"
	"strings"
	"time"
)

const builtinVersion = "1.0.0"

type builtinDetector struct {
	entry    CatalogEntry
	evaluate func(context.Context, preparedSession) ([]Match, error)
}

func (d builtinDetector) ID() string                 { return d.entry.DetectorID }
func (d builtinDetector) Version() string            { return d.entry.DetectorVersion }
func (d builtinDetector) FingerprintVersion() string { return d.entry.FingerprintVersion }

func (d builtinDetector) Evaluate(ctx context.Context, input SessionInput) ([]Match, error) {
	session, err := prepareSession(ctx, input)
	if err != nil {
		return nil, err
	}
	return d.evaluate(ctx, session)
}

func (d builtinDetector) evaluatePrepared(
	ctx context.Context,
	session preparedSession,
) ([]Match, error) {
	return d.evaluate(ctx, session)
}

func BuiltinDetectors() []Detector {
	return []Detector{
		newBuiltin(
			"explicit_command_failure",
			"command_failure",
			"issue.explicit_command_failure",
			"inspect_command_failure",
			false,
			detectExplicitCommandFailures,
		),
		newBuiltin(
			"repeated_command_attempts",
			"attention",
			"issue.repeated_command_attempts",
			"inspect_repeated_attempts",
			true,
			detectRepeatedCommandAttempts,
		),
		newBuiltin(
			"explicit_permission_denial",
			"permission",
			"issue.explicit_permission_denial",
			"review_permission_boundary",
			false,
			detectPermissionDenials,
		),
		newBuiltin(
			"verification_not_observed",
			"evidence_gap",
			"issue.verification_not_observed",
			"add_verification",
			false,
			detectVerificationGap,
		),
		newBuiltin(
			"unresolved_verification_failure_at_completion",
			"unresolved_verification",
			"issue.unresolved_verification_failure_at_completion",
			"rerun_verification",
			false,
			detectUnresolvedVerification,
		),
	}
}

func newBuiltin(
	id string,
	category string,
	titleCode string,
	action string,
	experimental bool,
	evaluate func(context.Context, preparedSession) ([]Match, error),
) builtinDetector {
	return builtinDetector{
		entry: CatalogEntry{
			DetectorID:          id,
			DetectorVersion:     builtinVersion,
			FingerprintVersion:  "1",
			Category:            category,
			TitleCode:           titleCode,
			SuggestedActionType: action,
			Experimental:        experimental,
		},
		evaluate: evaluate,
	}
}

func detectExplicitCommandFailures(
	ctx context.Context,
	session preparedSession,
) ([]Match, error) {
	attempts, err := commandAttempts(ctx, session)
	if err != nil {
		return nil, err
	}
	bySignature := make(map[string][]commandAttempt)
	for _, attempt := range attempts {
		if attempt.outcome == outcomeFailed {
			bySignature[attempt.signature] = append(bySignature[attempt.signature], attempt)
		}
	}
	signatures := sortedKeys(bySignature)
	matches := make([]Match, 0, len(signatures))
	for index, signature := range signatures {
		if err := contextCheck(ctx, index); err != nil {
			return nil, err
		}
		failed := bySignature[signature]
		failedCount := 0
		var evidence []preparedEvent
		confidence := ConfidenceHigh
		for _, attempt := range failed {
			evidence = append(evidence, attempt.events...)
			if attempt.repetitionEligible {
				failedCount++
			}
			if attempt.trust.failureConfidence() != ConfidenceHigh {
				confidence = ConfidenceLow
			}
		}
		if failedCount == 0 {
			failedCount = 1
		}
		first, last := attemptBounds(failed)
		severity := SeverityLow
		if failedCount >= 3 {
			severity = SeverityMedium
		}
		matches = append(matches, builtinMatch(
			session,
			"explicit_command_failure",
			severity,
			confidence,
			first,
			last,
			evidence,
			FingerprintDimension{Name: "command_signature_id", Value: signature},
		))
	}
	return matches, nil
}

func detectRepeatedCommandAttempts(
	ctx context.Context,
	session preparedSession,
) ([]Match, error) {
	attempts, err := commandAttempts(ctx, session)
	if err != nil {
		return nil, err
	}
	bySignature := make(map[string][]commandAttempt)
	for _, attempt := range attempts {
		if !attempt.repetitionEligible {
			continue
		}
		bySignature[attempt.signature] = append(bySignature[attempt.signature], attempt)
	}
	signatures := sortedKeys(bySignature)
	var matches []Match
	for index, signature := range signatures {
		if err := contextCheck(ctx, index); err != nil {
			return nil, err
		}
		repeated := bySignature[signature]
		if len(repeated) < 4 {
			continue
		}
		var evidence []preparedEvent
		allPaired := true
		for _, attempt := range repeated {
			evidence = append(evidence, attempt.events...)
			allPaired = allPaired && attempt.trust.paired()
		}
		confidence := ConfidenceLow
		if allPaired {
			confidence = ConfidenceMedium
		}
		first, last := attemptBounds(repeated)
		matches = append(matches, builtinMatch(
			session,
			"repeated_command_attempts",
			SeverityInfo,
			confidence,
			first,
			last,
			evidence,
			FingerprintDimension{Name: "command_signature_id", Value: signature},
		))
	}
	return matches, nil
}

func detectPermissionDenials(
	ctx context.Context,
	session preparedSession,
) ([]Match, error) {
	byClass := make(map[string][]preparedEvent)
	for index, item := range session.events {
		if err := contextCheck(ctx, index); err != nil {
			return nil, err
		}
		if item.event.Observation.Type != "permission.denied" {
			continue
		}
		class := strings.TrimSpace(item.enrichment.PermissionClass)
		if class == "" {
			class = "permission.unknown"
		}
		byClass[class] = append(byClass[class], item)
	}
	classes := sortedKeys(byClass)
	matches := make([]Match, 0, len(classes))
	for _, class := range classes {
		evidence := byClass[class]
		first, last := eventTimeBounds(evidence)
		confidence := ConfidenceHigh
		if class == "permission.unknown" {
			confidence = ConfidenceMedium
		}
		matches = append(matches, builtinMatch(
			session,
			"explicit_permission_denial",
			SeverityLow,
			confidence,
			first,
			last,
			evidence,
			FingerprintDimension{Name: "permission_class", Value: class},
		))
	}
	return matches, nil
}

func detectVerificationGap(
	ctx context.Context,
	session preparedSession,
) ([]Match, error) {
	if !d4Compatible(session) {
		return nil, nil
	}
	finalMutation := -1
	terminal := -1
	for index, item := range session.events {
		if err := contextCheck(ctx, index); err != nil {
			return nil, err
		}
		switch item.event.Observation.Type {
		case "file.write", "file.delete":
			if item.event.Source.Kind == "hook" && item.event.Coverage.Depth == "tool_call" {
				finalMutation = index
			}
		case "session.end":
			if item.event.Source.Kind == "hook" &&
				item.event.Coverage.Depth == "lifecycle" {
				terminal = index
			}
		}
	}
	if finalMutation < 0 || terminal <= finalMutation {
		return nil, nil
	}
	for index := finalMutation + 1; index < terminal; index++ {
		item := session.events[index]
		if (item.event.Observation.Type == "command.exec" ||
			item.event.Observation.Type == "command.result") &&
			isVerificationClass(normalizedCommandClass(item.enrichment.CommandClass)) {
			return nil, nil
		}
	}
	return []Match{builtinMatch(
		session,
		"verification_not_observed",
		SeverityInfo,
		ConfidenceMedium,
		session.events[finalMutation].event.OccurredAt,
		session.events[terminal].event.OccurredAt,
		[]preparedEvent{session.events[finalMutation], session.events[terminal]},
		FingerprintDimension{Name: "verification_gap", Value: "post_mutation"},
	)}, nil
}

func d4Compatible(session preparedSession) bool {
	harness := ""
	hasMutationCoverage := false
	hasCommandCoverage := false
	hasTerminalCoverage := false
	for _, item := range session.events {
		event := item.event
		if item.historical {
			return false
		}
		if event.Source.Agent != "" {
			if harness == "" {
				harness = event.Source.Agent
			} else if harness != event.Source.Agent {
				return false
			}
		}
		if event.Source.Kind != "hook" {
			continue
		}
		switch event.Observation.Type {
		case "file.write", "file.delete":
			hasMutationCoverage = hasMutationCoverage || event.Coverage.Depth == "tool_call"
		case "command.exec", "command.result":
			hasCommandCoverage = hasCommandCoverage || event.Coverage.Depth == "tool_call"
		case "session.end":
			hasTerminalCoverage = hasTerminalCoverage || event.Coverage.Depth == "lifecycle"
		}
	}
	return (harness == "codex" || harness == "claude-code") &&
		hasMutationCoverage &&
		hasCommandCoverage &&
		hasTerminalCoverage
}

func detectUnresolvedVerification(
	ctx context.Context,
	session preparedSession,
) ([]Match, error) {
	attempts, err := commandAttempts(ctx, session)
	if err != nil {
		return nil, err
	}
	lastTerminal := -1
	var terminalEvent preparedEvent
	for index, item := range session.events {
		if item.event.Observation.Type == "session.end" {
			lastTerminal = index
			terminalEvent = item
		}
	}
	if lastTerminal < 0 {
		return nil, nil
	}

	bySignature := make(map[string][]commandAttempt)
	for _, attempt := range attempts {
		if attempt.lastPos < lastTerminal &&
			attempt.trust.trustworthyPair() &&
			isVerificationClass(attempt.class) {
			bySignature[attempt.signature] = append(bySignature[attempt.signature], attempt)
		}
	}
	signatures := sortedKeys(bySignature)
	var matches []Match
	for index, signature := range signatures {
		if err := contextCheck(ctx, index); err != nil {
			return nil, err
		}
		series := bySignature[signature]
		var unresolved *commandAttempt
		for attemptIndex := range series {
			attempt := &series[attemptIndex]
			switch attempt.outcome {
			case outcomeFailed:
				unresolved = attempt
			case outcomeSucceeded:
				unresolved = nil
			}
		}
		if unresolved == nil {
			continue
		}
		evidence := append([]preparedEvent(nil), unresolved.events...)
		evidence = append(evidence, terminalEvent)
		matches = append(matches, builtinMatch(
			session,
			"unresolved_verification_failure_at_completion",
			SeverityHigh,
			ConfidenceHigh,
			unresolved.first,
			terminalEvent.event.OccurredAt,
			evidence,
			FingerprintDimension{Name: "command_class", Value: unresolved.class},
			FingerprintDimension{Name: "command_signature_id", Value: signature},
		))
	}
	return matches, nil
}

func builtinMatch(
	session preparedSession,
	detectorID string,
	severity string,
	confidence string,
	first time.Time,
	last time.Time,
	evidence []preparedEvent,
	fingerprint ...FingerprintDimension,
) Match {
	entry := builtinEntry(detectorID)
	citations, complete := citationIDs(evidence...)
	return Match{
		DetectorID:          entry.DetectorID,
		DetectorVersion:     entry.DetectorVersion,
		FingerprintVersion:  entry.FingerprintVersion,
		Category:            entry.Category,
		TitleCode:           entry.TitleCode,
		SuggestedActionType: entry.SuggestedActionType,
		Severity:            severity,
		Confidence:          confidence,
		Experimental:        entry.Experimental,
		FirstObservedAt:     first,
		LastObservedAt:      last,
		CitedEventIDs:       citations,
		EvidenceComplete:    complete,
		Fingerprint:         dimensions(session, fingerprint...),
	}
}

func builtinEntry(detectorID string) CatalogEntry {
	for _, detector := range BuiltinDetectors() {
		builtin := detector.(builtinDetector)
		if builtin.entry.DetectorID == detectorID {
			return builtin.entry
		}
	}
	return CatalogEntry{}
}

func sortedKeys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func attemptBounds(attempts []commandAttempt) (time.Time, time.Time) {
	if len(attempts) == 0 {
		return time.Time{}, time.Time{}
	}
	first := attempts[0].first
	last := attempts[0].last
	for _, attempt := range attempts[1:] {
		if attempt.first.Before(first) {
			first = attempt.first
		}
		if attempt.last.After(last) {
			last = attempt.last
		}
	}
	return first, last
}
