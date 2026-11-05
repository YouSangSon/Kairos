package portfoliodomain

import (
	"errors"
	"math/big"

	"omni-folio/services/core/internal/exact"
)

type BenchmarkRateResult struct {
	Status string  `json:"status"`
	Rate   *string `json:"rate"`
	Issue  *string `json:"issue"`
}

func UnavailableBenchmarkRate(issue string) BenchmarkRateResult {
	return BenchmarkRateResult{Status: "unavailable", Issue: &issue}
}

func BenchmarkRate(growth *big.Rat) BenchmarkRateResult {
	rate, err := exact.FormatDecimal(exact.QuantizeHalfEven(new(big.Rat).Sub(growth, big.NewRat(1, 1)), 12))
	if err != nil || len(rate) > 128 {
		return UnavailableBenchmarkRate("numerical_limit")
	}
	return BenchmarkRateResult{Status: "calculated", Rate: &rate}
}

func BenchmarkDifference(portfolio, benchmark string) (*string, error) {
	p, err := exact.ParseDecimal(portfolio)
	if err != nil {
		return nil, err
	}
	b, err := exact.ParseDecimal(benchmark)
	if err != nil {
		return nil, err
	}
	// Difference of published rates, in percentage points; no second rounding.
	text, err := exact.FormatDecimal(new(big.Rat).Mul(new(big.Rat).Sub(p, b), big.NewRat(100, 1)))
	if err != nil || len(text) > 132 {
		return nil, errors.New("benchmark difference exceeds its declared bounds")
	}
	return &text, nil
}
