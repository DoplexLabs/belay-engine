// Package local owns Belay Local's SQLite persistence and repository queries.
// It accepts only canonical events and payload-free operational records.
package local

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct {
	db        *sql.DB
	cipher    *payloadCipher
	storeID   string
	mutations *mutationAuthorizer
}

type OpenOptions struct {
	KeyProvider KeyProvider
	Random      io.Reader
}

type Finding struct {
	FindingID     string
	SourceRunID   string
	SessionKey    string
	DetectedAt    time.Time
	RuleID        string
	RuleVersion   string
	Severity      string
	SourceAgent   string
	Confidence    string
	CitedEventIDs []string
}

type ImportSummary struct {
	SourceRunID       string
	Status            string
	Complete          bool
	ArtifactsScanned  int
	EventsEmitted     int
	FindingsEmitted   int
	IndicatorsEmitted int
	Diagnostics       int
}

type Quarantine struct {
	SourceRunID  string
	LineNumber   int64
	Category     string
	Reason       string
	RecordSHA256 string
}

var ErrUnresolvedCitation = errors.New("finding citation could not be resolved")

func Open(path string, keyProvider KeyProvider) (*Store, error) {
	return OpenWithOptions(path, OpenOptions{KeyProvider: keyProvider})
}

func OpenWithOptions(path string, options OpenOptions) (*Store, error) {
	if options.KeyProvider == nil {
		return nil, errors.New("local key provider is required")
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	mutations, err := newMutationAuthorizer()
	if err != nil {
		return nil, err
	}
	dsn, err := sqliteDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open local database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect local database: %w", err)
	}
	if _, err := db.Exec("PRAGMA secure_delete = ON"); err != nil {
		_ = db.Close()
		return nil, errors.New("enable secure local deletion")
	}
	store := &Store{db: db, mutations: mutations}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.installMutationTriggers(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.initializeEncryptedPayloads(
		context.Background(),
		options.KeyProvider,
		options.Random,
	); err != nil {
		if store.cipher != nil {
			store.cipher.close()
		}
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func sqliteDSN(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("local database path is required")
	}

	var databaseURL url.URL
	if path == ":memory:" {
		databaseURL = url.URL{Scheme: "file", Opaque: ":memory:"}
	} else {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve local database path: %w", err)
		}
		databaseURL = url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	}

	query := databaseURL.Query()
	query.Set("_busy_timeout", "5000")
	query.Set("_foreign_keys", "on")
	query.Set("_journal_mode", "WAL")
	query.Set("_synchronous", "FULL")
	query.Set("_txlock", "exclusive")
	databaseURL.RawQuery = query.Encode()
	return databaseURL.String(), nil
}

func (s *Store) Close() error {
	if s.cipher != nil {
		s.cipher.close()
	}
	return s.db.Close()
}

func (s *Store) AppendEvent(ctx context.Context, event model.Event) (bool, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return false, fmt.Errorf("encode canonical event: %w", err)
	}
	body, err = s.cipher.seal("event", event.EventID, "canonical_json", body)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO events (
			event_id, source_deduplication_key, schema_version, installation_id,
			session_key, occurred_at, observed_at, source_sequence, event_type,
			actor, action, outcome, source_agent, source_kind, source_record_id,
			source_run_id, historical, canonical_json, canonical_encoding, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_deduplication_key) DO NOTHING`,
		event.EventID,
		event.Source.DeduplicationKey,
		event.SchemaVersion,
		event.InstallationID,
		event.Session.Key,
		event.OccurredAt.Format(time.RFC3339Nano),
		event.ObservedAt.Format(time.RFC3339Nano),
		event.Source.Sequence,
		event.Observation.Type,
		event.Observation.Actor,
		event.Observation.Action,
		event.Observation.Outcome,
		event.Source.Agent,
		event.Source.Kind,
		event.Source.RecordID,
		event.Source.RunID,
		boolInt(event.Historical.IsHistorical),
		body,
		payloadEncodingAESGCM,
		now,
	)
	if err != nil {
		return false, fmt.Errorf("append canonical event: %w", err)
	}
	inserted, err := result.RowsAffected()
	return inserted == 1, err
}

func (s *Store) TouchImportRun(ctx context.Context, runID string) error {
	if runID == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO import_runs (source_run_id, first_seen_at, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(source_run_id) DO UPDATE SET updated_at = excluded.updated_at`,
		runID, now, now,
	)
	return err
}

func (s *Store) RecordImportSummary(ctx context.Context, summary ImportSummary) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO import_runs (
			source_run_id, status, complete, artifacts_scanned, events_emitted,
			findings_emitted, indicators_emitted, diagnostics, first_seen_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_run_id) DO UPDATE SET
			status = excluded.status,
			complete = excluded.complete,
			artifacts_scanned = excluded.artifacts_scanned,
			events_emitted = excluded.events_emitted,
			findings_emitted = excluded.findings_emitted,
			indicators_emitted = excluded.indicators_emitted,
			diagnostics = excluded.diagnostics,
			updated_at = excluded.updated_at`,
		summary.SourceRunID,
		summary.Status,
		boolInt(summary.Complete),
		summary.ArtifactsScanned,
		summary.EventsEmitted,
		summary.FindingsEmitted,
		summary.IndicatorsEmitted,
		summary.Diagnostics,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("record import summary: %w", err)
	}
	return nil
}

func (s *Store) RecordFinding(ctx context.Context, finding Finding) (bool, error) {
	if finding.SourceRunID == "" || finding.SessionKey == "" || len(finding.CitedEventIDs) == 0 {
		return false, ErrUnresolvedCitation
	}
	cited, err := json.Marshal(finding.CitedEventIDs)
	if err != nil {
		return false, err
	}
	cited, err = s.cipher.seal("finding", finding.FindingID, "cited_event_ids_json", cited)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, errors.New("begin finding persistence")
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO findings (
			finding_id, source_run_id, session_key, detected_at, rule_id,
			rule_version, severity, source_agent, confidence,
			cited_event_ids_json, cited_event_ids_encoding, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(finding_id) DO NOTHING`,
		finding.FindingID,
		finding.SourceRunID,
		nullable(finding.SessionKey),
		finding.DetectedAt.Format(time.RFC3339Nano),
		finding.RuleID,
		finding.RuleVersion,
		finding.Severity,
		finding.SourceAgent,
		finding.Confidence,
		cited,
		payloadEncodingAESGCM,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return false, fmt.Errorf("record finding: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, errors.New("inspect finding persistence")
	}
	if inserted == 0 {
		if err := tx.Commit(); err != nil {
			return false, errors.New("complete duplicate finding persistence")
		}
		return false, nil
	}
	for _, eventID := range finding.CitedEventIDs {
		var matching int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM events
			WHERE event_id = ? AND source_run_id = ? AND session_key = ?`,
			eventID,
			finding.SourceRunID,
			finding.SessionKey,
		).Scan(&matching); err != nil {
			return false, errors.New("validate canonical finding citation")
		}
		if matching != 1 {
			return false, ErrUnresolvedCitation
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO finding_event_citations (finding_id, event_id)
			VALUES (?, ?)`,
			finding.FindingID,
			eventID,
		); err != nil {
			return false, errors.New("persist canonical finding citation")
		}
	}
	if err := tx.Commit(); err != nil {
		return false, errors.New("commit finding persistence")
	}
	return true, nil
}

func (s *Store) ResolveCanonicalEventIDs(
	ctx context.Context,
	sourceRunID string,
	sessionKey string,
	sourceRecordIDs []string,
) ([]string, error) {
	if sourceRunID == "" || sessionKey == "" || len(sourceRecordIDs) == 0 {
		return nil, ErrUnresolvedCitation
	}
	resolved := make([]string, 0, len(sourceRecordIDs))
	for _, sourceRecordID := range sourceRecordIDs {
		rows, err := s.db.QueryContext(ctx, `
			SELECT event_id
			FROM events
			WHERE source_run_id = ? AND session_key = ? AND source_record_id = ?
			ORDER BY event_id
			LIMIT 2`,
			sourceRunID,
			sessionKey,
			sourceRecordID,
		)
		if err != nil {
			return nil, errors.New("resolve canonical finding citation")
		}
		var matches []string
		for rows.Next() {
			var eventID string
			if err := rows.Scan(&eventID); err != nil {
				rows.Close()
				return nil, errors.New("resolve canonical finding citation")
			}
			matches = append(matches, eventID)
		}
		if err := rows.Close(); err != nil {
			return nil, errors.New("resolve canonical finding citation")
		}
		if len(matches) != 1 {
			return nil, ErrUnresolvedCitation
		}
		resolved = append(resolved, matches[0])
	}
	return resolved, nil
}

func (s *Store) RecordQuarantine(ctx context.Context, quarantine Quarantine) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO quarantine (
			source_run_id, line_number, category, reason, record_sha256, created_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		nullable(quarantine.SourceRunID),
		quarantine.LineNumber,
		quarantine.Category,
		quarantine.Reason,
		quarantine.RecordSHA256,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) RecordDiagnostic(ctx context.Context, runID string, line int64, level, code string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO diagnostics (source_run_id, line_number, level, code, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		nullable(runID),
		line,
		level,
		code,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]model.SessionSummary, time.Time, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			session_key,
			MIN(source_agent),
			MIN(occurred_at),
			MAX(occurred_at),
			COUNT(*),
			CASE
				WHEN SUM(CASE WHEN outcome = 'failed' THEN 1 ELSE 0 END) > 0 THEN 'failed'
				WHEN SUM(CASE WHEN outcome = 'succeeded' THEN 1 ELSE 0 END) > 0 THEN 'succeeded'
				ELSE 'unknown'
			END,
			MAX(historical)
		FROM events
		GROUP BY session_key
		ORDER BY MAX(occurred_at) DESC, session_key
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []model.SessionSummary
	for rows.Next() {
		var summary model.SessionSummary
		var startedAt, endedAt string
		var historical int
		if err := rows.Scan(
			&summary.SessionID,
			&summary.Harness,
			&startedAt,
			&endedAt,
			&summary.EventCount,
			&summary.Outcome,
			&historical,
		); err != nil {
			return nil, time.Time{}, err
		}
		summary.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
		summary.EndedAt, _ = time.Parse(time.RFC3339Nano, endedAt)
		summary.Historical = historical == 1
		sessions = append(sessions, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, err
	}
	dataThrough, err := s.dataThrough(ctx)
	return sessions, dataThrough, err
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (model.SessionSummary, time.Time, error) {
	var summary model.SessionSummary
	var startedAt, endedAt string
	var historical int
	err := s.db.QueryRowContext(ctx, `
		SELECT
			session_key,
			MIN(source_agent),
			MIN(occurred_at),
			MAX(occurred_at),
			COUNT(*),
			CASE
				WHEN SUM(CASE WHEN outcome = 'failed' THEN 1 ELSE 0 END) > 0 THEN 'failed'
				WHEN SUM(CASE WHEN outcome = 'succeeded' THEN 1 ELSE 0 END) > 0 THEN 'succeeded'
				ELSE 'unknown'
			END,
			MAX(historical)
		FROM events
		WHERE session_key = ?
		GROUP BY session_key`,
		sessionID,
	).Scan(
		&summary.SessionID,
		&summary.Harness,
		&startedAt,
		&endedAt,
		&summary.EventCount,
		&summary.Outcome,
		&historical,
	)
	if err != nil {
		return model.SessionSummary{}, time.Time{}, err
	}
	summary.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
	summary.EndedAt, _ = time.Parse(time.RFC3339Nano, endedAt)
	summary.Historical = historical == 1
	dataThrough, err := s.dataThrough(ctx)
	return summary, dataThrough, err
}

func (s *Store) GetSessionTimeline(ctx context.Context, sessionID string, limit int) ([]model.Event, time.Time, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_id, canonical_json, canonical_encoding
		FROM events
		WHERE session_key = ?
		ORDER BY source_sequence ASC, occurred_at ASC, event_id ASC
		LIMIT ?`,
		sessionID,
		limit,
	)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("get session timeline: %w", err)
	}
	defer rows.Close()

	var events []model.Event
	for rows.Next() {
		var eventID, encoding string
		var body []byte
		if err := rows.Scan(&eventID, &body, &encoding); err != nil {
			return nil, time.Time{}, err
		}
		body, err = s.cipher.open("event", eventID, "canonical_json", encoding, body)
		if err != nil {
			return nil, time.Time{}, err
		}
		var event model.Event
		if err := json.Unmarshal(body, &event); err != nil {
			return nil, time.Time{}, fmt.Errorf("decode stored canonical event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, err
	}
	dataThrough, err := s.dataThrough(ctx)
	return events, dataThrough, err
}

func (s *Store) QueryActivity(ctx context.Context, filter model.ActivityFilter) ([]model.Event, time.Time, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	var clauses []string
	var args []any
	if filter.OccurredAfter != nil {
		clauses = append(clauses, "occurred_at >= ?")
		args = append(args, filter.OccurredAfter.UTC().Format(time.RFC3339Nano))
	}
	if filter.OccurredBefore != nil {
		clauses = append(clauses, "occurred_at <= ?")
		args = append(args, filter.OccurredBefore.UTC().Format(time.RFC3339Nano))
	}
	if filter.Harness != "" {
		clauses = append(clauses, "source_agent = ?")
		args = append(args, filter.Harness)
	}
	if filter.Outcome != "" {
		clauses = append(clauses, "outcome = ?")
		args = append(args, filter.Outcome)
	}
	query := `
		SELECT event_id, canonical_json, canonical_encoding
		FROM events`
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	// Resource kind lives only in the encrypted payload. Read a bounded
	// candidate window and apply that optional filter after decryption.
	candidateLimit := filter.Limit
	if filter.ResourceKind != "" {
		candidateLimit *= 5
		if candidateLimit > 1000 {
			candidateLimit = 1000
		}
	}
	query += " ORDER BY occurred_at DESC, source_sequence DESC, event_id DESC LIMIT ?"
	args = append(args, candidateLimit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("query local activity: %w", err)
	}
	defer rows.Close()
	events := make([]model.Event, 0, filter.Limit)
	for rows.Next() {
		var eventID, encoding string
		var body []byte
		if err := rows.Scan(&eventID, &body, &encoding); err != nil {
			return nil, time.Time{}, err
		}
		body, err = s.cipher.open("event", eventID, "canonical_json", encoding, body)
		if err != nil {
			return nil, time.Time{}, err
		}
		var event model.Event
		if err := json.Unmarshal(body, &event); err != nil {
			return nil, time.Time{}, fmt.Errorf("decode stored canonical event: %w", err)
		}
		if filter.ResourceKind != "" &&
			(event.Observation.Resource == nil || event.Observation.Resource.Kind != filter.ResourceKind) {
			continue
		}
		events = append(events, event)
		if len(events) == filter.Limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, err
	}
	dataThrough, err := s.dataThrough(ctx)
	return events, dataThrough, err
}

func (s *Store) ListFindings(ctx context.Context, limit int) ([]model.FindingSummary, time.Time, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT finding_id, COALESCE(session_key, ''), detected_at, rule_id,
			rule_version, severity, source_agent, confidence,
			cited_event_ids_json, cited_event_ids_encoding
		FROM findings
		ORDER BY detected_at DESC, finding_id DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("list local findings: %w", err)
	}
	defer rows.Close()
	findings := make([]model.FindingSummary, 0, limit)
	for rows.Next() {
		var finding model.FindingSummary
		var detectedAt, encoding string
		var cited []byte
		if err := rows.Scan(
			&finding.FindingID,
			&finding.SessionID,
			&detectedAt,
			&finding.RuleID,
			&finding.RuleVersion,
			&finding.Severity,
			&finding.Harness,
			&finding.Confidence,
			&cited,
			&encoding,
		); err != nil {
			return nil, time.Time{}, err
		}
		finding.DetectedAt, err = time.Parse(time.RFC3339Nano, detectedAt)
		if err != nil {
			return nil, time.Time{}, errors.New("decode stored finding timestamp")
		}
		cited, err = s.cipher.open(
			"finding",
			finding.FindingID,
			"cited_event_ids_json",
			encoding,
			cited,
		)
		if err != nil {
			return nil, time.Time{}, err
		}
		if err := json.Unmarshal(cited, &finding.CitedEventIDs); err != nil {
			return nil, time.Time{}, errors.New("decode stored finding payload")
		}
		findings = append(findings, finding)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, err
	}
	dataThrough, err := s.dataThrough(ctx)
	return findings, dataThrough, err
}

func (s *Store) GetStats(ctx context.Context) (model.LocalStats, time.Time, error) {
	stats := model.LocalStats{
		HarnessCounts: make(map[string]int),
		OutcomeCounts: make(map[string]int),
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(DISTINCT session_key),
			(SELECT COUNT(*) FROM findings),
			COUNT(DISTINCT CASE WHEN historical = 1 THEN session_key END)
		FROM events`,
	).Scan(
		&stats.EventCount,
		&stats.SessionCount,
		&stats.FindingCount,
		&stats.HistoricalRuns,
	); err != nil {
		return model.LocalStats{}, time.Time{}, fmt.Errorf("read local stats: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_agent, COUNT(*)
		FROM events
		GROUP BY source_agent
		ORDER BY source_agent`)
	if err != nil {
		return model.LocalStats{}, time.Time{}, fmt.Errorf("read local harness stats: %w", err)
	}
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			rows.Close()
			return model.LocalStats{}, time.Time{}, err
		}
		stats.HarnessCounts[key] = count
	}
	if err := rows.Close(); err != nil {
		return model.LocalStats{}, time.Time{}, err
	}
	rows, err = s.db.QueryContext(ctx, `
		SELECT outcome, COUNT(*)
		FROM events
		GROUP BY outcome
		ORDER BY outcome`)
	if err != nil {
		return model.LocalStats{}, time.Time{}, fmt.Errorf("read local outcome stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return model.LocalStats{}, time.Time{}, err
		}
		stats.OutcomeCounts[key] = count
	}
	if err := rows.Err(); err != nil {
		return model.LocalStats{}, time.Time{}, err
	}
	dataThrough, err := s.dataThrough(ctx)
	return stats, dataThrough, err
}

func (s *Store) GetFinding(ctx context.Context, findingID string) (Finding, error) {
	var result Finding
	var sessionKey sql.NullString
	var detectedAt, encoding string
	var cited []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT finding_id, source_run_id, session_key, detected_at, rule_id,
			rule_version, severity, source_agent, confidence,
			cited_event_ids_json, cited_event_ids_encoding
		FROM findings
		WHERE finding_id = ?`,
		findingID,
	).Scan(
		&result.FindingID,
		&result.SourceRunID,
		&sessionKey,
		&detectedAt,
		&result.RuleID,
		&result.RuleVersion,
		&result.Severity,
		&result.SourceAgent,
		&result.Confidence,
		&cited,
		&encoding,
	)
	if err != nil {
		return Finding{}, err
	}
	result.SessionKey = sessionKey.String
	result.DetectedAt, err = time.Parse(time.RFC3339Nano, detectedAt)
	if err != nil {
		return Finding{}, errors.New("decode stored finding timestamp")
	}
	cited, err = s.cipher.open(
		"finding",
		findingID,
		"cited_event_ids_json",
		encoding,
		cited,
	)
	if err != nil {
		return Finding{}, err
	}
	if err := json.Unmarshal(cited, &result.CitedEventIDs); err != nil {
		return Finding{}, errors.New("decode stored finding payload")
	}
	return result, nil
}

func (s *Store) ImportRun(ctx context.Context, runID string) (ImportSummary, error) {
	var result ImportSummary
	var complete int
	err := s.db.QueryRowContext(ctx, `
		SELECT source_run_id, status, complete, artifacts_scanned, events_emitted,
			findings_emitted, indicators_emitted, diagnostics
		FROM import_runs WHERE source_run_id = ?`,
		runID,
	).Scan(
		&result.SourceRunID,
		&result.Status,
		&complete,
		&result.ArtifactsScanned,
		&result.EventsEmitted,
		&result.FindingsEmitted,
		&result.IndicatorsEmitted,
		&result.Diagnostics,
	)
	result.Complete = complete == 1
	return result, err
}

func (s *Store) Quarantines(ctx context.Context) ([]Quarantine, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(source_run_id, ''), line_number, category, reason, record_sha256
		FROM quarantine
		ORDER BY id`)
	if err != nil {
		return nil, errors.New("list quarantine diagnostics")
	}
	defer rows.Close()
	var result []Quarantine
	for rows.Next() {
		var quarantine Quarantine
		if err := rows.Scan(
			&quarantine.SourceRunID,
			&quarantine.LineNumber,
			&quarantine.Category,
			&quarantine.Reason,
			&quarantine.RecordSHA256,
		); err != nil {
			return nil, errors.New("read quarantine diagnostic")
		}
		result = append(result, quarantine)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("list quarantine diagnostics")
	}
	return result, nil
}

func (s *Store) DiagnosticCodes(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT code FROM diagnostics ORDER BY id")
	if err != nil {
		return nil, errors.New("list diagnostic codes")
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, errors.New("read diagnostic code")
		}
		result = append(result, code)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("list diagnostic codes")
	}
	return result, nil
}

func (s *Store) Count(ctx context.Context, table string) (int, error) {
	allowed := map[string]bool{
		"events": true, "findings": true, "import_runs": true,
		"quarantine": true, "diagnostics": true, "local_store_metadata": true,
		"finding_event_citations": true,
	}
	if !allowed[table] {
		return 0, errors.New("unsupported count table")
	}
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
	return count, err
}

func (s *Store) dataThrough(ctx context.Context) (time.Time, error) {
	var value sql.NullString
	if err := s.db.QueryRowContext(ctx, "SELECT MAX(observed_at) FROM events").Scan(&value); err != nil {
		return time.Time{}, err
	}
	if !value.Valid {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value.String)
}

func (s *Store) migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for index, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version := index + 1
		var applied int
		err := s.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version,
		).Scan(&applied)
		if err != nil && !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if applied > 0 {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)",
			version, time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
