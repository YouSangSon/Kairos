package application

import (
	"context"
	"errors"
	"omni-folio/services/core/internal/codec"
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

func (q *Queries) read(ctx context.Context, query c.InputQuery) (c.Inputs, error) {
	if q == nil || q.Inputs == nil || q.Now == nil {
		return c.Inputs{}, errors.New("portfolio service is unavailable")
	}
	all, err := q.Inputs.ReadValuationInputs(ctx, query)
	if err != nil {
		return c.Inputs{}, err
	}
	if query.At != nil && !all.Cutoff.Equal(*query.At) {
		return c.Inputs{}, errors.New("valuation inputs have a different cutoff")
	}
	if err := domain.ValidateInputs(all); err != nil {
		return c.Inputs{}, err
	}
	return all, nil
}
func (q *Queries) requested(raw, kind string) (time.Time, error) {
	if !codec.CanonicalUTC(raw) {
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
	at, _ := time.Parse(time.RFC3339Nano, raw)
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
	return domain.ComputeCashValuation(ctx, all, base, raw)
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
	return domain.ComputeHoldingValuation(ctx, all, raw)
}
func (q *Queries) CurrentValuation(ctx context.Context) (*c.CurrentPortfolioValuation, error) {
	all, err := q.read(ctx, c.InputQuery{FX: true, Prices: true})
	if err != nil {
		return nil, err
	}
	return domain.CurrentValuation(ctx, all)
}
func (q *Queries) XIRR(ctx context.Context) (*c.PortfolioXIRR, error) {
	all, err := q.read(ctx, c.InputQuery{FX: true, Prices: true})
	if err != nil {
		return nil, err
	}
	return domain.XIRR(ctx, all)
}
func (q *Queries) TWR(ctx context.Context) (*c.PortfolioTWR, error) {
	all, err := q.read(ctx, c.InputQuery{FX: true, Prices: true})
	if err != nil {
		return nil, err
	}
	return domain.TWR(ctx, all, nil)
}
func (q *Queries) TWRCheckpoints(ctx context.Context) (*c.PortfolioTWRCheckpoints, error) {
	all, err := q.read(ctx, c.InputQuery{FX: true, Prices: true})
	if err != nil {
		return nil, err
	}
	return domain.TWRCheckpoints(ctx, all)
}
func (q *Queries) Benchmarks(ctx context.Context) (*c.PortfolioBenchmarks, error) {
	all, err := q.read(ctx, c.InputQuery{FX: true, Prices: true, Benchmarks: true})
	if err != nil {
		return nil, err
	}
	return domain.Benchmarks(ctx, all)
}
