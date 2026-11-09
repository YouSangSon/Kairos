package sqlite

import (
	"context"
	"database/sql"
	"errors"

	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
)

type Store struct {
	db        *sql.DB
	reader    *observation.ReadResolver
	writer    *observation.ImportResolver
	canonical func(string) string
}
type scope struct {
	q        queryer
	tx       *sql.Tx
	imported *observation.ImportHandle
}

func New(db *sql.DB, reader *observation.ReadResolver, writer *observation.ImportResolver, canonical func(string) string) *Store {
	return &Store{db, reader, writer, canonical}
}

func (s *Store) Write(ctx context.Context, work func(application.Scope) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&scope{q: databaseReader{tx, s.canonical}, tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Read(ctx context.Context, work func(application.Scope) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&scope{q: databaseReader{tx, s.canonical}}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ScopedImport(ctx context.Context, ref storage.ObservationRef, work func(application.Scope) error) error {
	handle, err := s.writer.Resolve(ref)
	if err != nil {
		return err
	}
	return work(&scope{q: scopedReader{handle.ReadHandle, s.canonical}, imported: handle})
}

func (s *scope) Schema(ctx context.Context) error { return requireSchemaContext(ctx, s.q) }
func (s *scope) FXBySource(ctx context.Context, source, id string) (*c.FXObservation, error) {
	v, err := loadFXObservationBySourceID(ctx, s.q, source, id)
	if errors.Is(err, sql.ErrNoRows) {
		err = application.ErrNotFound
	}
	return v, err
}
func (s *scope) PriceBySource(ctx context.Context, source, id string) (*c.SecurityPriceObservation, error) {
	v, err := loadSecurityPriceObservationBySourceID(ctx, s.q, source, id)
	if errors.Is(err, sql.ErrNoRows) {
		err = application.ErrNotFound
	}
	return v, err
}
func (s *scope) FXObservations(ctx context.Context) ([]c.FXObservation, application.ObservationProof, error) {
	values, proof, err := replayFXObservations(ctx, s.q)
	return values, application.ObservationProof{SHA256: proof.SHA256, Observations: proof.Observations}, err
}
func (s *scope) PriceObservations(ctx context.Context) ([]c.SecurityPriceObservation, application.ObservationProof, error) {
	values, proof, err := replaySecurityPriceObservations(ctx, s.q)
	return values, application.ObservationProof{SHA256: proof.SHA256, Observations: proof.Observations}, err
}
func (s *scope) Listings(ctx context.Context) (map[string]c.InstrumentListingEvent, application.ListingProof, error) {
	values, proof, err := replayInstrumentListingRegistry(ctx, s.q)
	return values, application.ListingProof{SHA256: proof.SHA256, Events: proof.Events, Active: proof.Active}, err
}
func (s *scope) ResolveListing(ctx context.Context, venue, symbol, currency string) (*c.InstrumentListingEvent, error) {
	return resolveInstrumentListing(ctx, s.q, venue, symbol, currency)
}
func (s *scope) PreviousListing(ctx context.Context, venue, symbol, currency string) (string, error) {
	var previous string
	err := s.q.QueryRowContext(ctx, `SELECT event_id FROM instrument_listing_events WHERE venue=? AND symbol=? AND currency=? ORDER BY sequence DESC LIMIT 1`, venue, symbol, currency).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return noInstrumentListingEvent, nil
	}
	return previous, err
}
func (s *scope) OwnerFX(ctx context.Context, base, quote, version string) (*application.OwnerFXRecord, application.SeriesProof, error) {
	return replayOwnerFXSeries(ctx, s.q, base, quote, version)
}
func (s *scope) OwnerPrice(ctx context.Context, admitted map[string]struct{}, instrument, currency, version string) (*application.OwnerPriceRecord, application.SeriesProof, error) {
	return replayOwnerPriceSeries(ctx, s.q, admitted, instrument, currency, version)
}
func (s *scope) Benchmark(ctx context.Context, id, version string) (*application.BenchmarkRecord, application.SeriesProof, error) {
	return replayBenchmarkSeries(ctx, s.q, id, version)
}
func (s *scope) LatestBenchmark(ctx context.Context, id string) (*c.BenchmarkSeriesInput, error) {
	var result c.BenchmarkSeriesInput
	err := s.q.QueryRowContext(ctx, `SELECT json_extract(series_json,'$.currency'),json_extract(series_json,'$.return_basis'),json_extract(series_json,'$.source_reference'),json_extract(series_json,'$.sample') FROM benchmark_series_versions WHERE benchmark_id=? ORDER BY sequence DESC LIMIT 1`, id).Scan(&result.Currency, &result.ReturnBasis, &result.SourceReference, &result.Sample)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &result, err
}
func (s *scope) LastRecorded(ctx context.Context, kind string) (string, error) {
	var query string
	switch kind {
	case "security_price":
		query = `SELECT recorded_at FROM owner_price_series_versions ORDER BY sequence DESC LIMIT 1`
	case "fx":
		query = `SELECT recorded_at FROM owner_fx_series_versions ORDER BY sequence DESC LIMIT 1`
	case "benchmark":
		query = `SELECT recorded_at FROM benchmark_series_versions ORDER BY sequence DESC LIMIT 1`
	default:
		return "", errors.New("owner data clock kind is invalid")
	}
	var recorded string
	err := s.q.QueryRowContext(ctx, query).Scan(&recorded)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return recorded, err
}

func (s *scope) insert(ctx context.Context, query string, args ...any) error {
	var result sql.Result
	var err error
	if s.imported != nil {
		result, err = s.imported.Execute(ctx, query, args...)
	} else if s.tx != nil {
		result, err = s.tx.ExecContext(ctx, query, args...)
	} else {
		return errors.New("market write scope unavailable")
	}
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return errors.New("market record was not stored exactly once")
	}
	return nil
}

func (s *scope) InsertFX(ctx context.Context, v c.FXObservation, hash string) error {
	return s.insert(ctx, `INSERT INTO fx_observations(observation_id,source,source_observation_id,base_currency,quote_currency,rate,observed_at,fetched_at,record_sha256,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, v.ObservationID, v.Source, v.SourceObservationID, v.BaseCurrency, v.QuoteCurrency, v.Rate, v.ObservedAt, v.FetchedAt, hash, v.RecordedAt)
}
func (s *scope) InsertPrice(ctx context.Context, v c.SecurityPriceObservation, hash string) error {
	return s.insert(ctx, `INSERT INTO security_price_observations(observation_id,source,source_observation_id,instrument_id,symbol,venue,currency,price,price_adjustment,observed_at,fetched_at,record_sha256,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ObservationID, v.Source, v.SourceObservationID, v.InstrumentID, v.Symbol, v.Venue, v.Currency, v.Price, v.PriceAdjustment, v.ObservedAt, v.FetchedAt, hash, v.RecordedAt)
}
func (s *scope) InsertListing(ctx context.Context, v c.InstrumentListingEvent, hash string) error {
	return s.insert(ctx, `INSERT INTO instrument_listing_events(event_id,event_type,authority,instrument_id,venue,symbol,currency,expected_previous_event_id,record_sha256,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, v.EventID, v.EventType, v.Authority, v.InstrumentID, v.Venue, v.Symbol, v.Currency, v.ExpectedPreviousEventID, hash, v.RecordedAt)
}
func (s *scope) InsertOwnerFX(ctx context.Context, r application.OwnerFXRecord, raw []byte, hash string) error {
	return s.insert(ctx, `INSERT INTO owner_fx_series_versions(sequence,base_currency,quote_currency,source_version,series_json,series_sha256,recorded_at,record_sha256) VALUES(?,?,?,?,?,?,?,?)`, r.Sequence, r.Input.BaseCurrency, r.Input.QuoteCurrency, r.Input.SourceVersion, string(raw), r.DataSHA256, r.RecordedAt, hash)
}
func (s *scope) InsertOwnerPrice(ctx context.Context, r application.OwnerPriceRecord, raw []byte, hash string) error {
	return s.insert(ctx, `INSERT INTO owner_price_series_versions(sequence,instrument_id,currency,source_version,series_json,series_sha256,recorded_at,record_sha256) VALUES(?,?,?,?,?,?,?,?)`, r.Sequence, r.Input.InstrumentID, r.Input.Currency, r.Input.SourceVersion, string(raw), r.DataSHA256, r.RecordedAt, hash)
}
func (s *scope) InsertBenchmark(ctx context.Context, r application.BenchmarkRecord, raw []byte, hash string) error {
	return s.insert(ctx, `INSERT INTO benchmark_series_versions(sequence,benchmark_id,source_version,series_json,series_sha256,recorded_at,record_sha256) VALUES(?,?,?,?,?,?,?)`, r.Sequence, r.Input.BenchmarkID, r.Input.SourceVersion, string(raw), r.DataSHA256, r.RecordedAt, hash)
}
