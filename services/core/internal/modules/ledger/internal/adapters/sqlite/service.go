package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	ledger "omni-folio/services/core/internal/modules/ledger/internal/application"
	"omni-folio/services/core/internal/storage/observation"
)

type Service struct {
	*ledger.UseCases
	db       *sql.DB
	resolver *observation.ReadResolver
}
type queryer = Querier
type orderQuerier = Querier
type Transaction = contracts.Transaction
type PortfolioSnapshot = contracts.PortfolioSnapshot
type ledgerReplayPreview = contracts.ReplayPreview
type KiwoomEnvironment = contracts.BrokerEnvironment
type KiwoomExchange = contracts.BrokerExchange

const (
	KiwoomProduction    KiwoomEnvironment = "production"
	KiwoomMock          KiwoomEnvironment = "mock"
	KiwoomKRX           KiwoomExchange    = "KRX"
	fifoCostBasisPolicy                   = contracts.FIFOCostBasisPolicy
	zeroTime                              = "1970-01-01T00:00:00Z"
)

func revision(v int64) string { return fmt.Sprintf("rev_%010d", v) }
func snapshotFrom(ctx context.Context, q queryer) (*PortfolioSnapshot, error) {
	return ledger.Snapshot(ctx, NewLedgerReader(q))
}
func snapshotFromPolicy(ctx context.Context, q queryer, p string, r int64) (*PortfolioSnapshot, error) {
	return ledger.SnapshotAt(ctx, NewLedgerReader(q), p, r)
}
func requireSchemaContext(ctx context.Context, q queryer) error {
	var first, latest, count int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MIN(version), 0), COALESCE(MAX(version), 0), COUNT(*) FROM schema_migrations`).Scan(&first, &latest, &count); err != nil {
		return fmt.Errorf("database is not migrated; run migrate first: %w", err)
	}
	if first != 1 || latest != 27 || count != 27 {
		return fmt.Errorf("unsupported schema history %d..%d (%d migrations)", first, latest, count)
	}
	return nil
}
