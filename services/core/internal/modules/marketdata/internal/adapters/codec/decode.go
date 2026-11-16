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
	"omni-folio/services/core/internal/modules/marketdata/internal/domain"
	"time"
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

var orderJSONHash = sharedcodec.JSONHash
var validJSONSurrogates = sharedcodec.ValidJSONSurrogates
var ensureJSONEOF = sharedcodec.EnsureJSONEOF

type ownerFXSeriesRecord = application.OwnerFXRecord
type ownerPriceSeriesRecord = application.OwnerPriceRecord
type benchmarkSeriesRecord = application.BenchmarkRecord

var errFXObservationNotFound = errors.New("FX observation not found")

var errSecurityPriceObservationNotFound = errors.New("security price observation not found")

var errInstrumentListingNotFound = c.ErrListingNotFound

const ownerFXSeriesSchema = "owner-fx-series.v1"

const ownerFXSource = "owner_declared"

var errOwnerFXInput = c.ErrOwnerFXInput

const ownerPriceSeriesSchema = "owner-price-series.v1"

const ownerPriceSource = "owner_declared"

var errOwnerPriceInput = c.ErrOwnerPriceInput

const benchmarkSeriesSchema = "benchmark-index-series.v1"

var errBenchmarkSeriesInput = c.ErrBenchmarkInput

var errBenchmarkSeriesNotFound = c.ErrBenchmarkNotFound

func ValidateFXObservationInput(input FXObservationInput) error {
	return domain.ValidateFXInput(domain.FXInput(input))
}

func ValidateFXObservation(observation FXObservation) error {
	if !domain.DurableMetadataValid(observation.ObservationID, observation.RecordedAt) {
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
	return domain.ValidateFXQuery(source, base, quote, asOf)
}

func ValidSecurityPriceObservationSource(source string) bool {
	return domain.ValidPriceSource(source)
}

func KiwoomSecurityPriceObservationSource(source string) bool {
	return domain.KiwoomSource(source)
}

func ValidateSecurityPriceObservationInput(input SecurityPriceObservationInput) error {
	value := domain.PriceInput(input)
	if err := domain.ValidatePriceIdentity(value); err != nil {
		return err
	}
	if KiwoomSecurityPriceObservationSource(input.Source) {
		wantID, err := KiwoomLatestTradeObservationID(input.Source, input.InstrumentID, input.Symbol, input.Venue, input.Currency, input.PriceAdjustment, input.ObservedAt)
		if err != nil || input.SourceObservationID != wantID {
			return errors.New("Kiwoom price observation identifier is invalid")
		}
	}
	return domain.ValidatePriceValues(value)
}

func ValidateSecurityPriceObservation(observation SecurityPriceObservation) error {
	if !domain.DurableMetadataValid(observation.ObservationID, observation.RecordedAt) {
		return errors.New("security price observation durable metadata is invalid")
	}
	if err := ValidateSecurityPriceObservationInput(SecurityPriceObservationInput{
		Source: observation.Source, SourceObservationID: observation.SourceObservationID, InstrumentID: observation.InstrumentID,
		Symbol: observation.Symbol, Venue: observation.Venue, Currency: observation.Currency, Price: observation.Price,
		PriceAdjustment: observation.PriceAdjustment, ObservedAt: observation.ObservedAt, FetchedAt: observation.FetchedAt,
	}); err != nil {
		return err
	}
	if !domain.PriceRecordedAfterCapture(observation.FetchedAt, observation.RecordedAt) {
		return errors.New("security price observation recorded_at precedes fetched_at")
	}
	return nil
}

func SecurityPriceObservationSHA(observation SecurityPriceObservation) (string, error) {
	_, hash, err := orderJSONHash(observation)
	return hash, err
}

func ValidateSecurityPriceObservationQuery(source, instrumentID, symbol, venue, currency, adjustment, asOf string) error {
	return domain.ValidatePriceQuery(source, instrumentID, symbol, venue, currency, adjustment, asOf)
}

func KiwoomLatestTradeObservationID(source, instrumentID, symbol, venue, currency, adjustment, observedAt string) (string, error) {
	_, hash, err := orderJSONHash([]any{
		"kiwoom_latest_trade_observation.v1", source, "ka10079", instrumentID,
		venue, symbol, currency, adjustment, observedAt,
	})
	return hash, err
}

func ValidateInstrumentListingInput(input InstrumentListingInput) error {
	return domain.ValidateListingInput(domain.ListingInput(input))
}

func ValidateInstrumentListingEvent(event InstrumentListingEvent) error {
	if err := ValidateInstrumentListingInput(InstrumentListingInput{InstrumentID: event.InstrumentID, Venue: event.Venue, Symbol: event.Symbol, Currency: event.Currency}); err != nil {
		return err
	}
	return domain.ValidateListingMetadata(domain.ListingEvent(event))
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
	if err != nil || json.Unmarshal(scalars, &input) != nil || input.SchemaVersion != ownerFXSeriesSchema || !domain.OwnerFXIdentityValid(input.SourceVersion, input.BaseCurrency, input.QuoteCurrency, input.FetchedAt) {
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
		if !domain.SeriesPointCountAllowed(len(input.Points)) {
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
		if json.Unmarshal(rawPoint, &point) != nil || !domain.SeriesPointAdmitted(point.Rate, point.ObservedAt, fetched, previous, len(input.Points), true) {
			return nil, nil, "", errOwnerFXInput
		}
		at, _ := time.Parse(time.RFC3339Nano, point.ObservedAt)
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
	if err != nil || json.Unmarshal(scalars, &input) != nil || input.SchemaVersion != ownerPriceSeriesSchema || !domain.OwnerPriceIdentifiersValid(input.SourceVersion, input.InstrumentID, input.Symbol, input.Venue) || !domain.OwnerPriceCaptureValid(canonicalID(input.Symbol), input.InstrumentID, input.Currency, input.PriceAdjustment, input.FetchedAt) {
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
		if !domain.SeriesPointCountAllowed(len(input.Points)) {
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
		if json.Unmarshal(rawPoint, &point) != nil || !domain.SeriesPointAdmitted(point.Price, point.ObservedAt, fetched, previous, len(input.Points), true) {
			return nil, nil, "", errOwnerPriceInput
		}
		at, _ := time.Parse(time.RFC3339Nano, point.ObservedAt)
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
	if input.SchemaVersion != benchmarkSeriesSchema || !domain.BenchmarkIdentityValid(input.BenchmarkID, input.SourceVersion, input.Label, input.Currency, input.FetchedAt) {
		return nil, nil, "", errBenchmarkSeriesInput
	}
	if !domain.BenchmarkReturnBasisValid(input.ReturnBasis) {
		return nil, nil, "", errBenchmarkSeriesInput
	}
	ref, err := url.Parse(input.SourceReference)
	if err != nil || !domain.BenchmarkReferenceValid(domain.SourceReference{
		Bytes: len(input.SourceReference), Scheme: ref.Scheme, Hostname: ref.Hostname(), HasUser: ref.User != nil,
		RawQuery: ref.RawQuery, ForceQuery: ref.ForceQuery, Fragment: ref.Fragment, Opaque: ref.Opaque, Canonical: ref.String() == input.SourceReference,
	}) {
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
		if !domain.SeriesPointCountAllowed(len(input.Points)) {
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
		if json.Unmarshal(row, &point) != nil || !domain.SeriesPointAdmitted(point.Level, point.ObservedAt, fetched, previous, len(input.Points), false) {
			return nil, nil, "", errBenchmarkSeriesInput
		}
		at, _ := time.Parse(time.RFC3339Nano, point.ObservedAt)
		previous = at
		input.Points = append(input.Points, point)
	}
	if token, err := d.Token(); err != nil || token != json.Delim(']') || ensureJSONEOF(d) != nil || !domain.SeriesContentValid(len(input.Points), true) {
		return nil, nil, "", errBenchmarkSeriesInput
	}
	canonical, sha, err := orderJSONHash(input)
	if err != nil || len(canonical) > maxBodyBytes {
		return nil, nil, "", errBenchmarkSeriesInput
	}
	return &input, canonical, sha, nil
}

func SameBenchmarkDefinition(a, b BenchmarkSeriesInput) bool {
	return domain.SameBenchmarkDefinition(domain.BenchmarkDefinition{Currency: a.Currency, ReturnBasis: a.ReturnBasis, SourceReference: a.SourceReference, Sample: a.Sample}, domain.BenchmarkDefinition{Currency: b.Currency, ReturnBasis: b.ReturnBasis, SourceReference: b.SourceReference, Sample: b.Sample})
}

func BenchmarkRecordSHA(record benchmarkSeriesRecord) (string, error) {
	_, sha, err := orderJSONHash(struct {
		Sequence                                           int64
		BenchmarkID, SourceVersion, DataSHA256, RecordedAt string
	}{record.Sequence, record.Input.BenchmarkID, record.Input.SourceVersion, record.DataSHA256, record.RecordedAt})
	return sha, err
}
