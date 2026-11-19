package domain

import (
	"context"
	"errors"
	"math/big"
	"time"
)

const (
	currentPortfolioValuationPolicy           = "current_base_portfolio_valuation_v1"
	currentPortfolioValuationOwnerFXPolicy    = "current_base_portfolio_valuation_v2"
	currentPortfolioValuationOwnerPricePolicy = "current_base_portfolio_valuation_v3"
	currentPortfolioValuationBaseCurrency     = "KRW"
)

func CurrentValuation(ctx context.Context, all Inputs) (*CurrentPortfolioValuation, error) {
	valuationTime := all.Cutoff
	valuationAsOf := valuationTime.Format(time.RFC3339Nano)
	snapshot := &all.Ledger.Snapshot
	ledgerAsOfRaw := all.Ledger.AsOf.Format(time.RFC3339Nano)
	if valuationTime.Before(all.Ledger.AsOf) || valuationTime.Before(all.Ledger.RecordedAt) || all.Ledger.FutureRecorded {
		return nil, errors.New("server valuation cutoff precedes ledger state")
	}
	inputs, err := fxInputs(all)
	if err != nil {
		return nil, err
	}
	prices, err := priceInputs(all)
	if err != nil {
		return nil, err
	}
	fxObservations, fxTimes := inputs.observations, inputs.times
	priceObservations, priceTimes := prices.observations, prices.times

	result := &CurrentPortfolioValuation{
		Scope: "whole_portfolio", PolicyVersion: currentPortfolioValuationPolicy,
		MaxObservationAgeSeconds: int64(cashFXMaxAge / time.Second),
		BaseCurrency:             currentPortfolioValuationBaseCurrency, ValuationAsOf: valuationAsOf,
		LedgerRevision: snapshot.LedgerRevision, LedgerAsOf: ledgerAsOfRaw, LedgerRecordedAt: snapshot.RecordedAt,
		Status: "complete", Issues: []APIError{},
	}
	if len(snapshot.Cash) == 0 && len(snapshot.Holdings) == 0 {
		result.Status = "empty"
		return result, nil
	}

	cashTotal := new(big.Rat)
	holdingsTotal := new(big.Rat)
	available := true
	usedSample := false
	usedOwnerFX := false
	usedOwnerPrice := false
	seenFX := make(map[string]bool)
	conversionRate := func(currency string) (*big.Rat, error) {
		fx := latestEligibleCashFX(fxObservations, fxTimes, currency, currentPortfolioValuationBaseCurrency, valuationTime)
		if fx == nil {
			result.Issues = appendCurrentValuationIssue(result.Issues, APIError{
				Code: "missing_direct_fx", Message: "an eligible direct FX observation is unavailable", Field: currency,
			})
			return nil, nil
		}
		usedSample = usedSample || fxIsSample(fx)
		usedOwnerFX = usedOwnerFX || fx.Source == ownerFXSource
		key := fxTimeKey(*fx)
		stale := valuationTime.Sub(fxTimes[key][0]) > cashFXMaxAge
		if !seenFX[key] {
			receipt := publicCashValuationFX(fx)
			if stale {
				receipt.State = "stale"
			}
			result.ConversionFX = append(result.ConversionFX, *receipt)
			seenFX[key] = true
		}
		if stale {
			result.Issues = appendCurrentValuationIssue(result.Issues, APIError{
				Code: "stale_direct_fx", Message: "the direct FX observation exceeds the policy age", Field: currency,
			})
			return nil, nil
		}
		rate, err := parseDecimal(fx.Rate)
		if err != nil {
			return nil, errors.New("FX rate is invalid")
		}
		return rate, nil
	}
	for _, cash := range snapshot.Cash {
		amount, err := parseDecimal(cash.Amount)
		if err != nil {
			return nil, errors.New("cash amount is invalid")
		}
		if amount.Sign() == 0 || cash.Currency == currentPortfolioValuationBaseCurrency {
			cashTotal.Add(cashTotal, amount)
			continue
		}
		rate, err := conversionRate(cash.Currency)
		if err != nil {
			return nil, err
		}
		if rate == nil {
			available = false
			continue
		}
		cashTotal.Add(cashTotal, new(big.Rat).Mul(amount, rate))
	}

	for _, holding := range snapshot.Holdings {
		observation, selection := latestEligibleHoldingPrice(priceObservations, priceTimes, holding, valuationTime)
		if observation == nil {
			result.Issues = appendCurrentValuationIssue(result.Issues, *holdingPriceIssue(selection, holding.Symbol))
			available = false
			continue
		}
		usedSample = usedSample || priceIsSample(observation)
		usedOwnerPrice = usedOwnerPrice || observation.Source == ownerPriceSource
		if !prices.afterLastSplit(observation) {
			result.Issues = appendCurrentValuationIssue(result.Issues, *holdingPriceIssue("split_basis", holding.Symbol))
			available = false
			continue
		}
		if valuationTime.Sub(priceTimes[priceTimeKey(*observation)][0]) > holdingPriceMaxAge {
			result.Issues = appendCurrentValuationIssue(result.Issues, *holdingPriceIssue("stale", holding.Symbol))
			available = false
			continue
		}
		quantity, quantityErr := parseDecimal(holding.Quantity)
		price, priceErr := parseDecimal(observation.Price)
		if quantityErr != nil || priceErr != nil {
			return nil, errors.New("holding valuation decimal is invalid")
		}
		marketValue := new(big.Rat).Mul(quantity, price)
		if holding.Currency != currentPortfolioValuationBaseCurrency {
			rate, err := conversionRate(holding.Currency)
			if err != nil {
				return nil, err
			}
			if rate == nil {
				available = false
				continue
			}
			marketValue.Mul(marketValue, rate)
		}
		holdingsTotal.Add(holdingsTotal, marketValue)
	}

	result.Sample = usedSample
	if usedOwnerFX {
		result.PolicyVersion = currentPortfolioValuationOwnerFXPolicy
		result.Issues = append(result.Issues, ownerFXIssue())
	}
	if usedOwnerPrice {
		result.PolicyVersion = currentPortfolioValuationOwnerPricePolicy
		result.Issues = append(result.Issues, ownerPriceIssue())
	}
	if !available {
		result.Status = "unavailable"
	} else {
		cashText, cashErr := formatDecimal(cashTotal)
		holdingsText, holdingsErr := formatDecimal(holdingsTotal)
		totalText, totalErr := formatDecimal(new(big.Rat).Add(cashTotal, holdingsTotal))
		if cashErr != nil || holdingsErr != nil || totalErr != nil {
			return nil, errors.New("current portfolio valuation is not an exact finite decimal")
		}
		result.CashValue = &Money{Currency: currentPortfolioValuationBaseCurrency, Amount: cashText}
		result.HoldingsValue = &Money{Currency: currentPortfolioValuationBaseCurrency, Amount: holdingsText}
		result.TotalValue = &Money{Currency: currentPortfolioValuationBaseCurrency, Amount: totalText}
		if usedSample {
			result.Status = "stale_sample"
			result.Issues = append(result.Issues, APIError{
				Code: "sample_data", Message: "portfolio valuation uses stored local sample prices or FX data",
			})
		}
	}
	return result, nil
}

func currentFXObservationTimes(observations []FXObservation) (map[string][3]time.Time, error) {
	times := make(map[string][3]time.Time, len(observations))
	for _, observation := range observations {
		parsed, err := currentObservationTimes(observation.ObservedAt, observation.FetchedAt, observation.RecordedAt)
		if err != nil {
			return nil, errors.New("FX observation timestamps are inconsistent")
		}
		times[fxTimeKey(observation)] = parsed
	}
	return times, nil
}

func currentPriceObservationTimes(observations []SecurityPriceObservation) (map[string][3]time.Time, error) {
	times := make(map[string][3]time.Time, len(observations))
	for _, observation := range observations {
		parsed, err := currentObservationTimes(observation.ObservedAt, observation.FetchedAt, observation.RecordedAt)
		if err != nil {
			return nil, errors.New("security price observation timestamps are inconsistent")
		}
		times[priceTimeKey(observation)] = parsed
	}
	return times, nil
}

func currentObservationTimes(observedRaw, fetchedRaw, recordedRaw string) ([3]time.Time, error) {
	observed, observedErr := time.Parse(time.RFC3339Nano, observedRaw)
	fetched, fetchedErr := time.Parse(time.RFC3339Nano, fetchedRaw)
	recorded, recordedErr := time.Parse(time.RFC3339Nano, recordedRaw)
	if observedErr != nil || fetchedErr != nil || recordedErr != nil || fetched.Before(observed) || recorded.Before(fetched) {
		return [3]time.Time{}, errors.New("observation timestamps are inconsistent")
	}
	return [3]time.Time{observed, fetched, recorded}, nil
}

func appendCurrentValuationIssue(issues []APIError, candidate APIError) []APIError {
	for _, issue := range issues {
		if issue.Code == candidate.Code && issue.Field == candidate.Field {
			return issues
		}
	}
	return append(issues, candidate)
}
