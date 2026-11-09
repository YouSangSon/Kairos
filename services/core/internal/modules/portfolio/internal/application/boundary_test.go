package application

import (
	"context"
	"errors"
	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	c "omni-folio/services/core/internal/modules/portfolio/contracts"
	"testing"
	"time"
)

func TestApplicationBoundaryPreservesValuationReturnsAndSourceMetadata(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	start := "2025-01-01T00:00:00Z"
	end := at.Format(time.RFC3339Nano)
	deposit := ledger.ValuationEvent{Type: "DEPOSIT", OccurredAt: start, Currency: "USD", Amount: "100"}
	all := c.Inputs{Cutoff: at, Ledger: ledger.ValuationFacts{Snapshot: ledger.PortfolioSnapshot{LedgerRevision: "rev_0000000002", AsOf: start, RecordedAt: end, Cash: []ledger.Money{{Currency: "USD", Amount: "80"}}, Holdings: []ledger.Holding{{InstrumentID: "instrument_aapl", Symbol: "AAPL", Currency: "USD", Quantity: "2", CostBasis: "20"}}}, AsOf: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), RecordedAt: at, RestatedEvents: []ledger.ValuationEvent{deposit, {Type: "BUY", OccurredAt: start, InstrumentID: "instrument_aapl", Symbol: "AAPL", Quantity: "2", Currency: "USD", Amount: "-20"}}, ExternalFlows: []ledger.ValuationEvent{deposit}}, Market: market.ValuationInputs{Cutoff: at, BenchmarkVersionCount: 1}}
	for i, observed := range []string{start, end} {
		id := []string{"start", "end"}[i]
		all.Market.FX = append(all.Market.FX, market.FXObservation{ObservationID: id, Source: "owner_declared", BaseCurrency: "USD", QuoteCurrency: "KRW", Rate: "1000", ObservedAt: observed, FetchedAt: end, RecordedAt: end, OwnerVersion: "fx-v1"})
		all.Market.Prices = append(all.Market.Prices, market.SecurityPriceObservation{ObservationID: id, Source: "owner_declared", InstrumentID: "instrument_aapl", Symbol: "AAPL", Venue: "XNAS", Currency: "USD", Price: []string{"10", "30"}[i], PriceAdjustment: "unadjusted", ObservedAt: observed, FetchedAt: end, RecordedAt: end, OwnerVersion: "price-v1"})
	}
	all.Market.Benchmarks = []market.BenchmarkVersion{{Sequence: 7, DataSHA256: "benchmark-sha", RecordedAt: end, Input: market.BenchmarkSeriesInput{BenchmarkID: "index", Label: "Index label", Currency: "KRW", ReturnBasis: "price", SourceReference: "owner-reference", SourceVersion: "benchmark-v1", Sample: true, FetchedAt: end, Points: []market.BenchmarkIndexPoint{{ObservedAt: start, Level: "100"}, {ObservedAt: end, Level: "110"}}}}}
	q := &Queries{Now: func() time.Time { return at }, Inputs: inputReader(func(context.Context, c.InputQuery) (c.Inputs, error) { return all, nil })}
	current, err := q.CurrentValuation(ctx)
	if err != nil || current.TotalValue == nil || current.TotalValue.Amount != "140000" || current.CashValue.Amount != "80000" || current.HoldingsValue.Amount != "60000" || current.Sample || current.LedgerRevision != "rev_0000000002" {
		t.Fatalf("valuation boundary changed: %+v %v", current, err)
	}
	holding, err := q.HoldingValuation(ctx, end)
	if err != nil || len(holding.Lines) != 1 || holding.Lines[0].Price == nil || holding.Lines[0].Price.SourceVersion != "price-v1" || holding.Lines[0].Price.State != "unknown" || *holding.Lines[0].MarketValue != "60" || *holding.Lines[0].UnrealizedPnL != "40" {
		t.Fatalf("holding boundary changed: %+v %v", holding, err)
	}
	cash, err := q.CashValuation(ctx, "KRW", end)
	if err != nil || cash.Total == nil || cash.Total.Amount != "80000" || cash.Lines[0].FX.SourceVersion != "fx-v1" || cash.Lines[0].FX.Sample {
		t.Fatalf("cash boundary changed: %+v %v", cash, err)
	}
	twr, err := q.TWR(ctx)
	if err != nil || twr.CumulativeRate == nil || *twr.CumulativeRate != "0.4" || twr.NetContributions.Amount != "100000" || *twr.ExternalEventCount != 1 || *twr.BoundaryCount != 1 {
		t.Fatalf("TWR boundary changed: %+v %v", twr, err)
	}
	checkpoints, err := q.TWRCheckpoints(ctx)
	if err != nil || len(checkpoints.Points) != 2 || checkpoints.Points[0].At != start || checkpoints.Points[0].CumulativeRate != "0" || checkpoints.Points[1].CumulativeRate != "0.4" {
		t.Fatalf("checkpoint boundary changed: %+v %v", checkpoints, err)
	}
	xirr, err := q.XIRR(ctx)
	if err != nil || xirr.AnnualRate == nil || *xirr.AnnualRate != "0.4" || xirr.CandidateRate != nil || xirr.NetContributions.Amount != "100000" {
		t.Fatalf("XIRR boundary changed: %+v %v", xirr, err)
	}
	benchmarks, err := q.Benchmarks(ctx)
	if err != nil || len(benchmarks.Benchmarks) != 1 {
		t.Fatalf("benchmark boundary changed: %+v %v", benchmarks, err)
	}
	b := benchmarks.Benchmarks[0]
	if b.Series.Label != "Index label" || b.Series.DataSHA256 != "benchmark-sha" || b.Series.SourceVersion != "benchmark-v1" || !b.Sample || *b.Native.Rate != "0.1" || *b.KRW.Rate != "0.1" || *b.DifferencePercentagePoints != "30" || b.Start.Level != "100" || b.End.Level != "110" {
		t.Fatalf("benchmark metadata changed: %+v", b)
	}
	all.Ledger.LastSplits = []ledger.SplitFact{{InstrumentID: "instrument_aapl", Currency: "USD", OccurredAt: at}}
	holding, err = q.HoldingValuation(ctx, end)
	if err != nil || holding.Status != "unavailable" || holding.Totals != nil || holding.Lines[0].Issue == nil || holding.Lines[0].Issue.Code != "split_price_unavailable" || holding.Lines[0].MarketValue != nil {
		t.Fatalf("split fact or nulls changed: %+v %v", holding, err)
	}
	all.Ledger.FutureRecorded = true
	_, err = q.HoldingValuation(ctx, end)
	var failure *c.Failure
	if !errors.As(err, &failure) || !failure.Conflict || failure.Code != "valuation_as_of_precedes_ledger_revision" {
		t.Fatalf("domain failure lost public semantics: %v", err)
	}
	all = c.Inputs{Cutoff: at, Market: market.ValuationInputs{Cutoff: at}}
	empty, err := q.CurrentValuation(ctx)
	if err != nil || empty.Status != "empty" || empty.Issues == nil || empty.TotalValue != nil {
		t.Fatalf("empty boundary changed: %+v %v", empty, err)
	}
	emptyHolding, err := q.HoldingValuation(ctx, end)
	if err != nil || emptyHolding.Totals != nil || emptyHolding.Lines == nil || len(emptyHolding.Lines) != 0 || emptyHolding.Issues == nil {
		t.Fatalf("nil and empty arrays changed: %+v %v", emptyHolding, err)
	}
}
