package observations

import (
	"context"
	"errors"
	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	portfolio "omni-folio/services/core/internal/modules/portfolio/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"time"
)

type ValuationLedger interface {
	ValuationFacts(context.Context, storage.ObservationRef) (ledger.ValuationFacts, error)
}
type Portfolio struct {
	Registry *observation.Registry
	Ledger   ValuationLedger
	Market   market.Queries
}

func (p Portfolio) ReadValuationInputs(ctx context.Context, query portfolio.InputQuery) (portfolio.Inputs, error) {
	var inputs portfolio.Inputs
	err := p.Registry.ReadValuationAt(ctx, query.At, func(ref storage.ObservationRef, cutoff time.Time) error {
		facts, err := p.Ledger.ValuationFacts(ctx, ref)
		if err != nil {
			return err
		}
		marketInputs, err := p.Market.ReadValuationInputs(ctx, ref, market.ValuationQuery{FX: query.FX, Prices: query.Prices, Benchmarks: query.Benchmarks})
		if err != nil {
			return err
		}
		if !marketInputs.Cutoff.Equal(cutoff) {
			return errors.New("market inputs do not belong to the valuation observation")
		}
		inputs = portfolio.Inputs{Cutoff: cutoff, Ledger: facts, Market: marketInputs}
		return nil
	})
	if err != nil {
		return portfolio.Inputs{}, err
	}
	return inputs, nil
}
