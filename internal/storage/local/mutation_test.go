package local

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMutationGuardsRemainValidAcrossStoresAndConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "belay.sqlite")
	provider := newMemoryKeyProvider()
	first, err := Open(path, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	stores := []*Store{first, second}
	errs := make(chan error, len(stores)*5)
	var wait sync.WaitGroup
	for storeIndex, store := range stores {
		for eventIndex := range 5 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				unique := storeIndex*10 + eventIndex + 1
				event := storageTestEvent(
					fmt.Sprintf("00000000-0000-7000-8000-%012d", unique),
					fmt.Sprintf("session-multi-store-%d", storeIndex),
					int64(eventIndex+1),
					time.Date(2026, 9, 8, 18, 0, unique, 0, time.UTC),
				)
				event.Source.RecordID = fmt.Sprintf("multi-store-%d", unique)
				event.Source.DeduplicationKey = fmt.Sprintf("sha256:%064x", unique)
				if _, err := store.AppendEventResolved(ctx, event); err != nil {
					errs <- err
				}
			}()
		}
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent append error = %v", err)
	}
	if t.Failed() {
		return
	}
	for index, store := range stores {
		if _, err := store.db.ExecContext(ctx,
			"UPDATE events SET action = 'unauthorized' WHERE session_key = ?",
			fmt.Sprintf("session-multi-store-%d", index),
		); err == nil {
			t.Fatalf("store %d allowed unauthorized update", index)
		}
		if _, err := store.MarkSessionDirty(
			ctx,
			fmt.Sprintf("session-multi-store-%d", index),
			"multi_store_test",
		); err != nil {
			t.Fatalf("store %d authorized projection mutation failed: %v", index, err)
		}
	}
}

func TestMutationGuardsAreInstalledOnEveryPooledConnection(t *testing.T) {
	ctx := context.Background()
	store := openStorageTestStore(t)
	event := storageTestEvent(
		"00000000-0000-7000-8000-000000000180",
		"session-pooled-guards",
		1,
		time.Date(2026, 9, 8, 19, 0, 0, 0, time.UTC),
	)
	if _, err := store.AppendEventResolved(ctx, event); err != nil {
		t.Fatal(err)
	}
	store.db.SetMaxOpenConns(4)

	connections := make([]*sql.Conn, 0, 4)
	for range 4 {
		connection, err := store.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
	}
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}()

	for index, connection := range connections {
		var triggerCount int
		if err := connection.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM sqlite_temp_schema
			WHERE type = 'trigger' AND name LIKE 'belay_guard_%'`,
		).Scan(&triggerCount); err != nil {
			t.Fatalf("connection %d inspect guards: %v", index, err)
		}
		if triggerCount != 10 {
			t.Fatalf("connection %d guard count = %d, want 10", index, triggerCount)
		}
		if _, err := connection.ExecContext(ctx,
			"UPDATE events SET action = 'unauthorized' WHERE event_id = ?",
			event.EventID,
		); err == nil {
			t.Fatalf("connection %d allowed unauthorized update", index)
		}
		if _, err := connection.ExecContext(ctx,
			"DELETE FROM events WHERE event_id = ?",
			event.EventID,
		); err == nil {
			t.Fatalf("connection %d allowed unauthorized delete", index)
		}
	}
}

func TestMigrationDropsLegacyPersistentMutationTriggers(t *testing.T) {
	ctx := context.Background()
	store := openStorageTestStore(t)
	var persistent int
	if err := store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM main.sqlite_schema
		WHERE type = 'trigger'
			AND name IN (
				'events_no_update',
				'events_no_delete',
				'findings_no_update',
				'findings_no_delete',
				'issue_occurrences_guard_update',
				'issue_occurrences_guard_delete',
				'session_analysis_revisions_guard_update',
				'session_analysis_revisions_guard_delete',
				'analysis_diagnostics_guard_update',
				'analysis_diagnostics_guard_delete'
			)`,
	).Scan(&persistent); err != nil {
		t.Fatal(err)
	}
	if persistent != 0 {
		t.Fatalf("persistent mutation trigger count = %d, want 0", persistent)
	}
}
