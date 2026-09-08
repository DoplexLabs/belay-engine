package local

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
)

func (s *Store) LookupSessionEvents(
	ctx context.Context,
	query model.EventLookupQuery,
) (model.EventLookupResult, error) {
	if query.SessionID == "" ||
		len(query.SessionID) > 256 ||
		len(query.EventIDs) == 0 ||
		len(query.EventIDs) > model.MaxEventLookupIDs {
		return model.EventLookupResult{}, errors.New("invalid event lookup request")
	}
	requested := make([]string, 0, len(query.EventIDs))
	seen := make(map[string]struct{}, len(query.EventIDs))
	for _, eventID := range query.EventIDs {
		if !model.IsCanonicalUUIDv7(eventID) {
			return model.EventLookupResult{}, errors.New("invalid event lookup request")
		}
		if _, exists := seen[eventID]; exists {
			continue
		}
		seen[eventID] = struct{}{}
		requested = append(requested, eventID)
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return model.EventLookupResult{}, errors.New("begin event lookup")
	}
	defer tx.Rollback()

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(requested)), ",")
	args := make([]any, 0, len(requested)+1)
	args = append(args, query.SessionID)
	for _, eventID := range requested {
		args = append(args, eventID)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT event_id, canonical_json, canonical_encoding
		FROM events
		WHERE session_key = ? AND event_id IN (`+placeholders+`)
		ORDER BY source_sequence ASC, occurred_at ASC, event_id ASC`,
		args...,
	)
	if err != nil {
		return model.EventLookupResult{}, fmt.Errorf("lookup session events: %w", err)
	}
	events, err := s.decodeEventRows(rows)
	if err != nil {
		return model.EventLookupResult{}, err
	}
	if events == nil {
		events = make([]model.Event, 0)
	}
	dataThrough, err := dataThroughQuery(ctx, tx, "")
	if err != nil {
		return model.EventLookupResult{}, errors.New("read event lookup watermark")
	}

	found := make(map[string]struct{}, len(events))
	for _, event := range events {
		found[event.EventID] = struct{}{}
	}
	missing := make([]string, 0, len(requested)-len(events))
	for _, eventID := range requested {
		if _, exists := found[eventID]; !exists {
			missing = append(missing, eventID)
		}
	}
	if err := tx.Commit(); err != nil {
		return model.EventLookupResult{}, errors.New("complete event lookup")
	}
	return model.EventLookupResult{
		Data:            events,
		RequestedCount:  len(requested),
		FoundCount:      len(events),
		MissingCount:    len(missing),
		MissingEventIDs: missing,
		DataThrough:     dataThrough,
	}, nil
}
