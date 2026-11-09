package observations

import (
	"context"
	"errors"
	"time"

	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
)

type OwnerPrice struct {
	Registry *observation.Registry
	Ledger   interface {
		AdmittedPriceIdentities(context.Context, storage.ObservationRef) (ledger.AdmittedPrices, error)
	}
	Scoped market.ScopedOwnerPriceImport
}

type ownerPriceContextKey struct{}

func (c *OwnerPrice) ImportOwnerPriceAtomic(ctx context.Context, command market.OwnerPriceCommand) (*market.OwnerPriceSeriesReceipt, error) {
	if ctx.Value(ownerPriceContextKey{}) != nil {
		return nil, errors.New("owner price import cannot reenter its atomic coordinator")
	}
	ctx = context.WithValue(ctx, ownerPriceContextKey{}, true)
	var result *market.OwnerPriceSeriesReceipt
	err := c.Registry.ImportOwnerPrice(ctx, func(ref storage.ObservationRef, _ time.Time) error {
		admitted, err := c.Ledger.AdmittedPriceIdentities(ctx, ref)
		if err != nil {
			return err
		}
		identities := market.PriceIdentities{LedgerRevision: admitted.LedgerRevision, Identities: make(map[string]struct{}, len(admitted.Identities))}
		for _, value := range admitted.Identities {
			identities.Identities[value.InstrumentID+"\x00"+value.Currency] = struct{}{}
		}
		result, err = c.Scoped.ImportOwnerPriceScoped(ctx, ref, command, identities)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
