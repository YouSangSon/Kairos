package application

import (
	"context"
	"errors"
	c "omni-folio/services/core/internal/modules/portfolio/contracts"
	"omni-folio/services/core/internal/modules/portfolio/internal/domain"
	"regexp"
	"time"
)

type Queries struct {
	Inputs c.InputsReader
	Now    func() time.Time
}

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

func (q *Queries) read(ctx context.Context, query c.InputQuery) (domain.Inputs, error) {
	if q == nil || q.Inputs == nil || q.Now == nil {
		return domain.Inputs{}, errors.New("portfolio service is unavailable")
	}
	all, err := q.Inputs.ReadValuationInputs(ctx, query)
	if err != nil {
		return domain.Inputs{}, err
	}
	if query.At != nil && !all.Cutoff.Equal(*query.At) {
		return domain.Inputs{}, errors.New("valuation inputs have a different cutoff")
	}
	canonical := valuationInputs(all)
	if err := domain.ValidateInputs(canonical); err != nil {
		return domain.Inputs{}, err
	}
	return canonical, nil
}
func (q *Queries) requested(raw, kind string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || at.UTC().Format(time.RFC3339Nano) != raw {
		message := "holding valuation as_of is invalid"
		if kind == "cash" {
			message = "cash valuation query values are invalid"
		}
		f := &c.Failure{Code: "invalid_query", Message: message}
		if kind == "holding" {
			f.Field = "as_of"
		}
		return time.Time{}, f
	}
	if at.After(q.Now().UTC()) {
		return time.Time{}, &c.Failure{Code: "invalid_query", Message: "valuation as_of cannot be in the future", Field: "as_of"}
	}
	return at, nil
}
func (q *Queries) CashValuation(ctx context.Context, base, raw string) (*c.CashValuation, error) {
	if !currencyCode.MatchString(base) {
		return nil, &c.Failure{Code: "invalid_query", Message: "cash valuation query values are invalid"}
	}
	at, err := q.requested(raw, "cash")
	if err != nil {
		return nil, err
	}
	all, err := q.read(ctx, c.InputQuery{At: &at, FX: true})
	if err != nil {
		return nil, err
	}
	result, err := domain.ComputeCashValuation(ctx, all, base, raw)
	return publicCashValuation(result), domainError(err)
}
func (q *Queries) HoldingValuation(ctx context.Context, raw string) (*c.HoldingValuation, error) {
	at, err := q.requested(raw, "holding")
	if err != nil {
		return nil, err
	}
	all, err := q.read(ctx, c.InputQuery{At: &at, Prices: true})
	if err != nil {
		return nil, err
	}
	result, err := domain.ComputeHoldingValuation(ctx, all, raw)
	return publicHoldingValuation(result), domainError(err)
}
func (q *Queries) CurrentValuation(ctx context.Context) (*c.CurrentPortfolioValuation, error) {
	all, err := q.read(ctx, c.InputQuery{FX: true, Prices: true})
	if err != nil {
		return nil, err
	}
	result, err := domain.CurrentValuation(ctx, all)
	return publicCurrentPortfolioValuation(result), domainError(err)
}
func (q *Queries) XIRR(ctx context.Context) (*c.PortfolioXIRR, error) {
	all, err := q.read(ctx, c.InputQuery{FX: true, Prices: true})
	if err != nil {
		return nil, err
	}
	result, err := domain.XIRR(ctx, all)
	return publicPortfolioXIRR(result), domainError(err)
}
func (q *Queries) TWR(ctx context.Context) (*c.PortfolioTWR, error) {
	all, err := q.read(ctx, c.InputQuery{FX: true, Prices: true})
	if err != nil {
		return nil, err
	}
	result, err := domain.TWR(ctx, all, nil)
	return publicPortfolioTWR(result), domainError(err)
}
func (q *Queries) TWRCheckpoints(ctx context.Context) (*c.PortfolioTWRCheckpoints, error) {
	all, err := q.read(ctx, c.InputQuery{FX: true, Prices: true})
	if err != nil {
		return nil, err
	}
	result, err := domain.TWRCheckpoints(ctx, all)
	return publicPortfolioTWRCheckpoints(result), domainError(err)
}
func (q *Queries) Benchmarks(ctx context.Context) (*c.PortfolioBenchmarks, error) {
	all, err := q.read(ctx, c.InputQuery{FX: true, Prices: true, Benchmarks: true})
	if err != nil {
		return nil, err
	}
	result, err := domain.Benchmarks(ctx, all)
	return publicPortfolioBenchmarks(result), domainError(err)
}
