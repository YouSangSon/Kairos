package ledgerdomain

import (
	"errors"
	"math/big"
	"omni-folio/services/core/internal/exact"
)

func InstrumentAverageCost(quantityRaw, costRaw string) (string, error) {
	quantity, quantityErr := exact.ParseDecimal(quantityRaw)
	cost, costErr := exact.ParseDecimal(costRaw)
	if quantityErr != nil || costErr != nil || quantity.Sign() <= 0 || cost.Sign() < 0 {
		return "", errors.New("instrument ledger holding is invalid")
	}
	// Round only the display ratio; the exact remaining FIFO cost stays unchanged.
	return exact.FormatDecimal(exact.QuantizeHalfEven(new(big.Rat).Quo(cost, quantity), 8))
}
