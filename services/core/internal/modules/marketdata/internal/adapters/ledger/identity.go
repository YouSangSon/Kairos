package ledger

import (
	"context"
	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
)

func New(q ledger.Queries) *ledgerAdapter { return &ledgerAdapter{q} }

type ledgerAdapter struct{ ledger.Queries }

func (a ledgerAdapter) AdmittedPriceIdentities(ctx context.Context, ref storage.ObservationRef) (c.PriceIdentities, error) {
	values, err := a.Queries.AdmittedPriceIdentities(ctx, ref)
	if err != nil {
		return c.PriceIdentities{}, err
	}
	result := c.PriceIdentities{LedgerRevision: values.LedgerRevision, Identities: make(map[string]struct{}, len(values.Identities))}
	for _, value := range values.Identities {
		result.Identities[value.InstrumentID+"\x00"+value.Currency] = struct{}{}
	}
	return result, nil
}
