// Package readmodel is the presentation boundary shared by future Local HTTP
// and MCP adapters. It reads through a repository interface rather than SQLite.
package readmodel

import (
	"context"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
)

const SchemaVersion = "belay.read.v1"

type Repository interface {
	ListSessions(context.Context, int) ([]model.SessionSummary, time.Time, error)
	GetSession(context.Context, string) (model.SessionSummary, time.Time, error)
	GetSessionTimeline(context.Context, string, int) ([]model.Event, time.Time, error)
	QueryActivity(context.Context, model.ActivityFilter) ([]model.Event, time.Time, error)
	ListFindings(context.Context, int) ([]model.FindingSummary, time.Time, error)
	GetStats(context.Context) (model.LocalStats, time.Time, error)
}

type Service struct {
	repository Repository
}

type SessionList struct {
	SchemaVersion string                 `json:"schema_version"`
	Data          []model.SessionSummary `json:"data"`
	NextCursor    *string                `json:"next_cursor"`
	DataThrough   time.Time              `json:"data_through"`
}

type SessionTimeline struct {
	SchemaVersion string        `json:"schema_version"`
	SessionID     string        `json:"session_id"`
	Data          []model.Event `json:"data"`
	NextCursor    *string       `json:"next_cursor"`
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
	DataThrough   time.Time     `json:"data_through"`
}

type FindingList struct {
	SchemaVersion string                 `json:"schema_version"`
	Data          []model.FindingSummary `json:"data"`
	NextCursor    *string                `json:"next_cursor"`
	DataThrough   time.Time              `json:"data_through"`
}

type StatsResponse struct {
	SchemaVersion string           `json:"schema_version"`
	MetricVersion string           `json:"metric_version"`
	Data          model.LocalStats `json:"data"`
	DataThrough   time.Time        `json:"data_through"`
}

func New(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) ListSessions(ctx context.Context, limit int) (SessionList, error) {
	data, dataThrough, err := s.repository.ListSessions(ctx, limit)
	return SessionList{
		SchemaVersion: SchemaVersion,
		Data:          data,
		DataThrough:   dataThrough,
	}, err
}

func (s *Service) GetSession(ctx context.Context, sessionID string) (SessionDetail, error) {
	data, dataThrough, err := s.repository.GetSession(ctx, sessionID)
	return SessionDetail{
		SchemaVersion: SchemaVersion,
		Data:          data,
		DataThrough:   dataThrough,
	}, err
}

func (s *Service) GetSessionTimeline(ctx context.Context, sessionID string, limit int) (SessionTimeline, error) {
	data, dataThrough, err := s.repository.GetSessionTimeline(ctx, sessionID, limit)
	return SessionTimeline{
		SchemaVersion: SchemaVersion,
		SessionID:     sessionID,
		Data:          data,
		DataThrough:   dataThrough,
	}, err
}

func (s *Service) QueryActivity(ctx context.Context, filter model.ActivityFilter) (ActivityList, error) {
	data, dataThrough, err := s.repository.QueryActivity(ctx, filter)
	return ActivityList{
		SchemaVersion: SchemaVersion,
		Data:          data,
		DataThrough:   dataThrough,
	}, err
}

func (s *Service) ListFindings(ctx context.Context, limit int) (FindingList, error) {
	data, dataThrough, err := s.repository.ListFindings(ctx, limit)
	return FindingList{
		SchemaVersion: SchemaVersion,
		Data:          data,
		DataThrough:   dataThrough,
	}, err
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
