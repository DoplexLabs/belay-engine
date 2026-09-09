package local

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/issueintel"
)

const maxReportFixLimit = 100

const reportSessionCTE = `
	WITH canonical_sessions AS (
		SELECT
			session_key,
			MIN(source_agent) AS agent,
			MIN(occurred_at) AS started_at,
			MAX(occurred_at) AS ended_at
		FROM events
		GROUP BY session_key
	),
	report_sessions AS (
		SELECT
			session_key,
			agent,
			COALESCE(ended_at, started_at, updated_at) AS activity_at,
			wall_duration_ms,
			total_tokens,
			total_cost_usd
		FROM transcript_sessions
		UNION ALL
		SELECT
			canonical.session_key,
			canonical.agent,
			COALESCE(canonical.ended_at, canonical.started_at) AS activity_at,
			NULL AS wall_duration_ms,
			NULL AS total_tokens,
			NULL AS total_cost_usd
		FROM canonical_sessions canonical
		WHERE NOT EXISTS (
			SELECT 1
			FROM transcript_sessions transcript
			WHERE transcript.session_key = canonical.session_key
		)
	)
`

func (s *Store) ReadUsageSnapshot(
	ctx context.Context,
) (issueintel.UsageSnapshot, error) {
	result := issueintel.UsageSnapshot{
		Totals: issueintel.UsageTotals{
			Harnesses: make([]string, 0),
		},
		Weeks: make([]issueintel.UsageWeek, 0),
	}
	var durationKnown, tokenKnown, costKnown int
	var firstValue, lastValue sql.NullString
	err := s.db.QueryRowContext(ctx, reportSessionCTE+`
		SELECT COUNT(*),
			COALESCE(SUM(wall_duration_ms), 0),
			COUNT(wall_duration_ms),
			COALESCE(SUM(total_tokens), 0),
			COUNT(total_tokens),
			COALESCE(SUM(total_cost_usd), 0),
			COUNT(total_cost_usd),
			MIN(activity_at),
			MAX(activity_at)
		FROM report_sessions`,
	).Scan(
		&result.Totals.SessionCount,
		&result.Totals.WallDurationMS,
		&durationKnown,
		&result.Totals.TotalTokens,
		&tokenKnown,
		&result.Totals.TotalCostUSD,
		&costKnown,
		&firstValue,
		&lastValue,
	)
	if err != nil {
		return issueintel.UsageSnapshot{}, errors.New(
			"read report usage totals",
		)
	}
	result.Totals.WallDurationLowerBound =
		durationKnown < result.Totals.SessionCount
	result.Totals.TokensLowerBound =
		tokenKnown < result.Totals.SessionCount
	result.Totals.CostLowerBound =
		costKnown < result.Totals.SessionCount
	if firstValue.Valid {
		value, err := parseProjectionTime(firstValue.String)
		if err != nil {
			return issueintel.UsageSnapshot{}, errors.New(
				"read report first activity",
			)
		}
		result.Totals.FirstActivityAt = &value
	}
	if lastValue.Valid {
		value, err := parseProjectionTime(lastValue.String)
		if err != nil {
			return issueintel.UsageSnapshot{}, errors.New(
				"read report last activity",
			)
		}
		result.Totals.LastActivityAt = &value
	}
	rows, err := s.db.QueryContext(ctx, reportSessionCTE+`
		SELECT DISTINCT agent
		FROM report_sessions
		WHERE TRIM(agent) <> ''
		ORDER BY agent ASC`)
	if err != nil {
		return issueintel.UsageSnapshot{}, errors.New(
			"read report harnesses",
		)
	}
	for rows.Next() {
		var harness string
		if err := rows.Scan(&harness); err != nil {
			rows.Close()
			return issueintel.UsageSnapshot{}, errors.New(
				"read report harness",
			)
		}
		result.Totals.Harnesses = append(
			result.Totals.Harnesses,
			harness,
		)
	}
	if err := rows.Close(); err != nil {
		return issueintel.UsageSnapshot{}, errors.New(
			"close report harnesses",
		)
	}
	currentWeekStart := reportWeekStart(s.nowUTC())
	windowStart := currentWeekStart.AddDate(0, 0, -77)
	windowEnd := currentWeekStart.AddDate(0, 0, 7)
	rows, err = s.db.QueryContext(ctx, reportSessionCTE+`
		SELECT
			date(
				activity_at,
				'-' || (
					(CAST(strftime('%w', activity_at) AS INTEGER) + 6) % 7
				) || ' days'
			) AS week_start,
			COUNT(*),
			COALESCE(SUM(wall_duration_ms), 0),
			COUNT(wall_duration_ms),
			COALESCE(SUM(total_tokens), 0),
			COUNT(total_tokens),
			COALESCE(SUM(total_cost_usd), 0),
			COUNT(total_cost_usd)
		FROM report_sessions
		WHERE activity_at >= ? AND activity_at < ?
		GROUP BY week_start
		ORDER BY week_start DESC`,
		windowStart.Format(time.RFC3339Nano),
		windowEnd.Format(time.RFC3339Nano),
	)
	if err != nil {
		return issueintel.UsageSnapshot{}, errors.New(
			"read report weekly usage",
		)
	}
	for rows.Next() {
		var week issueintel.UsageWeek
		var weekStart string
		if err := rows.Scan(
			&weekStart,
			&week.SessionCount,
			&week.WallDurationMS,
			&durationKnown,
			&week.TotalTokens,
			&tokenKnown,
			&week.TotalCostUSD,
			&costKnown,
		); err != nil {
			rows.Close()
			return issueintel.UsageSnapshot{}, errors.New(
				"read report usage week",
			)
		}
		week.WeekStart, err = time.Parse("2006-01-02", weekStart)
		if err != nil {
			rows.Close()
			return issueintel.UsageSnapshot{}, errors.New(
				"parse report usage week",
			)
		}
		week.WallDurationLowerBound = durationKnown < week.SessionCount
		week.TokensLowerBound = tokenKnown < week.SessionCount
		week.CostLowerBound = costKnown < week.SessionCount
		result.Weeks = append(result.Weeks, week)
	}
	if err := rows.Close(); err != nil {
		return issueintel.UsageSnapshot{}, errors.New(
			"close report weekly usage",
		)
	}
	sort.Slice(result.Weeks, func(i, j int) bool {
		return result.Weeks[i].WeekStart.Before(result.Weeks[j].WeekStart)
	})
	if len(result.Weeks) > 0 {
		weeksByStart := make(
			map[string]issueintel.UsageWeek,
			len(result.Weeks),
		)
		for _, week := range result.Weeks {
			weeksByStart[week.WeekStart.Format("2006-01-02")] = week
		}
		filled := make([]issueintel.UsageWeek, 0, 12)
		for weekStart := windowStart; !weekStart.After(currentWeekStart); weekStart = weekStart.AddDate(0, 0, 7) {
			week, ok := weeksByStart[weekStart.Format("2006-01-02")]
			if !ok {
				week = issueintel.UsageWeek{WeekStart: weekStart}
			}
			filled = append(filled, week)
		}
		result.Weeks = filled
	}
	result.Coverage, err = s.TranscriptCoverage(ctx)
	if err != nil {
		return issueintel.UsageSnapshot{}, err
	}
	return result, nil
}

func reportWeekStart(value time.Time) time.Time {
	value = value.UTC()
	daysSinceMonday := (int(value.Weekday()) + 6) % 7
	return time.Date(
		value.Year(),
		value.Month(),
		value.Day()-daysSinceMonday,
		0,
		0,
		0,
		0,
		time.UTC,
	)
}

func (s *Store) ReadCostIssueTotals(
	ctx context.Context,
) (issueintel.IssueCostTotals, error) {
	var result issueintel.IssueCostTotals
	var lowerBoundCount int
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(wasted_usd), 0),
			COALESCE(SUM(
				CASE
					WHEN wasted_usd_known = 0 OR lower_bound = 1 THEN 1
					ELSE 0
				END
			), 0),
			COUNT(*)
		FROM cost_issues`,
	).Scan(
		&result.AttributedUSD,
		&lowerBoundCount,
		&result.IssueCount,
	)
	if err != nil {
		return issueintel.IssueCostTotals{}, errors.New(
			"read report issue cost totals",
		)
	}
	result.LowerBound = lowerBoundCount > 0
	return result, nil
}

func (s *Store) ListCostIssueFixes(
	ctx context.Context,
	limit int,
) ([]issueintel.FixRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > maxReportFixLimit {
		return nil, errors.New("invalid report fix limit")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT fix_id
		FROM cost_issue_fixes
		ORDER BY COALESCE(applied_at, proposed_at) DESC, fix_id ASC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, errors.New("list report fixes")
	}
	var ids []string
	for rows.Next() {
		var fixID string
		if err := rows.Scan(&fixID); err != nil {
			rows.Close()
			return nil, errors.New("read report fix")
		}
		ids = append(ids, fixID)
	}
	if err := rows.Close(); err != nil {
		return nil, errors.New("close report fixes")
	}
	result := make([]issueintel.FixRecord, 0, len(ids))
	for _, fixID := range ids {
		record, err := s.GetCostIssueFix(ctx, fixID)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, nil
}
