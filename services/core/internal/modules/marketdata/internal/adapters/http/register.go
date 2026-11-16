package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	recordcodec "omni-folio/services/core/internal/codec"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	marketcodec "omni-folio/services/core/internal/modules/marketdata/internal/adapters/codec"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	"omni-folio/services/core/internal/transport/httpjson"
	"regexp"
	"time"
)

const maxBodyBytes = 1 << 20
const ownerDataPreviewResponseLimit = 6*maxBodyBytes + 16*1024
const ownerPriceSeriesSchema = "owner-price-series.v1"
const ownerFXSeriesSchema = "owner-fx-series.v1"
const benchmarkSeriesSchema = "benchmark-index-series.v1"
const ownerDataPriceKind = "security_price"
const ownerDataFXKind = "fx"
const ownerDataBenchmarkKind = "benchmark"
const marketDataAdjustmentUnspecified = "unspecified"

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}
type appError struct {
	Status int
	Body   APIError
}
type MarketDataSeries = c.MarketDataSeries
type MarketDataBar = c.MarketDataBar
type api struct {
	imports      c.Imports
	observations c.Observations
	ownerPrice   c.OwnerPriceImports
	marketData   func() c.CandleSource
	canonical    func(string) string
	now          func() time.Time
}

func Register(mux *http.ServeMux, imports c.Imports, observations c.Observations, ownerPrice c.OwnerPriceImports, candles func() c.CandleSource, canonical func(string) string, now func() time.Time) {
	a := &api{imports: imports, observations: observations, ownerPrice: ownerPrice, marketData: candles, canonical: canonical, now: now}
	mux.HandleFunc("GET /v1/market-data/candles", a.handleMarketDataCandles)
	mux.HandleFunc("GET /v1/market-data/fx/latest", a.handleLatestFXObservation)
	mux.HandleFunc("POST /v1/market-data/imports/preview", a.handleOwnerDataPreview)
	mux.HandleFunc("POST /v1/market-data/imports/apply", a.handleOwnerDataApply)
}
func writeJSON(w http.ResponseWriter, status int, body any) { httpjson.Write(w, status, body) }
func writeError(w http.ResponseWriter, err *appError)       { httpjson.Write(w, err.Status, err.Body) }
func internalError(err error) *appError {
	log.Printf("internal error: %v", err)
	return &appError{500, APIError{Code: "internal_error", Message: "internal server error"}}
}

type OwnerDataImportPreview struct {
	Kind    string                     `json:"kind"`
	Summary map[string]json.RawMessage `json:"summary"`
	Input   string                     `json:"input"`
}

type OwnerDataImportResult struct {
	Kind    string `json:"kind"`
	Receipt any    `json:"receipt"`
}

func ownerDataKind(raw []byte) (string, error) {
	var header struct {
		SchemaVersion string `json:"schema_version"`
	}
	if len(raw) == 0 || len(raw) > maxBodyBytes || json.Unmarshal(raw, &header) != nil {
		return "", c.ErrRejected
	}
	switch header.SchemaVersion {
	case ownerPriceSeriesSchema:
		return ownerDataPriceKind, nil
	case ownerFXSeriesSchema:
		return ownerDataFXKind, nil
	case benchmarkSeriesSchema:
		return ownerDataBenchmarkKind, nil
	default:
		return "", c.ErrRejected
	}
}

func (s *api) previewOwnerData(ctx context.Context, raw []byte) (*OwnerDataImportPreview, error) {
	kind, err := ownerDataKind(raw)
	if err != nil {
		return nil, err
	}
	var receipt any
	switch kind {
	case ownerDataPriceKind:
		i, _, sha, err := marketcodec.DecodeOwnerPriceSeries(ctx, raw, s.canonical)
		if err != nil {
			return nil, err
		}
		receipt = application.OwnerPriceReceipt(&application.OwnerPriceRecord{Input: *i, DataSHA256: sha})
	case ownerDataFXKind:
		i, _, sha, err := marketcodec.DecodeOwnerFXSeries(ctx, raw)
		if err != nil {
			return nil, err
		}
		receipt = application.OwnerFXReceipt(&application.OwnerFXRecord{Input: *i, DataSHA256: sha})
	case ownerDataBenchmarkKind:
		i, _, sha, err := marketcodec.DecodeBenchmarkSeries(ctx, raw)
		if err != nil {
			return nil, err
		}
		receipt = application.BenchmarkReceipt(&application.BenchmarkRecord{Input: *i, DataSHA256: sha})
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	var summary map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &summary); err != nil {
		return nil, err
	}

	delete(summary, "storage_check")
	delete(summary, "recorded_at")
	return &OwnerDataImportPreview{Kind: kind, Summary: summary, Input: string(raw)}, nil
}

func ownerDataImportError(err error) *appError {
	switch {
	case errors.Is(err, c.ErrRejected), errors.Is(err, c.ErrOwnerPriceInput), errors.Is(err, c.ErrOwnerFXInput), errors.Is(err, c.ErrBenchmarkInput):
		return &appError{Status: http.StatusBadRequest, Body: APIError{Code: "owner_data_invalid", Message: "owner data input does not meet the declared contract"}}
	case errors.Is(err, c.ErrConflict):
		return &appError{Status: http.StatusConflict, Body: APIError{Code: "owner_data_conflict", Message: "owner data identity or source conflicts with stored data"}}
	default:

		return &appError{Status: http.StatusInternalServerError, Body: APIError{Code: "owner_data_unavailable", Message: "owner data storage could not be confirmed; retry the same input"}}
	}
}

func readOwnerDataBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, c.ErrRejected
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, c.ErrRejected
	}
	return raw, nil
}

func (s *api) handleOwnerDataPreview(w http.ResponseWriter, r *http.Request) {
	raw, err := readOwnerDataBody(w, r)
	if err != nil {
		writeError(w, ownerDataImportError(err))
		return
	}
	preview, err := s.previewOwnerData(r.Context(), raw)
	if err != nil {
		writeError(w, ownerDataImportError(err))
		return
	}
	encoded, err := json.Marshal(preview)
	if err != nil || len(encoded)+1 > ownerDataPreviewResponseLimit {
		writeError(w, ownerDataImportError(errors.New("owner review response exceeds its bound")))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
}

func (s *api) handleOwnerDataApply(w http.ResponseWriter, r *http.Request) {
	raw, err := readOwnerDataBody(w, r)
	if err != nil {
		writeError(w, ownerDataImportError(err))
		return
	}
	kind, err := ownerDataKind(raw)
	if err != nil {
		writeError(w, ownerDataImportError(err))
		return
	}
	var receipt any
	switch kind {
	case ownerDataPriceKind:
		receipt, err = s.ownerPrice.ImportOwnerPrice(r.Context(), raw)
	case ownerDataFXKind:
		receipt, err = s.imports.ImportOwnerFX(r.Context(), raw)
	case ownerDataBenchmarkKind:
		receipt, err = s.imports.ImportBenchmark(r.Context(), raw)
	}
	if err != nil {
		writeError(w, ownerDataImportError(err))
		return
	}
	writeJSON(w, http.StatusOK, OwnerDataImportResult{Kind: kind, Receipt: receipt})
}

func validMarketDataIdentity(series *MarketDataSeries) bool {
	if series.InstrumentID == nil && series.Currency == nil {
		return true
	}
	return series.InstrumentID != nil && series.Currency != nil &&
		safeOrderID(*series.InstrumentID) && currencyCodePattern.MatchString(*series.Currency)
}

func (s *api) handleMarketDataCandles(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	for key := range query {
		if key != "symbol" && key != "interval" {
			writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: "unknown query parameter " + key, Field: key}})
			return
		}
	}
	for _, key := range []string{"symbol", "interval"} {
		values := query[key]
		if len(values) == 0 || values[0] == "" {
			writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: key + " query parameter is required", Field: key}})
			return
		}
		if len(values) != 1 {
			writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: key + " query parameter must appear exactly once", Field: key}})
			return
		}
	}
	var source c.CandleSource
	if s.marketData != nil {
		source = s.marketData()
	}
	if source == nil {
		writeError(w, &appError{Status: http.StatusServiceUnavailable, Body: APIError{Code: "market_data_unavailable", Message: "market data is unavailable"}})
		return
	}
	series, err := source.Candles(r.Context(), query.Get("symbol"), query.Get("interval"))
	if errors.Is(err, c.ErrMarketDataNotFound) {
		writeError(w, &appError{Status: http.StatusNotFound, Body: APIError{Code: "market_data_not_found", Message: "market data was not found"}})
		return
	}
	if err != nil {
		writeError(w, internalError(err))
		return
	}
	if series == nil || len(series.Bars) == 0 {
		writeError(w, internalError(errors.New("market data port returned no bars")))
		return
	}
	if series.Symbol != query.Get("symbol") || series.Interval != query.Get("interval") {
		writeError(w, internalError(errors.New("market data port returned a mismatched series")))
		return
	}
	if series.PriceAdjustment != marketDataAdjustmentUnspecified {
		writeError(w, internalError(errors.New("local market data returned an unsupported price adjustment")))
		return
	}
	if !validMarketDataIdentity(series) {
		writeError(w, internalError(errors.New("local market data returned an invalid identity")))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		InstrumentID    *string         `json:"instrument_id"`
		Currency        *string         `json:"currency"`
		BarTimeBasis    string          `json:"bar_time_basis"`
		Symbol          string          `json:"symbol"`
		Venue           string          `json:"venue"`
		Timezone        string          `json:"timezone"`
		Interval        string          `json:"interval"`
		PriceAdjustment string          `json:"price_adjustment"`
		Source          string          `json:"source"`
		Sample          bool            `json:"sample"`
		State           string          `json:"state"`
		SourceAsOf      string          `json:"source_as_of"`
		FetchedAt       string          `json:"fetched_at"`
		Issues          []APIError      `json:"issues"`
		Bars            []MarketDataBar `json:"bars"`
	}{series.InstrumentID, series.Currency, "unspecified", series.Symbol, series.Venue, series.Timezone, series.Interval, series.PriceAdjustment, "local_fixture", true, "stale", series.Bars[len(series.Bars)-1].At, s.now().UTC().Format(time.RFC3339Nano), []APIError{{Code: "sample_data", Message: "market data is a local sample and not live"}}, series.Bars})
}

func (s *api) handleLatestFXObservation(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: "query parameters are invalid"}})
		return
	}
	for key := range query {
		if key != "source" && key != "base_currency" && key != "quote_currency" && key != "as_of" {
			writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: "unknown query parameter " + key, Field: key}})
			return
		}
	}
	for _, key := range []string{"source", "base_currency", "quote_currency", "as_of"} {
		if len(query[key]) != 1 || query[key][0] == "" {
			writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: key + " query parameter must appear exactly once", Field: key}})
			return
		}
	}
	if err := marketcodec.ValidateFXObservationQuery(query.Get("source"), query.Get("base_currency"), query.Get("quote_currency"), query.Get("as_of")); err != nil {
		writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: "direct FX observation query values are invalid"}})
		return
	}
	observation, err := s.observations.LatestFX(r.Context(), query.Get("source"), query.Get("base_currency"), query.Get("quote_currency"), query.Get("as_of"))
	if errors.Is(err, c.ErrFXNotFound) {
		writeError(w, &appError{Status: http.StatusNotFound, Body: APIError{Code: "fx_observation_not_found", Message: "direct FX observation was not found"}})
		return
	}
	if err != nil {
		writeError(w, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		ObservationID string     `json:"observation_id"`
		Source        string     `json:"source"`
		BaseCurrency  string     `json:"base_currency"`
		QuoteCurrency string     `json:"quote_currency"`
		Rate          string     `json:"rate"`
		ObservedAt    string     `json:"observed_at"`
		FetchedAt     string     `json:"fetched_at"`
		RecordedAt    string     `json:"recorded_at"`
		Sample        bool       `json:"sample"`
		State         string     `json:"state"`
		Issues        []APIError `json:"issues"`
	}{observation.ObservationID, observation.Source, observation.BaseCurrency, observation.QuoteCurrency,
		observation.Rate, observation.ObservedAt, observation.FetchedAt, observation.RecordedAt, true, "stale",
		[]APIError{{Code: "sample_data", Message: "FX rate is a local sample and not live"}}})
}

var safeOrderID = recordcodec.SafeID
var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)
