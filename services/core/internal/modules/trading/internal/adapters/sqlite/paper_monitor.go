package sqlite

import (
	"context"

	"omni-folio/services/core/internal/modules/trading/contracts"
)

type PaperMonitor = contracts.PaperMonitor
type PaperMonitorRunner = contracts.PaperMonitorRunner
type PaperMonitorPolicy = contracts.PaperMonitorPolicy

func (s *Store) PaperMonitor(ctx context.Context) (*PaperMonitor, error) {
	return s.business().PaperMonitor(ctx)
}
