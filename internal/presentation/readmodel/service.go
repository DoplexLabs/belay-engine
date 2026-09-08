// Package readmodel is the presentation boundary shared by Local HTTP and MCP
// adapters. It owns the public filtering and opaque cursor semantics while
// reading through a repository interface rather than SQLite.
package readmodel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
)

const SchemaVersion = "belay.read.v1"

const (
	defaultSessionLimit  = 20
	maxSessionLimit      = 100
	defaultTimelineLimit = 100
	maxTimelineLimit     = 500
	defaultActivityLimit = 50
	maxActivityLimit     = 200
	defaultFindingLimit  = 20
	maxFindingLimit      = 100
	cursorVersion        = 1
	maxCursorBytes       = 2048
)

var (
	ErrInvalidCursor  = errors.New("invalid read cursor")
	ErrInvalidRequest = errors.New("invalid read request")
)

type Repository interface {
	QuerySessions(context.Context, model.SessionQuery) (model.SessionPage, error)
	GetSession(context.Context, string) (model.SessionSummary, time.Time, error)
	QuerySessionTimeline(context.Context, model.TimelineQuery) (model.EventPage, error)
	QueryActivityPage(context.Context, model.ActivityQuery) (model.EventPage, error)
	QueryFindings(context.Context, model.FindingQuery) (model.FindingPage, error)
	GetStats(context.Context) (model.LocalStats, time.Time, error)
}

type Service struct {
	repository Repository
}

type SessionListRequest struct {
	Limit          int
	Cursor         string
	Harness        string
	Outcome        string
	History        string
	OccurredAfter  *time.Time
	OccurredBefore *time.Time
	Query          string
}

type TimelineRequest struct {
	SessionID string
	Limit     int
	Cursor    string
}

type ActivityRequest struct {
	Filter model.ActivityFilter
	Cursor string
}

type FindingListRequest struct {
	Limit     int
	Cursor    string
	Since     *time.Time
	Severity  string
	SessionID string
}

type SessionList struct {
	SchemaVersion  string                 `json:"schema_version"`
	Data           []model.SessionSummary `json:"data"`
	NextCursor     *string                `json:"next_cursor"`
	HasMore        bool                   `json:"has_more"`
	ReturnedCount  int                    `json:"returned_count"`
	Limit          int                    `json:"limit"`
	FiltersApplied bool                   `json:"filters_applied"`
	DataThrough    time.Time              `json:"data_through"`
}

type SessionTimeline struct {
	SchemaVersion string        `json:"schema_version"`
	SessionID     string        `json:"session_id"`
	Data          []model.Event `json:"data"`
	NextCursor    *string       `json:"next_cursor"`
	HasMore       bool          `json:"has_more"`
	ReturnedCount int           `json:"returned_count"`
	Limit         int           `json:"limit"`
	DataThrough   time.Time     `json:"data_through"`
}

type SessionDetail struct {
	SchemaVersion string               `json:"schema_version"`
	Data          model.SessionSummary `json:"data"`
	DataThrough   time.Time            `json:"data_through"`
}

type ActivityList struct {
	SchemaVersion string        `json:"schema_version"`
	Data          []model.Event `json:"data"`
	NextCursor    *string       `json:"next_cursor"`
	HasMore       bool          `json:"has_more"`
	ReturnedCount int           `json:"returned_count"`
	Limit         int           `json:"limit"`
	DataThrough   time.Time     `json:"data_through"`
}

type FindingList struct {
	SchemaVersion string                 `json:"schema_version"`
	Data          []model.FindingSummary `json:"data"`
	NextCursor    *string                `json:"next_cursor"`
	HasMore       bool                   `json:"has_more"`
	ReturnedCount int                    `json:"returned_count"`
	Limit         int                    `json:"limit"`
	DataThrough   time.Time              `json:"data_through"`
}

type StatsResponse struct {
	SchemaVersion string           `json:"schema_version"`
	MetricVersion string           `json:"metric_version"`
	Data          model.LocalStats `json:"data"`
	DataThrough   time.Time        `json:"data_through"`
}

type cursorEnvelope struct {
	Version     int    `json:"v"`
	Kind        string `json:"k"`
	Snapshot    int64  `json:"s"`
	Fingerprint string `json:"f"`
	Time        string `json:"t"`
	Sequence    int64  `json:"q,omitempty"`
	ID          string `json:"i"`
}

func New(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) ListSessions(ctx context.Context, limit int) (SessionList, error) {
	return s.ListSessionsPage(ctx, SessionListRequest{Limit: limit})
}

func (s *Service) ListSessionsPage(ctx context.Context, request SessionListRequest) (SessionList, error) {
	request.Limit = boundedLimit(request.Limit, defaultSessionLimit, maxSessionLimit)
	request.Harness = strings.TrimSpace(request.Harness)
	request.Outcome = strings.ToLower(strings.TrimSpace(request.Outcome))
	request.History = strings.ToLower(strings.TrimSpace(request.History))
	request.Query = strings.TrimSpace(request.Query)
	request.OccurredAfter = utcTime(request.OccurredAfter)
	request.OccurredBefore = utcTime(request.OccurredBefore)
	if err := validateSessionRequest(request); err != nil {
		return SessionList{}, err
	}
	fingerprint := sessionFingerprint(request)
	query := model.SessionQuery{
		Limit:          request.Limit + 1,
		Harness:        request.Harness,
		Outcome:        request.Outcome,
		History:        request.History,
		OccurredAfter:  request.OccurredAfter,
		OccurredBefore: request.OccurredBefore,
		Search:         request.Query,
	}
	if request.Cursor != "" {
		cursor, err := decodeCursor(request.Cursor, "sessions", fingerprint)
		if err != nil {
			return SessionList{}, err
		}
		cursorTime, _ := time.Parse(time.RFC3339Nano, cursor.Time)
		query.Snapshot = cursor.Snapshot
		query.CursorEndedAt = &cursorTime
		query.CursorSessionID = cursor.ID
	}
	page, err := s.repository.QuerySessions(ctx, query)
	if err != nil {
		return SessionList{}, err
	}
	data, hasMore := boundedPage(page.Data, request.Limit)
	nextCursor, err := sessionNextCursor(data, hasMore, page.Snapshot, fingerprint)
	if err != nil {
		return SessionList{}, err
	}
	return SessionList{
		SchemaVersion:  SchemaVersion,
		Data:           nonNil(data),
		NextCursor:     nextCursor,
		HasMore:        hasMore,
		ReturnedCount:  len(data),
		Limit:          request.Limit,
		FiltersApplied: true,
		DataThrough:    page.DataThrough,
	}, nil
}

func (s *Service) GetSession(ctx context.Context, sessionID string) (SessionDetail, error) {
	data, dataThrough, err := s.repository.GetSession(ctx, sessionID)
	return SessionDetail{
		SchemaVersion: SchemaVersion,
		Data:          data,
		DataThrough:   dataThrough,
	}, err
}

func (s *Service) GetSessionTimeline(
	ctx context.Context,
	sessionID string,
	limit int,
) (SessionTimeline, error) {
	return s.GetSessionTimelinePage(ctx, TimelineRequest{
		SessionID: sessionID,
		Limit:     limit,
	})
}

func (s *Service) GetSessionTimelinePage(
	ctx context.Context,
	request TimelineRequest,
) (SessionTimeline, error) {
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.Limit = boundedLimit(request.Limit, defaultTimelineLimit, maxTimelineLimit)
	if request.SessionID == "" || len(request.SessionID) > 256 {
		return SessionTimeline{}, invalidRequest("session_id is required and must not exceed 256 bytes")
	}
	fingerprint := stableFingerprint(struct {
		SessionID string `json:"session_id"`
	}{SessionID: request.SessionID})
	query := model.TimelineQuery{
		SessionID: request.SessionID,
		Limit:     request.Limit + 1,
	}
	if request.Cursor != "" {
		cursor, err := decodeCursor(request.Cursor, "session_events", fingerprint)
		if err != nil {
			return SessionTimeline{}, err
		}
		cursorTime, _ := time.Parse(time.RFC3339Nano, cursor.Time)
		query.Snapshot = cursor.Snapshot
		query.Cursor = &model.EventPosition{
			OccurredAt:     cursorTime,
			SourceSequence: cursor.Sequence,
			EventID:        cursor.ID,
		}
	}
	page, err := s.repository.QuerySessionTimeline(ctx, query)
	if err != nil {
		return SessionTimeline{}, err
	}
	data, hasMore := boundedPage(page.Data, request.Limit)
	nextCursor, err := eventNextCursor(
		data,
		hasMore,
		page.Snapshot,
		"session_events",
		fingerprint,
	)
	if err != nil {
		return SessionTimeline{}, err
	}
	return SessionTimeline{
		SchemaVersion: SchemaVersion,
		SessionID:     request.SessionID,
		Data:          nonNil(data),
		NextCursor:    nextCursor,
		HasMore:       hasMore,
		ReturnedCount: len(data),
		Limit:         request.Limit,
		DataThrough:   page.DataThrough,
	}, nil
}

func (s *Service) QueryActivity(
	ctx context.Context,
	filter model.ActivityFilter,
) (ActivityList, error) {
	return s.QueryActivityPage(ctx, ActivityRequest{Filter: filter})
}

func (s *Service) QueryActivityPage(
	ctx context.Context,
	request ActivityRequest,
) (ActivityList, error) {
	request.Filter.Limit = boundedLimit(
		request.Filter.Limit,
		defaultActivityLimit,
		maxActivityLimit,
	)
	request.Filter.Harness = strings.TrimSpace(request.Filter.Harness)
	request.Filter.ResourceKind = strings.ToLower(strings.TrimSpace(request.Filter.ResourceKind))
	request.Filter.Outcome = strings.ToLower(strings.TrimSpace(request.Filter.Outcome))
	request.Filter.OccurredAfter = utcTime(request.Filter.OccurredAfter)
	request.Filter.OccurredBefore = utcTime(request.Filter.OccurredBefore)
	if err := validateActivityRequest(request); err != nil {
		return ActivityList{}, err
	}
	fingerprint := activityFingerprint(request.Filter)
	query := model.ActivityQuery{Filter: request.Filter}
	query.Filter.Limit++
	if request.Cursor != "" {
		cursor, err := decodeCursor(request.Cursor, "activity", fingerprint)
		if err != nil {
			return ActivityList{}, err
		}
		cursorTime, _ := time.Parse(time.RFC3339Nano, cursor.Time)
		query.Snapshot = cursor.Snapshot
		query.Cursor = &model.EventPosition{
			OccurredAt:     cursorTime,
			SourceSequence: cursor.Sequence,
			EventID:        cursor.ID,
		}
	}
	page, err := s.repository.QueryActivityPage(ctx, query)
	if err != nil {
		return ActivityList{}, err
	}
	data, hasMore := boundedPage(page.Data, request.Filter.Limit)
	nextCursor, err := eventNextCursor(
		data,
		hasMore,
		page.Snapshot,
		"activity",
		fingerprint,
	)
	if err != nil {
		return ActivityList{}, err
	}
	return ActivityList{
		SchemaVersion: SchemaVersion,
		Data:          nonNil(data),
		NextCursor:    nextCursor,
		HasMore:       hasMore,
		ReturnedCount: len(data),
		Limit:         request.Filter.Limit,
		DataThrough:   page.DataThrough,
	}, nil
}

func (s *Service) ListFindings(ctx context.Context, limit int) (FindingList, error) {
	return s.ListFindingsPage(ctx, FindingListRequest{Limit: limit})
}

func (s *Service) ListFindingsPage(
	ctx context.Context,
	request FindingListRequest,
) (FindingList, error) {
	request.Limit = boundedLimit(request.Limit, defaultFindingLimit, maxFindingLimit)
	request.Severity = strings.ToLower(strings.TrimSpace(request.Severity))
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.Since = utcTime(request.Since)
	if err := validateFindingRequest(request); err != nil {
		return FindingList{}, err
	}
	fingerprint := stableFingerprint(struct {
		Since     string `json:"since"`
		Severity  string `json:"severity"`
		SessionID string `json:"session_id"`
	}{
		Since:     timeString(request.Since),
		Severity:  request.Severity,
		SessionID: request.SessionID,
	})
	query := model.FindingQuery{
		Filter: model.FindingFilter{
			DetectedAfter: request.Since,
			Severity:      request.Severity,
			SessionID:     request.SessionID,
			Limit:         request.Limit + 1,
		},
	}
	if request.Cursor != "" {
		cursor, err := decodeCursor(request.Cursor, "findings", fingerprint)
		if err != nil {
			return FindingList{}, err
		}
		cursorTime, _ := time.Parse(time.RFC3339Nano, cursor.Time)
		query.Snapshot = cursor.Snapshot
		query.Cursor = &model.FindingPosition{
			DetectedAt: cursorTime,
			FindingID:  cursor.ID,
		}
	}
	page, err := s.repository.QueryFindings(ctx, query)
	if err != nil {
		return FindingList{}, err
	}
	data, hasMore := boundedPage(page.Data, request.Limit)
	var nextCursor *string
	if hasMore {
		last := data[len(data)-1]
		encoded, err := encodeCursor(cursorEnvelope{
			Version:     cursorVersion,
			Kind:        "findings",
			Snapshot:    page.Snapshot,
			Fingerprint: fingerprint,
			Time:        last.DetectedAt.UTC().Format(time.RFC3339Nano),
			ID:          last.FindingID,
		})
		if err != nil {
			return FindingList{}, err
		}
		nextCursor = &encoded
	}
	return FindingList{
		SchemaVersion: SchemaVersion,
		Data:          nonNil(data),
		NextCursor:    nextCursor,
		HasMore:       hasMore,
		ReturnedCount: len(data),
		Limit:         request.Limit,
		DataThrough:   page.DataThrough,
	}, nil
}

func (s *Service) GetStats(ctx context.Context) (StatsResponse, error) {
	data, dataThrough, err := s.repository.GetStats(ctx)
	return StatsResponse{
		SchemaVersion: SchemaVersion,
		MetricVersion: "belay.metrics.local.v1",
		Data:          data,
		DataThrough:   dataThrough,
	}, err
}

func sessionNextCursor(
	data []model.SessionSummary,
	hasMore bool,
	snapshot int64,
	fingerprint string,
) (*string, error) {
	if !hasMore {
		return nil, nil
	}
	last := data[len(data)-1]
	encoded, err := encodeCursor(cursorEnvelope{
		Version:     cursorVersion,
		Kind:        "sessions",
		Snapshot:    snapshot,
		Fingerprint: fingerprint,
		Time:        last.EndedAt.UTC().Format(time.RFC3339Nano),
		ID:          last.SessionID,
	})
	if err != nil {
		return nil, err
	}
	return &encoded, nil
}

func eventNextCursor(
	data []model.Event,
	hasMore bool,
	snapshot int64,
	kind string,
	fingerprint string,
) (*string, error) {
	if !hasMore {
		return nil, nil
	}
	last := data[len(data)-1]
	encoded, err := encodeCursor(cursorEnvelope{
		Version:     cursorVersion,
		Kind:        kind,
		Snapshot:    snapshot,
		Fingerprint: fingerprint,
		Time:        last.OccurredAt.UTC().Format(time.RFC3339Nano),
		Sequence:    last.Source.Sequence,
		ID:          last.EventID,
	})
	if err != nil {
		return nil, err
	}
	return &encoded, nil
}

func validateSessionRequest(request SessionListRequest) error {
	if len(request.Harness) > 128 {
		return invalidRequest("harness must not exceed 128 bytes")
	}
	if len(request.Query) > 128 {
		return invalidRequest("query must not exceed 128 bytes")
	}
	if request.Outcome != "" && !validProjectionOutcome(request.Outcome) {
		return invalidRequest("outcome must be incomplete, succeeded, failed, interrupted, or unknown")
	}
	if request.History != "" &&
		request.History != "historical" &&
		request.History != "live" &&
		request.History != "mixed" {
		return invalidRequest("history must be historical, live, or mixed")
	}
	return validateWindow(request.OccurredAfter, request.OccurredBefore)
}

func validateActivityRequest(request ActivityRequest) error {
	if len(request.Filter.Harness) > 128 {
		return invalidRequest("harness must not exceed 128 bytes")
	}
	if len(request.Filter.ResourceKind) > 64 {
		return invalidRequest("resource_kind must not exceed 64 bytes")
	}
	if request.Filter.Outcome != "" && !validEventOutcome(request.Filter.Outcome) {
		return invalidRequest("outcome must be succeeded, failed, interrupted, or unknown")
	}
	return validateWindow(request.Filter.OccurredAfter, request.Filter.OccurredBefore)
}

func validateFindingRequest(request FindingListRequest) error {
	if len(request.Severity) > 32 {
		return invalidRequest("severity must not exceed 32 bytes")
	}
	if len(request.SessionID) > 256 {
		return invalidRequest("session_id must not exceed 256 bytes")
	}
	return nil
}

func validProjectionOutcome(value string) bool {
	switch value {
	case "incomplete", "succeeded", "failed", "interrupted", "unknown":
		return true
	default:
		return false
	}
}

func validEventOutcome(value string) bool {
	switch value {
	case "succeeded", "failed", "interrupted", "unknown":
		return true
	default:
		return false
	}
}

func validateWindow(after, before *time.Time) error {
	if after != nil && before != nil && after.After(*before) {
		return invalidRequest("occurred_after must not be after occurred_before")
	}
	return nil
}

func sessionFingerprint(request SessionListRequest) string {
	return stableFingerprint(struct {
		Harness        string `json:"harness"`
		Outcome        string `json:"outcome"`
		History        string `json:"history"`
		OccurredAfter  string `json:"occurred_after"`
		OccurredBefore string `json:"occurred_before"`
		Query          string `json:"query"`
	}{
		Harness:        strings.ToLower(request.Harness),
		Outcome:        request.Outcome,
		History:        request.History,
		OccurredAfter:  timeString(request.OccurredAfter),
		OccurredBefore: timeString(request.OccurredBefore),
		Query:          strings.ToLower(request.Query),
	})
}

func activityFingerprint(filter model.ActivityFilter) string {
	return stableFingerprint(struct {
		OccurredAfter  string `json:"occurred_after"`
		OccurredBefore string `json:"occurred_before"`
		Harness        string `json:"harness"`
		ResourceKind   string `json:"resource_kind"`
		Outcome        string `json:"outcome"`
	}{
		OccurredAfter:  timeString(filter.OccurredAfter),
		OccurredBefore: timeString(filter.OccurredBefore),
		Harness:        strings.ToLower(filter.Harness),
		ResourceKind:   filter.ResourceKind,
		Outcome:        filter.Outcome,
	})
}

func stableFingerprint(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func encodeCursor(cursor cursorEnvelope) (string, error) {
	body, err := json.Marshal(cursor)
	if err != nil {
		return "", errors.New("encode read cursor")
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

func decodeCursor(value, kind, fingerprint string) (cursorEnvelope, error) {
	if value == "" || len(value) > maxCursorBytes {
		return cursorEnvelope{}, invalidCursor()
	}
	body, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(body) == 0 || len(body) > maxCursorBytes {
		return cursorEnvelope{}, invalidCursor()
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var cursor cursorEnvelope
	if err := decoder.Decode(&cursor); err != nil {
		return cursorEnvelope{}, invalidCursor()
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return cursorEnvelope{}, invalidCursor()
	}
	if cursor.Version != cursorVersion ||
		cursor.Kind != kind ||
		cursor.Snapshot <= 0 ||
		cursor.Fingerprint != fingerprint ||
		cursor.ID == "" ||
		len(cursor.ID) > 256 {
		return cursorEnvelope{}, invalidCursor()
	}
	parsed, err := time.Parse(time.RFC3339Nano, cursor.Time)
	if err != nil || parsed.Location() != time.UTC {
		return cursorEnvelope{}, invalidCursor()
	}
	return cursor, nil
}

func invalidCursor() error {
	return fmt.Errorf("%w: opaque cursor is malformed or does not match the request", ErrInvalidCursor)
}

func invalidRequest(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, message)
}

func boundedLimit(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func boundedPage[T any](data []T, limit int) ([]T, bool) {
	if len(data) <= limit {
		return data, false
	}
	return data[:limit], true
}

func nonNil[T any](data []T) []T {
	if data == nil {
		return []T{}
	}
	return data
}

func utcTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := value.UTC()
	return &result
}

func timeString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
