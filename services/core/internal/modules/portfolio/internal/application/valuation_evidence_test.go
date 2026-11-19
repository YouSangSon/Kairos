package application

import (
	"context"
	"errors"
	"testing"
	"time"

	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	c "omni-folio/services/core/internal/modules/portfolio/contracts"
)

func evidenceInputs() c.Inputs {
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	raw := at.Format(time.RFC3339Nano)
	return c.Inputs{
		Cutoff: at,
		Ledger: ledger.ValuationFacts{
			AsOf: at, RecordedAt: at,
			Snapshot: ledger.PortfolioSnapshot{
				LedgerRevision: "rev_0000000002", AsOf: raw, RecordedAt: raw,
				Cash: []ledger.Money{{Currency: "KRW", Amount: "100"}, {Currency: "USD", Amount: "0"}},
				Holdings: []ledger.Holding{{
					InstrumentID: "instrument_aapl", Symbol: "AAPL", Currency: "USD",
					Quantity: "2", CostBasis: "20",
				}},
			},
		},
		Market: market.ValuationInputs{
			Cutoff: at,
			FX: []market.FXObservation{{
				ObservationID: "selected-fx", Source: "owner_declared", OwnerVersion: "fx-v1",
				BaseCurrency: "USD", QuoteCurrency: "KRW", Rate: "1000",
				ObservedAt: raw, FetchedAt: raw, RecordedAt: raw,
			}},
			Prices: []market.SecurityPriceObservation{{
				ObservationID: "private-price", Source: "owner_declared", OwnerVersion: "price-v1",
				InstrumentID: "instrument_aapl", Symbol: "AAPL", Venue: "XNAS", Currency: "USD",
				Price: "30", PriceAdjustment: "unadjusted", ObservedAt: raw, FetchedAt: raw, RecordedAt: raw,
			}},
		},
	}
}

func TestValuationEvidenceSingleReadAndNativeValues(t *testing.T) {
	all := evidenceInputs()
	calls := 0
	q := &Queries{Now: func() time.Time { return all.Cutoff }, Inputs: inputReader(func(_ context.Context, query c.InputQuery) (c.Inputs, error) {
		calls++
		if query.At != nil || !query.FX || !query.Prices || query.Benchmarks {
			t.Fatalf("wrong read scope: %+v", query)
		}
		return all, nil
	})}
	report, err := q.ValuationEvidence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || report.SchemaVersion != "portfolio-valuation-evidence.v1" || report.SourceAvailabilityAtCutoff != "unverified" {
		t.Fatalf("wrong envelope: %+v calls=%d", report, calls)
	}
	usd := report.Cash.Lines[1]
	if usd.Currency != "USD" || usd.Amount != "0" || usd.FX != nil {
		t.Fatalf("expected a zero cash line without a conversion: %+v", usd)
	}
	if len(report.ConversionFX) != 1 || report.ConversionFX[0].ObservationID != "selected-fx" || report.ConversionFX[0].SourceVersion != "fx-v1" || report.ConversionFX[0].State != "unknown" {
		t.Fatalf("selected holding FX lost: %+v", report.ConversionFX)
	}

}

func TestValuationEvidenceMissingStaleEmptyAndDeduplicatedFX(t *testing.T) {
	for _, state := range []string{"missing-fx", "missing-price", "stale-fx", "empty", "shared-fx"} {
		t.Run(state, func(t *testing.T) {
			all := evidenceInputs()
			switch state {
			case "missing-fx":
				all.Market.FX = nil
			case "missing-price":
				all.Market.Prices = nil
			case "stale-fx":
				all.Market.FX[0].ObservedAt = all.Cutoff.Add(-24*time.Hour - time.Nanosecond).Format(time.RFC3339Nano)
			case "empty":
				all.Ledger.Snapshot.Cash = nil
				all.Ledger.Snapshot.Holdings = nil
			case "shared-fx":
				all.Ledger.Snapshot.Cash[1].Amount = "10"
			}
			q := &Queries{Now: func() time.Time { return all.Cutoff }, Inputs: inputReader(func(context.Context, c.InputQuery) (c.Inputs, error) { return all, nil })}
			report, err := q.ValuationEvidence(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case "missing-fx", "stale-fx":
				if report.Current.TotalValue != nil || report.Current.Status != "unavailable" || report.Holdings.Lines[0].Currency != "USD" ||
					report.Holdings.Lines[0].MarketValue == nil || *report.Holdings.Lines[0].MarketValue != "60" {
					t.Fatalf("native value lost: %+v", report)
				}
				if state == "stale-fx" && (len(report.ConversionFX) != 1 || report.ConversionFX[0].State != "stale") {
					t.Fatal(report.ConversionFX)
				}
				if state == "missing-fx" && len(report.ConversionFX) != 0 {
					t.Fatal(report.ConversionFX)
				}
			case "missing-price":
				if report.Current.TotalValue != nil || report.Holdings.Lines[0].Price != nil || len(report.ConversionFX) != 0 {
					t.Fatal(report)
				}
			case "empty":
				if report.Current.Status != "empty" || report.Cash.Status != "empty" || report.Holdings.Status != "empty" || report.ConversionFX == nil {
					t.Fatal(report)
				}
			case "shared-fx":
				if len(report.ConversionFX) != 1 || report.Current.TotalValue == nil || report.Current.TotalValue.Amount != "70100" {
					t.Fatal(report)
				}
			}
		})
	}
}

func TestValuationEvidenceFailureHasNoPartialReport(t *testing.T) {
	stop := errors.New("reader failure")
	for _, state := range []string{"reader", "mixed-cutoff", "future-ledger", "server-cutoff", "invalid-price"} {
		t.Run(state, func(t *testing.T) {
			all := evidenceInputs()
			switch state {
			case "mixed-cutoff":
				all.Market.Cutoff = all.Cutoff.Add(time.Second)
			case "future-ledger":
				all.Ledger.FutureRecorded = true
			case "server-cutoff":
				all.Ledger.AsOf = all.Cutoff.Add(time.Second)
			case "invalid-price":
				all.Market.Prices[0].FetchedAt = "invalid"
			}
			q := &Queries{Now: func() time.Time { return all.Cutoff }, Inputs: inputReader(func(context.Context, c.InputQuery) (c.Inputs, error) {
				if state == "reader" {
					return c.Inputs{}, stop
				}
				return all, nil
			})}
			report, err := q.ValuationEvidence(context.Background())
			var failure *c.Failure
			if report != nil || err == nil || errors.As(err, &failure) {
				t.Fatalf("partial report or public query error: %+v %v", report, err)
			}
			if state == "reader" && !errors.Is(err, stop) {
				t.Fatal(err)
			}
		})
	}
}
