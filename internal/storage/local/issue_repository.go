package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
)

const issueCursorLifetime = 15 * time.Minute

var ErrIssueSnapshotExpired = errors.New("issue snapshot has expired")

func (s *Store) QueryIssues(
	ctx context.Context,
	query model.IssueQuery,
) (model.IssuePage, error) {
	query.Limit = boundedReadLimit(query.Limit, 20, 100)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return model.IssuePage{}, errors.New("begin issue snapshot read")
	}
	defer tx.Rollback()
	snapshot, err := issueSnapshot(ctx, tx, query.Snapshot, query.IssuedAt)
	if err != nil {
		return model.IssuePage{}, err
	}
	matchClauses := []string{"1 = 1"}
	matchArgs := make([]any, 0, 16)
	if query.Filter.Harness != "" {
		matchClauses = append(matchClauses, "LOWER(harness) = LOWER(?)")
		matchArgs = append(matchArgs, query.Filter.Harness)
	}
	if query.Filter.ObservedAfter != nil {
		matchClauses = append(matchClauses, "last_observed_at >= ?")
		matchArgs = append(matchArgs, formatProjectionTime(*query.Filter.ObservedAfter))
	}
	if query.Filter.SessionID != "" {
		matchClauses = append(matchClauses, "session_key = ?")
		matchArgs = append(matchArgs, query.Filter.SessionID)
	}

	summaryClauses := []string{"1 = 1"}
	summaryArgs := make([]any, 0, 16)
	if query.Filter.Severity != "" {
		summaryClauses = append(summaryClauses, "severity = ?")
		summaryArgs = append(summaryArgs, query.Filter.Severity)
	}
	if query.Filter.Category != "" {
		summaryClauses = append(summaryClauses, "category = ?")
		summaryArgs = append(summaryArgs, query.Filter.Category)
	}
	if query.Filter.Origin != "" {
		summaryClauses = append(summaryClauses, "origin = ?")
		summaryArgs = append(summaryArgs, query.Filter.Origin)
	}
	if query.Filter.AnalysisStatus != "" {
		summaryClauses = append(summaryClauses, "analysis_status = ?")
		summaryArgs = append(summaryArgs, query.Filter.AnalysisStatus)
	}
	if query.Filter.Recurrence == "single" {
		summaryClauses = append(summaryClauses, "repeated = 0")
	} else if query.Filter.Recurrence == "repeated" {
		summaryClauses = append(summaryClauses, "repeated = 1")
	}
	if query.Filter.FingerprintID != "" {
		summaryClauses = append(summaryClauses, "fingerprint_id = ?")
		summaryArgs = append(summaryArgs, query.Filter.FingerprintID)
	}
	if query.Filter.IssueID != "" {
		summaryClauses = append(summaryClauses, "issue_id = ?")
		summaryArgs = append(summaryArgs, query.Filter.IssueID)
	}
	if query.Cursor != nil {
		repeated := 0
		if query.Cursor.Repeated {
			repeated = 1
		}
		summaryClauses = append(summaryClauses, `(
			severity_rank < ? OR
			(severity_rank = ? AND repeated < ?) OR
			(severity_rank = ? AND repeated = ? AND last_observed_at < ?) OR
			(severity_rank = ? AND repeated = ? AND last_observed_at = ? AND issue_id > ?)
		)`)
		lastObserved := formatProjectionTime(query.Cursor.LastObserved)
		summaryArgs = append(
			summaryArgs,
			query.Cursor.SeverityRank,
			query.Cursor.SeverityRank,
			repeated,
			query.Cursor.SeverityRank,
			repeated,
			lastObserved,
			query.Cursor.SeverityRank,
			repeated,
			lastObserved,
			query.Cursor.IssueID,
		)
	}

	args := []any{snapshot, snapshot, snapshot, snapshot}
	args = append(args, matchArgs...)
	args = append(args, summaryArgs...)
	args = append(args, query.Limit+1)
	rows, err := tx.QueryContext(ctx, `
		WITH visible AS (
			SELECT
				io.issue_id,
				io.fingerprint_id,
				io.fingerprint_version,
				io.origin,
				io.session_key,
				io.harness,
				io.detector_id,
				io.detector_version,
				io.category,
				io.title_code,
				io.severity,
				CASE io.severity
					WHEN 'critical' THEN 5
					WHEN 'high' THEN 4
					WHEN 'medium' THEN 3
					WHEN 'low' THEN 2
					ELSE 1
				END AS severity_rank,
				io.confidence,
				CASE io.confidence
					WHEN 'low' THEN 1
					WHEN 'medium' THEN 2
					ELSE 3
				END AS confidence_rank,
				io.scope_quality,
				io.first_observed_at,
				io.last_observed_at,
				io.evidence_complete,
				io.retained_history_only,
				sar.status AS analysis_status,
				CASE sar.status
					WHEN 'failed' THEN 4
					WHEN 'pending' THEN 3
					WHEN 'truncated' THEN 2
					ELSE 1
				END AS analysis_status_rank
			FROM issue_occurrences io
			JOIN session_analysis_revisions sar
				ON sar.session_key = io.session_key
				AND sar.visible_from_generation <= ?
				AND (
					sar.visible_until_generation IS NULL OR
					sar.visible_until_generation > ?
				)
			WHERE io.visible_from_generation <= ?
				AND (
					io.visible_until_generation IS NULL OR
					io.visible_until_generation > ?
				)
		),
		eligible AS (
			SELECT DISTINCT issue_id
			FROM visible
			WHERE `+strings.Join(matchClauses, " AND ")+`
		),
		grouped AS (
			SELECT
				v.issue_id,
				MIN(v.fingerprint_id) AS fingerprint_id,
				MIN(v.fingerprint_version) AS fingerprint_version,
				MIN(v.origin) AS origin,
				MIN(v.detector_id) AS detector_id,
				MAX(v.detector_version) AS detector_version,
				MIN(v.category) AS category,
				MIN(v.title_code) AS title_code,
				MAX(v.severity_rank) AS severity_rank,
				CASE MAX(v.severity_rank)
					WHEN 5 THEN 'critical'
					WHEN 4 THEN 'high'
					WHEN 3 THEN 'medium'
					WHEN 2 THEN 'low'
					ELSE 'info'
				END AS severity,
				CASE MIN(v.confidence_rank)
					WHEN 1 THEN 'low'
					WHEN 2 THEN 'medium'
					ELSE 'high'
				END AS confidence,
				CASE MAX(
					CASE v.scope_quality
						WHEN 'conflict' THEN 4
						WHEN 'unscoped' THEN 3
						WHEN 'lexical' THEN 2
						ELSE 1
					END
				)
					WHEN 4 THEN 'conflict'
					WHEN 3 THEN 'unscoped'
					WHEN 2 THEN 'lexical'
					ELSE 'resolved'
				END AS scope_quality,
				MIN(v.first_observed_at) AS first_observed_at,
				MAX(v.last_observed_at) AS last_observed_at,
				COUNT(*) AS occurrence_count,
				COUNT(DISTINCT v.session_key) AS session_count,
				CASE WHEN COUNT(DISTINCT v.session_key) >= 2 THEN 1 ELSE 0 END AS repeated,
				GROUP_CONCAT(DISTINCT v.harness) AS harnesses,
				CASE MAX(v.analysis_status_rank)
					WHEN 4 THEN 'failed'
					WHEN 3 THEN 'pending'
					WHEN 2 THEN 'truncated'
					ELSE 'current'
				END AS analysis_status,
				MIN(v.evidence_complete) AS evidence_complete,
				MIN(v.retained_history_only) AS retained_history_only
			FROM visible v
			JOIN eligible e ON e.issue_id = v.issue_id
			GROUP BY v.issue_id
		)
		SELECT
			issue_id, fingerprint_id, fingerprint_version, origin,
			detector_id, detector_version, category, title_code, severity,
			severity_rank, confidence, scope_quality, first_observed_at,
			last_observed_at, occurrence_count, session_count, repeated,
			harnesses, analysis_status, evidence_complete, retained_history_only
		FROM grouped
		WHERE `+strings.Join(summaryClauses, " AND ")+`
		ORDER BY severity_rank DESC, repeated DESC, last_observed_at DESC, issue_id ASC
		LIMIT ?`,
		args...,
	)
	if err != nil {
		return model.IssuePage{}, fmt.Errorf("query issues: %w", err)
	}
	defer rows.Close()
	summaries := make([]model.IssueSummary, 0, query.Limit+1)
	for rows.Next() {
		summary, err := scanIssueSummary(rows)
		if err != nil {
			return model.IssuePage{}, err
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return model.IssuePage{}, err
	}
	hasMore := len(summaries) > query.Limit
	if hasMore {
		summaries = summaries[:query.Limit]
	}
	coverage, err := issueAnalysisCoverage(ctx, tx, snapshot)
	if err != nil {
		return model.IssuePage{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.IssuePage{}, errors.New("complete issue snapshot read")
	}
	return model.IssuePage{
		Data:     summaries,
		Analysis: coverage,
		Snapshot: snapshot,
		HasMore:  hasMore,
	}, nil
}

func (s *Store) QueryIssueOccurrences(
	ctx context.Context,
	query model.IssueOccurrenceQuery,
) (model.IssueOccurrencePage, error) {
	if query.IssueID == "" {
		return model.IssueOccurrencePage{}, errors.New("issue ID is required")
	}
	query.Limit = boundedReadLimit(query.Limit, 20, 100)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return model.IssueOccurrencePage{}, errors.New("begin issue occurrence snapshot read")
	}
	defer tx.Rollback()
	snapshot, err := issueSnapshot(ctx, tx, query.Snapshot, query.IssuedAt)
	if err != nil {
		return model.IssueOccurrencePage{}, err
	}
	clauses := []string{
		"io.issue_id = ?",
		"io.visible_from_generation <= ?",
		"(io.visible_until_generation IS NULL OR io.visible_until_generation > ?)",
		"sar.visible_from_generation <= ?",
		"(sar.visible_until_generation IS NULL OR sar.visible_until_generation > ?)",
	}
	args := []any{query.IssueID, snapshot, snapshot, snapshot, snapshot}
	if query.Cursor != nil {
		clauses = append(clauses, `(
			io.last_observed_at < ? OR
			(io.last_observed_at = ? AND io.occurrence_id > ?)
		)`)
		lastObserved := formatProjectionTime(query.Cursor.LastObserved)
		args = append(args, lastObserved, lastObserved, query.Cursor.OccurrenceID)
	}
	args = append(args, query.Limit+1)
	rows, err := tx.QueryContext(ctx, `
		SELECT
			io.revision_id, io.occurrence_id, io.issue_id, io.fingerprint_id,
			io.fingerprint_version, io.origin, COALESCE(io.origin_record_id, ''),
			io.session_key, io.harness, io.detector_id, io.detector_version,
			io.projection_version, io.category, io.title_code, io.severity,
			io.confidence, io.scope_quality, io.first_observed_at,
			io.last_observed_at, io.evidence_complete, io.retained_history_only,
			sar.status, io.analysis_generation, io.evidence_payload,
			io.evidence_encoding
		FROM issue_occurrences io
		JOIN session_analysis_revisions sar ON sar.session_key = io.session_key
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY io.last_observed_at DESC, io.occurrence_id ASC
		LIMIT ?`,
		args...,
	)
	if err != nil {
		return model.IssueOccurrencePage{}, fmt.Errorf("query issue occurrences: %w", err)
	}
	defer rows.Close()
	occurrences := make([]model.IssueOccurrence, 0, query.Limit+1)
	for rows.Next() {
		occurrence, err := s.scanIssueOccurrence(rows)
		if err != nil {
			return model.IssueOccurrencePage{}, err
		}
		occurrences = append(occurrences, occurrence)
	}
	if err := rows.Err(); err != nil {
		return model.IssueOccurrencePage{}, err
	}
	hasMore := len(occurrences) > query.Limit
	if hasMore {
		occurrences = occurrences[:query.Limit]
	}
	if err := tx.Commit(); err != nil {
		return model.IssueOccurrencePage{}, errors.New("complete issue occurrence snapshot read")
	}
	return model.IssueOccurrencePage{
		Data:     occurrences,
		Snapshot: snapshot,
		HasMore:  hasMore,
	}, nil
}

func issueSnapshot(
	ctx context.Context,
	queryer queryRower,
	requested int64,
	issuedAt time.Time,
) (int64, error) {
	var current, oldest int64
	if err := queryer.QueryRowContext(ctx, `
		SELECT current_generation, oldest_retained_generation
		FROM issue_projection_metadata
		WHERE singleton = 1`,
	).Scan(&current, &oldest); err != nil {
		return 0, errors.New("read issue projection generation")
	}
	if requested == 0 {
		return current, nil
	}
	if issuedAt.IsZero() {
		return 0, ErrIssueSnapshotExpired
	}
	if requested < oldest || requested > current {
		return 0, ErrIssueSnapshotExpired
	}
	if !issuedAt.IsZero() {
		now := time.Now()
		if issuedAt.After(now) || now.Sub(issuedAt) > issueCursorLifetime {
			return 0, ErrIssueSnapshotExpired
		}
	}
	return requested, nil
}

func issueAnalysisCoverage(
	ctx context.Context,
	queryer queryRower,
	snapshot int64,
) (model.IssueAnalysisCoverage, error) {
	var result model.IssueAnalysisCoverage
	var analysisThrough sql.NullString
	if err := queryer.QueryRowContext(ctx, `
		WITH event_sessions AS (
			SELECT DISTINCT session_key FROM events
		),
		visible_analysis AS (
			SELECT session_key, status, scope_quality, created_at
			FROM session_analysis_revisions
			WHERE visible_from_generation <= ?
				AND (visible_until_generation IS NULL OR visible_until_generation > ?)
		)
		SELECT
			COALESCE(SUM(CASE WHEN va.status = 'current' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN va.status = 'pending' OR va.status IS NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN va.status = 'failed' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN va.status = 'truncated' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN COALESCE(va.scope_quality, 'unscoped') = 'unscoped'
				THEN 1 ELSE 0 END), 0),
			MAX(CASE WHEN va.status = 'current' THEN va.created_at END)
		FROM event_sessions es
		LEFT JOIN visible_analysis va ON va.session_key = es.session_key`,
		snapshot,
		snapshot,
	).Scan(
		&result.CurrentSessions,
		&result.PendingSessions,
		&result.FailedSessions,
		&result.TruncatedSessions,
		&result.UnscopedSessions,
		&analysisThrough,
	); err != nil {
		return model.IssueAnalysisCoverage{}, errors.New("read issue analysis coverage")
	}
	if analysisThrough.Valid {
		parsed, err := parseProjectionTime(analysisThrough.String)
		if err != nil {
			return model.IssueAnalysisCoverage{}, errors.New("decode issue analysis timestamp")
		}
		result.AnalysisThrough = parsed
	}
	result.Complete = result.PendingSessions == 0 &&
		result.FailedSessions == 0 &&
		result.TruncatedSessions == 0
	return result, nil
}

func scanIssueSummary(row rowScanner) (model.IssueSummary, error) {
	var result model.IssueSummary
	var severityRank, sessionCount, repeated int
	var firstObserved, lastObserved, harnesses string
	var evidenceComplete, retainedHistoryOnly int
	if err := row.Scan(
		&result.IssueID,
		&result.FingerprintID,
		&result.FingerprintVersion,
		&result.Origin,
		&result.DetectorID,
		&result.DetectorVersion,
		&result.Category,
		&result.TitleCode,
		&result.Severity,
		&severityRank,
		&result.Confidence,
		&result.ScopeQuality,
		&firstObserved,
		&lastObserved,
		&result.OccurrenceCount,
		&sessionCount,
		&repeated,
		&harnesses,
		&result.AnalysisStatus,
		&evidenceComplete,
		&retainedHistoryOnly,
	); err != nil {
		return model.IssueSummary{}, err
	}
	result.SessionCount = sessionCount
	result.EvidenceComplete = evidenceComplete == 1
	result.RetainedHistoryOnly = retainedHistoryOnly == 1
	var err error
	result.FirstObservedAt, err = parseProjectionTime(firstObserved)
	if err != nil {
		return model.IssueSummary{}, errors.New("decode issue first-observed timestamp")
	}
	result.LastObservedAt, err = parseProjectionTime(lastObserved)
	if err != nil {
		return model.IssueSummary{}, errors.New("decode issue last-observed timestamp")
	}
	if harnesses != "" {
		result.Harnesses = strings.Split(harnesses, ",")
		sort.Strings(result.Harnesses)
	}
	return result, nil
}

func (s *Store) scanIssueOccurrence(row rowScanner) (model.IssueOccurrence, error) {
	var revisionID string
	var result model.IssueOccurrence
	var firstObserved, lastObserved, encoding string
	var evidenceComplete, retainedHistoryOnly int
	var evidence []byte
	if err := row.Scan(
		&revisionID,
		&result.OccurrenceID,
		&result.IssueID,
		&result.FingerprintID,
		&result.FingerprintVersion,
		&result.Origin,
		&result.OriginRecordID,
		&result.SessionID,
		&result.Harness,
		&result.Provenance.DetectorID,
		&result.Provenance.DetectorVersion,
		&result.Provenance.ProjectionVersion,
		&result.Category,
		&result.TitleCode,
		&result.Severity,
		&result.Confidence,
		&result.ScopeQuality,
		&firstObserved,
		&lastObserved,
		&evidenceComplete,
		&retainedHistoryOnly,
		&result.AnalysisStatus,
		&result.AnalysisGeneration,
		&evidence,
		&encoding,
	); err != nil {
		return model.IssueOccurrence{}, err
	}
	result.Provenance.FingerprintVersion = result.FingerprintVersion
	result.EvidenceComplete = evidenceComplete == 1
	result.RetainedHistoryOnly = retainedHistoryOnly == 1
	var err error
	result.FirstObservedAt, err = parseProjectionTime(firstObserved)
	if err != nil {
		return model.IssueOccurrence{}, errors.New("decode issue occurrence start")
	}
	result.LastObservedAt, err = parseProjectionTime(lastObserved)
	if err != nil {
		return model.IssueOccurrence{}, errors.New("decode issue occurrence end")
	}
	evidence, err = s.cipher.open(
		"issue_occurrence",
		revisionID,
		"evidence_payload",
		encoding,
		evidence,
	)
	if err != nil {
		return model.IssueOccurrence{}, err
	}
	if err := json.Unmarshal(evidence, &result.Evidence); err != nil {
		return model.IssueOccurrence{}, errors.New("decode issue occurrence evidence")
	}
	return result, nil
}
