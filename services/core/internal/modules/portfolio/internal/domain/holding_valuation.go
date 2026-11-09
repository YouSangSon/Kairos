package domain

import (
	"context"
	"errors"
	"math/big"
	"time"
)

const (
	holdingValuationPolicy           = "native_holding_valuation_v1"
	holdingValuationOwnerPricePolicy = "native_holding_valuation_v2"
	holdingPriceMaxAge               = 24 * time.Hour
)

func ComputeHoldingValuation(ctx context.Context, all Inputs, asOf string) (*HoldingValuation, error) {
	valuationTime, _ := time.Parse(time.RFC3339Nano, asOf)
	snapshot := &all.Ledger.Snapshot
	ledgerAsOf, _ := time.Parse(time.RFC3339Nano, snapshot.AsOf)
	if valuationTime.Before(ledgerAsOf) || valuationTime.Before(all.Ledger.RecordedAt) || all.Ledger.FutureRecorded {
		return nil, &Failure{Conflict: true, Code: "valuation_as_of_precedes_ledger_revision", Message: "valuation as_of precedes the current ledger state", Field: "as_of"}
	}
	inputs, err := priceInputs(all)
	if err != nil {
		return nil, err
	}
	observations, times := inputs.observations, inputs.times

	result := &HoldingValuation{
		Scope: "holdings_only", PolicyVersion: holdingValuationPolicy,
		MaxObservationAgeSeconds: int64(holdingPriceMaxAge / time.Second), ValuationAsOf: asOf,
		LedgerRevision: snapshot.LedgerRevision, LedgerAsOf: snapshot.AsOf, LedgerRecordedAt: snapshot.RecordedAt,
		Status: "complete", Lines: []HoldingValuationLine{}, Issues: []APIError{},
	}
	if len(snapshot.Holdings) == 0 {
		result.Status = "empty"
		return result, nil
	}

	costTotals := map[string]*big.Rat{}
	marketTotals := map[string]*big.Rat{}
	pnlTotals := map[string]*big.Rat{}
	available := true
	usedOwnerPrice := false
	for _, holding := range snapshot.Holdings {
		line := HoldingValuationLine{
			InstrumentID: holding.InstrumentID, Symbol: holding.Symbol, Quantity: holding.Quantity,
			CostBasis: holding.CostBasis, Currency: holding.Currency,
		}
		observation, selection := latestEligibleHoldingPrice(observations, times, holding, valuationTime)
		if observation == nil {
			line.Status = selection
			line.Issue = holdingPriceIssue(selection, holding.Symbol)
			result.Issues = append(result.Issues, *line.Issue)
			result.Lines = append(result.Lines, line)
			available = false
			continue
		}
		line.Price = publicHoldingValuationPrice(observation)
		if valuationTime.Sub(times[priceTimeKey(*observation)][0]) > holdingPriceMaxAge {
			line.Price.State = "stale"
		}
		result.Sample = result.Sample || priceIsSample(observation)
		usedOwnerPrice = usedOwnerPrice || observation.Source == ownerPriceSource
		if !inputs.afterLastSplit(observation) {
			line.Status = "split_basis"
			line.Issue = holdingPriceIssue("split_basis", holding.Symbol)
			result.Issues = append(result.Issues, *line.Issue)
			result.Lines = append(result.Lines, line)
			available = false
			continue
		}
		if valuationTime.Sub(times[priceTimeKey(*observation)][0]) > holdingPriceMaxAge {
			line.Status = "stale"
			line.Price.State = "stale"
			line.Issue = holdingPriceIssue("stale", holding.Symbol)
			result.Issues = append(result.Issues, *line.Issue)
			result.Lines = append(result.Lines, line)
			available = false
			continue
		}

		quantity, quantityErr := parseDecimal(holding.Quantity)
		cost, costErr := parseDecimal(holding.CostBasis)
		price, priceErr := parseDecimal(observation.Price)
		if quantityErr != nil || costErr != nil || priceErr != nil {
			return nil, errors.New("holding valuation decimal is invalid")
		}
		market := new(big.Rat).Mul(quantity, price)
		pnl := new(big.Rat).Sub(new(big.Rat).Set(market), cost)
		marketText, marketErr := formatDecimal(market)
		pnlText, pnlErr := formatDecimal(pnl)
		if marketErr != nil || pnlErr != nil {
			return nil, errors.New("holding valuation is not an exact finite decimal")
		}
		line.Status = "valued"
		line.MarketValue = stringPointer(marketText)
		line.UnrealizedPnL = stringPointer(pnlText)
		addRat(costTotals, holding.Currency, cost)
		addRat(marketTotals, holding.Currency, market)
		addRat(pnlTotals, holding.Currency, pnl)
		result.Lines = append(result.Lines, line)
	}

	if usedOwnerPrice {
		result.PolicyVersion = holdingValuationOwnerPricePolicy
		result.Issues = append(result.Issues, ownerPriceIssue())
	}
	if !available {
		result.Status = "unavailable"
	} else {
		for _, currency := range sortedKeys(marketTotals) {
			cost, costErr := formatDecimal(costTotals[currency])
			market, marketErr := formatDecimal(marketTotals[currency])
			pnl, pnlErr := formatDecimal(pnlTotals[currency])
			if costErr != nil || marketErr != nil || pnlErr != nil {
				return nil, errors.New("holding valuation total is not an exact finite decimal")
			}
			result.Totals = append(result.Totals, HoldingValuationTotal{currency, cost, market, pnl})
		}
		if result.Sample {
			result.Status = "stale_sample"
			result.Issues = append(result.Issues, APIError{Code: "sample_data", Message: "holding valuation uses local sample security prices"})
		}
	}
	return result, nil
}

// ponytail: linear selection is enough for a personal price series; add a projection only after measured volume makes it hot.
func latestEligibleHoldingPrice(observations []SecurityPriceObservation, times map[string][3]time.Time, holding Holding, asOf time.Time) (*SecurityPriceObservation, string) {
	return latestEligibleHoldingPriceAt(observations, times, holding, asOf, asOf)
}

func latestEligibleHoldingPriceAt(observations []SecurityPriceObservation, times map[string][3]time.Time, holding Holding, economicAt, knownAt time.Time) (*SecurityPriceObservation, string) {
	venues := map[string]struct{}{}
	var selected *SecurityPriceObservation
	for index := range observations {
		observation := &observations[index]
		owner := observation.Source == ownerPriceSource && observation.PriceAdjustment == ownerPriceAdjustment
		fixture := observation.Source == "local_fixture" && observation.PriceAdjustment == marketDataAdjustmentUnspecified
		if (!owner && !fixture) || observation.InstrumentID != holding.InstrumentID || observation.Currency != holding.Currency {
			continue
		}
		if owner {
			if instrumentIDForSymbol(holding.Symbol) != observation.InstrumentID {
				continue
			}
		} else if observation.Symbol != holding.Symbol {
			continue
		}
		observationTimes := times[priceTimeKey(*observation)]
		if observationTimes[0].After(economicAt) || observationTimes[1].After(knownAt) || observationTimes[2].After(knownAt) {
			continue
		}
		venues[observation.Venue] = struct{}{}
		if selected == nil || observationTimes[0].After(times[priceTimeKey(*selected)][0]) {
			selected = observation
		}
	}
	if selected == nil {
		return nil, "missing"
	}
	if len(venues) != 1 {
		return nil, "ambiguous"
	}
	return selected, "valued"
}

func holdingPriceIssue(status, symbol string) *APIError {
	switch status {
	case "ambiguous":
		return &APIError{Code: "ambiguous_security_price_venue", Message: "security price venue identity is ambiguous", Field: symbol}
	case "split_basis":
		return &APIError{Code: "split_price_unavailable", Message: "an owner price strictly after the latest stock split is required", Field: symbol}
	case "stale":
		return &APIError{Code: "stale_security_price", Message: "the security price observation exceeds the policy age", Field: symbol}
	default:
		return &APIError{Code: "missing_security_price", Message: "an eligible security price observation is unavailable", Field: symbol}
	}
}

func publicHoldingValuationPrice(observation *SecurityPriceObservation) *HoldingValuationPrice {
	p := &HoldingValuationPrice{
		ObservationID: observation.ObservationID, Source: observation.Source, Venue: observation.Venue, Currency: observation.Currency, Price: observation.Price, PriceAdjustment: observation.PriceAdjustment, ObservedAt: observation.ObservedAt, FetchedAt: observation.FetchedAt, RecordedAt: observation.RecordedAt, Sample: priceIsSample(observation), State: "stale",
	}
	if observation.Source == ownerPriceSource {
		p.SourceVersion = observation.OwnerVersion
		p.State = "unknown"
	}
	return p
}
