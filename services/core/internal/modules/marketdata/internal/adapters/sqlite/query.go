package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"omni-folio/services/core/internal/codec"
	"omni-folio/services/core/internal/storage/observation"
	"omni-folio/services/core/migrations"
)

const maxBodyBytes = 1 << 20
const latestSchema = 27
const ownerDataPriceKind = "security_price"
const ownerDataFXKind = "fx"
const ownerDataBenchmarkKind = "benchmark"

var errOwnerDataRejected = errors.New("owner data input is not admissible")
var errOwnerDataConflict = errors.New("owner data input conflicts with stored identity or source")
var migrationFiles = migrations.Files
var safeOrderID = codec.SafeID
var canonicalUTCString = codec.CanonicalUTC
var canonicalDecimal = codec.CanonicalDecimal
var orderJSONHash = codec.JSONHash
var validJSONSurrogates = codec.ValidJSONSurrogates
var ensureJSONEOF = codec.EnsureJSONEOF

type queryer interface {
	CanonicalInstrumentID(string) string
	QueryContext(context.Context, string, ...any) (observation.Rows, error)
	QueryRowContext(context.Context, string, ...any) observation.Scanner
}
type orderQuerier = queryer

type rawQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type databaseReader struct {
	q         rawQueryer
	canonical func(string) string
}

func (r databaseReader) QueryContext(ctx context.Context, q string, args ...any) (observation.Rows, error) {
	return r.q.QueryContext(ctx, q, args...)
}
func (r databaseReader) QueryRowContext(ctx context.Context, q string, args ...any) observation.Scanner {
	return r.q.QueryRowContext(ctx, q, args...)
}

type scopedReader struct {
	handle    *observation.ReadHandle
	canonical func(string) string
}

func (r scopedReader) QueryContext(ctx context.Context, q string, args ...any) (observation.Rows, error) {
	return r.handle.QueryContext(ctx, q, args...)
}
func (r scopedReader) QueryRowContext(ctx context.Context, q string, args ...any) observation.Scanner {
	return r.handle.QueryOne(ctx, q, args...)
}

func stringPointer(value string) *string               { return &value }
func ownerPriceKey(instrument, currency string) string { return instrument + "\x00" + currency }

func requireSchemaContext(ctx context.Context, q queryer) error {
	var version int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	if version != latestSchema {
		return errors.New("database schema is not current")
	}
	return nil
}

func ownerDataRecordTime(ctx context.Context, q queryer, kind string, clock func() time.Time, fetched string) (string, error) {
	var query string
	switch kind {
	case ownerDataPriceKind:
		query = `SELECT recorded_at FROM owner_price_series_versions ORDER BY sequence DESC LIMIT 1`
	case ownerDataFXKind:
		query = `SELECT recorded_at FROM owner_fx_series_versions ORDER BY sequence DESC LIMIT 1`
	case ownerDataBenchmarkKind:
		query = `SELECT recorded_at FROM benchmark_series_versions ORDER BY sequence DESC LIMIT 1`
	default:
		return "", errors.New("owner data clock kind is invalid")
	}
	var previous string
	err := q.QueryRowContext(ctx, query).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	now := clock().UTC()
	recorded := now.Format(time.RFC3339Nano)
	if !canonicalUTCString(recorded) {
		return "", errors.New("owner data record clock is invalid")
	}
	if previous != "" {
		last, err := time.Parse(time.RFC3339Nano, previous)
		if err != nil || !canonicalUTCString(previous) || now.Before(last) {
			return "", errors.New("owner data record clock moved backwards")
		}
	}
	captured, err := time.Parse(time.RFC3339Nano, fetched)
	if err != nil || !canonicalUTCString(fetched) {
		return "", errors.New("owner data capture time is invalid")
	}
	if now.Before(captured) {
		return "", errOwnerDataRejected
	}
	return recorded, nil
}

const marketDataAdjustmentUnspecified = "unspecified"

var kiwoomStockPattern = regexp.MustCompile(`^[0-9]{6}$`)

func (r databaseReader) CanonicalInstrumentID(s string) string { return r.canonical(s) }
func (r scopedReader) CanonicalInstrumentID(s string) string   { return r.canonical(s) }
