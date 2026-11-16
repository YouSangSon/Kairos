package sqlite

import (
	"context"

	"omni-folio/services/core/internal/modules/trading/contracts"
)

type LocalOrderLog = contracts.LocalOrderLog
type LocalOrderView = contracts.LocalOrderView

func (s *Store) LocalOrderLog(ctx context.Context) (*LocalOrderLog, error) {
	return s.business().LocalOrderLog(ctx)
}
