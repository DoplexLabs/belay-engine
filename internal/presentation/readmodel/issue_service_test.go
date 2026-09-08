package readmodel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
)

const testEventID = "01890f2e-6d4b-7c8a-9b0c-123456789abc"

type issueTestCoreRepository struct{}

func (issueTestCoreRepository) QuerySessions(
	context.Context,
	model.SessionQuery,
) (model.SessionPage, error) {
	return model.SessionPage{}, nil
}

func (issueTestCoreRepository) GetSession(
	context.Context,
	string,
) (model.SessionSummary, time.Time, error) {
	return model.SessionSummary{}, time.Time{}, nil
}

func (issueTestCoreRepository) QuerySessionTimeline(
	context.Context,
	model.TimelineQuery,
) (model.EventPage, error) {
	return model.EventPage{}, nil
}

func (issueTestCoreRepository) QueryActivityPage(
	context.Context,
	model.ActivityQuery,
) (model.EventPage, error) {
	return model.EventPage{}, nil
}

func (issueTestCoreRepository) QueryFindings(
	context.Context,
	model.FindingQuery,
) (model.FindingPage, error) {
	return model.FindingPage{}, nil
}

func (issueTestCoreRepository) GetStats(
	context.Context,
) (model.LocalStats, time.Time, error) {
	return model.LocalStats{}, time.Time{}, nil
}

type issueTestRepository struct {
	queryIssues      func(model.IssueQuery) (model.IssuePage, error)
	queryOccurrences func(model.IssueOccurrenceQuery) (model.IssueOccurrencePage, error)
	lookupEvents     func(model.EventLookupQuery) (model.EventLookupResult, error)
}

type issueCapableCoreRepository struct {
	issueTestCoreRepository
	*issueTestRepository
}

func (repository *issueTestRepository) QueryIssues(
	_ context.Context,
	query model.IssueQuery,
) (model.IssuePage, error) {
	if repository.queryIssues == nil {
		return model.IssuePage{}, nil
	}
	return repository.queryIssues(query)
}

func (repository *issueTestRepository) QueryIssueOccurrences(
	_ context.Context,
	query model.IssueOccurrenceQuery,
) (model.IssueOccurrencePage, error) {
	if repository.queryOccurrences == nil {
		return model.IssueOccurrencePage{}, nil
	}
	return repository.queryOccurrences(query)
}

func (repository *issueTestRepository) LookupSessionEvents(
	_ context.Context,
	query model.EventLookupQuery,
) (model.EventLookupResult, error) {
	if repository.lookupEvents == nil {
		return model.EventLookupResult{}, nil
	}
	return repository.lookupEvents(query)
}

func TestIssueListNormalizesAndPreservesCursorSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	issueID := testIssueID("a")
	fingerprintID := testFingerprintID("b")
	observedAfter := now.Add(-time.Hour).In(time.FixedZone("offset", 2*60*60))
	var queries []model.IssueQuery
	repository := &issueTestRepository{}
	repository.queryIssues = func(query model.IssueQuery) (model.IssuePage, error) {
		queries = append(queries, query)
		if len(queries) == 1 {
			return model.IssuePage{
				Data: []model.IssueSummary{{
					IssueID:          issueID,
					Severity:         "high",
					SessionCount:     2,
					LastObservedAt:   now.Add(-time.Minute),
					FingerprintID:    fingerprintID,
					AnalysisStatus:   model.AnalysisCurrent,
					EvidenceComplete: true,
				}},
				Analysis: model.IssueAnalysisCoverage{CurrentSessions: 3, Complete: true},
				Snapshot: 17,
				HasMore:  true,
			}, nil
		}
		return model.IssuePage{
			Data: []model.IssueSummary{{
				IssueID:        testIssueID("c"),
				Severity:       "medium",
				SessionCount:   1,
				LastObservedAt: now.Add(-2 * time.Minute),
			}},
			Snapshot: 17,
		}, nil
	}
	service := New(
		issueTestCoreRepository{},
		WithIssueRepository(repository),
		WithClock(func() time.Time { return now }),
	)
	request := IssueListRequest{
		Limit:          1,
		Severity:       " HIGH ",
		Category:       " COMMAND_FAILURE ",
		Harness:        " Codex ",
		Origin:         " BELAY ",
		AnalysisStatus: " CURRENT ",
		ObservedAfter:  &observedAfter,
		Recurrence:     " REPEATED ",
		SessionID:      " session-1 ",
		FingerprintID:  " " + strings.ToUpper(fingerprintID) + " ",
		AttentionKind:  " ISSUE ",
		Experimental:   " INCLUDE ",
	}
	first, err := service.ListIssues(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProjectionVersion != IssueProjectionVersion ||
		first.NextCursor == nil ||
		first.ViewCursor == "" ||
		!first.HasMore ||
		first.ReturnedCount != 1 ||
		first.Limit != 1 {
		t.Fatalf("first issue page metadata = %+v", first)
	}
	if first.Data[0].Harnesses == nil {
		t.Fatal("issue summary harnesses must be a non-nil slice")
	}
	query := queries[0]
	if query.Limit != 1 ||
		query.Snapshot != 0 ||
		query.Filter.Severity != "high" ||
		query.Filter.Category != "command_failure" ||
		query.Filter.Harness != "Codex" ||
		query.Filter.Origin != "belay" ||
		query.Filter.AnalysisStatus != model.AnalysisCurrent ||
		query.Filter.ObservedAfter == nil ||
		!query.Filter.ObservedAfter.Equal(observedAfter.UTC()) ||
		query.Filter.Recurrence != "repeated" ||
		query.Filter.SessionID != "session-1" ||
		query.Filter.FingerprintID != fingerprintID ||
		query.Filter.AttentionKind != model.AttentionKindIssue ||
		query.Filter.Experimental != model.ExperimentalInclude {
		t.Fatalf("normalized issue query = %+v", query)
	}

	now = now.Add(5 * time.Minute)
	request.Cursor = *first.NextCursor
	second, err := service.ListIssues(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.HasMore || second.NextCursor != nil {
		t.Fatalf("second issue page metadata = %+v", second)
	}
	query = queries[1]
	if query.Snapshot != 17 ||
		!query.IssuedAt.Equal(time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)) ||
		query.Cursor == nil ||
		query.Cursor.IssueID != issueID ||
		query.Cursor.SeverityRank != 4 ||
		!query.Cursor.Repeated {
		t.Fatalf("continued issue query = %+v", query)
	}
	view, err := decodeViewCursor(second.ViewCursor, now)
	if err != nil {
		t.Fatal(err)
	}
	if view.Snapshot != 17 ||
		view.IssuedAt != "2026-09-08T12:00:00Z" {
		t.Fatalf("continued view cursor = %+v", view)
	}

	request.Category = "different"
	if _, err := service.ListIssues(context.Background(), request); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("changed-filter cursor error = %v, want ErrInvalidCursor", err)
	}
	if len(queries) != 2 {
		t.Fatalf("repository called after cursor/filter mismatch: %d calls", len(queries))
	}
}

func TestIssueDetailUsesViewSnapshotAndIssueBoundOccurrenceCursor(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	issueID := testIssueID("d")
	otherIssueID := testIssueID("e")
	var summaryQueries []model.IssueQuery
	var occurrenceQueries []model.IssueOccurrenceQuery
	repository := &issueTestRepository{}
	repository.queryIssues = func(query model.IssueQuery) (model.IssuePage, error) {
		summaryQueries = append(summaryQueries, query)
		return model.IssuePage{
			Data: []model.IssueSummary{{
				IssueID:        issueID,
				Severity:       "medium",
				SessionCount:   2,
				LastObservedAt: now.Add(-time.Minute),
			}},
			Snapshot: 23,
		}, nil
	}
	repository.queryOccurrences = func(query model.IssueOccurrenceQuery) (model.IssueOccurrencePage, error) {
		occurrenceQueries = append(occurrenceQueries, query)
		return model.IssueOccurrencePage{
			Data: []model.IssueOccurrence{{
				OccurrenceID:   "occurrence-1",
				IssueID:        issueID,
				LastObservedAt: now.Add(-time.Minute),
			}},
			Snapshot: 23,
			HasMore:  len(occurrenceQueries) == 1,
		}, nil
	}
	service := New(
		issueTestCoreRepository{},
		WithIssueRepository(repository),
		WithClock(func() time.Time { return now }),
	)
	viewCursor, err := encodeCursor(cursorEnvelope{
		Version:     cursorVersion,
		Kind:        "issue_view",
		Snapshot:    23,
		Fingerprint: issueViewFingerprint(),
		IssuedAt:    now.Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.GetIssue(context.Background(), IssueDetailRequest{
		IssueID:    strings.ToUpper(issueID),
		Limit:      1,
		ViewCursor: viewCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor == nil ||
		!first.HasMore ||
		first.Data.Occurrences == nil ||
		first.Data.Occurrences[0].Evidence.CitedEventIDs == nil {
		t.Fatalf("first issue detail = %+v", first)
	}
	if summaryQueries[0].Snapshot != 23 ||
		summaryQueries[0].Filter.AttentionKind != model.AttentionKindAll ||
		summaryQueries[0].Filter.Experimental != model.ExperimentalInclude ||
		occurrenceQueries[0].Snapshot != 23 ||
		!occurrenceQueries[0].IssuedAt.Equal(now) {
		t.Fatalf("view-transferred queries = summary %+v occurrence %+v",
			summaryQueries[0], occurrenceQueries[0])
	}

	now = now.Add(2 * time.Minute)
	second, err := service.GetIssue(context.Background(), IssueDetailRequest{
		IssueID: issueID,
		Limit:   1,
		Cursor:  *first.NextCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.HasMore || second.NextCursor != nil {
		t.Fatalf("second issue detail = %+v", second)
	}
	if occurrenceQueries[1].Snapshot != 23 ||
		!occurrenceQueries[1].IssuedAt.Equal(time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)) ||
		occurrenceQueries[1].Cursor == nil ||
		occurrenceQueries[1].Cursor.OccurrenceID != "occurrence-1" {
		t.Fatalf("continued occurrence query = %+v", occurrenceQueries[1])
	}
	if _, err := service.GetIssue(context.Background(), IssueDetailRequest{
		IssueID: otherIssueID,
		Cursor:  *first.NextCursor,
	}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cross-issue occurrence cursor error = %v", err)
	}
	if _, err := service.GetIssue(context.Background(), IssueDetailRequest{
		IssueID:    issueID,
		Cursor:     *first.NextCursor,
		ViewCursor: viewCursor,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cursor mutual exclusion error = %v", err)
	}
}

func TestDecodeIssueViewCursorReturnsNeutralClaims(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service := New(
		issueTestCoreRepository{},
		WithClock(func() time.Time { return now }),
	)
	cursor, err := encodeCursor(cursorEnvelope{
		Version:     cursorVersion,
		Kind:        "issue_view",
		Snapshot:    23,
		Fingerprint: issueViewFingerprint(),
		IssuedAt:    now.Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.DecodeIssueViewCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Snapshot != 23 || !claims.IssuedAt.Equal(now) {
		t.Fatalf("claims = %+v", claims)
	}

	empty, err := encodeCursor(cursorEnvelope{
		Version:     cursorVersion,
		Kind:        "issue_view",
		Snapshot:    0,
		Fingerprint: issueViewFingerprint(),
		IssuedAt:    now.Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeIssueViewCursor(empty); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty snapshot error = %v, want ErrNotFound", err)
	}
	if _, err := service.DecodeIssueViewCursor("PRIVATE_CURSOR"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("malformed cursor error = %v, want ErrInvalidCursor", err)
	}
	now = now.Add(issueCursorLifetime + time.Nanosecond)
	if _, err := service.DecodeIssueViewCursor(cursor); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expired cursor error = %v, want ErrCursorExpired", err)
	}
}

func TestIssueErrorPrecedenceAndLegacyCursorIsolation(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	issueID := testIssueID("f")
	repository := &issueTestRepository{}
	service := New(
		issueTestCoreRepository{},
		WithIssueRepository(repository),
		WithClock(func() time.Time { return now }),
	)
	view := func(issuedAt time.Time) string {
		value, err := encodeCursor(cursorEnvelope{
			Version:     cursorVersion,
			Kind:        "issue_view",
			Snapshot:    7,
			Fingerprint: issueViewFingerprint(),
			IssuedAt:    issuedAt.Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	if _, err := service.GetIssue(context.Background(), IssueDetailRequest{
		IssueID:    "PRIVATE_ISSUE_CANARY",
		ViewCursor: "PRIVATE_CURSOR_CANARY",
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("malformed identifier precedence error = %v", err)
	}
	if _, err := service.GetIssue(context.Background(), IssueDetailRequest{
		IssueID:    issueID,
		ViewCursor: view(now.Add(time.Second)),
	}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("future cursor error = %v", err)
	}
	if _, err := service.GetIssue(context.Background(), IssueDetailRequest{
		IssueID:    issueID,
		ViewCursor: view(now.Add(-issueCursorLifetime - time.Nanosecond)),
	}); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expired cursor error = %v", err)
	}

	repository.queryIssues = func(model.IssueQuery) (model.IssuePage, error) {
		return model.IssuePage{}, model.ErrIssueSnapshotExpired
	}
	if _, err := service.GetIssue(context.Background(), IssueDetailRequest{
		IssueID:    issueID,
		ViewCursor: view(now),
	}); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("compacted cursor error = %v", err)
	}
	repository.queryIssues = func(model.IssueQuery) (model.IssuePage, error) {
		return model.IssuePage{}, model.ErrIssueSnapshotInvalid
	}
	if _, err := service.GetIssue(context.Background(), IssueDetailRequest{
		IssueID:    issueID,
		ViewCursor: view(now),
	}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("newer snapshot error = %v", err)
	}
	repository.queryIssues = func(model.IssueQuery) (model.IssuePage, error) {
		return model.IssuePage{Snapshot: 7}, nil
	}
	if _, err := service.GetIssue(context.Background(), IssueDetailRequest{
		IssueID:    issueID,
		ViewCursor: view(now),
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent issue error = %v", err)
	}

}

func TestNewRequiresExplicitIssueRepositoryCapability(t *testing.T) {
	repository := &issueCapableCoreRepository{
		issueTestRepository: &issueTestRepository{},
	}
	service := New(repository)

	if _, err := service.ListIssues(context.Background(), IssueListRequest{}); err == nil {
		t.Fatal("New(repository) implicitly enabled issue reads")
	}
	if _, err := service.LookupSessionEvents(context.Background(), EventLookupRequest{
		SessionID: "session-1",
		EventIDs:  []string{testEventID},
	}); err == nil {
		t.Fatal("New(repository) implicitly enabled exact event lookup")
	}
}

func TestLegacyCursorsRemainCompatibleAndRejectIssueOnlyFields(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	kinds := []struct {
		name     string
		kind     string
		sequence int64
	}{
		{name: "sessions", kind: "sessions"},
		{name: "session timeline", kind: "session_events", sequence: 7},
		{name: "activity", kind: "activity", sequence: 7},
		{name: "findings", kind: "findings"},
	}
	issueFields := []struct {
		name   string
		mutate func(*cursorEnvelope)
	}{
		{
			name: "issued_at",
			mutate: func(cursor *cursorEnvelope) {
				cursor.IssuedAt = now.Format(time.RFC3339Nano)
			},
		},
		{
			name: "severity_rank",
			mutate: func(cursor *cursorEnvelope) {
				cursor.SeverityRank = 1
			},
		},
		{
			name: "repeated",
			mutate: func(cursor *cursorEnvelope) {
				cursor.Repeated = true
			},
		},
	}

	for _, kind := range kinds {
		t.Run(kind.name, func(t *testing.T) {
			legacy := cursorEnvelope{
				Version:     cursorVersion,
				Kind:        kind.kind,
				Snapshot:    1,
				Fingerprint: "fingerprint",
				Time:        now.Format(time.RFC3339Nano),
				Sequence:    kind.sequence,
				ID:          "position-1",
			}
			encoded, err := encodeCursor(legacy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeCursor(encoded, kind.kind, "fingerprint"); err != nil {
				t.Fatalf("legacy cursor rejected: %v", err)
			}

			for _, field := range issueFields {
				t.Run(field.name, func(t *testing.T) {
					withIssueField := legacy
					field.mutate(&withIssueField)
					encoded, err := encodeCursor(withIssueField)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := decodeCursor(
						encoded,
						kind.kind,
						"fingerprint",
					); !errors.Is(err, ErrInvalidCursor) {
						t.Fatalf("issue-only field accepted: %v", err)
					}
				})
			}
		})
	}
}

func TestExactEventLookupValidatesDeduplicatesAndReturnsNonNilSlices(t *testing.T) {
	var query model.EventLookupQuery
	repository := &issueTestRepository{
		lookupEvents: func(value model.EventLookupQuery) (model.EventLookupResult, error) {
			query = value
			return model.EventLookupResult{
				Data:           nil,
				RequestedCount: len(value.EventIDs),
				MissingCount:   len(value.EventIDs),
			}, nil
		},
	}
	service := New(
		issueTestCoreRepository{},
		WithIssueRepository(repository),
	)
	response, err := service.LookupSessionEvents(context.Background(), EventLookupRequest{
		SessionID: " session-1 ",
		EventIDs:  []string{testEventID, testEventID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if query.SessionID != "session-1" ||
		len(query.EventIDs) != 1 ||
		response.RequestedCount != 1 ||
		response.Data == nil ||
		response.MissingEventIDs == nil {
		t.Fatalf("event lookup query=%+v response=%+v", query, response)
	}
	if _, err := service.LookupSessionEvents(context.Background(), EventLookupRequest{
		SessionID: "session-1",
		EventIDs:  []string{"PRIVATE_EVENT_CANARY"},
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("malformed event ID error = %v", err)
	}
}

func testIssueID(character string) string {
	return "iss_" + strings.Repeat(character, 52)
}

func testFingerprintID(character string) string {
	return "ifp_" + strings.Repeat(character, 52)
}
