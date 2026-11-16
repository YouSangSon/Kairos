package domain

import (
	"errors"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/exact"
	"strings"
	"time"
	"unicode"
)

type FXInput struct {
	Source              string
	SourceObservationID string
	BaseCurrency        string
	QuoteCurrency       string
	Rate                string
	ObservedAt          string
	FetchedAt           string
}
type PriceInput struct {
	Source              string
	SourceObservationID string
	InstrumentID        string
	Symbol              string
	Venue               string
	Currency            string
	Price               string
	PriceAdjustment     string
	ObservedAt          string
	FetchedAt           string
}
type ListingInput struct{ InstrumentID, Venue, Symbol, Currency string }
type ListingEvent struct {
	EventID, EventType, Authority, InstrumentID, Venue, Symbol, Currency, ExpectedPreviousEventID, RecordedAt string
}
type LatestTrade struct{ Source, Environment, Exchange, Symbol, Currency, Price, ObservedAt, FetchedAt string }
type BenchmarkDefinition struct {
	Currency, ReturnBasis, SourceReference string
	Sample                                 bool
}

var ErrCaptureFuture = errors.New("owner data capture is in the future")

func OwnerRecordedAt(previous, fetched string, now time.Time) (string, error) {
	now = now.UTC()
	recorded := now.Format(time.RFC3339Nano)
	if !CanonicalUTC(recorded) {
		return "", errors.New("owner data record clock is invalid")
	}
	if previous != "" {
		last, err := time.Parse(time.RFC3339Nano, previous)
		if err != nil || !CanonicalUTC(previous) || now.Before(last) {
			return "", errors.New("owner data record clock moved backwards")
		}
	}
	captured, err := time.Parse(time.RFC3339Nano, fetched)
	if err != nil || !CanonicalUTC(fetched) {
		return "", errors.New("owner data capture time is invalid")
	}
	if now.Before(captured) {
		return "", ErrCaptureFuture
	}
	return recorded, nil
}

func VersionConflicts(previousSHA, incomingSHA string) bool { return previousSHA != incomingSHA }
func LegacyFXConflicts(base, quote, legacyBase, legacyQuote string) bool {
	return base == legacyBase && quote == legacyQuote
}
func LegacyPriceConflicts(instrument, currency, legacyInstrument, legacyCurrency string) bool {
	return instrument == legacyInstrument && currency == legacyCurrency
}
func SameBenchmarkDefinition(a, b BenchmarkDefinition) bool { return a == b }
func SameFXObservation(a, b FXInput) bool                   { return a == b }
func SamePriceObservation(a, b PriceInput) bool {
	if strings.HasPrefix(b.Source, "kiwoom_") {
		a.FetchedAt = b.FetchedAt
	}
	return a == b
}
func RequiresOwnerListing(source string) bool { return strings.HasPrefix(source, "kiwoom_") }
func KiwoomSource(source string) bool {
	return source == "kiwoom_mock" || source == "kiwoom_production"
}
func ValidPriceSource(source string) bool { return source == "local_fixture" || KiwoomSource(source) }
func CurrencyCode(raw string) bool {
	if len(raw) != 3 {
		return false
	}
	for i := range raw {
		if raw[i] < 'A' || raw[i] > 'Z' {
			return false
		}
	}
	return true
}
func canonicalDecimal(raw string, nonNegative bool) bool {
	if len(raw) == 0 || len(raw) > 64 {
		return false
	}
	value, err := exact.ParseDecimal(raw)
	if err != nil || (nonNegative && value.Sign() < 0) {
		return false
	}
	canonical, err := exact.FormatDecimal(value)
	return err == nil && canonical == raw
}
func ValidateFXInput(input FXInput) error {
	if input.Source != "local_fixture" {
		return errors.New("FX observation source is outside the local fixture boundary")
	}
	if !SafeID(input.SourceObservationID) {
		return errors.New("FX source observation identifier is invalid")
	}
	if !CurrencyCode(input.BaseCurrency) || !CurrencyCode(input.QuoteCurrency) || input.BaseCurrency == input.QuoteCurrency {
		return errors.New("FX observation currencies must be distinct uppercase three-letter codes")
	}
	if !canonicalDecimal(input.Rate, true) || input.Rate == "0" {
		return errors.New("FX observation rate must be a positive canonical decimal")
	}
	if !CanonicalUTC(input.ObservedAt) || !CanonicalUTC(input.FetchedAt) {
		return errors.New("FX observation timestamps must be canonical UTC")
	}
	return nil
}
func DurableMetadataValid(id, recorded string) bool { return SafeID(id) && CanonicalUTC(recorded) }
func ValidateFXQuery(source, base, quote, asOf string) error {
	if source != "local_fixture" || !CurrencyCode(base) || !CurrencyCode(quote) || base == quote || !CanonicalUTC(asOf) {
		return errors.New("invalid direct FX observation query")
	}
	return nil
}
func ValidatePriceIdentity(input PriceInput) error {
	if !ValidPriceSource(input.Source) {
		return errors.New("security price observation source is outside the supported boundary")
	}
	if !SafeID(input.SourceObservationID) || !SafeID(input.InstrumentID) || !SafeID(input.Symbol) || !SafeID(input.Venue) || input.Symbol != strings.ToUpper(input.Symbol) || input.Venue != strings.ToUpper(input.Venue) {
		return errors.New("security price observation identifier is invalid")
	}
	if KiwoomSource(input.Source) && (!KRXSymbol(input.Symbol) || input.Venue != "XKRX" || input.Currency != "KRW") {
		return errors.New("Kiwoom price observation market identity is invalid")
	}
	return nil
}
func ValidatePriceValues(input PriceInput) error {
	if !CurrencyCode(input.Currency) || !canonicalDecimal(input.Price, true) || input.Price == "0" {
		return errors.New("security price observation price or currency is invalid")
	}
	if input.PriceAdjustment != "unspecified" {
		return errors.New("security price observation adjustment is outside the local fixture boundary")
	}
	if !CanonicalUTC(input.ObservedAt) || !CanonicalUTC(input.FetchedAt) {
		return errors.New("security price observation timestamps must be canonical UTC")
	}
	observed, _ := time.Parse(time.RFC3339Nano, input.ObservedAt)
	fetched, _ := time.Parse(time.RFC3339Nano, input.FetchedAt)
	if fetched.Before(observed) {
		return errors.New("security price observation fetched_at precedes observed_at")
	}
	return nil
}
func PriceRecordedAfterCapture(fetched, recorded string) bool {
	capture, _ := time.Parse(time.RFC3339Nano, fetched)
	record, _ := time.Parse(time.RFC3339Nano, recorded)
	return !record.Before(capture)
}
func ValidatePriceQuery(source, instrument, symbol, venue, currency, adjustment, asOf string) error {
	if source != "local_fixture" || !SafeID(instrument) || !SafeID(symbol) || !SafeID(venue) || symbol != strings.ToUpper(symbol) || venue != strings.ToUpper(venue) || !CurrencyCode(currency) || adjustment != "unspecified" || !CanonicalUTC(asOf) {
		return errors.New("invalid security price observation query")
	}
	return nil
}
func ValidateListingInput(input ListingInput) error {
	if !SafeID(input.InstrumentID) || !SafeID(input.Venue) || !SafeID(input.Symbol) || input.Venue != strings.ToUpper(input.Venue) || input.Symbol != strings.ToUpper(input.Symbol) || !CurrencyCode(input.Currency) {
		return errors.New("instrument listing identity is invalid")
	}
	return nil
}
func ValidateListingMetadata(event ListingEvent) error {
	if !SafeID(event.EventID) || !SafeID(event.ExpectedPreviousEventID) || (event.EventType != "DECLARE" && event.EventType != "REVOKE") || event.Authority != "owner_declared" || !CanonicalUTC(event.RecordedAt) {
		return errors.New("instrument listing event metadata is invalid")
	}
	return nil
}
func ListingQueryValid(venue, symbol, currency string) bool {
	return SafeID(venue) && SafeID(symbol) && venue == strings.ToUpper(venue) && symbol == strings.ToUpper(symbol) && CurrencyCode(currency)
}

type ListingDisposition int

const (
	ListingAppend ListingDisposition = iota
	ListingDuplicate
	ListingConflict
	ListingMissing
)

func ListingDecision(eventType, requestedInstrument, currentInstrument string, exists bool) ListingDisposition {
	if eventType == "DECLARE" && exists {
		if currentInstrument != requestedInstrument {
			return ListingConflict
		}
		return ListingDuplicate
	}
	if eventType == "REVOKE" && (!exists || currentInstrument != requestedInstrument) {
		return ListingMissing
	}
	return ListingAppend
}
func ListingTransitionValid(event, previous ListingEvent, exists bool) bool {
	if !exists {
		return event.ExpectedPreviousEventID == "no_event" && event.EventType == "DECLARE"
	}
	return event.ExpectedPreviousEventID == previous.EventID &&
		!(previous.EventType == "DECLARE" && event.EventType != "REVOKE") &&
		!(previous.EventType == "REVOKE" && event.EventType != "DECLARE") &&
		!(event.EventType == "REVOKE" && event.InstrumentID != previous.InstrumentID)
}
func LatestTradeIdentityValid(trade LatestTrade) bool {
	return trade.Source == "kiwoom" && (trade.Environment == "mock" || trade.Environment == "production") && trade.Exchange == "KRX" && trade.Currency == "KRW" && KRXSymbol(trade.Symbol)
}
func OwnerFXIdentityValid(version, base, quote, fetched string) bool {
	return SafeID(version) && CurrencyCode(base) && CurrencyCode(quote) && base != quote && len(fetched) <= 30 && CanonicalUTC(fetched)
}
func OwnerPriceIdentifiersValid(version, instrument, symbol, venue string) bool {
	return SafeID(version) && SafeID(instrument) && SafeID(symbol) && SafeID(venue) && symbol == strings.ToUpper(symbol) && venue == strings.ToUpper(venue)
}
func OwnerPriceCaptureValid(canonical, instrument, currency, adjustment, fetched string) bool {
	return canonical == instrument && CurrencyCode(currency) && adjustment == "unadjusted" && len(fetched) <= 30 && CanonicalUTC(fetched)
}
func BenchmarkIdentityValid(id, version, label, currency, fetched string) bool {
	return SafeID(id) && SafeID(version) && label != "" && len(label) <= 160 && strings.TrimSpace(label) == label && strings.IndexFunc(label, unicode.IsControl) < 0 && (currency == "KRW" || currency == "USD") && len(fetched) <= 30 && CanonicalUTC(fetched)
}
func BenchmarkReturnBasisValid(basis string) bool {
	return basis == "price_index" || basis == "gross_total_return" || basis == "net_total_return"
}

type SourceReference struct {
	Bytes                                        int
	Scheme, Hostname, RawQuery, Fragment, Opaque string
	HasUser, ForceQuery, Canonical               bool
}

func BenchmarkReferenceValid(reference SourceReference) bool {
	return reference.Bytes <= 512 && reference.Scheme == "https" && reference.Hostname != "" && !reference.HasUser && reference.RawQuery == "" && !reference.ForceQuery && reference.Fragment == "" && reference.Opaque == "" && reference.Canonical
}
func SeriesPointCountAllowed(count int) bool { return count < 10000 }
func SeriesPointAdmitted(value, observed string, fetched, previous time.Time, count int, positive bool) bool {
	if len(observed) > 30 || !CanonicalUTC(observed) || !canonicalDecimal(value, true) || (positive && value == "0") {
		return false
	}
	at, _ := time.Parse(time.RFC3339Nano, observed)
	return !at.After(fetched) && (count == 0 || at.After(previous))
}
func SeriesContentValid(count int, benchmark bool) bool { return !benchmark || count > 0 }
func SeriesRecordTimeValid(at, fetched, previous time.Time, hasPrevious bool) bool {
	return !at.Before(fetched) && (!hasPrevious || !at.Before(previous))
}
func PriceKnownAt(observed, fetched, recorded string, cutoff time.Time) bool {
	observation, _ := time.Parse(time.RFC3339Nano, observed)
	capture, _ := time.Parse(time.RFC3339Nano, fetched)
	record, _ := time.Parse(time.RFC3339Nano, recorded)
	return !observation.After(cutoff) && !capture.After(cutoff) && !record.After(cutoff)
}
func LaterPrice(observed, id, selectedObserved, selectedID string) bool {
	at, _ := time.Parse(time.RFC3339Nano, observed)
	selected, _ := time.Parse(time.RFC3339Nano, selectedObserved)
	return at.After(selected) || (at.Equal(selected) && id > selectedID)
}
func CandleIdentityValid(instrument, currency *string) bool {
	return (instrument == nil && currency == nil) || (instrument != nil && currency != nil && SafeID(*instrument) && CurrencyCode(*currency))
}
func CandleMetadataValid(symbol, venue, timezone, interval string) bool {
	return symbol != "" && venue != "" && timezone != "" && interval != ""
}
func CandleTimeOrdered(at, previous time.Time) bool { return previous.IsZero() || at.After(previous) }
func ValidateOHLC(open, high, low, close, volume string) error {
	prices := make([]*big.Rat, 4)
	for index, item := range []struct{ name, raw string }{{"open", open}, {"high", high}, {"low", low}, {"close", close}} {
		value, err := exact.ParseDecimal(item.raw)
		if err != nil || value.Sign() <= 0 {
			return fmt.Errorf("has invalid %s", item.name)
		}
		prices[index] = value
	}
	parsedVolume, err := exact.ParseDecimal(volume)
	if err != nil || parsedVolume.Sign() < 0 {
		return errors.New("has invalid volume")
	}
	if prices[2].Cmp(prices[0]) > 0 || prices[2].Cmp(prices[3]) > 0 || prices[0].Cmp(prices[1]) > 0 || prices[3].Cmp(prices[1]) > 0 {
		return errors.New("has invalid OHLC range")
	}
	return nil
}

type PriceIdentity struct{ Source, InstrumentID, Symbol, Venue, Currency, PriceAdjustment string }

func PriceIdentityMatches(a, b PriceIdentity) bool { return a == b }
func ListingAuthorizes(currentInstrument, requestedInstrument string) bool {
	return currentInstrument == requestedInstrument
}

type CandleMetadata struct{ Symbol, Venue, Timezone, Interval string }

func CandleMetadataMatches(a, b CandleMetadata) bool { return a == b }
func CandleIdentityMatches(instrument, currency *string, rowInstrument, rowCurrency string) bool {
	return instrument == nil || (*instrument == rowInstrument && *currency == rowCurrency)
}
