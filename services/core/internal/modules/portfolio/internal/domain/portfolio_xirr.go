package domain

import (
	"context"
	"math/big"
	"omni-folio/services/core/internal/exact"
	"time"
)

const portfolioXIRRPolicy = "restated_external_cash_utc_date_actual_365_v1"
const portfolioXIRROwnerFXPolicy = "restated_external_cash_utc_date_actual_365_v2"
const portfolioXIRROwnerPricePolicy = "restated_external_cash_utc_date_actual_365_v3"

func XIRR(ctx context.Context, all Inputs) (*PortfolioXIRR, error) {
	asOf := all.Cutoff
	inputs, err := fxInputs(all)
	if err != nil {
		return nil, err
	}
	valuation, appErr := CurrentValuation(ctx, all)
	if appErr != nil {
		return nil, appErr
	}

	result := &PortfolioXIRR{PolicyVersion: portfolioXIRRPolicy, SolverPolicy: XIRRSolverPolicy, Scope: "whole_portfolio_since_inception", Valuation: valuation, Status: "unavailable", Sample: valuation.Sample, PeriodEnd: asOf.Format("2006-01-02"), Uniqueness: "not_applicable", Issues: []APIError{}}
	usedOwnerFX := usesOwnerFX(valuation.Issues)
	finish := func() (*PortfolioXIRR, error) {
		if result.Sample {
			result.Issues = appendCurrentValuationIssue(result.Issues, APIError{Code: "sample_data", Message: "return inputs include stored local sample observations"})
		}
		if usedOwnerFX {
			result.PolicyVersion = portfolioXIRROwnerFXPolicy
			result.Issues = appendCurrentValuationIssue(result.Issues, ownerFXIssue())
		}
		if usesOwnerPrice(valuation.Issues) {
			result.PolicyVersion = portfolioXIRROwnerPricePolicy
			result.Issues = appendCurrentValuationIssue(result.Issues, ownerPriceIssue())
		}
		return result, nil
	}
	if valuation.Status == "empty" {
		result.Status = "empty"
		result.ExternalEventCount = new(int)
		result.SignChanges = new(int)
		return finish()
	}
	if valuation.TotalValue == nil {
		result.Issues = append(result.Issues, APIError{Code: "valuation_unavailable", Message: "a complete current portfolio value is required"})
		return finish()
	}
	// A void restates its original external flow, rather than inventing a withdrawal.
	type externalFlow struct {
		at               time.Time
		currency, amount string
	}
	external := make([]externalFlow, 0, len(all.Ledger.ExternalFlows))
	for _, e := range all.Ledger.ExternalFlows {
		at, err := time.Parse(time.RFC3339Nano, e.OccurredAt)
		if err != nil {
			return nil, err
		}
		external = append(external, externalFlow{at, e.Currency, e.Amount})
	}

	eventCount := len(external)
	result.ExternalEventCount = &eventCount
	observations, fxTimes := inputs.observations, inputs.times
	flows := make([]XIRRCashFlow, 0, len(external)+1)
	contributions := new(big.Rat)
	for _, flow := range external {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		amount, err := exact.ParseDecimal(flow.amount)
		if err != nil {
			return nil, err
		}
		if result.PeriodStart == nil {
			day := flow.at.Format("2006-01-02")
			result.PeriodStart = &day
		}
		if flow.currency != "KRW" {
			fx := historicalXIRRFX(observations, fxTimes, flow.currency, flow.at, asOf)
			if fx == nil {
				result.Issues = appendCurrentValuationIssue(result.Issues, APIError{Code: "missing_historical_fx", Message: "an eligible historical direct FX observation is required", Field: flow.currency})
				continue
			}
			result.Sample = result.Sample || fxIsSample(fx)
			usedOwnerFX = usedOwnerFX || fx.Source == ownerFXSource
			if flow.at.Sub(fxTimes[fxTimeKey(*fx)][0]) > cashFXMaxAge {
				result.Issues = appendCurrentValuationIssue(result.Issues, APIError{Code: "stale_historical_fx", Message: "historical FX exceeds the event-time age policy", Field: flow.currency})
				continue
			}
			rate, err := exact.ParseDecimal(fx.Rate)
			if err != nil {
				return nil, err
			}
			amount.Mul(amount, rate)
		}
		contributions.Add(contributions, amount)
		flows = append(flows, XIRRCashFlow{Day: UTCDay(flow.at), Amount: new(big.Rat).Neg(amount)})
	}
	if len(result.Issues) > 0 {
		return finish()
	}
	text, err := exact.FormatDecimal(contributions)
	if err != nil {
		return nil, err
	}
	result.NetContributions = &Money{Currency: "KRW", Amount: text}
	terminal, err := exact.ParseDecimal(valuation.TotalValue.Amount)
	if err != nil {
		return nil, err
	}
	flows = append(flows, XIRRCashFlow{Day: UTCDay(asOf), Amount: terminal})
	solution, err := SolveXIRR(ctx, flows)
	if err != nil {
		return nil, err
	}
	if solution.Reason != "cash_flow_limit" {
		changes := solution.SignChanges
		result.SignChanges = &changes
	}
	result.AnnualRate = solution.Rate
	result.CandidateRate = solution.Candidate
	if solution.Rate != nil {
		result.Status = "calculated"
		result.Uniqueness = "unique"
	}
	if solution.Candidate != nil {
		result.Status = "candidate"
		result.Uniqueness = "not_proven"
	}
	if solution.Reason != "" {
		result.Issues = append(result.Issues, APIError{Code: solution.Reason, Message: "the numerical result is limited by the declared XIRR policy"})
	}
	return finish()
}

func historicalXIRRFX(observations []FXObservation, times map[string][3]time.Time, currency string, occurredAt, knownAt time.Time) *FXObservation {
	var selected *FXObservation
	for i := range observations {
		fx := &observations[i]
		if !supportedValuationFX(*fx) || fx.BaseCurrency != currency || fx.QuoteCurrency != "KRW" {
			continue
		}
		clock := times[fxTimeKey(*fx)]
		if clock[0].After(occurredAt) || clock[1].After(knownAt) || clock[2].After(knownAt) {
			continue
		}
		// Equal economic timestamps retain the first verified append, matching valuation selection.
		if selected == nil || clock[0].After(times[fxTimeKey(*selected)][0]) {
			selected = fx
		}
	}
	return selected
}
