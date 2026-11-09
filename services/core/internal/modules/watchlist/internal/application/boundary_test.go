package application

import (
	"context"
	"errors"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	c "omni-folio/services/core/internal/modules/watchlist/contracts"
	"testing"
	"time"
)

type inputsReader func(context.Context) (c.Inputs, error)

func (f inputsReader) ReadWatchlistInputs(ctx context.Context) (c.Inputs, error) { return f(ctx) }
func TestWatchlistBoundaryPreservesQuotesCASAndRetryClock(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	raw := at.Format(time.RFC3339Nano)
	entry := c.WatchlistEntry{Venue: "XNAS", Symbol: "AAPL", Currency: "USD"}
	state := c.WatchlistState{Revision: 1, UpdatedAt: &raw, Entries: []c.WatchlistEntry{entry}}
	s := Service{Inputs: inputsReader(func(context.Context) (c.Inputs, error) {
		return c.Inputs{Cutoff: at, State: state, Prices: []market.SecurityPriceObservation{{ObservationID: "price", Source: "owner_declared", InstrumentID: "instrument_aapl", Symbol: "AAPL", Venue: "XNAS", Currency: "USD", Price: "30", PriceAdjustment: "unadjusted", ObservedAt: raw, FetchedAt: raw, RecordedAt: raw, OwnerVersion: "price-v1", OwnerSample: true}}}, nil
	})}
	report, err := s.Read(context.Background())
	if err != nil || report.Watchlist.Revision != 1 || len(report.Quotes) != 1 || report.Quotes[0].Status != "unknown" || report.Quotes[0].Price == nil || !report.Quotes[0].Price.Sample || report.Quotes[0].Price.SourceVersion == nil || *report.Quotes[0].Price.SourceVersion != "price-v1" {
		t.Fatalf("quote boundary changed: %+v %v", report, err)
	}
	retry, changed, err := Transition(&state, c.WatchlistChange{ExpectedRevision: 0, Entries: []c.WatchlistEntry{entry}}, func() time.Time { t.Fatal("retry called clock"); return at })
	if err != nil || changed || retry.Revision != 1 || retry.UpdatedAt == nil || *retry.UpdatedAt != raw {
		t.Fatalf("retry changed state: %+v %v", retry, err)
	}
	if _, _, err := Transition(&state, c.WatchlistChange{ExpectedRevision: 0, Entries: []c.WatchlistEntry{}}, func() time.Time { return at }); !errors.Is(err, c.ErrChanged) {
		t.Fatalf("CAS error boundary changed: %v", err)
	}
	if _, err := CanonicalEntries(nil); !errors.Is(err, c.ErrInput) {
		t.Fatalf("invalid input error boundary changed: %v", err)
	}
	next, changed, err := Transition(&state, c.WatchlistChange{ExpectedRevision: 1, Entries: []c.WatchlistEntry{}}, func() time.Time { return at })
	if err != nil || !changed || next.Revision != 2 || next.Entries == nil || len(next.Entries) != 0 {
		t.Fatalf("empty state boundary changed: %+v %v", next, err)
	}
}
