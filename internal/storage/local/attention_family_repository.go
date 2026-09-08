package local

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
	"github.com/DoplexLabs/belay-engine/internal/canonical/sourcecatalog"
)

type attentionFamilyAggregate struct {
	summary   model.AttentionFamilySummary
	members   []model.IssueSummary
	sessions  map[string]struct{}
	harnesses map[string]struct{}
}

type attentionFamilyIssue struct {
	summary          model.IssueSummary
	severityRank     int
	mapping          sourcecatalog.Mapping
	mapped           bool
	rawFindingValid  bool
	matchedCount     int
	matchedSessions  map[string]struct{}
	matchedHarnesses map[string]struct{}
	firstObservedAt  time.Time
	lastObservedAt   time.Time
}

type attentionFamilyRow struct {
	summary            model.IssueSummary
	severityRank       int
	sessionID          string
	harness            string
	firstObservedAt    time.Time
	lastObservedAt     time.Time
	originRecordID     string
	findingID          string
	findingSessionID   string
	findingRuleID      string
	findingRuleVersion string
}

func (s *Store) QueryAttentionFamilies(
	ctx context.Context,
	query model.AttentionFamilyQuery,
) (model.AttentionFamilyPage, error) {
	query.Limit = boundedReadLimit(query.Limit, 20, 100)
	if err := normalizeAttentionFamilyFilter(&query.Filter); err != nil {
		return model.AttentionFamilyPage{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if ctx.Err() != nil {
			return model.AttentionFamilyPage{}, ctx.Err()
		}
		return model.AttentionFamilyPage{}, errors.New("begin attention family snapshot read")
	}
	defer tx.Rollback()
	state, err := s.issueMaterializedSnapshot(
		ctx,
		tx,
		query.CursorEpoch,
		query.Snapshot,
		query.RetentionGeneration,
		query.IssuedAt,
	)
	if err != nil {
		return model.AttentionFamilyPage{}, err
	}
	aggregates, err := s.loadAttentionFamiliesTx(
		ctx,
		tx,
		state.snapshot,
		query.Filter,
	)
	if err != nil {
		return model.AttentionFamilyPage{}, err
	}
	sort.Slice(aggregates, func(i, j int) bool {
		return attentionFamilyLess(aggregates[i].summary, aggregates[j].summary)
	})
	summaries := make([]model.AttentionFamilySummary, 0, query.Limit+1)
	for _, aggregate := range aggregates {
		if query.Cursor != nil &&
			!attentionFamilyAfterPosition(aggregate.summary, *query.Cursor) {
			continue
		}
		summaries = append(summaries, aggregate.summary)
		if len(summaries) == query.Limit+1 {
			break
		}
	}
	hasMore := len(summaries) > query.Limit
	if hasMore {
		summaries = summaries[:query.Limit]
	}
	coverage, err := materializedIssueAnalysisCoverage(ctx, tx, state.snapshot)
	if err != nil {
		return model.AttentionFamilyPage{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AttentionFamilyPage{}, errors.New("complete attention family snapshot read")
	}
	return model.AttentionFamilyPage{
		Data:                summaries,
		Analysis:            coverage,
		CursorEpoch:         state.epoch,
		Snapshot:            state.snapshot,
		RetentionGeneration: state.retentionGeneration,
		IssuedAt:            state.issuedAt,
		HasMore:             hasMore,
	}, nil
}

func (s *Store) QueryAttentionFamilyMembers(
	ctx context.Context,
	query model.AttentionFamilyMemberQuery,
) (model.AttentionFamilyMemberPage, error) {
	query.Limit = boundedReadLimit(query.Limit, 20, 100)
	if query.FamilyID == "" || query.GroupKey == "" {
		return model.AttentionFamilyMemberPage{}, errors.New("attention family identity is required")
	}
	if err := normalizeAttentionFamilyFilter(&query.Filter); err != nil {
		return model.AttentionFamilyMemberPage{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if ctx.Err() != nil {
			return model.AttentionFamilyMemberPage{}, ctx.Err()
		}
		return model.AttentionFamilyMemberPage{}, errors.New("begin attention family member read")
	}
	defer tx.Rollback()
	state, err := s.issueMaterializedSnapshot(
		ctx,
		tx,
		query.CursorEpoch,
		query.Snapshot,
		query.RetentionGeneration,
		query.IssuedAt,
	)
	if err != nil {
		return model.AttentionFamilyMemberPage{}, err
	}
	aggregates, err := s.loadAttentionFamiliesTx(
		ctx,
		tx,
		state.snapshot,
		query.Filter,
	)
	if err != nil {
		return model.AttentionFamilyMemberPage{}, err
	}
	var selected *attentionFamilyAggregate
	for index := range aggregates {
		aggregate := &aggregates[index]
		if aggregate.summary.GroupKey == query.GroupKey &&
			aggregate.summary.FamilyID == query.FamilyID &&
			aggregate.summary.Kind == model.AttentionFamilyKindMappedUpstream {
			selected = aggregate
			break
		}
	}
	coverage, err := materializedIssueAnalysisCoverage(ctx, tx, state.snapshot)
	if err != nil {
		return model.AttentionFamilyMemberPage{}, err
	}
	if selected == nil {
		if err := tx.Commit(); err != nil {
			return model.AttentionFamilyMemberPage{}, errors.New("complete absent attention family read")
		}
		return model.AttentionFamilyMemberPage{
			Analysis:            coverage,
			CursorEpoch:         state.epoch,
			Snapshot:            state.snapshot,
			RetentionGeneration: state.retentionGeneration,
			IssuedAt:            state.issuedAt,
		}, nil
	}
	sort.Slice(selected.members, func(i, j int) bool {
		return attentionFamilyMemberLess(selected.members[i], selected.members[j])
	})
	members := make([]model.IssueSummary, 0, query.Limit+1)
	for _, member := range selected.members {
		if query.Cursor != nil &&
			!attentionFamilyMemberAfterPosition(member, *query.Cursor) {
			continue
		}
		members = append(members, member)
		if len(members) == query.Limit+1 {
			break
		}
	}
	hasMore := len(members) > query.Limit
	if hasMore {
		members = members[:query.Limit]
	}
	if err := tx.Commit(); err != nil {
		return model.AttentionFamilyMemberPage{}, errors.New("complete attention family member read")
	}
	return model.AttentionFamilyMemberPage{
		Family:              selected.summary,
		Data:                members,
		Analysis:            coverage,
		CursorEpoch:         state.epoch,
		Snapshot:            state.snapshot,
		RetentionGeneration: state.retentionGeneration,
		IssuedAt:            state.issuedAt,
		HasMore:             hasMore,
		Found:               true,
	}, nil
}

func (s *Store) loadAttentionFamiliesTx(
	ctx context.Context,
	tx *sql.Tx,
	snapshot int64,
	filter model.AttentionFamilyFilter,
) ([]attentionFamilyAggregate, error) {
	clauses := []string{
		"sr.visible_from_generation <= ?",
		"(sr.visible_until_generation IS NULL OR sr.visible_until_generation > ?)",
		"io.visible_from_generation <= ?",
		"(io.visible_until_generation IS NULL OR io.visible_until_generation > ?)",
	}
	args := []any{snapshot, snapshot, snapshot, snapshot}
	switch filter.AttentionKind {
	case model.AttentionKindIssue:
		clauses = append(clauses, "sr.category <> 'evidence_gap'")
	case model.AttentionKindEvidenceGap:
		clauses = append(clauses, "sr.category = 'evidence_gap'")
	default:
		return nil, errors.New("unsupported attention family kind")
	}
	if filter.Experimental == model.ExperimentalStable {
		clauses = append(clauses, "sr.experimental = 0")
	}
	if filter.Origin != "" {
		clauses = append(clauses, "sr.origin = ?")
		args = append(args, filter.Origin)
	}
	if filter.AnalysisStatus != "" {
		clauses = append(clauses, "sr.analysis_status = ?")
		args = append(args, filter.AnalysisStatus)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT
			sr.issue_id, sr.fingerprint_id, sr.fingerprint_version, sr.origin,
			sr.detector_id, sr.detector_version, sr.category, sr.title_code,
			sr.source_signal_code, sr.severity, sr.severity_rank,
			sr.confidence, sr.scope_quality, sr.analysis_status,
			sr.evidence_complete, sr.retained_history_only, sr.experimental,
			io.session_key, io.harness, io.first_observed_at, io.last_observed_at,
			COALESCE(io.origin_record_id, ''),
			COALESCE(f.finding_id, ''), COALESCE(f.session_key, ''),
			COALESCE(f.rule_id, ''), COALESCE(f.rule_version, '')
		FROM issue_summary_revisions sr
		JOIN issue_occurrences io ON io.issue_id = sr.issue_id
		LEFT JOIN findings f
			ON io.origin = 'numbat' AND f.finding_id = io.origin_record_id
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY sr.issue_id, io.revision_id`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query attention family candidates: %w", err)
	}
	defer rows.Close()
	issues := make(map[string]*attentionFamilyIssue)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row, err := scanAttentionFamilyRow(rows)
		if err != nil {
			return nil, err
		}
		issue := issues[row.summary.IssueID]
		if issue == nil {
			mapping, mapped := sourcecatalog.MatchSummary(row.summary)
			issue = &attentionFamilyIssue{
				summary:          row.summary,
				severityRank:     row.severityRank,
				mapping:          mapping,
				mapped:           mapped,
				rawFindingValid:  true,
				matchedSessions:  make(map[string]struct{}),
				matchedHarnesses: make(map[string]struct{}),
			}
			issues[row.summary.IssueID] = issue
		}
		if issue.summary.Origin == "numbat" && issue.mapped {
			if row.originRecordID == "" ||
				row.findingID != row.originRecordID ||
				row.findingSessionID != row.sessionID ||
				row.findingRuleID != issue.mapping.SourceSignalCode ||
				!issue.mapping.AcceptsRawRuleVersion(row.findingRuleVersion) {
				issue.rawFindingValid = false
			}
		}
		if filter.Harness != "" && !strings.EqualFold(row.harness, filter.Harness) {
			continue
		}
		if filter.ObservedAfter != nil && row.lastObservedAt.Before(*filter.ObservedAfter) {
			continue
		}
		issue.matchedCount++
		issue.matchedSessions[row.sessionID] = struct{}{}
		issue.matchedHarnesses[strings.ToLower(row.harness)] = struct{}{}
		if issue.firstObservedAt.IsZero() || row.firstObservedAt.Before(issue.firstObservedAt) {
			issue.firstObservedAt = row.firstObservedAt
		}
		if issue.lastObservedAt.IsZero() || row.lastObservedAt.After(issue.lastObservedAt) {
			issue.lastObservedAt = row.lastObservedAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("read attention family candidates")
	}

	grouped := make(map[string]*attentionFamilyAggregate)
	issueIDs := make([]string, 0, len(issues))
	for issueID := range issues {
		issueIDs = append(issueIDs, issueID)
	}
	sort.Strings(issueIDs)
	for _, issueID := range issueIDs {
		issue := issues[issueID]
		if issue.matchedCount == 0 {
			continue
		}
		var (
			groupKey        string
			kind            string
			mappingKey      string
			mappingVersion  string
			groupingVersion = "1"
			severity        = issue.summary.Severity
		)
		switch issue.summary.Origin {
		case "belay":
			kind = model.AttentionFamilyKindExactIssue
			groupKey = "exact:" + issue.summary.IssueID
		case "numbat":
			if !issue.mapped || !issue.rawFindingValid {
				continue
			}
			kind = model.AttentionFamilyKindMappedUpstream
			mappingKey = issue.mapping.Key
			mappingVersion = issue.mapping.Version
			groupingVersion = issue.mapping.GroupingVersion
			severity = issue.mapping.AttentionSeverity
			groupKey = strings.Join([]string{
				"mapped",
				mappingKey,
				mappingVersion,
				groupingVersion,
				issue.summary.FingerprintVersion,
			}, ":")
		default:
			continue
		}
		if filter.Severity != "" && severity != filter.Severity {
			continue
		}
		member := issue.summary
		member.FirstObservedAt = issue.firstObservedAt
		member.LastObservedAt = issue.lastObservedAt
		member.OccurrenceCount = issue.matchedCount
		member.SessionCount = len(issue.matchedSessions)
		member.Harnesses = sortedSet(issue.matchedHarnesses)

		aggregate := grouped[groupKey]
		if aggregate == nil {
			familyID, err := s.DeriveAttentionFamilyID(
				groupKey,
				sourcecatalog.CatalogVersion,
				groupingVersion,
			)
			if err != nil {
				return nil, err
			}
			aggregate = &attentionFamilyAggregate{
				summary: model.AttentionFamilySummary{
					FamilyID:            familyID,
					GroupKey:            groupKey,
					Kind:                kind,
					MappingKey:          mappingKey,
					MappingVersion:      mappingVersion,
					GroupingVersion:     groupingVersion,
					AttentionKind:       filter.AttentionKind,
					Severity:            severity,
					Confidence:          member.Confidence,
					EvidenceComplete:    true,
					RetainedHistoryOnly: true,
					AnalysisStatus:      model.AnalysisCurrent,
				},
				sessions:  make(map[string]struct{}),
				harnesses: make(map[string]struct{}),
			}
			grouped[groupKey] = aggregate
		}
		aggregate.members = append(aggregate.members, member)
		aggregate.summary.SupportingIssueCount++
		aggregate.summary.OccurrenceCount += member.OccurrenceCount
		aggregate.summary.Experimental = aggregate.summary.Experimental || member.Experimental
		for sessionID := range issue.matchedSessions {
			aggregate.sessions[sessionID] = struct{}{}
		}
		for harness := range issue.matchedHarnesses {
			aggregate.harnesses[harness] = struct{}{}
		}
		incrementScopeCount(&aggregate.summary.Scope, member.ScopeQuality)
		if aggregate.summary.FirstObservedAt.IsZero() ||
			member.FirstObservedAt.Before(aggregate.summary.FirstObservedAt) {
			aggregate.summary.FirstObservedAt = member.FirstObservedAt
		}
		if aggregate.summary.LastObservedAt.IsZero() ||
			member.LastObservedAt.After(aggregate.summary.LastObservedAt) {
			aggregate.summary.LastObservedAt = member.LastObservedAt
		}
		if confidenceRank(member.Confidence) < confidenceRank(aggregate.summary.Confidence) {
			aggregate.summary.Confidence = member.Confidence
		}
		if analysisStatusRank(member.AnalysisStatus) >
			analysisStatusRank(aggregate.summary.AnalysisStatus) {
			aggregate.summary.AnalysisStatus = member.AnalysisStatus
		}
		aggregate.summary.EvidenceComplete =
			aggregate.summary.EvidenceComplete && member.EvidenceComplete
		aggregate.summary.RetainedHistoryOnly =
			aggregate.summary.RetainedHistoryOnly && member.RetainedHistoryOnly
		if aggregate.summary.RepresentativeIssueID == "" ||
			attentionFamilyMemberLess(member, aggregate.summary.Representative) {
			aggregate.summary.RepresentativeIssueID = member.IssueID
			aggregate.summary.Representative = member
		}
	}
	result := make([]attentionFamilyAggregate, 0, len(grouped))
	for _, aggregate := range grouped {
		aggregate.summary.SessionCount = len(aggregate.sessions)
		aggregate.summary.Harnesses = sortedSet(aggregate.harnesses)
		result = append(result, *aggregate)
	}
	return result, nil
}

func scanAttentionFamilyRow(row rowScanner) (attentionFamilyRow, error) {
	var result attentionFamilyRow
	var sourceSignalCode sql.NullString
	var firstObserved, lastObserved string
	var evidenceComplete, retainedHistoryOnly, experimental int
	if err := row.Scan(
		&result.summary.IssueID,
		&result.summary.FingerprintID,
		&result.summary.FingerprintVersion,
		&result.summary.Origin,
		&result.summary.DetectorID,
		&result.summary.DetectorVersion,
		&result.summary.Category,
		&result.summary.TitleCode,
		&sourceSignalCode,
		&result.summary.Severity,
		&result.severityRank,
		&result.summary.Confidence,
		&result.summary.ScopeQuality,
		&result.summary.AnalysisStatus,
		&evidenceComplete,
		&retainedHistoryOnly,
		&experimental,
		&result.sessionID,
		&result.harness,
		&firstObserved,
		&lastObserved,
		&result.originRecordID,
		&result.findingID,
		&result.findingSessionID,
		&result.findingRuleID,
		&result.findingRuleVersion,
	); err != nil {
		return attentionFamilyRow{}, errors.New("decode attention family candidate")
	}
	if sourceSignalCode.Valid {
		result.summary.SourceSignalCode = &sourceSignalCode.String
	}
	result.summary.EvidenceComplete = evidenceComplete == 1
	result.summary.RetainedHistoryOnly = retainedHistoryOnly == 1
	result.summary.Experimental = experimental == 1
	var err error
	result.firstObservedAt, err = parseProjectionTime(firstObserved)
	if err != nil {
		return attentionFamilyRow{}, errors.New("decode attention family first-observed timestamp")
	}
	result.lastObservedAt, err = parseProjectionTime(lastObserved)
	if err != nil {
		return attentionFamilyRow{}, errors.New("decode attention family last-observed timestamp")
	}
	return result, nil
}

func normalizeAttentionFamilyFilter(filter *model.AttentionFamilyFilter) error {
	filter.Severity = strings.ToLower(strings.TrimSpace(filter.Severity))
	filter.Harness = strings.TrimSpace(filter.Harness)
	filter.Origin = strings.ToLower(strings.TrimSpace(filter.Origin))
	filter.AnalysisStatus = model.AnalysisStatus(
		strings.ToLower(strings.TrimSpace(string(filter.AnalysisStatus))),
	)
	filter.AttentionKind = strings.ToLower(strings.TrimSpace(filter.AttentionKind))
	filter.Experimental = strings.ToLower(strings.TrimSpace(filter.Experimental))
	if filter.ObservedAfter != nil {
		value := filter.ObservedAfter.UTC()
		filter.ObservedAfter = &value
	}
	if filter.AttentionKind == "" {
		filter.AttentionKind = model.AttentionKindIssue
	}
	if filter.Experimental == "" {
		filter.Experimental = model.ExperimentalStable
	}
	switch filter.Severity {
	case "", "info", "low", "medium", "high", "critical":
	default:
		return errors.New("unsupported attention family severity")
	}
	if len(filter.Harness) > 128 {
		return errors.New("attention family harness is too long")
	}
	switch filter.Origin {
	case "", "belay", "numbat":
	default:
		return errors.New("unsupported attention family origin")
	}
	switch filter.AnalysisStatus {
	case "", model.AnalysisCurrent, model.AnalysisPending,
		model.AnalysisFailed, model.AnalysisTruncated:
	default:
		return errors.New("unsupported attention family analysis status")
	}
	switch filter.AttentionKind {
	case model.AttentionKindIssue, model.AttentionKindEvidenceGap:
	default:
		return errors.New("unsupported attention family kind")
	}
	switch filter.Experimental {
	case model.ExperimentalStable, model.ExperimentalInclude:
	default:
		return errors.New("unsupported attention family experimental mode")
	}
	return nil
}

func attentionFamilyLess(
	left model.AttentionFamilySummary,
	right model.AttentionFamilySummary,
) bool {
	leftRank := familySeverityRank(left.Severity)
	rightRank := familySeverityRank(right.Severity)
	if leftRank != rightRank {
		return leftRank > rightRank
	}
	if left.SupportingIssueCount != right.SupportingIssueCount {
		return left.SupportingIssueCount > right.SupportingIssueCount
	}
	if !left.LastObservedAt.Equal(right.LastObservedAt) {
		return left.LastObservedAt.After(right.LastObservedAt)
	}
	return left.GroupKey < right.GroupKey
}

func attentionFamilyAfterPosition(
	value model.AttentionFamilySummary,
	position model.AttentionFamilyPosition,
) bool {
	rank := familySeverityRank(value.Severity)
	if rank != position.SeverityRank {
		return rank < position.SeverityRank
	}
	if value.SupportingIssueCount != position.SupportingIssueCount {
		return value.SupportingIssueCount < position.SupportingIssueCount
	}
	if !value.LastObservedAt.Equal(position.LastObserved) {
		return value.LastObservedAt.Before(position.LastObserved)
	}
	return value.GroupKey > position.GroupKey
}

func attentionFamilyMemberLess(left, right model.IssueSummary) bool {
	leftRank := analysisStatusRank(left.AnalysisStatus)
	rightRank := analysisStatusRank(right.AnalysisStatus)
	if leftRank != rightRank {
		return leftRank > rightRank
	}
	if !left.LastObservedAt.Equal(right.LastObservedAt) {
		return left.LastObservedAt.After(right.LastObservedAt)
	}
	return left.IssueID < right.IssueID
}

func attentionFamilyMemberAfterPosition(
	value model.IssueSummary,
	position model.AttentionFamilyMemberPosition,
) bool {
	rank := analysisStatusRank(value.AnalysisStatus)
	if rank != position.AnalysisStatusRank {
		return rank < position.AnalysisStatusRank
	}
	if !value.LastObservedAt.Equal(position.LastObserved) {
		return value.LastObservedAt.Before(position.LastObserved)
	}
	return value.IssueID > position.IssueID
}

func analysisStatusRank(status model.AnalysisStatus) int {
	switch status {
	case model.AnalysisFailed:
		return 4
	case model.AnalysisPending:
		return 3
	case model.AnalysisTruncated:
		return 2
	case model.AnalysisCurrent:
		return 1
	default:
		return 0
	}
}

func familySeverityRank(severity string) int {
	switch severity {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

func confidenceRank(confidence string) int {
	switch confidence {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	default:
		return 0
	}
}

func incrementScopeCount(
	counts *model.AttentionFamilyScopeCounts,
	quality model.ScopeQuality,
) {
	switch quality {
	case model.ScopeResolved:
		counts.Resolved++
	case model.ScopeLexical:
		counts.Lexical++
	case model.ScopeUnscoped:
		counts.Unscoped++
	case model.ScopeConflict:
		counts.Conflict++
	}
}
