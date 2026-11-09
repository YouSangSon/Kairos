package domain

import (
	"errors"
	"reflect"
	"regexp"
	"sort"
	"time"
)

const watchlistPolicy = "watchlist_stored_prices_24h.v1"
const watchlistRevisionLimit = RevisionLimit
const ownerPriceSource = "owner_declared"
const ownerPriceAdjustment = "unadjusted"
const marketDataAdjustmentUnspecified = "unspecified"

var watchlistUSSymbol = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{0,31}$`)
var watchlistKRSymbol = regexp.MustCompile(`^[0-9]{6}$`)
var errWatchlistInput = ErrInput

func canonicalUTCString(raw string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	return err == nil && parsed.UTC().Format(time.RFC3339Nano) == raw
}

func stringPointer(v string) *string { return &v }
func priceTimeKey(p SecurityPriceObservation) string {
	if p.Source == ownerPriceSource {
		return "\x00" + p.ObservationID
	}
	return p.ObservationID
}
func priceIsSample(p *SecurityPriceObservation) bool {
	return p != nil && (p.Source == "local_fixture" || p.OwnerSample)
}
func watchlistKey(e WatchlistEntry) string {
	return e.Venue + "\x00" + e.Symbol + "\x00" + e.Currency
}

func CanonicalEntries(entries []WatchlistEntry) ([]WatchlistEntry, error) {
	if entries == nil || len(entries) > 50 {
		return nil, errWatchlistInput
	}
	result := append([]WatchlistEntry{}, entries...)
	for _, e := range result {
		kr := e.Venue == "XKRX" && e.Currency == "KRW" && watchlistKRSymbol.MatchString(e.Symbol)
		us := (e.Venue == "XNAS" || e.Venue == "XNYS") && e.Currency == "USD" && watchlistUSSymbol.MatchString(e.Symbol)
		if !kr && !us {
			return nil, errWatchlistInput
		}
	}
	sort.Slice(result, func(i, j int) bool { return watchlistKey(result[i]) < watchlistKey(result[j]) })
	for i := 1; i < len(result); i++ {
		if result[i-1] == result[i] {
			return nil, errWatchlistInput
		}
	}
	return result, nil
}

func Transition(state *WatchlistState, change WatchlistChange, now func() time.Time) (*WatchlistState, bool, error) {
	entries, err := CanonicalEntries(change.Entries)
	if err != nil || change.ExpectedRevision < 0 || change.ExpectedRevision > watchlistRevisionLimit {
		return nil, false, ErrInput
	}
	same := reflect.DeepEqual(state.Entries, entries)
	if state.Revision != change.ExpectedRevision {
		if state.Revision == change.ExpectedRevision+1 && same {
			return state, false, nil
		}
		return nil, false, ErrChanged
	}
	if same {
		return state, false, nil
	}
	if state.Revision == watchlistRevisionLimit {
		return nil, false, ErrInput
	}
	at := now().UTC()
	if !canonicalUTCString(at.Format(time.RFC3339Nano)) || (state.UpdatedAt != nil && at.Before(mustParseWatchlistTime(*state.UpdatedAt))) {
		return nil, false, errors.New("watchlist clock precedes saved state")
	}
	next := WatchlistState{Revision: state.Revision + 1, UpdatedAt: stringPointer(at.Format(time.RFC3339Nano)), Entries: entries}
	return &next, true, nil
}
func mustParseWatchlistTime(value string) time.Time {
	at, _ := time.Parse(time.RFC3339Nano, value) // Validated at the input/storage boundary.
	return at
}

func watchlistQuote(entry WatchlistEntry, observations []SecurityPriceObservation, times map[string][3]time.Time, at time.Time) WatchlistQuote {
	quote := WatchlistQuote{WatchlistEntry: entry, Status: "missing"}
	identities := map[string]bool{}
	var selected *SecurityPriceObservation
	ambiguous := false
	for i := range observations {
		p := &observations[i]
		owner := p.Source == ownerPriceSource && p.PriceAdjustment == ownerPriceAdjustment
		fixture := p.Source == "local_fixture" && p.PriceAdjustment == marketDataAdjustmentUnspecified
		if (!owner && !fixture) || p.Symbol != entry.Symbol || p.Venue != entry.Venue || p.Currency != entry.Currency {
			continue
		}
		t := times[priceTimeKey(*p)]
		if t[0].After(at) || t[1].After(at) || t[2].After(at) {
			continue
		}
		identities[p.InstrumentID] = true
		if selected == nil || t[0].After(times[priceTimeKey(*selected)][0]) {
			selected, ambiguous = p, false
		} else if t[0].Equal(times[priceTimeKey(*selected)][0]) && !reflect.DeepEqual(watchlistPrice(p), watchlistPrice(selected)) {
			ambiguous = true
		}
	}
	if selected == nil {
		return quote
	}
	if ambiguous || len(identities) != 1 {
		quote.Status = "ambiguous"
		return quote
	}
	quote.Price = watchlistPrice(selected)
	quote.Status = "unknown"
	if selected.Source == "local_fixture" || at.Sub(times[priceTimeKey(*selected)][0]) > 24*time.Hour {
		quote.Status = "stale"
	}
	return quote
}

func watchlistPrice(p *SecurityPriceObservation) *WatchlistPrice {
	price := &WatchlistPrice{Source: p.Source, Price: p.Price, PriceAdjustment: p.PriceAdjustment, ObservedAt: p.ObservedAt, FetchedAt: p.FetchedAt, RecordedAt: p.RecordedAt, Sample: priceIsSample(p)}
	if p.Source == ownerPriceSource {
		price.SourceVersion = stringPointer(p.OwnerVersion)
	}
	return price
}

func Report(all Inputs) (*WatchlistReport, error) {
	at := all.Cutoff
	state := all.State
	if !canonicalUTCString(at.Format(time.RFC3339Nano)) || (state.UpdatedAt != nil && at.Before(mustParseWatchlistTime(*state.UpdatedAt))) {
		return nil, errors.New("watchlist clock precedes saved state")
	}
	times := map[string][3]time.Time{}
	for _, p := range all.Prices {
		a, e1 := time.Parse(time.RFC3339Nano, p.ObservedAt)
		f, e2 := time.Parse(time.RFC3339Nano, p.FetchedAt)
		r, e3 := time.Parse(time.RFC3339Nano, p.RecordedAt)
		if e1 != nil || e2 != nil || e3 != nil || f.Before(a) || r.Before(f) {
			return nil, errors.New("security price observation timestamps are inconsistent")
		}
		times[priceTimeKey(p)] = [3]time.Time{a, f, r}
	}
	result := &WatchlistReport{PolicyVersion: watchlistPolicy, MaxObservationAgeSeconds: 86400, ObservedAt: at.Format(time.RFC3339Nano), Watchlist: state, Quotes: []WatchlistQuote{}}
	for _, entry := range state.Entries {
		result.Quotes = append(result.Quotes, watchlistQuote(entry, all.Prices, times, at))
	}
	return result, nil
}
