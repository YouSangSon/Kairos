package application

import (
	"errors"
	c "omni-folio/services/core/internal/modules/portfolio/contracts"
	"omni-folio/services/core/internal/modules/portfolio/internal/domain"
)

func domainError(err error) error {
	var failure *domain.Failure
	if errors.As(err, &failure) {
		return &c.Failure{Code: failure.Code, Message: failure.Message, Field: failure.Field, Conflict: failure.Conflict}
	}
	return err
}
func valuationInputs(v c.Inputs) domain.Inputs {
	result := domain.Inputs{Cutoff: v.Cutoff, Ledger: domain.LedgerFacts{AsOf: v.Ledger.AsOf, RecordedAt: v.Ledger.RecordedAt, FutureRecorded: v.Ledger.FutureRecorded, Snapshot: domain.PortfolioSnapshot{LedgerRevision: v.Ledger.Snapshot.LedgerRevision, AsOf: v.Ledger.Snapshot.AsOf, RecordedAt: v.Ledger.Snapshot.RecordedAt}}, Market: domain.MarketInputs{Cutoff: v.Market.Cutoff, BenchmarkVersionCount: v.Market.BenchmarkVersionCount}}
	if v.Ledger.Snapshot.Cash != nil {
		result.Ledger.Snapshot.Cash = make([]domain.Money, len(v.Ledger.Snapshot.Cash))
		for i, value := range v.Ledger.Snapshot.Cash {
			result.Ledger.Snapshot.Cash[i] = domain.Money(value)
		}
	}
	if v.Ledger.Snapshot.Holdings != nil {
		result.Ledger.Snapshot.Holdings = make([]domain.Holding, len(v.Ledger.Snapshot.Holdings))
		for i, value := range v.Ledger.Snapshot.Holdings {
			result.Ledger.Snapshot.Holdings[i] = domain.Holding(value)
		}
	}
	if v.Ledger.LastSplits != nil {
		result.Ledger.LastSplits = make([]domain.SplitFact, len(v.Ledger.LastSplits))
		for i, value := range v.Ledger.LastSplits {
			result.Ledger.LastSplits[i] = domain.SplitFact(value)
		}
	}
	if v.Ledger.RestatedEvents != nil {
		result.Ledger.RestatedEvents = make([]domain.Transaction, len(v.Ledger.RestatedEvents))
		for i, value := range v.Ledger.RestatedEvents {
			result.Ledger.RestatedEvents[i] = domain.Transaction(value)
		}
	}
	if v.Ledger.ExternalFlows != nil {
		result.Ledger.ExternalFlows = make([]domain.Transaction, len(v.Ledger.ExternalFlows))
		for i, value := range v.Ledger.ExternalFlows {
			result.Ledger.ExternalFlows[i] = domain.Transaction(value)
		}
	}
	if v.Market.FX != nil {
		result.Market.FX = make([]domain.FXObservation, len(v.Market.FX))
		for i, value := range v.Market.FX {
			result.Market.FX[i] = domain.FXObservation(value)
		}
	}
	if v.Market.Prices != nil {
		result.Market.Prices = make([]domain.SecurityPriceObservation, len(v.Market.Prices))
		for i, value := range v.Market.Prices {
			result.Market.Prices[i] = domain.SecurityPriceObservation(value)
		}
	}
	if v.Market.Benchmarks != nil {
		result.Market.Benchmarks = make([]domain.BenchmarkVersion, len(v.Market.Benchmarks))
		for i, value := range v.Market.Benchmarks {
			input := value.Input
			canonical := domain.BenchmarkSeriesInput{SchemaVersion: input.SchemaVersion, BenchmarkID: input.BenchmarkID, Label: input.Label, Currency: input.Currency, ReturnBasis: input.ReturnBasis, SourceReference: input.SourceReference, SourceVersion: input.SourceVersion, Sample: input.Sample, FetchedAt: input.FetchedAt}
			if input.Points != nil {
				canonical.Points = make([]domain.BenchmarkIndexPoint, len(input.Points))
				for j, point := range input.Points {
					canonical.Points[j] = domain.BenchmarkIndexPoint(point)
				}
			}
			result.Market.Benchmarks[i] = domain.BenchmarkVersion{Sequence: value.Sequence, Input: canonical, DataSHA256: value.DataSHA256, RecordedAt: value.RecordedAt}
		}
	}
	return result
}
func publicHoldingValuationPrice(v *domain.HoldingValuationPrice) *c.HoldingValuationPrice {
	if v == nil {
		return nil
	}
	result := c.HoldingValuationPrice(*v)
	return &result
}
func publicHoldingValuationLine(v *domain.HoldingValuationLine) *c.HoldingValuationLine {
	if v == nil {
		return nil
	}
	result := c.HoldingValuationLine{InstrumentID: v.InstrumentID, Symbol: v.Symbol, Quantity: v.Quantity, CostBasis: v.CostBasis, Currency: v.Currency, Status: v.Status, Price: publicHoldingValuationPrice(v.Price), MarketValue: v.MarketValue, UnrealizedPnL: v.UnrealizedPnL, Issue: publicAPIError(v.Issue)}
	return &result
}
func publicHoldingValuation(v *domain.HoldingValuation) *c.HoldingValuation {
	if v == nil {
		return nil
	}
	result := c.HoldingValuation{Scope: v.Scope, PolicyVersion: v.PolicyVersion, MaxObservationAgeSeconds: v.MaxObservationAgeSeconds, ValuationAsOf: v.ValuationAsOf, LedgerRevision: v.LedgerRevision, LedgerAsOf: v.LedgerAsOf, LedgerRecordedAt: v.LedgerRecordedAt, Status: v.Status, Sample: v.Sample}
	if v.Totals != nil {
		result.Totals = make([]c.HoldingValuationTotal, len(v.Totals))
		for i, value := range v.Totals {
			result.Totals[i] = c.HoldingValuationTotal(value)
		}
	}
	if v.Lines != nil {
		result.Lines = make([]c.HoldingValuationLine, len(v.Lines))
		for i, value := range v.Lines {
			result.Lines[i] = *publicHoldingValuationLine(&value)
		}
	}
	if v.Issues != nil {
		result.Issues = make([]c.APIError, len(v.Issues))
		for i, value := range v.Issues {
			result.Issues[i] = c.APIError(value)
		}
	}
	return &result
}
func publicCashValuationFX(v *domain.CashValuationFX) *c.CashValuationFX {
	if v == nil {
		return nil
	}
	result := c.CashValuationFX(*v)
	return &result
}
func publicCashValuationLine(v *domain.CashValuationLine) *c.CashValuationLine {
	if v == nil {
		return nil
	}
	result := c.CashValuationLine{Currency: v.Currency, Amount: v.Amount, Status: v.Status, ValuedAmount: v.ValuedAmount, FX: publicCashValuationFX(v.FX), Issue: publicAPIError(v.Issue)}
	return &result
}
func publicCashValuation(v *domain.CashValuation) *c.CashValuation {
	if v == nil {
		return nil
	}
	result := c.CashValuation{Scope: v.Scope, PolicyVersion: v.PolicyVersion, MaxObservationAgeSeconds: v.MaxObservationAgeSeconds, BaseCurrency: v.BaseCurrency, ValuationAsOf: v.ValuationAsOf, LedgerRevision: v.LedgerRevision, LedgerAsOf: v.LedgerAsOf, LedgerRecordedAt: v.LedgerRecordedAt, Status: v.Status, Sample: v.Sample, Total: publicMoney(v.Total)}
	if v.Lines != nil {
		result.Lines = make([]c.CashValuationLine, len(v.Lines))
		for i, value := range v.Lines {
			result.Lines[i] = *publicCashValuationLine(&value)
		}
	}
	if v.Issues != nil {
		result.Issues = make([]c.APIError, len(v.Issues))
		for i, value := range v.Issues {
			result.Issues[i] = c.APIError(value)
		}
	}
	return &result
}
func publicCurrentPortfolioValuation(v *domain.CurrentPortfolioValuation) *c.CurrentPortfolioValuation {
	if v == nil {
		return nil
	}
	result := c.CurrentPortfolioValuation{Scope: v.Scope, PolicyVersion: v.PolicyVersion, MaxObservationAgeSeconds: v.MaxObservationAgeSeconds, BaseCurrency: v.BaseCurrency, ValuationAsOf: v.ValuationAsOf, LedgerRevision: v.LedgerRevision, LedgerAsOf: v.LedgerAsOf, LedgerRecordedAt: v.LedgerRecordedAt, Status: v.Status, Sample: v.Sample, CashValue: publicMoney(v.CashValue), HoldingsValue: publicMoney(v.HoldingsValue), TotalValue: publicMoney(v.TotalValue)}
	if v.Issues != nil {
		result.Issues = make([]c.APIError, len(v.Issues))
		for i, value := range v.Issues {
			result.Issues[i] = c.APIError(value)
		}
	}
	return &result
}
func publicPortfolioTWR(v *domain.PortfolioTWR) *c.PortfolioTWR {
	if v == nil {
		return nil
	}
	result := c.PortfolioTWR{PolicyVersion: v.PolicyVersion, Scope: v.Scope, Valuation: publicCurrentPortfolioValuation(v.Valuation), Status: v.Status, Sample: v.Sample, PeriodStart: v.PeriodStart, PeriodEnd: v.PeriodEnd, ExternalEventCount: v.ExternalEventCount, BoundaryCount: v.BoundaryCount, CumulativeRate: v.CumulativeRate, NetContributions: publicMoney(v.NetContributions)}
	if v.Issues != nil {
		result.Issues = make([]c.APIError, len(v.Issues))
		for i, value := range v.Issues {
			result.Issues[i] = c.APIError(value)
		}
	}
	return &result
}
func publicPortfolioTWRCheckpoints(v *domain.PortfolioTWRCheckpoints) *c.PortfolioTWRCheckpoints {
	if v == nil {
		return nil
	}
	result := c.PortfolioTWRCheckpoints{PolicyVersion: v.PolicyVersion, Report: publicPortfolioTWR(v.Report)}
	if v.Points != nil {
		result.Points = make([]c.PortfolioTWRCheckpoint, len(v.Points))
		for i, value := range v.Points {
			result.Points[i] = c.PortfolioTWRCheckpoint(value)
		}
	}
	return &result
}
func publicPortfolioXIRR(v *domain.PortfolioXIRR) *c.PortfolioXIRR {
	if v == nil {
		return nil
	}
	result := c.PortfolioXIRR{PolicyVersion: v.PolicyVersion, SolverPolicy: v.SolverPolicy, Scope: v.Scope, Valuation: publicCurrentPortfolioValuation(v.Valuation), Status: v.Status, Sample: v.Sample, PeriodStart: v.PeriodStart, PeriodEnd: v.PeriodEnd, ExternalEventCount: v.ExternalEventCount, SignChanges: v.SignChanges, Uniqueness: v.Uniqueness, AnnualRate: v.AnnualRate, CandidateRate: v.CandidateRate, NetContributions: publicMoney(v.NetContributions)}
	if v.Issues != nil {
		result.Issues = make([]c.APIError, len(v.Issues))
		for i, value := range v.Issues {
			result.Issues[i] = c.APIError(value)
		}
	}
	return &result
}
func publicBenchmarkFXMark(v *domain.BenchmarkFXMark) *c.BenchmarkFXMark {
	if v == nil {
		return nil
	}
	result := c.BenchmarkFXMark(*v)
	return &result
}
func publicPortfolioBenchmark(v *domain.PortfolioBenchmark) *c.PortfolioBenchmark {
	if v == nil {
		return nil
	}
	result := c.PortfolioBenchmark{Series: publicBenchmarkSeriesReceipt(v.Series), Sample: v.Sample, Start: publicBenchmarkIndexPoint(v.Start), End: publicBenchmarkIndexPoint(v.End), StartFX: publicBenchmarkFXMark(v.StartFX), EndFX: publicBenchmarkFXMark(v.EndFX), Native: c.BenchmarkRateResult(v.Native), KRW: c.BenchmarkRateResult(v.KRW), DifferencePercentagePoints: v.DifferencePercentagePoints}
	return &result
}
func publicPortfolioBenchmarks(v *domain.PortfolioBenchmarks) *c.PortfolioBenchmarks {
	if v == nil {
		return nil
	}
	result := c.PortfolioBenchmarks{PolicyVersion: v.PolicyVersion, MaxObservationAgeSeconds: v.MaxObservationAgeSeconds, Portfolio: publicPortfolioTWR(v.Portfolio)}
	if v.Benchmarks != nil {
		result.Benchmarks = make([]c.PortfolioBenchmark, len(v.Benchmarks))
		for i, value := range v.Benchmarks {
			result.Benchmarks[i] = *publicPortfolioBenchmark(&value)
		}
	}
	if v.Issues != nil {
		result.Issues = make([]c.APIError, len(v.Issues))
		for i, value := range v.Issues {
			result.Issues[i] = c.APIError(value)
		}
	}
	return &result
}
func publicBenchmarkSeriesReceipt(v *domain.BenchmarkSeriesReceipt) *c.BenchmarkSeriesReceipt {
	if v == nil {
		return nil
	}
	result := c.BenchmarkSeriesReceipt(*v)
	return &result
}
func publicBenchmarkIndexPoint(v *domain.BenchmarkIndexPoint) *c.BenchmarkIndexPoint {
	if v == nil {
		return nil
	}
	result := c.BenchmarkIndexPoint(*v)
	return &result
}
func publicAPIError(v *domain.APIError) *c.APIError {
	if v == nil {
		return nil
	}
	result := c.APIError(*v)
	return &result
}
func publicMoney(v *domain.Money) *c.Money {
	if v == nil {
		return nil
	}
	result := c.Money(*v)
	return &result
}
