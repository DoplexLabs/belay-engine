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
	GetSessionTimeline(context.Context, string, int) ([]model.Event, time.Time, error)
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

func (s *Service) GetSessionTimeline(ctx context.Context, sessionID string, limit int) (SessionTimeline, error) {
	data, dataThrough, err := s.repository.GetSessionTimeline(ctx, sessionID, limit)
	return SessionTimeline{
		SchemaVersion: SchemaVersion,
		SessionID:     sessionID,
		Data:          data,
		DataThrough:   dataThrough,
	}, err
}
