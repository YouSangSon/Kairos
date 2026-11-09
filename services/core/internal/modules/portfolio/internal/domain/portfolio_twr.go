package domain

import (
	"context"
	"errors"
	"math/big"
	"omni-folio/services/core/internal/exact"
	"time"
)

const portfolioTWRPolicy = "restated_external_first_exact_timestamp_twr_v1"
const portfolioTWROwnerFXPolicy = "restated_external_first_exact_timestamp_twr_v2"
const portfolioTWROwnerPricePolicy = "restated_external_first_exact_timestamp_twr_v3"

func TWR(ctx context.Context, all Inputs, points *[]PortfolioTWRCheckpoint) (*PortfolioTWR, error) {
	asOf := all.Cutoff
	inputs, err := fxInputs(all)
	if err != nil {
		return nil, err
	}
	prices, err := priceInputs(all)
	if err != nil {
		return nil, err
	}
	valuation, appErr := CurrentValuation(ctx, all)
	if appErr != nil {
		return nil, appErr
	}

	r := &PortfolioTWR{PolicyVersion: portfolioTWRPolicy, Scope: "whole_portfolio_since_inception", Valuation: valuation, Status: "unavailable", Sample: valuation.Sample, PeriodEnd: asOf.Format(time.RFC3339Nano), Issues: []APIError{}}
	marks := twrMarks{fx: inputs.observations, fxTimes: inputs.times, prices: prices.observations, priceTimes: prices.times}
	finish := func(issue string) (*PortfolioTWR, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if issue != "" {
			r.Issues = append(r.Issues, APIError{Code: issue, Message: "a required input does not meet the declared TWR policy"})
		}
		if r.Sample {
			r.Issues = append(r.Issues, APIError{Code: "sample_data", Message: "return inputs include stored local sample observations"})
		}
		if usesOwnerFX(valuation.Issues) || marks.ownerUsed {
			r.PolicyVersion = portfolioTWROwnerFXPolicy
			r.Issues = append(r.Issues, ownerFXIssue())
		}
		if usesOwnerPrice(valuation.Issues) || marks.ownerPriceUsed {
			r.PolicyVersion = portfolioTWROwnerPricePolicy
			r.Issues = append(r.Issues, ownerPriceIssue())
		}
		return r, nil
	}
	if valuation.Status == "empty" {
		r.Status = "empty"
		r.ExternalEventCount = new(int)
		r.BoundaryCount = new(int)
		return finish("")
	}
	if valuation.TotalValue == nil {
		return finish("valuation_unavailable")
	}
	// Restate both external and internal cash corrections in every historical balance.
	events := all.Ledger.RestatedEvents

	count, boundaries := 0, 0
	last := ""
	for _, e := range events {
		if e.Type != "DEPOSIT" && e.Type != "WITHDRAWAL" {
			continue
		}
		count++
		if r.PeriodStart == nil {
			r.PeriodStart = stringPointer(e.OccurredAt)
		}
		if e.OccurredAt != last {
			boundaries++
			last = e.OccurredAt
		}
	}
	r.ExternalEventCount, r.BoundaryCount = &count, &boundaries
	if count == 0 {
		return finish("no_external_cash_flows")
	}
	// This bounds return calculation, not the required full-ledger integrity proof.
	if boundaries > 1000 {
		return finish("cash_flow_limit")
	}
	b := newTWRBalances()
	factor := big.NewRat(1, 1)
	var capital *big.Rat
	net := new(big.Rat)
	for i := 0; i < len(events); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		j := i + 1
		for j < len(events) && events[j].OccurredAt == events[i].OccurredAt {
			j++
		}
		at, err := time.Parse(time.RFC3339Nano, events[i].OccurredAt)
		if err != nil {
			return nil, err
		}
		hasFlow, hasSplit := false, false
		for _, e := range events[i:j] {
			hasFlow = hasFlow || e.Type == "DEPOSIT" || e.Type == "WITHDRAWAL"
			hasSplit = hasSplit || e.Type == "SPLIT"
		}
		if hasFlow {
			if hasSplit {
				return finish("ambiguous_split_boundary")
			}
			before, used, issue := marks.value(ctx, b, at, asOf)
			r.Sample = r.Sample || used
			if issue != "" {
				return finish(issue)
			}
			if capital == nil {
				if !b.zero() {
					return finish("capital_path_unavailable")
				}
			} else {
				if before.Sign() <= 0 {
					return finish("capital_path_unavailable")
				}
				factor = TWRChain(factor, before, capital)
				if factor == nil {
					return finish("numerical_limit")
				}
			}
			flow := new(big.Rat)
			for _, e := range events[i:j] {
				if e.Type != "DEPOSIT" && e.Type != "WITHDRAWAL" {
					continue
				}
				amount, err := exact.ParseDecimal(e.Amount)
				if err != nil {
					return nil, err
				}
				v, used, issue := marks.convert(amount, e.Currency, at, asOf)
				r.Sample = r.Sample || used
				if issue != "" {
					return finish(issue)
				}
				flow.Add(flow, v)
				if err := b.Apply(e); err != nil {
					return nil, err
				}
			}
			net.Add(net, flow)
			capital = new(big.Rat).Add(before, flow)
			if capital.Sign() <= 0 {
				return finish("capital_path_unavailable")
			}
			if points != nil {
				rate, err := twrCheckpointRate(factor)
				if err != nil {
					return nil, err
				}
				*points = append(*points, PortfolioTWRCheckpoint{At: events[i].OccurredAt, Kind: "external_flow", CumulativeRate: rate})
			}
		}
		// External capital precedes internal events at the same exact timestamp.
		for _, e := range events[i:j] {
			if e.Type == "DEPOSIT" || e.Type == "WITHDRAWAL" {
				continue
			}
			if err := b.Apply(e); err != nil {
				return nil, err
			}
		}
		i = j
	}
	snapshot := &all.Ledger.Snapshot
	if !b.matches(snapshot) {
		return nil, errors.New("TWR projection differs from proved ledger balances")
	}
	terminal, used, issue := marks.value(ctx, b, asOf, asOf)
	r.Sample = r.Sample || used
	if issue != "" {
		return finish(issue)
	}
	current, err := exact.ParseDecimal(valuation.TotalValue.Amount)
	if err != nil || terminal.Cmp(current) != 0 {
		return nil, errors.New("TWR terminal valuation mismatch")
	}
	if terminal.Sign() < 0 {
		return finish("capital_path_unavailable")
	}
	factor = TWRChain(factor, terminal, capital)
	if factor == nil {
		return finish("numerical_limit")
	}
	rate, err := exact.FormatDecimal(exact.QuantizeHalfEven(new(big.Rat).Sub(factor, big.NewRat(1, 1)), 12))
	if err != nil || len(rate) > 128 {
		return finish("numerical_limit")
	}
	netText, err := exact.FormatDecimal(net)
	if err != nil {
		return nil, err
	}
	r.Status = "calculated"
	r.CumulativeRate = &rate
	r.NetContributions = &Money{Currency: "KRW", Amount: netText}
	if points != nil {
		*points = append(*points, PortfolioTWRCheckpoint{At: r.PeriodEnd, Kind: "valuation", CumulativeRate: rate})
	}
	return finish("")
}
