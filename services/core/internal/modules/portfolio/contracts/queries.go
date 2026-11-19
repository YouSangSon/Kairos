package contracts

import (
	"context"
	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	"time"
)

type Inputs struct {
	Cutoff time.Time
	Ledger ledger.ValuationFacts
	Market market.ValuationInputs
}
type InputQuery struct {
	At                     *time.Time
	FX, Prices, Benchmarks bool
}
type InputsReader interface {
	ReadValuationInputs(context.Context, InputQuery) (Inputs, error)
}
type Queries interface {
	CashValuation(context.Context, string, string) (*CashValuation, error)
	HoldingValuation(context.Context, string) (*HoldingValuation, error)
	ValuationEvidence(context.Context) (*PortfolioValuationEvidence, error)
	CurrentValuation(context.Context) (*CurrentPortfolioValuation, error)
	XIRR(context.Context) (*PortfolioXIRR, error)
	TWR(context.Context) (*PortfolioTWR, error)
	TWRCheckpoints(context.Context) (*PortfolioTWRCheckpoints, error)
	Benchmarks(context.Context) (*PortfolioBenchmarks, error)
}
type Failure struct {
	Code, Message, Field string
	Conflict             bool
}

func (e *Failure) Error() string { return e.Message }
