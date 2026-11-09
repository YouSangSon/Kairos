package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	trading "omni-folio/services/core/internal/modules/trading/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"time"
)

const latestSchema = 27

type Store struct {
	db           *sql.DB
	observations *observation.Registry
	trading      trading.Recovery
}

func New(db *sql.DB, observations *observation.Registry, trading trading.Recovery) *Store {
	return &Store{db: db, observations: observations, trading: trading}
}

type orderQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) readMigrationFacts(ctx context.Context, proof *observation.MigrationProof, query trading.MigrationQuery) (*trading.MigrationSnapshot, error) {
	var result *trading.MigrationSnapshot
	err := proof.Verify(ctx, func(ref storage.ObservationRef, _ time.Time) error {
		var err error
		result, err = s.trading.MigrationFacts(ctx, ref, query)
		return err
	})
	return result, err
}
func requireSchemaVersionContext(ctx context.Context, q orderQuerier, expected int) error {
	var first, latest, count int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MIN(version), 0), COALESCE(MAX(version), 0), COUNT(*) FROM schema_migrations`).Scan(&first, &latest, &count); err != nil {
		return fmt.Errorf("database is not migrated; run migrate first: %w", err)
	}
	if first != 1 || latest != expected || count != expected {
		return fmt.Errorf("unsupported schema history %d..%d (%d migrations)", first, latest, count)
	}
	return nil
}
func sameStrategyRegistryProof(left, right trading.StrategyRecovery) bool { return left == right }
