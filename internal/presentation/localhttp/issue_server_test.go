package localhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
	"github.com/DoplexLabs/belay-engine/internal/presentation/readmodel"
)

const httpTestEventID = "01890f2e-6d4b-7c8a-9b0c-123456789abc"

type issueHTTPRepository struct {
	testRepository
	issueQuery      model.IssueQuery
	occurrenceQuery model.IssueOccurrenceQuery
	eventQuery      model.EventLookupQuery
	issueErr        error
	issueAbsent     bool
}

func (repository *issueHTTPRepository) QueryIssues(
	_ context.Context,
	query model.IssueQuery,
) (model.IssuePage, error) {
	repository.issueQuery = query
	if repository.issueErr != nil {
		return model.IssuePage{}, repository.issueErr
	}
	if repository.issueAbsent && query.Filter.IssueID != "" {
		return model.IssuePage{Snapshot: 31}, nil
	}
	return model.IssuePage{
		Data: []model.IssueSummary{{
			IssueID:        httpTestIssueID("a"),
			FingerprintID:  httpTestFingerprintID("b"),
			Severity:       "high",
			SessionCount:   2,
			LastObservedAt: time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC),
		}},
		Analysis: model.IssueAnalysisCoverage{
			CurrentSessions: 1,
			Complete:        true,
		},
		Snapshot: 31,
		HasMore:  query.Filter.IssueID == "",
	}, nil
}

func (repository *issueHTTPRepository) QueryIssueOccurrences(
	_ context.Context,
	query model.IssueOccurrenceQuery,
) (model.IssueOccurrencePage, error) {
	repository.occurrenceQuery = query
	return model.IssueOccurrencePage{
		Data: []model.IssueOccurrence{{
			OccurrenceID:   "occurrence-1",
			IssueID:        httpTestIssueID("a"),
			SessionID:      "session-1",
			LastObservedAt: time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC),
		}},
		Snapshot: query.Snapshot,
	}, nil
}

func (repository *issueHTTPRepository) LookupSessionEvents(
	_ context.Context,
	query model.EventLookupQuery,
) (model.EventLookupResult, error) {
	repository.eventQuery = query
	return model.EventLookupResult{
		Data: []model.Event{{
			EventID:    httpTestEventID,
			OccurredAt: time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC),
		}},
		RequestedCount:  len(query.EventIDs),
		FoundCount:      1,
		MissingEventIDs: []string{},
		DataThrough:     time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
	}, nil
}

func TestIssueAndExactEventRoutesAreAuthorizedAndUseSharedContracts(t *testing.T) {
	repository := &issueHTTPRepository{}
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	server, err := New(readmodel.New(
		repository,
		readmodel.WithIssueRepository(repository),
		readmodel.WithClock(func() time.Time { return now }),
	), "launch-secret")
	if err != nil {
		t.Fatal(err)
	}

	unauthorized := issueHTTPRequest(
		http.MethodGet,
		"http://127.0.0.1/v1/issues",
		false,
	)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized issue status = %d", unauthorizedResponse.Code)
	}

	nonLoopback := issueHTTPRequest(
		http.MethodGet,
		"http://127.0.0.1/v1/issues",
		true,
	)
	nonLoopback.RemoteAddr = "203.0.113.9:1234"
	nonLoopbackResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(nonLoopbackResponse, nonLoopback)
	if nonLoopbackResponse.Code != http.StatusForbidden {
		t.Fatalf("non-loopback issue status = %d", nonLoopbackResponse.Code)
	}

	listRequest := issueHTTPRequest(
		http.MethodGet,
		"http://127.0.0.1/v1/issues?limit=1&severity=HIGH&category=COMMAND_FAILURE&harness=Codex&origin=BELAY&analysis_status=CURRENT&observed_after=2026-09-08T10:00:00-02:00&recurrence=REPEATED&session_id=session-1&fingerprint_id="+strings.ToUpper(httpTestFingerprintID("b"))+"&attention_kind=ALL&experimental=INCLUDE",
		true,
	)
	listResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("issue list status = %d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var list readmodel.IssueList
	if err := json.NewDecoder(listResponse.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.ViewCursor == "" ||
		list.NextCursor == nil ||
		list.ProjectionVersion != readmodel.IssueProjectionVersion ||
		!list.HasMore ||
		list.ReturnedCount != 1 {
		t.Fatalf("issue list response = %+v", list)
	}
	query := repository.issueQuery
	if query.Filter.Severity != "high" ||
		query.Filter.Category != "command_failure" ||
		query.Filter.Harness != "Codex" ||
		query.Filter.Origin != "belay" ||
		query.Filter.AnalysisStatus != model.AnalysisCurrent ||
		query.Filter.ObservedAfter == nil ||
		query.Filter.ObservedAfter.Format(time.RFC3339Nano) != "2026-09-08T12:00:00Z" ||
		query.Filter.Recurrence != "repeated" ||
		query.Filter.SessionID != "session-1" ||
		query.Filter.FingerprintID != httpTestFingerprintID("b") ||
		query.Filter.AttentionKind != model.AttentionKindAll ||
		query.Filter.Experimental != model.ExperimentalInclude {
		t.Fatalf("HTTP issue query = %+v", query)
	}

	detailRequest := issueHTTPRequest(
		http.MethodGet,
		"http://127.0.0.1/v1/issues/"+httpTestIssueID("a")+"/occurrences?limit=1&view_cursor="+list.ViewCursor,
		true,
	)
	detailResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("issue detail status = %d body=%s", detailResponse.Code, detailResponse.Body.String())
	}
	var detail readmodel.IssueDetail
	if err := json.NewDecoder(detailResponse.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.Data.Issue.IssueID != httpTestIssueID("a") ||
		len(detail.Data.Occurrences) != 1 ||
		repository.issueQuery.Snapshot != 31 ||
		repository.occurrenceQuery.Snapshot != 31 ||
		!repository.issueQuery.IssuedAt.Equal(now) ||
		!repository.occurrenceQuery.IssuedAt.Equal(now) {
		t.Fatalf("snapshot-stable detail=%+v summary=%+v occurrence=%+v",
			detail, repository.issueQuery, repository.occurrenceQuery)
	}

	lookupRequest := issueHTTPRequest(
		http.MethodGet,
		"http://127.0.0.1/v1/sessions/session-1/events/lookup?event_id="+httpTestEventID+"&event_id="+httpTestEventID,
		true,
	)
	lookupResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(lookupResponse, lookupRequest)
	if lookupResponse.Code != http.StatusOK {
		t.Fatalf("event lookup status = %d body=%s", lookupResponse.Code, lookupResponse.Body.String())
	}
	var lookup readmodel.EventLookup
	if err := json.NewDecoder(lookupResponse.Body).Decode(&lookup); err != nil {
		t.Fatal(err)
	}
	if len(repository.eventQuery.EventIDs) != 1 ||
		lookup.RequestedCount != 1 ||
		lookup.FoundCount != 1 ||
		lookup.MissingEventIDs == nil {
		t.Fatalf("event lookup query=%+v response=%+v", repository.eventQuery, lookup)
	}
}

func TestIssueHTTPErrorMatrixIsFixedAndNonReflective(t *testing.T) {
	repository := &issueHTTPRepository{}
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	server, err := New(readmodel.New(
		repository,
		readmodel.WithIssueRepository(repository),
		readmodel.WithClock(func() time.Time { return now }),
	), "launch-secret")
	if err != nil {
		t.Fatal(err)
	}

	assertProblem := func(path string, status int, forbidden string) problem {
		t.Helper()
		request := issueHTTPRequest(http.MethodGet, "http://127.0.0.1"+path, true)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("%s status = %d body=%s", path, response.Code, response.Body.String())
		}
		if forbidden != "" && strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("%s reflected private input %q", path, forbidden)
		}
		var value problem
		if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}

	assertProblem(
		"/v1/issues/PRIVATE_ISSUE_CANARY/occurrences?view_cursor=PRIVATE_CURSOR_CANARY",
		http.StatusBadRequest,
		"PRIVATE_ISSUE_CANARY",
	)
	assertProblem(
		"/v1/sessions/session-1/events/lookup?event_id=PRIVATE_EVENT_CANARY",
		http.StatusBadRequest,
		"PRIVATE_EVENT_CANARY",
	)

	repository.issueAbsent = true
	assertProblem(
		"/v1/issues/"+httpTestIssueID("a")+"/occurrences",
		http.StatusNotFound,
		httpTestIssueID("a"),
	)
	repository.issueAbsent = false

	listRequest := issueHTTPRequest(
		http.MethodGet,
		"http://127.0.0.1/v1/issues?limit=1",
		true,
	)
	listResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("cursor source status = %d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var list readmodel.IssueList
	if err := json.NewDecoder(listResponse.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	assertProblem(
		"/v1/issues/"+httpTestIssueID("a")+"/occurrences?cursor="+*list.NextCursor,
		http.StatusBadRequest,
		*list.NextCursor,
	)
	assertProblem(
		"/v1/issues/"+httpTestIssueID("a")+"/occurrences?cursor=PRIVATE_PAGE_CURSOR&view_cursor=PRIVATE_VIEW_CURSOR",
		http.StatusBadRequest,
		"PRIVATE_PAGE_CURSOR",
	)

	repository.issueErr = model.ErrIssueSnapshotExpired
	expired := assertProblem(
		"/v1/issues?limit=1&cursor="+*list.NextCursor,
		http.StatusGone,
		*list.NextCursor,
	)
	if expired.Type != "belay.local/cursor-expired" ||
		expired.Title != "Cursor expired" ||
		expired.Detail != "The issue view changed. Restart pagination without a cursor." {
		t.Fatalf("expired problem = %+v", expired)
	}

	repository.issueErr = model.ErrIssueSnapshotInvalid
	assertProblem(
		"/v1/issues?limit=1&cursor="+*list.NextCursor,
		http.StatusBadRequest,
		*list.NextCursor,
	)

	repository.issueErr = errors.New("PRIVATE_INTERNAL_CANARY")
	assertProblem(
		"/v1/issues",
		http.StatusInternalServerError,
		"PRIVATE_INTERNAL_CANARY",
	)
}

func TestExactEventLookupRejectsEveryParameterExceptRepeatedEventID(t *testing.T) {
	repository := &issueHTTPRepository{}
	server, err := New(readmodel.New(
		repository,
		readmodel.WithIssueRepository(repository),
	), "launch-secret")
	if err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{
		"event_id=" + httpTestEventID + "&cursor=PRIVATE_CURSOR_CANARY",
		"event_id=" + httpTestEventID + "&query=PRIVATE_QUERY_CANARY",
		"event_id=" + httpTestEventID + "&unknown=PRIVATE_UNKNOWN_CANARY",
		"event_id=" + httpTestEventID + "&bad=%zz",
	} {
		request := issueHTTPRequest(
			http.MethodGet,
			"http://127.0.0.1/v1/sessions/session-1/events/lookup?"+query,
			true,
		)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d body=%s", query, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "PRIVATE_") {
			t.Fatalf("query %q reflected input: %s", query, response.Body.String())
		}
	}
	if repository.eventQuery.SessionID != "" {
		t.Fatalf("rejected query reached repository: %+v", repository.eventQuery)
	}

	request := issueHTTPRequest(
		http.MethodGet,
		"http://127.0.0.1/v1/sessions/session-1/events/lookup?event_id="+httpTestEventID+"&event_id="+httpTestEventID,
		true,
	)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("repeated event_id status = %d body=%s", response.Code, response.Body.String())
	}
	if len(repository.eventQuery.EventIDs) != 1 {
		t.Fatalf("deduplicated event query = %+v", repository.eventQuery)
	}
}

func issueHTTPRequest(method, target string, authorized bool) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.RemoteAddr = "127.0.0.1:1234"
	if authorized {
		request.Header.Set("Authorization", "Bearer launch-secret")
	}
	return request
}

func httpTestIssueID(character string) string {
	return "iss_" + strings.Repeat(character, 52)
}

func httpTestFingerprintID(character string) string {
	return "ifp_" + strings.Repeat(character, 52)
}
