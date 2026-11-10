package sqlite

import (
	"context"
	"database/sql"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	csvimport "omni-folio/services/core/internal/modules/ledger/internal/adapters/csv"
	ledger "omni-folio/services/core/internal/modules/ledger/internal/application"
	"omni-folio/services/core/internal/storage/observation"
	"time"
)

func NewService(db *sql.DB, now func() time.Time, id func(string) string, ttl func() time.Duration, key [32]byte, resolver *observation.ReadResolver) *Service {
	return &Service{UseCases: ledger.New(NewLedger(db), now, id, ttl), db: db, activityCursorKey: key, resolver: resolver}
}
func (s *Service) PreviewImport(ctx context.Context, raw []byte) (*contracts.ImportPreview, *contracts.Error) {
	input, err := csvimport.Decode(raw)
	if err != nil {
		return nil, err
	}
	return s.UseCases.PreviewImport(ctx, input)
}
func (s *Service) VerifyBrokerSync(ctx context.Context) error {
	return preflightKiwoomMockSync(ctx, newQueryer(s.db))
}
func (s *Service) CheckBrokerBinding(ctx context.Context, account string) error {
	return requireKiwoomMockBinding(ctx, newQueryer(s.db), account)
}
