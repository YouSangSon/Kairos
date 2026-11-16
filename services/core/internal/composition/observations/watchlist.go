package observations

import (
	"context"
	"errors"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	watch "omni-folio/services/core/internal/modules/watchlist/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"time"
)

type Watchlist struct {
	Registry *observation.Registry
	States   watch.StateReader
	Market   market.Queries
}

func (w *Watchlist) ReadWatchlistInputs(ctx context.Context) (watch.Inputs, error) {
	var all watch.Inputs
	err := w.Registry.ReadWatchlist(ctx, func(ref storage.ObservationRef, cutoff time.Time) error {
		state, err := w.States.ReadState(ctx, ref)
		if err != nil {
			return err
		}
		inputs, err := w.Market.ReadValuationInputs(ctx, ref, market.ValuationQuery{Prices: true})
		if err != nil {
			return err
		}
		if !inputs.Cutoff.Equal(cutoff) {
			return errors.New("market inputs do not belong to the watchlist observation")
		}
		all = watch.Inputs{Cutoff: cutoff, State: state, Prices: inputs.Prices}
		return nil
	})
	if err != nil {
		return watch.Inputs{}, err
	}
	return all, nil
}
