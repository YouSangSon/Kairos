package codec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	sharedcodec "omni-folio/services/core/internal/codec"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type BenchmarkIndexPoint = c.BenchmarkIndexPoint
type BenchmarkSeriesInput = c.BenchmarkSeriesInput
type BenchmarkSeriesReceipt = c.BenchmarkSeriesReceipt
type FXObservation = c.FXObservation
type FXObservationInput = c.FXObservationInput
type InstrumentListingEvent = c.InstrumentListingEvent
type InstrumentListingInput = c.InstrumentListingInput
type OwnerFXPoint = c.OwnerFXPoint
type OwnerFXSeriesInput = c.OwnerFXSeriesInput
type OwnerFXSeriesReceipt = c.OwnerFXSeriesReceipt
type OwnerPricePoint = c.OwnerPricePoint
type OwnerPriceSeriesInput = c.OwnerPriceSeriesInput
type OwnerPriceSeriesReceipt = c.OwnerPriceSeriesReceipt
type SecurityPriceObservation = c.SecurityPriceObservation
type SecurityPriceObservationInput = c.SecurityPriceObservationInput

const maxBodyBytes = 1 << 20
const marketDataAdjustmentUnspecified = "unspecified"

var kiwoomStockPattern = regexp.MustCompile(`^[0-9]{6}$`)
var safeOrderID = sharedcodec.SafeID
var canonicalUTCString = sharedcodec.CanonicalUTC
var canonicalDecimal = sharedcodec.CanonicalDecimal
var orderJSONHash = sharedcodec.JSONHash
var validJSONSurrogates = sharedcodec.ValidJSONSurrogates
var ensureJSONEOF = sharedcodec.EnsureJSONEOF

type ownerFXSeriesRecord = application.OwnerFXRecord
type ownerPriceSeriesRecord = application.OwnerPriceRecord
type benchmarkSeriesRecord = application.BenchmarkRecord

var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

var errFXObservationNotFound = errors.New("FX observation not found")

var errSecurityPriceObservationNotFound = errors.New("security price observation not found")

const (
	instrumentListingDeclare   = "DECLARE"
	instrumentListingRevoke    = "REVOKE"
	instrumentListingAuthority = "owner_declared"
	noInstrumentListingEvent   = "no_event"
)

var errInstrumentListingNotFound = c.ErrListingNotFound

const ownerFXSeriesSchema = "owner-fx-series.v1"

const ownerFXSource = "owner_declared"

var errOwnerFXInput = c.ErrOwnerFXInput

const ownerPriceSeriesSchema = "owner-price-series.v1"

const ownerPriceSource = "owner_declared"

const ownerPriceAdjustment = "unadjusted"

var errOwnerPriceInput = c.ErrOwnerPriceInput

const benchmarkSeriesSchema = "benchmark-index-series.v1"

var errBenchmarkSeriesInput = c.ErrBenchmarkInput

var errBenchmarkSeriesNotFound = c.ErrBenchmarkNotFound

func ValidateFXObservationInput(input FXObservationInput) error {
	if input.Source != "local_fixture" {
		return errors.New("FX observation source is outside the local fixture boundary")
	}
	if !safeOrderID(input.SourceObservationID) {
		return errors.New("FX source observation identifier is invalid")
	}
	if !currencyCodePattern.MatchString(input.BaseCurrency) || !currencyCodePattern.MatchString(input.QuoteCurrency) || input.BaseCurrency == input.QuoteCurrency {
		return errors.New("FX observation currencies must be distinct uppercase three-letter codes")
	}
	if !canonicalDecimal(input.Rate, true) || input.Rate == "0" {
		return errors.New("FX observation rate must be a positive canonical decimal")
	}
	if !canonicalUTCString(input.ObservedAt) || !canonicalUTCString(input.FetchedAt) {
		return errors.New("FX observation timestamps must be canonical UTC")
	}
	return nil
}

func ValidateFXObservation(observation FXObservation) error {
	if !safeOrderID(observation.ObservationID) || !canonicalUTCString(observation.RecordedAt) {
		return errors.New("FX observation durable metadata is invalid")
	}
	return ValidateFXObservationInput(FXObservationInput{
		Source: observation.Source, SourceObservationID: observation.SourceObservationID,
		BaseCurrency: observation.BaseCurrency, QuoteCurrency: observation.QuoteCurrency, Rate: observation.Rate,
		ObservedAt: observation.ObservedAt, FetchedAt: observation.FetchedAt,
	})
}

func FxObservationSHA(observation FXObservation) (string, error) {
	_, hash, err := orderJSONHash(observation)
	return hash, err
}

func ValidateFXObservationQuery(source, base, quote, asOf string) error {
	if source != "local_fixture" || !currencyCodePattern.MatchString(base) || !currencyCodePattern.MatchString(quote) || base == quote || !canonicalUTCString(asOf) {
		return errors.New("invalid direct FX observation query")
	}
	return nil
}

func ValidSecurityPriceObservationSource(source string) bool {
	return source == "local_fixture" || source == "kiwoom_mock" || source == "kiwoom_production"
}

func KiwoomSecurityPriceObservationSource(source string) bool {
	return source == "kiwoom_mock" || source == "kiwoom_production"
}

func ValidateSecurityPriceObservationInput(input SecurityPriceObservationInput) error {
	if !ValidSecurityPriceObservationSource(input.Source) {
		return errors.New("security price observation source is outside the supported boundary")
	}
	if !safeOrderID(input.SourceObservationID) || !safeOrderID(input.InstrumentID) || !safeOrderID(input.Symbol) || !safeOrderID(input.Venue) ||
		input.Symbol != strings.ToUpper(input.Symbol) || input.Venue != strings.ToUpper(input.Venue) {
		return errors.New("security price observation identifier is invalid")
	}
	if KiwoomSecurityPriceObservationSource(input.Source) {
		if !kiwoomStockPattern.MatchString(input.Symbol) || input.Venue != "XKRX" || input.Currency != "KRW" {
			return errors.New("Kiwoom price observation market identity is invalid")
		}
		wantID, err := KiwoomLatestTradeObservationID(input.Source, input.InstrumentID, input.Symbol, input.Venue, input.Currency, input.PriceAdjustment, input.ObservedAt)
		if err != nil || input.SourceObservationID != wantID {
			return errors.New("Kiwoom price observation identifier is invalid")
		}
	}
	if !currencyCodePattern.MatchString(input.Currency) || !canonicalDecimal(input.Price, true) || input.Price == "0" {
		return errors.New("security price observation price or currency is invalid")
	}
	if input.PriceAdjustment != marketDataAdjustmentUnspecified {
		return errors.New("security price observation adjustment is outside the local fixture boundary")
	}
	if !canonicalUTCString(input.ObservedAt) || !canonicalUTCString(input.FetchedAt) {
		return errors.New("security price observation timestamps must be canonical UTC")
	}
	observed, _ := time.Parse(time.RFC3339Nano, input.ObservedAt)
	fetched, _ := time.Parse(time.RFC3339Nano, input.FetchedAt)
	if fetched.Before(observed) {
		return errors.New("security price observation fetched_at precedes observed_at")
	}
	return nil
}

func ValidateSecurityPriceObservation(observation SecurityPriceObservation) error {
	if !safeOrderID(observation.ObservationID) || !canonicalUTCString(observation.RecordedAt) {
		return errors.New("security price observation durable metadata is invalid")
	}
	if err := ValidateSecurityPriceObservationInput(SecurityPriceObservationInput{
		Source: observation.Source, SourceObservationID: observation.SourceObservationID, InstrumentID: observation.InstrumentID,
		Symbol: observation.Symbol, Venue: observation.Venue, Currency: observation.Currency, Price: observation.Price,
		PriceAdjustment: observation.PriceAdjustment, ObservedAt: observation.ObservedAt, FetchedAt: observation.FetchedAt,
	}); err != nil {
		return err
	}
	fetched, _ := time.Parse(time.RFC3339Nano, observation.FetchedAt)
	recorded, _ := time.Parse(time.RFC3339Nano, observation.RecordedAt)
	if recorded.Before(fetched) {
		return errors.New("security price observation recorded_at precedes fetched_at")
	}
	return nil
}

func SecurityPriceObservationSHA(observation SecurityPriceObservation) (string, error) {
	_, hash, err := orderJSONHash(observation)
	return hash, err
}

func ValidateSecurityPriceObservationQuery(source, instrumentID, symbol, venue, currency, adjustment, asOf string) error {
	if source != "local_fixture" || !safeOrderID(instrumentID) || !safeOrderID(symbol) || !safeOrderID(venue) ||
		symbol != strings.ToUpper(symbol) || venue != strings.ToUpper(venue) ||
		!currencyCodePattern.MatchString(currency) || adjustment != marketDataAdjustmentUnspecified || !canonicalUTCString(asOf) {
		return errors.New("invalid security price observation query")
	}
	return nil
}

func KiwoomLatestTradeObservationID(source, instrumentID, symbol, venue, currency, adjustment, observedAt string) (string, error) {
	_, hash, err := orderJSONHash([]any{
		"kiwoom_latest_trade_observation.v1", source, "ka10079", instrumentID,
		venue, symbol, currency, adjustment, observedAt,
	})
	return hash, err
}

func ValidateInstrumentListingInput(input InstrumentListingInput) error {
	if !safeOrderID(input.InstrumentID) || !safeOrderID(input.Venue) || !safeOrderID(input.Symbol) ||
		input.Venue != strings.ToUpper(input.Venue) || input.Symbol != strings.ToUpper(input.Symbol) ||
		!currencyCodePattern.MatchString(input.Currency) {
		return errors.New("instrument listing identity is invalid")
	}
	return nil
}

func ValidateInstrumentListingEvent(event InstrumentListingEvent) error {
	if err := ValidateInstrumentListingInput(InstrumentListingInput{
		InstrumentID: event.InstrumentID, Venue: event.Venue, Symbol: event.Symbol, Currency: event.Currency,
	}); err != nil {
		return err
	}
	if !safeOrderID(event.EventID) || !safeOrderID(event.ExpectedPreviousEventID) ||
		(event.EventType != instrumentListingDeclare && event.EventType != instrumentListingRevoke) ||
		event.Authority != instrumentListingAuthority || !canonicalUTCString(event.RecordedAt) {
		return errors.New("instrument listing event metadata is invalid")
	}
	return nil
}

func InstrumentListingEventSHA(event InstrumentListingEvent) (string, error) {
	_, hash, err := orderJSONHash(event)
	return hash, err
}

func InstrumentListingKey(venue, symbol, currency string) string {
	return venue + "\x00" + symbol + "\x00" + currency
}

func DecodeOwnerFXSeries(ctx context.Context, raw []byte) (*OwnerFXSeriesInput, []byte, string, error) {
	if ctx == nil || len(raw) == 0 || len(raw) > maxBodyBytes || !utf8.Valid(raw) || !validJSONSurrogates(raw) {
		return nil, nil, "", errOwnerFXInput
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, "", err
	}
	fields, err := BenchmarkObject(raw, "schema_version", "source_version", "base_currency", "quote_currency", "sample", "fetched_at", "points")
	if err != nil {
		return nil, nil, "", errOwnerFXInput
	}
	points := fields["points"]
	delete(fields, "points")
	scalars, err := json.Marshal(fields)
	var input OwnerFXSeriesInput
	if err != nil || json.Unmarshal(scalars, &input) != nil || input.SchemaVersion != ownerFXSeriesSchema || !safeOrderID(input.SourceVersion) ||
		!currencyCodePattern.MatchString(input.BaseCurrency) || !currencyCodePattern.MatchString(input.QuoteCurrency) || input.BaseCurrency == input.QuoteCurrency || len(input.FetchedAt) > 30 || !canonicalUTCString(input.FetchedAt) {
		return nil, nil, "", errOwnerFXInput
	}
	d := json.NewDecoder(bytes.NewReader(points))
	if token, err := d.Token(); err != nil || token != json.Delim('[') {
		return nil, nil, "", errOwnerFXInput
	}
	input.Points = []OwnerFXPoint{}
	fetched, _ := time.Parse(time.RFC3339Nano, input.FetchedAt)
	var previous time.Time
	for d.More() {
		if err := ctx.Err(); err != nil {
			return nil, nil, "", err
		}
		if len(input.Points) >= 10000 {
			return nil, nil, "", errOwnerFXInput
		}
		var rawPoint json.RawMessage
		if d.Decode(&rawPoint) != nil {
			return nil, nil, "", errOwnerFXInput
		}
		if _, err := BenchmarkObject(rawPoint, "observed_at", "rate"); err != nil {
			return nil, nil, "", errOwnerFXInput
		}
		var point OwnerFXPoint
		if json.Unmarshal(rawPoint, &point) != nil || len(point.ObservedAt) > 30 || !canonicalUTCString(point.ObservedAt) || !canonicalDecimal(point.Rate, true) || point.Rate == "0" {
			return nil, nil, "", errOwnerFXInput
		}
		at, _ := time.Parse(time.RFC3339Nano, point.ObservedAt)
		if at.After(fetched) || (len(input.Points) > 0 && !at.After(previous)) {
			return nil, nil, "", errOwnerFXInput
		}
		input.Points = append(input.Points, point)
		previous = at
	}
	if token, err := d.Token(); err != nil || token != json.Delim(']') || ensureJSONEOF(d) != nil {
		return nil, nil, "", errOwnerFXInput
	}
	canonical, sha, err := orderJSONHash(input)
	if err != nil || len(canonical) > maxBodyBytes {
		return nil, nil, "", errOwnerFXInput
	}
	return &input, canonical, sha, nil
}

func OwnerFXRecordSHA(r ownerFXSeriesRecord) (string, error) {
	_, sha, err := orderJSONHash([]any{r.Sequence, r.Input.BaseCurrency, r.Input.QuoteCurrency, r.Input.SourceVersion, r.DataSHA256, r.RecordedAt})
	return sha, err
}

func DecodeOwnerPriceSeries(ctx context.Context, raw []byte, canonicalID func(string) string) (*OwnerPriceSeriesInput, []byte, string, error) {
	if ctx == nil || len(raw) == 0 || len(raw) > maxBodyBytes || !utf8.Valid(raw) || !validJSONSurrogates(raw) {
		return nil, nil, "", errOwnerPriceInput
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, "", err
	}
	fields, err := BenchmarkObject(raw, "schema_version", "source_version", "instrument_id", "symbol", "venue", "currency", "price_adjustment", "sample", "fetched_at", "points")
	if err != nil {
		return nil, nil, "", errOwnerPriceInput
	}
	points := fields["points"]
	delete(fields, "points")
	scalars, err := json.Marshal(fields)
	var input OwnerPriceSeriesInput
	if err != nil || json.Unmarshal(scalars, &input) != nil || input.SchemaVersion != ownerPriceSeriesSchema || !safeOrderID(input.SourceVersion) ||
		!safeOrderID(input.InstrumentID) || !safeOrderID(input.Symbol) || !safeOrderID(input.Venue) || input.Symbol != strings.ToUpper(input.Symbol) || input.Venue != strings.ToUpper(input.Venue) || canonicalID(input.Symbol) != input.InstrumentID || !currencyCodePattern.MatchString(input.Currency) || input.PriceAdjustment != ownerPriceAdjustment || len(input.FetchedAt) > 30 || !canonicalUTCString(input.FetchedAt) {
		return nil, nil, "", errOwnerPriceInput
	}
	d := json.NewDecoder(bytes.NewReader(points))
	if token, err := d.Token(); err != nil || token != json.Delim('[') {
		return nil, nil, "", errOwnerPriceInput
	}
	input.Points = []OwnerPricePoint{}
	fetched, _ := time.Parse(time.RFC3339Nano, input.FetchedAt)
	var previous time.Time
	for d.More() {
		if err := ctx.Err(); err != nil {
			return nil, nil, "", err
		}
		if len(input.Points) >= 10000 {
			return nil, nil, "", errOwnerPriceInput
		}
		var rawPoint json.RawMessage
		if d.Decode(&rawPoint) != nil {
			return nil, nil, "", errOwnerPriceInput
		}
		if _, err := BenchmarkObject(rawPoint, "observed_at", "price"); err != nil {
			return nil, nil, "", errOwnerPriceInput
		}
		var point OwnerPricePoint
		if json.Unmarshal(rawPoint, &point) != nil || len(point.ObservedAt) > 30 || !canonicalUTCString(point.ObservedAt) || !canonicalDecimal(point.Price, true) || point.Price == "0" {
			return nil, nil, "", errOwnerPriceInput
		}
		at, _ := time.Parse(time.RFC3339Nano, point.ObservedAt)
		if at.After(fetched) || (len(input.Points) > 0 && !at.After(previous)) {
			return nil, nil, "", errOwnerPriceInput
		}
		input.Points = append(input.Points, point)
		previous = at
	}
	if token, err := d.Token(); err != nil || token != json.Delim(']') || ensureJSONEOF(d) != nil {
		return nil, nil, "", errOwnerPriceInput
	}
	canonical, sha, err := orderJSONHash(input)
	if err != nil || len(canonical) > maxBodyBytes {
		return nil, nil, "", errOwnerPriceInput
	}
	return &input, canonical, sha, nil
}

func OwnerPriceRecordSHA(r ownerPriceSeriesRecord) (string, error) {
	_, sha, err := orderJSONHash([]any{r.Sequence, r.Input.InstrumentID, r.Input.Currency, r.Input.SourceVersion, r.DataSHA256, r.RecordedAt})
	return sha, err
}

// This small closed-object reader is shared by the envelope and its point rows.
func BenchmarkObject(raw []byte, keys ...string) (map[string]json.RawMessage, error) {
	return ClosedJSONObject(raw, nil, keys...)
}

func ClosedJSONObject(raw []byte, nullable map[string]bool, keys ...string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return nil, errBenchmarkSeriesInput
	}
	fields := map[string]json.RawMessage{}
	allowed := map[string]bool{}
	for _, key := range keys {
		allowed[key] = true
	}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || fields[key] != nil {
			return nil, errBenchmarkSeriesInput
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || (!nullable[key] && bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
			return nil, errBenchmarkSeriesInput
		}
		fields[key] = value
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') || len(fields) != len(keys) || ensureJSONEOF(d) != nil {
		return nil, errBenchmarkSeriesInput
	}
	return fields, nil
}

func DecodeBenchmarkSeries(ctx context.Context, raw []byte) (*BenchmarkSeriesInput, []byte, string, error) {
	if len(raw) == 0 || len(raw) > maxBodyBytes || !utf8.Valid(raw) || !validJSONSurrogates(raw) {
		return nil, nil, "", errBenchmarkSeriesInput
	}
	fields, err := BenchmarkObject(raw, "schema_version", "benchmark_id", "label", "currency", "return_basis", "source_reference", "source_version", "sample", "fetched_at", "points")
	if err != nil {
		return nil, nil, "", err
	}
	var input BenchmarkSeriesInput

	points := fields["points"]
	delete(fields, "points")
	scalars, err := json.Marshal(fields)
	if err != nil || json.Unmarshal(scalars, &input) != nil {
		return nil, nil, "", errBenchmarkSeriesInput
	}
	if input.SchemaVersion != benchmarkSeriesSchema || !safeOrderID(input.BenchmarkID) || !safeOrderID(input.SourceVersion) ||
		input.Label == "" || len(input.Label) > 160 || strings.TrimSpace(input.Label) != input.Label || strings.IndexFunc(input.Label, unicode.IsControl) >= 0 ||
		(input.Currency != "KRW" && input.Currency != "USD") || len(input.FetchedAt) > 30 || !canonicalUTCString(input.FetchedAt) {
		return nil, nil, "", errBenchmarkSeriesInput
	}
	switch input.ReturnBasis {
	case "price_index", "gross_total_return", "net_total_return":
	default:
		return nil, nil, "", errBenchmarkSeriesInput
	}
	ref, err := url.Parse(input.SourceReference)
	if err != nil || len(input.SourceReference) > 512 || ref.Scheme != "https" || ref.Hostname() == "" || ref.User != nil || ref.RawQuery != "" || ref.ForceQuery || ref.Fragment != "" || ref.Opaque != "" || ref.String() != input.SourceReference {
		return nil, nil, "", errBenchmarkSeriesInput
	}
	d := json.NewDecoder(bytes.NewReader(points))
	if token, err := d.Token(); err != nil || token != json.Delim('[') {
		return nil, nil, "", errBenchmarkSeriesInput
	}
	fetched, _ := time.Parse(time.RFC3339Nano, input.FetchedAt)
	var previous time.Time
	for d.More() {
		if err := ctx.Err(); err != nil {
			return nil, nil, "", err
		}
		if len(input.Points) >= 10000 {
			return nil, nil, "", errBenchmarkSeriesInput
		}
		var row json.RawMessage
		if d.Decode(&row) != nil {
			return nil, nil, "", errBenchmarkSeriesInput
		}
		if _, err := BenchmarkObject(row, "observed_at", "level"); err != nil {
			return nil, nil, "", err
		}
		var point BenchmarkIndexPoint
		if json.Unmarshal(row, &point) != nil || len(point.ObservedAt) > 30 || !canonicalUTCString(point.ObservedAt) || !canonicalDecimal(point.Level, true) {
			return nil, nil, "", errBenchmarkSeriesInput
		}
		at, _ := time.Parse(time.RFC3339Nano, point.ObservedAt)
		if len(input.Points) > 0 && !at.After(previous) || at.After(fetched) {
			return nil, nil, "", errBenchmarkSeriesInput
		}
		previous = at
		input.Points = append(input.Points, point)
	}
	if token, err := d.Token(); err != nil || token != json.Delim(']') || ensureJSONEOF(d) != nil || len(input.Points) == 0 {
		return nil, nil, "", errBenchmarkSeriesInput
	}
	canonical, sha, err := orderJSONHash(input)
	if err != nil || len(canonical) > maxBodyBytes {
		return nil, nil, "", errBenchmarkSeriesInput
	}
	return &input, canonical, sha, nil
}

func SameBenchmarkDefinition(a, b BenchmarkSeriesInput) bool {
	return a.Currency == b.Currency && a.ReturnBasis == b.ReturnBasis && a.SourceReference == b.SourceReference && a.Sample == b.Sample
}

func BenchmarkRecordSHA(record benchmarkSeriesRecord) (string, error) {
	_, sha, err := orderJSONHash(struct {
		Sequence                                           int64
		BenchmarkID, SourceVersion, DataSHA256, RecordedAt string
	}{record.Sequence, record.Input.BenchmarkID, record.Input.SourceVersion, record.DataSHA256, record.RecordedAt})
	return sha, err
}
