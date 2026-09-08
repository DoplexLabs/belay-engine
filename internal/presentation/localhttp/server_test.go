package localhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
	"github.com/DoplexLabs/belay-engine/internal/presentation/readmodel"
)

type testRepository struct{}

func (testRepository) ListSessions(context.Context, int) ([]model.SessionSummary, time.Time, error) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	return []model.SessionSummary{{
		SessionID:  "session-1",
		Harness:    "codex",
		StartedAt:  now.Add(-time.Minute),
		EndedAt:    now,
		EventCount: 1,
		Outcome:    "succeeded",
	}}, now, nil
}

func (testRepository) GetSession(context.Context, string) (model.SessionSummary, time.Time, error) {
	items, through, _ := testRepository{}.ListSessions(context.Background(), 1)
	return items[0], through, nil
}

func (testRepository) GetSessionTimeline(context.Context, string, int) ([]model.Event, time.Time, error) {
	return []model.Event{{EventID: "event-1"}}, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), nil
}

func (testRepository) QueryActivity(context.Context, model.ActivityFilter) ([]model.Event, time.Time, error) {
	return []model.Event{{EventID: "event-1"}}, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), nil
}

func (testRepository) ListFindings(context.Context, int) ([]model.FindingSummary, time.Time, error) {
	return []model.FindingSummary{{FindingID: "finding-1"}}, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), nil
}

func (testRepository) GetStats(context.Context) (model.LocalStats, time.Time, error) {
	return model.LocalStats{EventCount: 1}, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), nil
}

type pagingRepository struct {
	testRepository
	sessionLimit  int
	timelineLimit int
}

func (r *pagingRepository) ListSessions(_ context.Context, limit int) ([]model.SessionSummary, time.Time, error) {
	r.sessionLimit = limit
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	sessions := make([]model.SessionSummary, limit)
	for index := range sessions {
		sessions[index] = model.SessionSummary{
			SessionID:  fmt.Sprintf("session-%d", index),
			Harness:    "codex",
			StartedAt:  now.Add(-time.Minute),
			EndedAt:    now,
			EventCount: 1,
			Outcome:    "incomplete",
		}
	}
	return sessions, now, nil
}

func (r *pagingRepository) GetSessionTimeline(_ context.Context, sessionID string, limit int) ([]model.Event, time.Time, error) {
	r.timelineLimit = limit
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	events := make([]model.Event, limit)
	for index := range events {
		events[index] = model.Event{
			EventID: fmt.Sprintf("event-%d", index),
			Session: model.SessionRef{Key: sessionID},
		}
	}
	return events, now, nil
}

func TestHandlerRequiresLoopbackAndLaunchToken(t *testing.T) {
	server, err := New(readmodel.New(testRepository{}), "launch-secret")
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/sessions", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", response.Code, http.StatusUnauthorized)
	}

	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/sessions", nil)
	request.RemoteAddr = "203.0.113.4:1234"
	request.Header.Set("Authorization", "Bearer launch-secret")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-loopback status = %d, want %d", response.Code, http.StatusForbidden)
	}

	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/sessions", nil)
	request.RemoteAddr = "[::1]:1234"
	request.Header.Set("Authorization", "Bearer launch-secret")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("missing Content-Security-Policy")
	}
	var completePage readmodel.SessionList
	if err := json.NewDecoder(response.Body).Decode(&completePage); err != nil {
		t.Fatalf("decode complete session page: %v", err)
	}
	if completePage.HasMore || completePage.ReturnedCount != 1 || completePage.Limit != 20 {
		t.Fatalf("complete session page metadata = %+v", completePage)
	}
}

func TestStaticBrowserDoesNotRequireToken(t *testing.T) {
	server, err := New(readmodel.New(testRepository{}), "launch-secret")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("static status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestStartRejectsNonLoopbackAddress(t *testing.T) {
	server, err := New(readmodel.New(testRepository{}), "launch-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.Start(context.Background(), "0.0.0.0:0"); err == nil {
		t.Fatal("Start accepted a non-loopback address")
	}
}

func TestSessionAndTimelineResponsesExposeTruncation(t *testing.T) {
	repository := &pagingRepository{}
	server, err := New(readmodel.New(repository), "launch-secret")
	if err != nil {
		t.Fatal(err)
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/sessions?limit=100", nil)
	sessionRequest.RemoteAddr = "127.0.0.1:1234"
	sessionRequest.Header.Set("Authorization", "Bearer launch-secret")
	sessionResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("session status = %d; body=%s", sessionResponse.Code, sessionResponse.Body.String())
	}
	var sessionPage readmodel.SessionList
	if err := json.NewDecoder(sessionResponse.Body).Decode(&sessionPage); err != nil {
		t.Fatalf("decode session page: %v", err)
	}
	if repository.sessionLimit != 101 {
		t.Fatalf("repository session limit = %d, want 101 look-ahead rows", repository.sessionLimit)
	}
	if !sessionPage.HasMore || sessionPage.ReturnedCount != 100 || sessionPage.Limit != 100 || len(sessionPage.Data) != 100 {
		t.Fatalf("session page = %+v, want 100 rows with has_more", sessionPage)
	}
	if sessionPage.NextCursor != nil {
		t.Fatalf("session next_cursor = %q, want nil until cursor paging is implemented", *sessionPage.NextCursor)
	}

	timelineRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/sessions/session-1/events?limit=500", nil)
	timelineRequest.RemoteAddr = "127.0.0.1:1234"
	timelineRequest.Header.Set("Authorization", "Bearer launch-secret")
	timelineResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(timelineResponse, timelineRequest)
	if timelineResponse.Code != http.StatusOK {
		t.Fatalf("timeline status = %d; body=%s", timelineResponse.Code, timelineResponse.Body.String())
	}
	var timelinePage readmodel.SessionTimeline
	if err := json.NewDecoder(timelineResponse.Body).Decode(&timelinePage); err != nil {
		t.Fatalf("decode timeline page: %v", err)
	}
	if repository.timelineLimit != 501 {
		t.Fatalf("repository timeline limit = %d, want 501 look-ahead rows", repository.timelineLimit)
	}
	if !timelinePage.HasMore || timelinePage.ReturnedCount != 500 || timelinePage.Limit != 500 || len(timelinePage.Data) != 500 {
		t.Fatalf("timeline page = %+v, want 500 rows with has_more", timelinePage)
	}
	if timelinePage.NextCursor != nil {
		t.Fatalf("timeline next_cursor = %q, want nil until cursor paging is implemented", *timelinePage.NextCursor)
	}
}
