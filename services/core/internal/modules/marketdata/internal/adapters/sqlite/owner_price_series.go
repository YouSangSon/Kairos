package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	"omni-folio/services/core/internal/modules/marketdata/internal/domain"
	"strings"
	"time"
)

const ownerPriceSeriesSchema = "owner-price-series.v1"

const ownerPriceSource = "owner_declared"

const ownerPriceAdjustment = "unadjusted"

var errOwnerPriceInput = errors.New("owner price series contract is invalid")

type ownerPriceSeriesRecord = application.OwnerPriceRecord

type ownerPriceSeriesRecoveryProof = application.SeriesProof

func requireOwnerPriceSeriesSchema(ctx context.Context, q queryer) error {
	ownerSQL, err := migrationFiles.ReadFile("migrations/025_owner_price_series_versions.sql")
	if err != nil {
		return err
	}
	fixtureSQL, err := migrationFiles.ReadFile("migrations/012_kiwoom_security_price_observations.sql")
	if err != nil {
		return err
	}
	for _, object := range []struct{ name, kind, table, script string }{
		{"owner_price_series_versions", "TABLE", "owner_price_series_versions", string(ownerSQL)},
		{"owner_price_series_latest_idx", "INDEX", "owner_price_series_versions", string(ownerSQL)},
		{"owner_price_series_versions_no_update", "TRIGGER", "owner_price_series_versions", string(ownerSQL)},
		{"owner_price_series_versions_no_delete", "TRIGGER", "owner_price_series_versions", string(ownerSQL)},
		{"owner_price_series_versions_insert_guard", "TRIGGER", "owner_price_series_versions", string(ownerSQL)},
		{"security_price_observations_owner_guard", "TRIGGER", "security_price_observations", string(ownerSQL)},
		{"security_price_observations", "TABLE", "security_price_observations", string(fixtureSQL)},
		{"security_price_observations_latest_idx", "INDEX", "security_price_observations", string(fixtureSQL)},
		{"security_price_observations_no_update", "TRIGGER", "security_price_observations", string(fixtureSQL)},
		{"security_price_observations_no_delete", "TRIGGER", "security_price_observations", string(fixtureSQL)},
	} {
		expected := ""

		for _, block := range strings.Split(object.script, "\n\n") {
			definition := strings.Join(strings.Fields(block), " ")
			if strings.HasPrefix(definition, "CREATE "+object.kind+" "+object.name+" ") && strings.HasSuffix(definition, ";") {
				expected = strings.TrimSuffix(definition, ";")
				break
			}
		}
		var actual string
		if expected == "" || q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=? AND type=? AND tbl_name=?`, object.name, strings.ToLower(object.kind), object.table).Scan(&actual) != nil || strings.Join(strings.Fields(actual), " ") != expected {
			return errors.New("owner price physical schema changed")
		}
	}
	for table, expected := range map[string]int{"owner_price_series_versions": 5, "security_price_observations": 7} {
		var count int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
            SELECT name FROM sqlite_master WHERE tbl_name=? AND type IN ('index','trigger')
            UNION ALL SELECT name FROM sqlite_temp_master WHERE tbl_name=? AND type IN ('index','trigger')
        )`, table, table).Scan(&count); err != nil || count != expected {
			return errors.New("owner price schema inventory changed")
		}
	}
	var mixed bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM owner_price_series_versions owner JOIN security_price_observations fixture ON owner.instrument_id=fixture.instrument_id AND owner.currency=fixture.currency)`).Scan(&mixed); err != nil || mixed {
		return errors.New("price sources are mixed")
	}
	return nil
}

func replayOwnerPriceSeries(ctx context.Context, q queryer, admitted map[string]struct{}, instrument, currency, version string) (*ownerPriceSeriesRecord, ownerPriceSeriesRecoveryProof, error) {
	var proof ownerPriceSeriesRecoveryProof
	if err := requireOwnerPriceSeriesSchema(ctx, q); err != nil {
		return nil, proof, err
	}
	var present bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM owner_price_series_versions)`).Scan(&present); err != nil {
		return nil, proof, err
	}
	if present && admitted == nil {
		return nil, proof, errors.New("owner price ledger identities unavailable")
	}

	rows, err := q.QueryContext(ctx, `SELECT sequence,instrument_id,currency,source_version,series_json,series_sha256,recorded_at,record_sha256 FROM owner_price_series_versions ORDER BY sequence`)
	if err != nil {
		return nil, proof, err
	}
	defer rows.Close()
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	var selected *ownerPriceSeriesRecord
	var previous time.Time
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, proof, err
		}
		var r ownerPriceSeriesRecord
		var storedBase, storedQuote, storedVersion, payload, storedSHA string
		if err := rows.Scan(&r.Sequence, &storedBase, &storedQuote, &storedVersion, &payload, &r.DataSHA256, &r.RecordedAt, &storedSHA); err != nil {
			return nil, proof, errors.New("stored owner price metadata is invalid")
		}
		input, canonical, sha, err := decodeOwnerPriceSeries(ctx, []byte(payload), q.CanonicalInstrumentID)
		if err != nil || r.Sequence != int64(proof.Versions+1) || string(canonical) != payload || sha != r.DataSHA256 || storedBase != input.InstrumentID || storedQuote != input.Currency || storedVersion != input.SourceVersion || !canonicalUTCString(r.RecordedAt) {
			return nil, proof, errors.New("stored owner price series is invalid")
		}
		if !domain.OwnerPriceAdmitted(admitted, input.InstrumentID, input.Currency) {
			return nil, proof, errors.New("owner price identity has no admitted ledger instrument")
		}
		r.Input = *input
		at, _ := time.Parse(time.RFC3339Nano, r.RecordedAt)
		fetched, _ := time.Parse(time.RFC3339Nano, input.FetchedAt)
		if !domain.SeriesRecordTimeValid(at, fetched, previous, proof.Versions > 0) {
			return nil, proof, errors.New("owner price record time is invalid")
		}
		previous = at
		want, err := ownerPriceRecordSHA(r)
		if err != nil || want != storedSHA {
			return nil, proof, errors.New("owner price record hash mismatch")
		}
		if err := encoder.Encode([]any{r.Sequence, storedSHA}); err != nil {
			return nil, proof, err
		}
		proof.Versions++
		proof.Points += len(input.Points)
		if instrument == input.InstrumentID && currency == input.Currency && version == input.SourceVersion {
			copy := r
			selected = &copy
		}
	}
	if err := rows.Err(); err != nil {
		return nil, proof, err
	}
	proof.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return selected, proof, nil
}

func proveOwnerPriceSeriesRecovery(ctx context.Context, q queryer, admitted map[string]struct{}) (ownerPriceSeriesRecoveryProof, error) {
	_, proof, err := replayOwnerPriceSeries(ctx, q, admitted, "", "", "")
	return proof, err
}
