package localhttp

import (
	"context"
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
