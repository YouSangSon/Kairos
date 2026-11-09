package sqlite

import (
	"context"
	"database/sql"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	csvimport "omni-folio/services/core/internal/modules/ledger/internal/adapters/csv"
	ledger "omni-folio/services/core/internal/modules/ledger/internal/application"
	ledgerdomain "omni-folio/services/core/internal/modules/ledger/internal/domain"
	"omni-folio/services/core/internal/storage/observation"
	"time"
)

func NewService(db *sql.DB, now func() time.Time, id func(string) string, ttl func() time.Duration, key [32]byte, resolver *observation.ReadResolver) *Service {
	return &Service{UseCases: ledger.New(NewLedger(db), now, id, ttl), db: db, now: now, id: id, activityCursorKey: key, resolver: resolver}
}
func (s *Service) Snapshot(ctx context.Context) (*contracts.PortfolioSnapshot, error) {
	return s.CurrentSnapshot(ctx)
}
func (s *Service) Activity(ctx context.Context, limit int, cursor *contracts.ActivityCursor) (*contracts.LedgerActivityPage, error) {
	return s.ledgerActivities(ctx, limit, cursor)
}
func (s *Service) InstrumentActivity(ctx context.Context, id, currency string) (*contracts.InstrumentLedger, *contracts.Error) {
	return s.instrumentLedger(ctx, id, currency)
}
func (s *Service) RecordBrokerSnapshot(ctx context.Context, account string, value *contracts.BrokerSnapshot) (*contracts.BrokerKnownGoodSnapshot, error) {
	return s.recordKiwoomSnapshot(ctx, account, value)
}
func (s *Service) LatestBrokerSnapshot(ctx context.Context, env contracts.BrokerEnvironment, exchange contracts.BrokerExchange, account string) (*contracts.BrokerKnownGoodSnapshot, error) {
	return s.latestKiwoomSnapshot(ctx, env, exchange, account)
}
func (s *Service) Reconciliation(ctx context.Context) (*contracts.BrokerReconciliationView, error) {
	return s.latestBrokerReconciliation(ctx)
}
func (s *Service) PreviewReplay(ctx context.Context) (*contracts.ReplayPreview, error) {
	return previewLedgerReplay(ctx, s.db)
}
func (s *Service) ActivateReplay(ctx context.Context, rev int64, eventSHA, snapshotSHA string) (*contracts.ReplayPreview, error) {
	return activateLedgerReplay(ctx, s.db, rev, eventSHA, snapshotSHA, s.now())
}

func (s *Service) PreviewImport(ctx context.Context, raw []byte) (*contracts.ImportPreview, *contracts.Error) {
	input, err := csvimport.Decode(raw)
	if err != nil {
		return nil, err
	}
	return s.UseCases.PreviewImport(ctx, input)
}
func (s *Service) CanonicalInstrumentID(symbol string) string {
	return ledgerdomain.InstrumentIDForSymbol(symbol)
}
