package domain

import (
	"testing"
	"time"
)

var watchAAPL = WatchlistEntry{Venue: "XNAS", Symbol: "AAPL", Currency: "USD"}

func mustTime(raw string) time.Time { at, _ := time.Parse(time.RFC3339Nano, raw); return at }
func quoteTimes(observations []SecurityPriceObservation) (map[string][3]time.Time, error) {
	times := map[string][3]time.Time{}
	for _, p := range observations {
		times[priceTimeKey(p)] = [3]time.Time{mustTime(p.ObservedAt), mustTime(p.FetchedAt), mustTime(p.RecordedAt)}
	}
	return times, nil
}
func TestWatchlistQuoteNeverGuessesIdentityTimeOrProvenance(t *testing.T) {
	at := mustTime("2026-01-02T00:00:00Z")
	original := SecurityPriceObservation{ObservationID: "first", Source: "local_fixture", InstrumentID: "instrument_aapl", Symbol: "AAPL", Venue: "XNAS", Currency: "USD", Price: "100", PriceAdjustment: "unspecified", ObservedAt: "2026-01-01T23:00:00Z", FetchedAt: "2026-01-01T23:00:00Z", RecordedAt: "2026-01-01T23:00:00Z"}
	tests := []struct {
		name   string
		edit   func(*SecurityPriceObservation)
		status string
	}{
		{"other venue", func(p *SecurityPriceObservation) { p.Venue = "XNYS" }, "stale"},
		{"other currency", func(p *SecurityPriceObservation) { p.Currency = "KRW" }, "stale"},
		{"other symbol", func(p *SecurityPriceObservation) { p.Symbol = "MSFT" }, "stale"},
		{"other instrument", func(p *SecurityPriceObservation) { p.InstrumentID = "unrelated" }, "ambiguous"},
		{"price conflict", func(p *SecurityPriceObservation) { p.Price = "101" }, "ambiguous"},
		{"time provenance conflict", func(p *SecurityPriceObservation) { p.FetchedAt = "2026-01-01T23:01:00Z"; p.RecordedAt = p.FetchedAt }, "ambiguous"},
		{"no broker promotion", func(p *SecurityPriceObservation) { p.Source = "kiwoom_production"; p.Price = "999" }, "stale"},
		{"future recorded", func(p *SecurityPriceObservation) { p.Price = "999"; p.RecordedAt = "2026-01-02T00:00:00.000000001Z" }, "stale"},
		{"future all", func(p *SecurityPriceObservation) {
			p.Price = "999"
			p.ObservedAt = "2026-01-02T00:00:00.000000001Z"
			p.FetchedAt = p.ObservedAt
			p.RecordedAt = p.ObservedAt
		}, "stale"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			other := original
			other.ObservationID = "second"
			tc.edit(&other)
			observations := []SecurityPriceObservation{original, other}
			times, err := quoteTimes(observations)
			if err != nil {
				t.Fatal(err)
			}
			q := watchlistQuote(watchAAPL, observations, times, at)
			if q.Status != tc.status || (q.Status == "ambiguous" && q.Price != nil) || (q.Price != nil && q.Price.Price != "100") {
				t.Fatalf("selection guessed: %+v", q)
			}
		})
	}
	owner := original
	owner.Source = "owner_declared"
	owner.PriceAdjustment = "unadjusted"
	owner.OwnerVersion = "v1"
	conflicting := owner
	conflicting.ObservationID = "second"
	conflicting.OwnerSample = true
	observations := []SecurityPriceObservation{owner, conflicting}
	times, err := quoteTimes(observations)
	if err != nil {
		t.Fatal(err)
	}
	if q := watchlistQuote(watchAAPL, observations, times, at); q.Status != "ambiguous" || q.Price != nil {
		t.Fatal("sample provenance silently chosen")
	}
}

func TestWatchlistOwnerQuoteComparesVersionValues(t *testing.T) {
	at := mustTime("2026-01-02T00:00:00Z")
	first := SecurityPriceObservation{ObservationID: "first", Source: "owner_declared", InstrumentID: "instrument_aapl", Symbol: "AAPL", Venue: "XNAS", Currency: "USD", Price: "100", PriceAdjustment: "unadjusted", ObservedAt: "2026-01-01T23:00:00Z", FetchedAt: "2026-01-01T23:00:00Z", RecordedAt: "2026-01-01T23:00:00Z", OwnerVersion: "v1"}
	for _, version := range []string{"v1", "v2"} {
		t.Run(version, func(t *testing.T) {
			second := first
			second.ObservationID = "second"
			second.OwnerVersion = version
			observations := []SecurityPriceObservation{first, second}
			times, _ := quoteTimes(observations)
			quote := watchlistQuote(watchAAPL, observations, times, at)
			if version == "v1" {
				if quote.Status != "unknown" || quote.Price == nil || quote.Price.SourceVersion == nil || *quote.Price.SourceVersion != "v1" {
					t.Fatalf("equal version values became ambiguous: %+v", quote)
				}
			} else if quote.Status != "ambiguous" || quote.Price != nil {
				t.Fatalf("conflicting versions became equal: %+v", quote)
			}
		})
	}
}
