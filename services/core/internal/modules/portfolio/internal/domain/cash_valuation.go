package domain

import (
	"context"
	"math/big"
	c "omni-folio/services/core/internal/modules/portfolio/contracts"
	"time"
)

const (
	cashValuationPolicy        = "direct_fx_cash_v1"
	cashValuationOwnerFXPolicy = "direct_fx_cash_v2"
	cashFXMaxAge               = 24 * time.Hour
)

func ComputeCashValuation(ctx context.Context, all Inputs, baseCurrency, asOf string) (*CashValuation, error) {
	valuationTime, _ := time.Parse(time.RFC3339Nano, asOf)
	snapshot := &all.Ledger.Snapshot
	ledgerAsOf, _ := time.Parse(time.RFC3339Nano, snapshot.AsOf)
	if valuationTime.Before(ledgerAsOf) || valuationTime.Before(all.Ledger.RecordedAt) || all.Ledger.FutureRecorded {
		return nil, &c.Failure{Conflict: true, Code: "valuation_as_of_precedes_ledger_revision", Message: "valuation as_of precedes the current ledger state", Field: "as_of"}
	}
	inputs, err := fxInputs(all)
	if err != nil {
		return nil, err
	}
	observations, times := inputs.observations, inputs.times

	result := &CashValuation{
		Scope: "cash_only", PolicyVersion: cashValuationPolicy, MaxObservationAgeSeconds: int64(cashFXMaxAge / time.Second),
		BaseCurrency: baseCurrency, ValuationAsOf: asOf, LedgerRevision: snapshot.LedgerRevision,
		LedgerAsOf: snapshot.AsOf, LedgerRecordedAt: snapshot.RecordedAt, Status: "complete",
		Lines: []CashValuationLine{}, Issues: []APIError{},
	}
	if len(snapshot.Cash) == 0 {
		result.Status = "empty"
		return result, nil
	}

	total := new(big.Rat)
	available, usedSample := true, false
	usedOwnerFX := false
	for _, cash := range snapshot.Cash {
		amount, err := parseDecimal(cash.Amount)
		if err != nil {
			return nil, err
		}
		line := CashValuationLine{Currency: cash.Currency, Amount: cash.Amount}
		switch {
		case amount.Sign() == 0:
			line.Status = "zero"
			line.ValuedAmount = stringPointer("0")
		case cash.Currency == baseCurrency:
			line.Status = "identity"
			line.ValuedAmount = stringPointer(cash.Amount)
			total.Add(total, amount)
		default:
			observation := latestEligibleCashFX(observations, times, cash.Currency, baseCurrency, valuationTime)
			if observation == nil {
				line.Status = "missing"
				line.Issue = &APIError{Code: "missing_direct_fx", Message: "an eligible direct FX observation is unavailable", Field: cash.Currency}
				result.Issues = append(result.Issues, *line.Issue)
				available = false
				break
			}
			line.FX = publicCashValuationFX(observation)
			usedSample = usedSample || fxIsSample(observation)
			usedOwnerFX = usedOwnerFX || observation.Source == ownerFXSource
			if valuationTime.Sub(times[fxTimeKey(*observation)][0]) > cashFXMaxAge {
				line.Status = "stale"
				line.FX.State = "stale"
				line.Issue = &APIError{Code: "stale_direct_fx", Message: "the direct FX observation exceeds the policy age", Field: cash.Currency}
				result.Issues = append(result.Issues, *line.Issue)
				available = false
				break
			}
			rate, err := parseDecimal(observation.Rate)
			if err != nil {
				return nil, err
			}
			valued := new(big.Rat).Mul(amount, rate)
			valuedText, err := formatDecimal(valued)
			if err != nil {
				return nil, err
			}
			line.Status = "valued"
			line.ValuedAmount = stringPointer(valuedText)
			total.Add(total, valued)
		}
		result.Lines = append(result.Lines, line)
	}
	result.Sample = usedSample
	if usedOwnerFX {
		result.PolicyVersion = cashValuationOwnerFXPolicy
		result.Issues = append(result.Issues, ownerFXIssue())
	}
	if !available {
		result.Status = "unavailable"
	} else {
		totalText, err := formatDecimal(total)
		if err != nil {
			return nil, err
		}
		result.Total = &Money{Currency: baseCurrency, Amount: totalText}
		if usedSample {
			result.Status = "stale_sample"
			result.Issues = append(result.Issues, APIError{Code: "sample_data", Message: "cash valuation uses local sample FX data"})
		}
	}
	return result, nil
}

// ponytail: linear selection is enough for a personal FX series; index-backed projection comes after measured volume.
func latestEligibleCashFX(observations []FXObservation, times map[string][3]time.Time, base, quote string, asOf time.Time) *FXObservation {
	var selected *FXObservation
	for index := range observations {
		observation := &observations[index]
		if !supportedValuationFX(*observation) || observation.BaseCurrency != base || observation.QuoteCurrency != quote {
			continue
		}
		observationTimes := times[fxTimeKey(*observation)]
		if observationTimes[0].After(asOf) || observationTimes[1].After(asOf) || observationTimes[2].After(asOf) {
			continue
		}
		if selected == nil || observationTimes[0].After(times[fxTimeKey(*selected)][0]) {
			selected = observation
		}
	}
	return selected
}

func publicCashValuationFX(observation *FXObservation) *CashValuationFX {
	result := &CashValuationFX{
		ObservationID: observation.ObservationID, Source: observation.Source, BaseCurrency: observation.BaseCurrency,
		QuoteCurrency: observation.QuoteCurrency, Rate: observation.Rate, ObservedAt: observation.ObservedAt,
		FetchedAt: observation.FetchedAt, RecordedAt: observation.RecordedAt, Sample: true, State: "stale",
	}
	if observation.Source == ownerFXSource {
		result.SourceVersion = observation.OwnerVersion
		result.Sample = fxIsSample(observation)
		result.State = "unknown"
	}
	return result
}

func stringPointer(value string) *string { return &value }
