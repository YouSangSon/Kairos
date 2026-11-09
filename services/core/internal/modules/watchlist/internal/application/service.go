package application

import (
	"context"
	c "omni-folio/services/core/internal/modules/watchlist/contracts"
	"omni-folio/services/core/internal/modules/watchlist/internal/domain"
)

type Service struct {
	Inputs c.InputsReader
	Store  c.Store
}

func (s *Service) Read(ctx context.Context) (*c.WatchlistReport, error) {
	all, err := s.Inputs.ReadWatchlistInputs(ctx)
	if err != nil {
		return nil, err
	}
	report, err := domain.Report(domainInputs(all))
	return publicReport(report), domainError(err)
}
func (s *Service) Replace(ctx context.Context, change c.WatchlistChange) (*c.WatchlistState, error) {
	return s.Store.Replace(ctx, change)
}
