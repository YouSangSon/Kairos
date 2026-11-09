package domain

import (
	"math/big"
	"omni-folio/services/core/internal/exact"
	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	c "omni-folio/services/core/internal/modules/portfolio/contracts"
	"sort"
)

type APIError = c.APIError
type Money = c.Money
type BenchmarkSeriesReceipt = c.BenchmarkSeriesReceipt
type BenchmarkIndexPoint = c.BenchmarkIndexPoint
type HoldingValuationPrice = c.HoldingValuationPrice
type HoldingValuationLine = c.HoldingValuationLine
type HoldingValuationTotal = c.HoldingValuationTotal
type HoldingValuation = c.HoldingValuation
type CashValuationFX = c.CashValuationFX
type CashValuationLine = c.CashValuationLine
type CashValuation = c.CashValuation
type CurrentPortfolioValuation = c.CurrentPortfolioValuation
type PortfolioTWR = c.PortfolioTWR
type PortfolioTWRCheckpoint = c.PortfolioTWRCheckpoint
type PortfolioTWRCheckpoints = c.PortfolioTWRCheckpoints
type PortfolioXIRR = c.PortfolioXIRR
type BenchmarkRateResult = c.BenchmarkRateResult
type BenchmarkFXMark = c.BenchmarkFXMark
type PortfolioBenchmark = c.PortfolioBenchmark
type PortfolioBenchmarks = c.PortfolioBenchmarks
type Holding = ledger.Holding
type PortfolioSnapshot = ledger.PortfolioSnapshot
type Transaction = ledger.ValuationEvent
type FXObservation = market.FXObservation
type SecurityPriceObservation = market.SecurityPriceObservation
type benchmarkSeriesRecord = market.BenchmarkVersion
type Inputs = c.Inputs

const ownerPriceSource = "owner_declared"
const ownerPriceAdjustment = "unadjusted"
const ownerFXSource = "owner_declared"
const marketDataAdjustmentUnspecified = "unspecified"

var parseDecimal = exact.ParseDecimal
var formatDecimal = exact.FormatDecimal
var quantizeHalfEven = exact.QuantizeHalfEven

func addRat(values map[string]*big.Rat, key string, value *big.Rat) {
	if values[key] == nil {
		values[key] = new(big.Rat)
	}
	values[key].Add(values[key], value)
}
func sortedKeys(values map[string]*big.Rat) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
