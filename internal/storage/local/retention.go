package local

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RetentionPolicy has no implicit defaults. At least one positive bound must be
// supplied by the caller. MaxPayloadBytes covers encrypted event and finding
// payload blobs, not SQLite indexes or page overhead.
type RetentionPolicy struct {
	MaxAge          time.Duration `json:"max_age"`
	MaxEventCount   int           `json:"max_event_count"`
	MaxPayloadBytes int64         `json:"max_payload_bytes"`
}

type RetentionDiagnostics struct {
	Policy               RetentionPolicy `json:"policy"`
	CurrentEventCount    int             `json:"current_event_count"`
	CurrentFindingCount  int             `json:"current_finding_count"`
	CurrentPayloadBytes  int64           `json:"current_payload_bytes"`
	EligibleEventCount   int             `json:"eligible_event_count"`
	EligibleFindingCount int             `json:"eligible_finding_count"`
	EligiblePayloadBytes int64           `json:"eligible_payload_bytes"`
	OldestPayloadAt      *time.Time      `json:"oldest_payload_at,omitempty"`
	NewestPayloadAt      *time.Time      `json:"newest_payload_at,omitempty"`
	EvaluatedAt          time.Time       `json:"evaluated_at"`
}

type PruneResult struct {
	Before             RetentionDiagnostics `json:"before"`
	PrunedEventCount   int                  `json:"pruned_event_count"`
	PrunedFindingCount int                  `json:"pruned_finding_count"`
	PrunedPayloadBytes int64                `json:"pruned_payload_bytes"`
	AfterEventCount    int                  `json:"after_event_count"`
	AfterFindingCount  int                  `json:"after_finding_count"`
	AfterPayloadBytes  int64                `json:"after_payload_bytes"`
	Maintenance        MaintenanceResult    `json:"maintenance"`
}

type MaintenanceResult struct {
	State     string `json:"state"`
	Code      string `json:"code,omitempty"`
	Retryable bool   `json:"retryable"`
}

type retentionItem struct {
	recordType string
	recordID   string
	occurredAt time.Time
	sequence   int64
	bytes      int64
	selected   bool
}

func (policy RetentionPolicy) validate() error {
	if policy.MaxAge < 0 || policy.MaxEventCount < 0 || policy.MaxPayloadBytes < 0 {
		return errors.New("retention bounds cannot be negative")
	}
	if policy.MaxAge == 0 && policy.MaxEventCount == 0 && policy.MaxPayloadBytes == 0 {
		return errors.New("at least one retention bound is required")
	}
	return nil
}

func (s *Store) RetentionDiagnostics(
	ctx context.Context,
	policy RetentionPolicy,
	now time.Time,
) (RetentionDiagnostics, error) {
	if err := policy.validate(); err != nil {
		return RetentionDiagnostics{}, err
	}
	if now.IsZero() {
		return RetentionDiagnostics{}, errors.New("retention evaluation time is required")
	}
	items, err := readRetentionItems(ctx, s.db)
	if err != nil {
		return RetentionDiagnostics{}, err
	}
	dependencies, err := readCitationDependencies(ctx, s.db)
	if err != nil {
		return RetentionDiagnostics{}, err
	}
	return evaluateRetention(policy, now.UTC(), items, dependencies), nil
}

func (s *Store) Prune(
	ctx context.Context,
	policy RetentionPolicy,
	now time.Time,
) (PruneResult, error) {
	if err := policy.validate(); err != nil {
		return PruneResult{}, err
	}
	if now.IsZero() {
		return PruneResult{}, errors.New("retention evaluation time is required")
	}
	now = now.UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PruneResult{}, errors.New("begin local retention prune")
	}
	defer tx.Rollback()
	items, err := readRetentionItems(ctx, tx)
	if err != nil {
		return PruneResult{}, err
	}
	dependencies, err := readCitationDependencies(ctx, tx)
	if err != nil {
		return PruneResult{}, err
	}
	before := evaluateRetention(policy, now, items, dependencies)
	result := PruneResult{Before: before}
	if err := s.mutations.with(mutationRetentionPrune, func() error {
		for _, recordType := range []string{"finding", "event"} {
			for _, item := range items {
				if !item.selected || item.recordType != recordType {
					continue
				}
				var statement string
				switch item.recordType {
				case "event":
					statement = "DELETE FROM events WHERE event_id = ?"
					result.PrunedEventCount++
				case "finding":
					statement = "DELETE FROM findings WHERE finding_id = ?"
					result.PrunedFindingCount++
				default:
					return errors.New("unsupported local retention record")
				}
				deleted, err := tx.ExecContext(ctx, statement, item.recordID)
				if err != nil {
					return errors.New("delete local retention record")
				}
				count, err := deleted.RowsAffected()
				if err != nil || count != 1 {
					return errors.New("local retention record changed during prune")
				}
				result.PrunedPayloadBytes += item.bytes
			}
		}
		return nil
	}); err != nil {
		return PruneResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return PruneResult{}, errors.New("commit local retention prune")
	}
	result.AfterEventCount = before.CurrentEventCount - result.PrunedEventCount
	result.AfterFindingCount = before.CurrentFindingCount - result.PrunedFindingCount
	result.AfterPayloadBytes = before.CurrentPayloadBytes - result.PrunedPayloadBytes
	result.Maintenance = s.runPostPruneMaintenance(ctx)
	return result, nil
}

type retentionQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readCitationDependencies(
	ctx context.Context,
	querier retentionQuerier,
) (map[string][]string, error) {
	rows, err := querier.QueryContext(ctx, `
		SELECT event_id, finding_id
		FROM finding_event_citations
		ORDER BY event_id, finding_id`)
	if err != nil {
		return nil, errors.New("read finding citation dependencies")
	}
	defer rows.Close()
	result := make(map[string][]string)
	for rows.Next() {
		var eventID, findingID string
		if err := rows.Scan(&eventID, &findingID); err != nil {
			return nil, errors.New("read finding citation dependency")
		}
		result[eventID] = append(result[eventID], findingID)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("read finding citation dependencies")
	}
	return result, nil
}

func readRetentionItems(ctx context.Context, querier retentionQuerier) ([]retentionItem, error) {
	rows, err := querier.QueryContext(ctx, `
		SELECT record_type, record_id, occurred_at, source_sequence, payload_bytes
		FROM (
			SELECT
				'event' AS record_type,
				event_id AS record_id,
				occurred_at,
				source_sequence,
				LENGTH(canonical_json) AS payload_bytes
			FROM events
			UNION ALL
			SELECT
				'finding' AS record_type,
				finding_id AS record_id,
				detected_at AS occurred_at,
				0 AS source_sequence,
				LENGTH(cited_event_ids_json) AS payload_bytes
			FROM findings
		)
		ORDER BY occurred_at ASC, source_sequence ASC, record_id ASC, record_type ASC`)
	if err != nil {
		return nil, errors.New("read local retention records")
	}
	defer rows.Close()
	var items []retentionItem
	for rows.Next() {
		var item retentionItem
		var occurredAt string
		if err := rows.Scan(
			&item.recordType,
			&item.recordID,
			&occurredAt,
			&item.sequence,
			&item.bytes,
		); err != nil {
			return nil, errors.New("read local retention record")
		}
		item.occurredAt, err = time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, errors.New("decode local retention timestamp")
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("read local retention records")
	}
	return items, nil
}

func evaluateRetention(
	policy RetentionPolicy,
	now time.Time,
	items []retentionItem,
	dependencies map[string][]string,
) RetentionDiagnostics {
	diagnostics := RetentionDiagnostics{
		Policy:      policy,
		EvaluatedAt: now,
	}
	if len(items) > 0 {
		oldest := items[0].occurredAt
		newest := items[len(items)-1].occurredAt
		diagnostics.OldestPayloadAt = &oldest
		diagnostics.NewestPayloadAt = &newest
	}
	findingIndexes := make(map[string]int)
	for index, item := range items {
		diagnostics.CurrentPayloadBytes += item.bytes
		if item.recordType == "event" {
			diagnostics.CurrentEventCount++
		} else {
			diagnostics.CurrentFindingCount++
			findingIndexes[item.recordID] = index
		}
	}

	remainingEvents := diagnostics.CurrentEventCount
	remainingBytes := diagnostics.CurrentPayloadBytes
	var cutoff time.Time
	if policy.MaxAge > 0 {
		cutoff = now.Add(-policy.MaxAge)
	}
	selectItem := func(index int) {
		item := &items[index]
		if item.selected {
			return
		}
		item.selected = true
		remainingBytes -= item.bytes
		if item.recordType == "event" {
			remainingEvents--
		}
	}
	for index := range items {
		item := &items[index]
		if item.selected {
			continue
		}
		expired := policy.MaxAge > 0 && item.occurredAt.Before(cutoff)
		overCount := policy.MaxEventCount > 0 &&
			item.recordType == "event" &&
			remainingEvents > policy.MaxEventCount
		overBytes := policy.MaxPayloadBytes > 0 &&
			remainingBytes > policy.MaxPayloadBytes
		if !expired && !overCount && !overBytes {
			continue
		}
		selectItem(index)
		if item.recordType == "event" {
			for _, findingID := range dependencies[item.recordID] {
				if findingIndex, ok := findingIndexes[findingID]; ok {
					selectItem(findingIndex)
				}
			}
		}
	}
	for _, item := range items {
		if !item.selected {
			continue
		}
		diagnostics.EligiblePayloadBytes += item.bytes
		if item.recordType == "event" {
			diagnostics.EligibleEventCount++
		} else {
			diagnostics.EligibleFindingCount++
		}
	}
	return diagnostics
}

func (s *Store) runPostPruneMaintenance(ctx context.Context) MaintenanceResult {
	pending := func(code string) MaintenanceResult {
		return MaintenanceResult{State: "pending", Code: code, Retryable: true}
	}
	if err := s.truncateWAL(ctx); err != nil {
		if errors.Is(err, ErrMaintenanceBusy) {
			return pending("wal_checkpoint_busy")
		}
		return pending("wal_checkpoint_failed")
	}
	if _, err := s.db.ExecContext(ctx, "VACUUM"); err != nil {
		if isSQLiteBusy(err) {
			return pending("vacuum_busy")
		}
		return pending("vacuum_failed")
	}
	if err := s.truncateWAL(ctx); err != nil {
		if errors.Is(err, ErrMaintenanceBusy) {
			return pending("post_vacuum_checkpoint_busy")
		}
		return pending("post_vacuum_checkpoint_failed")
	}
	return MaintenanceResult{State: "complete"}
}
