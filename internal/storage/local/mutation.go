package local

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"

	sqlite "modernc.org/sqlite"
)

type mutationPurpose string

const (
	mutationPayloadUpgrade    mutationPurpose = "payload_upgrade"
	mutationRetentionPrune    mutationPurpose = "retention_prune"
	mutationProjectionRebuild mutationPurpose = "projection_rebuild"
	mutationRecurrenceWorker  mutationPurpose = "recurrence_worker"
	guardedSQLiteDriverName                   = "belay_local_sqlite"
)

var registerGuardedSQLiteDriver sync.Once

func openGuardedSQLite(dsn string) (*sql.DB, error) {
	registerGuardedSQLiteDriver.Do(func() {
		sqliteDriver := &sqlite.Driver{}
		sqliteDriver.RegisterConnectionHook(initializeMutationConnection)
		sql.Register(guardedSQLiteDriverName, sqliteDriver)
	})
	return sql.Open(guardedSQLiteDriverName, dsn)
}

func initializeMutationConnection(
	connection sqlite.ExecQuerierContext,
	_ string,
) error {
	ctx := context.Background()
	if _, err := connection.ExecContext(ctx, "PRAGMA recursive_triggers = ON", nil); err != nil {
		return errors.New("enable recursive local mutation guards")
	}
	if _, err := connection.ExecContext(ctx, mutationAuthorizationTableSQL, nil); err != nil {
		return errors.New("initialize local mutation authorization")
	}
	ready, err := mutationTablesReady(ctx, connection)
	if err != nil {
		return err
	}
	if !ready {
		return nil
	}
	if _, err := connection.ExecContext(ctx, mutationTriggerSQL, nil); err != nil {
		return errors.New("install connection-local mutation guards")
	}
	fixReady, err := feature3MutationTablesReady(ctx, connection)
	if err != nil {
		return err
	}
	if fixReady {
		if _, err := connection.ExecContext(ctx, fixMutationTriggerSQL, nil); err != nil {
			return errors.New("install connection-local fix mutation guards")
		}
	}
	recurrenceReady, err := recurrenceMutationTablesReady(ctx, connection)
	if err != nil {
		return err
	}
	if recurrenceReady {
		if _, err := connection.ExecContext(ctx, recurrenceMutationTriggerSQL, nil); err != nil {
			return errors.New("install connection-local recurrence mutation guards")
		}
	}
	summaryReady, err := issueSummaryMutationTablesReady(ctx, connection)
	if err != nil {
		return err
	}
	if summaryReady {
		if _, err := connection.ExecContext(ctx, issueSummaryMutationTriggerSQL, nil); err != nil {
			return errors.New("install connection-local issue summary mutation guards")
		}
	}
	return nil
}

func mutationTablesReady(
	ctx context.Context,
	connection driver.QueryerContext,
) (bool, error) {
	rows, err := connection.QueryContext(ctx, `
		SELECT COUNT(*)
		FROM main.sqlite_schema
		WHERE type = 'table'
			AND name IN (
				'events',
				'findings',
				'issue_occurrences',
				'session_analysis_revisions',
				'analysis_diagnostics'
			)`,
		nil,
	)
	if err != nil {
		return false, errors.New("inspect local mutation schema")
	}
	defer rows.Close()
	values := make([]driver.Value, 1)
	if err := rows.Next(values); err != nil {
		if errors.Is(err, io.EOF) {
			return false, errors.New("inspect local mutation schema")
		}
		return false, errors.New("inspect local mutation schema")
	}
	count, ok := values[0].(int64)
	if !ok {
		return false, errors.New("inspect local mutation schema")
	}
	return count == 5, nil
}

func feature3MutationTablesReady(
	ctx context.Context,
	connection driver.QueryerContext,
) (bool, error) {
	rows, err := connection.QueryContext(ctx, `
		SELECT COUNT(*)
		FROM main.sqlite_schema
		WHERE type = 'table'
			AND name IN (
				'fix_annotations',
				'fix_annotation_retractions'
			)`,
		nil,
	)
	if err != nil {
		return false, errors.New("inspect local fix mutation schema")
	}
	defer rows.Close()
	values := make([]driver.Value, 1)
	if err := rows.Next(values); err != nil {
		return false, errors.New("inspect local fix mutation schema")
	}
	count, ok := values[0].(int64)
	if !ok {
		return false, errors.New("inspect local fix mutation schema")
	}
	return count == 2, nil
}

func recurrenceMutationTablesReady(
	ctx context.Context,
	connection driver.QueryerContext,
) (bool, error) {
	rows, err := connection.QueryContext(ctx, `
		SELECT COUNT(*)
		FROM main.sqlite_schema
		WHERE type = 'table'
			AND name IN (
				'fix_monitoring_metadata',
				'fix_monitoring_subjects',
				'session_analysis_capabilities',
				'fix_recurrence_jobs',
				'fix_recurrence_job_events',
				'fix_recurrence_observations',
				'fix_recurrence_observation_events'
			)`,
		nil,
	)
	if err != nil {
		return false, errors.New("inspect local recurrence mutation schema")
	}
	defer rows.Close()
	values := make([]driver.Value, 1)
	if err := rows.Next(values); err != nil {
		return false, errors.New("inspect local recurrence mutation schema")
	}
	count, ok := values[0].(int64)
	if !ok {
		return false, errors.New("inspect local recurrence mutation schema")
	}
	return count == 7, nil
}

func issueSummaryMutationTablesReady(
	ctx context.Context,
	connection driver.QueryerContext,
) (bool, error) {
	rows, err := connection.QueryContext(ctx, `
		SELECT COUNT(*)
		FROM main.sqlite_schema
		WHERE type = 'table'
			AND name IN (
				'issue_summary_metadata',
				'issue_projection_generation_times',
				'issue_summary_revisions',
				'issue_summary_harnesses',
				'issue_summary_sessions',
				'issue_analysis_coverage_revisions'
			)`,
		nil,
	)
	if err != nil {
		return false, errors.New("inspect local issue summary mutation schema")
	}
	defer rows.Close()
	values := make([]driver.Value, 1)
	if err := rows.Next(values); err != nil {
		return false, errors.New("inspect local issue summary mutation schema")
	}
	count, ok := values[0].(int64)
	if !ok {
		return false, errors.New("inspect local issue summary mutation schema")
	}
	return count == 6, nil
}

func (s *Store) installMutationGuards(ctx context.Context) error {
	connection, err := s.db.Conn(ctx)
	if err != nil {
		return errors.New("acquire local mutation connection")
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, mutationAuthorizationTableSQL); err != nil {
		return errors.New("initialize local mutation authorization")
	}
	if _, err := connection.ExecContext(ctx, mutationTriggerSQL); err != nil {
		return errors.New("install connection-local mutation guards")
	}
	if _, err := connection.ExecContext(ctx, fixMutationTriggerSQL); err != nil {
		return errors.New("install connection-local fix mutation guards")
	}
	if _, err := connection.ExecContext(ctx, recurrenceMutationTriggerSQL); err != nil {
		return errors.New("install connection-local recurrence mutation guards")
	}
	if _, err := connection.ExecContext(ctx, issueSummaryMutationTriggerSQL); err != nil {
		return errors.New("install connection-local issue summary mutation guards")
	}
	return nil
}

func withMutationTx(
	ctx context.Context,
	tx *sql.Tx,
	purpose mutationPurpose,
	operation func() error,
) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO temp.belay_mutation_authorization (purpose)
		VALUES (?)`,
		purpose,
	); err != nil {
		return errors.New("activate local mutation authorization")
	}
	operationErr := operation()
	_, clearErr := tx.ExecContext(ctx, `
		DELETE FROM temp.belay_mutation_authorization
		WHERE purpose = ?`,
		purpose,
	)
	if operationErr != nil {
		return operationErr
	}
	if clearErr != nil {
		return errors.New("clear local mutation authorization")
	}
	return nil
}

const mutationAuthorizationTableSQL = `
	CREATE TEMP TABLE IF NOT EXISTS belay_mutation_authorization (
		purpose TEXT PRIMARY KEY
			CHECK (
				purpose IN (
					'payload_upgrade',
					'retention_prune',
					'projection_rebuild',
					'recurrence_worker'
				)
			)
	) WITHOUT ROWID;
	DELETE FROM temp.belay_mutation_authorization;`

const mutationTriggerSQL = `
	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_events_update
	BEFORE UPDATE ON main.events
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose = 'payload_upgrade'
	)
	BEGIN
		SELECT RAISE(ABORT, 'canonical events are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_events_delete
	BEFORE DELETE ON main.events
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose = 'retention_prune'
	)
	BEGIN
		SELECT RAISE(ABORT, 'canonical events are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_findings_update
	BEFORE UPDATE ON main.findings
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose = 'payload_upgrade'
	)
	BEGIN
		SELECT RAISE(ABORT, 'findings are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_findings_delete
	BEFORE DELETE ON main.findings
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose = 'retention_prune'
	)
	BEGIN
		SELECT RAISE(ABORT, 'findings are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_occurrences_update
	BEFORE UPDATE ON main.issue_occurrences
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN (
			'projection_rebuild',
			'payload_upgrade',
			'retention_prune'
		)
	)
	BEGIN
		SELECT RAISE(ABORT, 'issue occurrence mutation is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_occurrences_delete
	BEFORE DELETE ON main.issue_occurrences
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'issue occurrence mutation is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_session_analysis_revisions_update
	BEFORE UPDATE ON main.session_analysis_revisions
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'analysis revision mutation is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_session_analysis_revisions_delete
	BEFORE DELETE ON main.session_analysis_revisions
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'analysis revision mutation is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_analysis_diagnostics_update
	BEFORE UPDATE ON main.analysis_diagnostics
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'analysis diagnostic mutation is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_analysis_diagnostics_delete
	BEFORE DELETE ON main.analysis_diagnostics
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'analysis diagnostic mutation is not authorized');
	END;`

const fixMutationTriggerSQL = `
	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_fix_annotations_update
	BEFORE UPDATE ON main.fix_annotations
	BEGIN
		SELECT RAISE(ABORT, 'fix annotations are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_fix_annotations_delete
	BEFORE DELETE ON main.fix_annotations
	BEGIN
		SELECT RAISE(ABORT, 'fix annotations are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_fix_retractions_update
	BEFORE UPDATE ON main.fix_annotation_retractions
	BEGIN
		SELECT RAISE(ABORT, 'fix annotation retractions are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_fix_retractions_delete
	BEFORE DELETE ON main.fix_annotation_retractions
	BEGIN
		SELECT RAISE(ABORT, 'fix annotation retractions are append-only');
	END;`

const recurrenceMutationTriggerSQL = `
	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_fix_monitoring_subjects_update
	BEFORE UPDATE ON main.fix_monitoring_subjects
	BEGIN
		SELECT RAISE(ABORT, 'fix monitoring subjects are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_fix_monitoring_subjects_delete
	BEFORE DELETE ON main.fix_monitoring_subjects
	BEGIN
		SELECT RAISE(ABORT, 'fix monitoring subjects are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_analysis_capabilities_update
	BEFORE UPDATE ON main.session_analysis_capabilities
	BEGIN
		SELECT RAISE(ABORT, 'analysis capabilities are immutable');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_analysis_capabilities_delete
	BEFORE DELETE ON main.session_analysis_capabilities
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'analysis capability mutation is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_recurrence_jobs_update
	BEFORE UPDATE ON main.fix_recurrence_jobs
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose = 'recurrence_worker'
	)
	BEGIN
		SELECT RAISE(ABORT, 'recurrence job mutation is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_recurrence_jobs_delete
	BEFORE DELETE ON main.fix_recurrence_jobs
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose = 'retention_prune'
	)
	BEGIN
		SELECT RAISE(ABORT, 'recurrence job deletion is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_recurrence_job_events_update
	BEFORE UPDATE ON main.fix_recurrence_job_events
	BEGIN
		SELECT RAISE(ABORT, 'recurrence job events are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_recurrence_job_events_delete
	BEFORE DELETE ON main.fix_recurrence_job_events
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose = 'retention_prune'
	)
	BEGIN
		SELECT RAISE(ABORT, 'recurrence job events are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_recurrence_observations_update
	BEFORE UPDATE ON main.fix_recurrence_observations
	BEGIN
		SELECT RAISE(ABORT, 'recurrence observations are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_recurrence_observations_delete
	BEFORE DELETE ON main.fix_recurrence_observations
	BEGIN
		SELECT RAISE(ABORT, 'recurrence observations are append-only');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_recurrence_observation_events_update
	BEFORE UPDATE ON main.fix_recurrence_observation_events
	BEGIN
		SELECT RAISE(ABORT, 'recurrence observation events are immutable');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_recurrence_observation_events_delete
	BEFORE DELETE ON main.fix_recurrence_observation_events
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose = 'retention_prune'
	)
	BEGIN
		SELECT RAISE(ABORT, 'recurrence observation event deletion is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_fix_monitoring_metadata_update
	BEFORE UPDATE ON main.fix_monitoring_metadata
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose = 'recurrence_worker'
	)
	BEGIN
		SELECT RAISE(ABORT, 'fix monitoring metadata mutation is not authorized');
	END;`

const issueSummaryMutationTriggerSQL = `
	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_summary_revisions_update
	BEFORE UPDATE ON main.issue_summary_revisions
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose = 'projection_rebuild'
	)
	BEGIN
		SELECT RAISE(ABORT, 'issue summary mutation is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_summary_revisions_delete
	BEFORE DELETE ON main.issue_summary_revisions
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'issue summary deletion is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_summary_harnesses_update
	BEFORE UPDATE ON main.issue_summary_harnesses
	BEGIN
		SELECT RAISE(ABORT, 'issue summary harnesses are immutable');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_summary_harnesses_delete
	BEFORE DELETE ON main.issue_summary_harnesses
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'issue summary harness deletion is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_summary_sessions_update
	BEFORE UPDATE ON main.issue_summary_sessions
	BEGIN
		SELECT RAISE(ABORT, 'issue summary sessions are immutable');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_summary_sessions_delete
	BEFORE DELETE ON main.issue_summary_sessions
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'issue summary session deletion is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_coverage_update
	BEFORE UPDATE ON main.issue_analysis_coverage_revisions
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose = 'projection_rebuild'
	)
	BEGIN
		SELECT RAISE(ABORT, 'issue coverage mutation is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_coverage_delete
	BEFORE DELETE ON main.issue_analysis_coverage_revisions
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'issue coverage deletion is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_generation_times_update
	BEFORE UPDATE ON main.issue_projection_generation_times
	BEGIN
		SELECT RAISE(ABORT, 'issue generation timestamps are immutable');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_generation_times_delete
	BEFORE DELETE ON main.issue_projection_generation_times
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'issue generation timestamp deletion is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_summary_metadata_update
	BEFORE UPDATE ON main.issue_summary_metadata
	WHEN NOT EXISTS (
		SELECT 1 FROM belay_mutation_authorization
		WHERE purpose IN ('projection_rebuild', 'retention_prune')
	)
	BEGIN
		SELECT RAISE(ABORT, 'issue summary metadata mutation is not authorized');
	END;

	CREATE TEMP TRIGGER IF NOT EXISTS belay_guard_issue_summary_metadata_delete
	BEFORE DELETE ON main.issue_summary_metadata
	BEGIN
		SELECT RAISE(ABORT, 'issue summary metadata cannot be deleted');
	END;`
