package local

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
	"github.com/DoplexLabs/belay-engine/internal/canonical/sourcecatalog"
)

func TestAttentionFamiliesGroupReviewedNumbatSignalsAndKeepExactBelayIssues(t *testing.T) {
	store := openStorageTestStore(t)
	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	first := addAttentionFamilyOccurrence(
		t, store, "numbat-a", "claude-code", "finding-a", "1.1", base, "low",
	)
	second := addAttentionFamilyOccurrence(
		t, store, "numbat-b", "codex", "finding-b", "1.1", base.Add(time.Minute), "high",
	)
	_ = addAttentionFamilyOccurrence(
		t, store, "unsupported", "codex", "finding-c", "1.2", base.Add(2*time.Minute), "high",
	)
	exact := addBelayAttentionFamilyOccurrence(
		t, store, "belay-a", "codex", base.Add(3*time.Minute),
	)

	page, err := store.QueryAttentionFamilies(context.Background(), model.AttentionFamilyQuery{
		Filter: model.AttentionFamilyFilter{
			AttentionKind: model.AttentionKindIssue,
			Experimental:  model.ExperimentalStable,
		},
		Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 2 || page.HasMore || page.CursorEpoch == "" ||
		page.Snapshot <= 0 || page.RetentionGeneration < 1 {
		t.Fatalf("page = %+v", page)
	}
	var mapped, native model.AttentionFamilySummary
	for _, family := range page.Data {
		switch family.Kind {
		case model.AttentionFamilyKindMappedUpstream:
			mapped = family
		case model.AttentionFamilyKindExactIssue:
			native = family
		}
	}
	if mapped.FamilyID == "" ||
		mapped.MappingKey != sourcecatalog.GuardrailsConfigurationMappingKey ||
		mapped.MappingVersion != "1" ||
		mapped.GroupingVersion != "1" ||
		mapped.Severity != "low" ||
		mapped.SupportingIssueCount != 2 ||
		mapped.OccurrenceCount != 2 ||
		mapped.SessionCount != 2 ||
		len(mapped.Harnesses) != 2 ||
		mapped.Scope.Unscoped != 2 ||
		mapped.RepresentativeIssueID != second.IssueID {
		t.Fatalf("mapped family = %+v first=%+v second=%+v", mapped, first, second)
	}
	if native.RepresentativeIssueID != exact.IssueID ||
		native.SupportingIssueCount != 1 ||
		native.OccurrenceCount != 1 ||
		native.SessionCount != 1 {
		t.Fatalf("exact family = %+v exact=%+v", native, exact)
	}

	members, err := store.QueryAttentionFamilyMembers(
		context.Background(),
		model.AttentionFamilyMemberQuery{
			FamilyID: mapped.FamilyID,
			GroupKey: mapped.GroupKey,
			Filter: model.AttentionFamilyFilter{
				AttentionKind: model.AttentionKindIssue,
				Experimental:  model.ExperimentalStable,
			},
			Limit:               1,
			CursorEpoch:         page.CursorEpoch,
			Snapshot:            page.Snapshot,
			RetentionGeneration: page.RetentionGeneration,
			IssuedAt:            page.IssuedAt,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !members.Found || len(members.Data) != 1 || !members.HasMore ||
		members.Data[0].IssueID != second.IssueID {
		t.Fatalf("first member page = %+v", members)
	}
	next, err := store.QueryAttentionFamilyMembers(
		context.Background(),
		model.AttentionFamilyMemberQuery{
			FamilyID: mapped.FamilyID,
			GroupKey: mapped.GroupKey,
			Filter: model.AttentionFamilyFilter{
				AttentionKind: model.AttentionKindIssue,
				Experimental:  model.ExperimentalStable,
			},
			Limit:               1,
			CursorEpoch:         page.CursorEpoch,
			Snapshot:            page.Snapshot,
			RetentionGeneration: page.RetentionGeneration,
			IssuedAt:            page.IssuedAt,
			Cursor: &model.AttentionFamilyMemberPosition{
				AnalysisStatusRank: analysisStatusRank(members.Data[0].AnalysisStatus),
				LastObserved:       members.Data[0].LastObservedAt,
				IssueID:            members.Data[0].IssueID,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Found || len(next.Data) != 1 || next.HasMore ||
		next.Data[0].IssueID != first.IssueID {
		t.Fatalf("second member page = %+v", next)
	}
}

func TestAttentionFamilyFiltersRecomputeVisibleCountsAndUseMappedSeverity(t *testing.T) {
	store := openStorageTestStore(t)
	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	_ = addAttentionFamilyOccurrence(
		t, store, "old-claude", "claude-code", "finding-old", "1.1", base, "critical",
	)
	recent := addAttentionFamilyOccurrence(
		t, store, "recent-codex", "codex", "finding-recent", "1.1",
		base.Add(time.Hour), "critical",
	)

	after := base.Add(30 * time.Minute)
	page, err := store.QueryAttentionFamilies(context.Background(), model.AttentionFamilyQuery{
		Filter: model.AttentionFamilyFilter{
			Severity:      "low",
			Harness:       "CODEX",
			ObservedAfter: &after,
			AttentionKind: model.AttentionKindIssue,
			Experimental:  model.ExperimentalStable,
		},
		Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 {
		t.Fatalf("filtered families = %+v", page.Data)
	}
	family := page.Data[0]
	if family.Severity != "low" ||
		family.SupportingIssueCount != 1 ||
		family.OccurrenceCount != 1 ||
		family.SessionCount != 1 ||
		family.RepresentativeIssueID != recent.IssueID ||
		len(family.Harnesses) != 1 ||
		family.Harnesses[0] != "codex" {
		t.Fatalf("filtered family = %+v", family)
	}
	page, err = store.QueryAttentionFamilies(context.Background(), model.AttentionFamilyQuery{
		Filter: model.AttentionFamilyFilter{
			Severity:      "critical",
			AttentionKind: model.AttentionKindIssue,
			Experimental:  model.ExperimentalStable,
		},
		Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 0 {
		t.Fatalf("source severity admitted mapped family: %+v", page.Data)
	}
}

func addAttentionFamilyOccurrence(
	t *testing.T,
	store *Store,
	sessionID string,
	harness string,
	findingID string,
	rawRuleVersion string,
	observedAt time.Time,
	sourceSeverity string,
) model.IssueOccurrence {
	t.Helper()
	ctx := context.Background()
	event := storageTestEvent(
		fmt.Sprintf(
			"00000000-0000-7000-8000-%012x",
			uint64(observedAt.UnixNano())&0xffffffffffff,
		),
		sessionID,
		1,
		observedAt,
	)
	event.Source.Agent = harness
	event.Source.RunID = "run-" + sessionID
	event.Observation.Type = "config.agent"
	appendResult, err := store.AppendEventResolved(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordFindingResolved(ctx, Finding{
		FindingID:     findingID,
		SourceRunID:   event.Source.RunID,
		SessionKey:    sessionID,
		DetectedAt:    observedAt,
		RuleID:        sourcecatalog.GuardrailsSourceSignalCode,
		RuleVersion:   rawRuleVersion,
		Severity:      sourceSeverity,
		SourceAgent:   harness,
		Confidence:    "high",
		CitedEventIDs: []string{event.EventID},
	}); err != nil {
		t.Fatal(err)
	}
	fingerprintID, issueID, err := store.DeriveIssueIdentity(
		"1",
		"numbat_finding",
		sessionID,
		"numbat",
		sourcecatalog.GuardrailsSourceSignalCode,
		rawRuleVersion,
		event.Observation.Type,
	)
	if err != nil {
		t.Fatal(err)
	}
	code := sourcecatalog.GuardrailsSourceSignalCode
	occurrence := testIssueOccurrence(
		sessionID,
		harness,
		event.EventID,
		fingerprintID,
		issueID,
		sourceSeverity,
		"high",
		observedAt,
		model.ScopeUnscoped,
	)
	occurrence.OccurrenceID = fmt.Sprintf("occ-%s", sessionID)
	occurrence.Origin = "numbat"
	occurrence.OriginRecordID = findingID
	occurrence.FingerprintVersion = "1"
	occurrence.Provenance = model.DetectorProvenance{
		DetectorID:         "numbat_finding",
		DetectorVersion:    sourcecatalog.OpaqueRuleVersion(rawRuleVersion),
		FingerprintVersion: "1",
		ProjectionVersion:  "1",
	}
	occurrence.Category = "numbat_finding"
	occurrence.TitleCode = "issue.numbat_finding"
	occurrence.SourceSignalCode = &code
	occurrence.Severity = sourceSeverity
	occurrence.Confidence = "high"
	occurrence.ScopeQuality = model.ScopeUnscoped
	occurrence.Evidence.CitedEventIDs = []string{event.EventID}
	occurrence.AnalysisGeneration = appendResult.ReadGeneration
	replaceAttentionFamilyProjection(t, store, sessionID, occurrence)
	return occurrence
}

func addBelayAttentionFamilyOccurrence(
	t *testing.T,
	store *Store,
	sessionID string,
	harness string,
	observedAt time.Time,
) model.IssueOccurrence {
	t.Helper()
	ctx := context.Background()
	event := storageTestEvent(
		fmt.Sprintf(
			"00000000-0000-7000-8000-%012x",
			uint64(observedAt.UnixNano())&0xffffffffffff,
		),
		sessionID,
		1,
		observedAt,
	)
	event.Source.Agent = harness
	event.Source.RunID = "run-" + sessionID
	appendResult, err := store.AppendEventResolved(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	fingerprintID, issueID, err := store.DeriveIssueIdentity(
		"1",
		"explicit_command_failure",
		sessionID,
		"belay",
		"explicit_command_failure",
	)
	if err != nil {
		t.Fatal(err)
	}
	occurrence := testIssueOccurrence(
		sessionID,
		harness,
		event.EventID,
		fingerprintID,
		issueID,
		"high",
		"high",
		observedAt,
		model.ScopeResolved,
	)
	occurrence.OccurrenceID = "occ-" + sessionID
	occurrence.Origin = "belay"
	occurrence.Severity = "high"
	occurrence.ScopeQuality = model.ScopeResolved
	occurrence.AnalysisGeneration = appendResult.ReadGeneration
	replaceAttentionFamilyProjection(t, store, sessionID, occurrence)
	return occurrence
}

func replaceAttentionFamilyProjection(
	t *testing.T,
	store *Store,
	sessionID string,
	occurrence model.IssueOccurrence,
) {
	t.Helper()
	var generation int64
	if err := store.db.QueryRow(`
		SELECT target_generation
		FROM dirty_sessions
		WHERE session_key = ?`,
		sessionID,
	).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	occurrence.AnalysisGeneration = generation
	if _, err := store.ReplaceIssueProjection(
		context.Background(),
		ProjectionReplacement{
			SessionKey:        sessionID,
			Origin:            occurrence.Origin,
			ClaimedGeneration: generation,
			Status:            model.AnalysisCurrent,
			ScopeQuality:      occurrence.ScopeQuality,
			Occurrences:       []model.IssueOccurrence{occurrence},
		},
	); err != nil {
		t.Fatal(err)
	}
}
