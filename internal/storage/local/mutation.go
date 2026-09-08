package local

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync/atomic"

	sqlite "modernc.org/sqlite"
)

const (
	mutationNone           = int32(0)
	mutationPayloadUpgrade = int32(1)
	mutationRetentionPrune = int32(2)
)

var mutationFunctionSequence atomic.Uint64

type mutationAuthorizer struct {
	functionName string
	mode         atomic.Int32
}

func newMutationAuthorizer() (*mutationAuthorizer, error) {
	authorizer := &mutationAuthorizer{
		functionName: fmt.Sprintf(
			"belay_mutation_allowed_%x",
			mutationFunctionSequence.Add(1),
		),
	}
	err := sqlite.RegisterScalarFunction(
		authorizer.functionName,
		1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) != 1 {
				return int64(0), nil
			}
			purpose, ok := args[0].(string)
			if !ok {
				return int64(0), nil
			}
			switch purpose {
			case "payload_upgrade":
				if authorizer.mode.Load() == mutationPayloadUpgrade {
					return int64(1), nil
				}
			case "retention_prune":
				if authorizer.mode.Load() == mutationRetentionPrune {
					return int64(1), nil
				}
			}
			return int64(0), nil
		},
	)
	if err != nil {
		return nil, errors.New("register local mutation authorization")
	}
	return authorizer, nil
}

func (a *mutationAuthorizer) with(
	purpose int32,
	operation func() error,
) error {
	if !a.mode.CompareAndSwap(mutationNone, purpose) {
		return errors.New("local mutation authorization is already active")
	}
	defer a.mode.Store(mutationNone)
	return operation()
}

func (s *Store) installMutationTriggers(ctx context.Context) error {
	function := s.mutations.functionName
	body := fmt.Sprintf(`
		DROP TRIGGER IF EXISTS events_no_update;
		DROP TRIGGER IF EXISTS events_no_delete;
		DROP TRIGGER IF EXISTS findings_no_update;
		DROP TRIGGER IF EXISTS findings_no_delete;

		CREATE TRIGGER events_no_update
		BEFORE UPDATE ON events
		WHEN %[1]s('payload_upgrade') != 1
		BEGIN
			SELECT RAISE(ABORT, 'canonical events are append-only');
		END;

		CREATE TRIGGER events_no_delete
		BEFORE DELETE ON events
		WHEN %[1]s('retention_prune') != 1
		BEGIN
			SELECT RAISE(ABORT, 'canonical events are append-only');
		END;

		CREATE TRIGGER findings_no_update
		BEFORE UPDATE ON findings
		WHEN %[1]s('payload_upgrade') != 1
		BEGIN
			SELECT RAISE(ABORT, 'findings are append-only');
		END;

		CREATE TRIGGER findings_no_delete
		BEFORE DELETE ON findings
		WHEN %[1]s('retention_prune') != 1
		BEGIN
			SELECT RAISE(ABORT, 'findings are append-only');
		END;`,
		function,
	)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("begin local mutation-trigger installation")
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return errors.New("install local mutation triggers")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("commit local mutation-trigger installation")
	}
	return nil
}
