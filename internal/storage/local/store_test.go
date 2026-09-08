package local

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionOutcomeRequiresTerminalEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "belay.sqlite"), newMemoryKeyProvider())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		sessionID       string
		terminalOutcome string
		wantOutcome     string
	}{
		{sessionID: "session-incomplete", wantOutcome: "incomplete"},
		{sessionID: "session-succeeded", terminalOutcome: "succeeded", wantOutcome: "succeeded"},
		{sessionID: "session-failed", terminalOutcome: "failed", wantOutcome: "failed"},
		{sessionID: "session-interrupted", terminalOutcome: "interrupted", wantOutcome: "interrupted"},
		{sessionID: "session-unknown", terminalOutcome: "unknown", wantOutcome: "unknown"},
	}

	unique := 1
	for index, test := range tests {
		appendSessionOutcomeEvent(t, ctx, store, test.sessionID, unique, 1, base.Add(time.Duration(index)*time.Minute), "session.start", "unknown")
		unique++
		// A successful intermediate action must not be mistaken for successful
		// session completion.
		appendSessionOutcomeEvent(t, ctx, store, test.sessionID, unique, 2, base.Add(time.Duration(index)*time.Minute+time.Second), "command.result", "succeeded")
		unique++
		if test.terminalOutcome != "" {
			appendSessionOutcomeEvent(t, ctx, store, test.sessionID, unique, 3, base.Add(time.Duration(index)*time.Minute+2*time.Second), "session.end", test.terminalOutcome)
			unique++
		}
	}

	sessions, _, err := store.ListSessions(ctx, 100)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	got := make(map[string]string, len(sessions))
	for _, session := range sessions {
		got[session.SessionID] = session.Outcome
	}
	for _, test := range tests {
		if got[test.sessionID] != test.wantOutcome {
			t.Errorf("%s outcome = %q, want %q", test.sessionID, got[test.sessionID], test.wantOutcome)
		}
		detail, _, err := store.GetSession(ctx, test.sessionID)
		if err != nil {
			t.Fatalf("GetSession(%q) error = %v", test.sessionID, err)
		}
		if detail.Outcome != test.wantOutcome {
			t.Errorf("GetSession(%q) outcome = %q, want %q", test.sessionID, detail.Outcome, test.wantOutcome)
		}
	}
}

func appendSessionOutcomeEvent(
	t *testing.T,
	ctx context.Context,
	store *Store,
	sessionID string,
	unique int,
	sequence int64,
	occurredAt time.Time,
	eventType string,
	outcome string,
) {
	t.Helper()
	eventID := fmt.Sprintf("00000000-0000-7000-8000-%012d", unique)
	event := storageTestEvent(eventID, sessionID, sequence, occurredAt)
	event.Source.RecordID = fmt.Sprintf("source-%d", unique)
	event.Source.DeduplicationKey = fmt.Sprintf("sha256:%064x", unique)
	event.Observation.Type = eventType
	event.Observation.Outcome = outcome
	if inserted, err := store.AppendEvent(ctx, event); err != nil || !inserted {
		t.Fatalf("AppendEvent(%q, %q) = (%t, %v), want inserted", sessionID, eventType, inserted, err)
	}
}
