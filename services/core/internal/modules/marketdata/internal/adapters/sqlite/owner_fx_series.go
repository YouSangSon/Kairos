package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	"omni-folio/services/core/internal/modules/marketdata/internal/domain"
	"strings"
	"time"
)

const ownerFXSeriesSchema = "owner-fx-series.v1"

const ownerFXSource = "owner_declared"

var errOwnerFXInput = errors.New("owner FX series contract is invalid")

type ownerFXSeriesRecord = application.OwnerFXRecord

type ownerFXSeriesRecoveryProof = application.SeriesProof

func requireOwnerFXSeriesSchema(ctx context.Context, q queryer) error {
	ownerSQL, err := migrationFiles.ReadFile("migrations/024_owner_fx_series_versions.sql")
	if err != nil {
		return err
	}
	fixtureSQL, err := migrationFiles.ReadFile("migrations/010_fx_observations.sql")
	if err != nil {
		return err
	}
	for _, object := range []struct{ name, kind, table, script string }{
		{"owner_fx_series_versions", "TABLE", "owner_fx_series_versions", string(ownerSQL)},
		{"owner_fx_series_latest_idx", "INDEX", "owner_fx_series_versions", string(ownerSQL)},
		{"owner_fx_series_versions_no_update", "TRIGGER", "owner_fx_series_versions", string(ownerSQL)},
		{"owner_fx_series_versions_no_delete", "TRIGGER", "owner_fx_series_versions", string(ownerSQL)},
		{"owner_fx_series_versions_insert_guard", "TRIGGER", "owner_fx_series_versions", string(ownerSQL)},
		{"fx_observations_owner_pair_guard", "TRIGGER", "fx_observations", string(ownerSQL)},
		{"fx_observations", "TABLE", "fx_observations", string(fixtureSQL)},
		{"fx_observations_latest_idx", "INDEX", "fx_observations", string(fixtureSQL)},
		{"fx_observations_no_update", "TRIGGER", "fx_observations", string(fixtureSQL)},
		{"fx_observations_no_delete", "TRIGGER", "fx_observations", string(fixtureSQL)},
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
			return errors.New("owner FX physical schema changed")
		}
	}
	for table, expected := range map[string]int{"owner_fx_series_versions": 5, "fx_observations": 7} {
		var count int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
            SELECT name FROM sqlite_master WHERE tbl_name=? AND type IN ('index','trigger')
            UNION ALL SELECT name FROM sqlite_temp_master WHERE tbl_name=? AND type IN ('index','trigger')
        )`, table, table).Scan(&count); err != nil || count != expected {
			return errors.New("owner FX schema inventory changed")
		}
	}
	var mixed bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM owner_fx_series_versions owner JOIN fx_observations fixture ON owner.base_currency=fixture.base_currency AND owner.quote_currency=fixture.quote_currency)`).Scan(&mixed); err != nil || mixed {
		return errors.New("FX pair sources are mixed")
	}
	return nil
}

func replayOwnerFXSeries(ctx context.Context, q queryer, base, quote, version string) (*ownerFXSeriesRecord, ownerFXSeriesRecoveryProof, error) {
	var proof ownerFXSeriesRecoveryProof
	if err := requireOwnerFXSeriesSchema(ctx, q); err != nil {
		return nil, proof, err
	}
	rows, err := q.QueryContext(ctx, `SELECT sequence,base_currency,quote_currency,source_version,series_json,series_sha256,recorded_at,record_sha256 FROM owner_fx_series_versions ORDER BY sequence`)
	if err != nil {
		return nil, proof, err
	}
	defer rows.Close()
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	var selected *ownerFXSeriesRecord
	var previous time.Time
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, proof, err
		}
		var r ownerFXSeriesRecord
		var storedBase, storedQuote, storedVersion, payload, storedSHA string
		if err := rows.Scan(&r.Sequence, &storedBase, &storedQuote, &storedVersion, &payload, &r.DataSHA256, &r.RecordedAt, &storedSHA); err != nil {
			return nil, proof, errors.New("stored owner FX metadata is invalid")
		}
		input, canonical, sha, err := decodeOwnerFXSeries(ctx, []byte(payload))
		if err != nil || r.Sequence != int64(proof.Versions+1) || string(canonical) != payload || sha != r.DataSHA256 || storedBase != input.BaseCurrency || storedQuote != input.QuoteCurrency || storedVersion != input.SourceVersion || !canonicalUTCString(r.RecordedAt) {
			return nil, proof, errors.New("stored owner FX series is invalid")
		}
		r.Input = *input
		at, _ := time.Parse(time.RFC3339Nano, r.RecordedAt)
		fetched, _ := time.Parse(time.RFC3339Nano, input.FetchedAt)
		if !domain.SeriesRecordTimeValid(at, fetched, previous, proof.Versions > 0) {
			return nil, proof, errors.New("owner FX record time is invalid")
		}
		previous = at
		want, err := ownerFXRecordSHA(r)
		if err != nil || want != storedSHA {
			return nil, proof, errors.New("owner FX record hash mismatch")
		}
		if err := encoder.Encode([]any{r.Sequence, storedSHA}); err != nil {
			return nil, proof, err
		}
		proof.Versions++
		proof.Points += len(input.Points)
		if base == input.BaseCurrency && quote == input.QuoteCurrency && version == input.SourceVersion {
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

func proveOwnerFXSeriesRecovery(ctx context.Context, q queryer) (ownerFXSeriesRecoveryProof, error) {
	_, proof, err := replayOwnerFXSeries(ctx, q, "", "", "")
	return proof, err
}

func valuationFXObservations(ctx context.Context, q queryer, knownAt time.Time) ([]FXObservation, error) {
	observations, _, err := replayFXObservations(ctx, q)
	if err != nil {
		return nil, err
	}
	if _, err := proveOwnerFXSeriesRecovery(ctx, q); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT o.sequence,o.series_json,o.recorded_at FROM owner_fx_series_versions o
        JOIN (SELECT base_currency,quote_currency,MAX(sequence) AS sequence FROM owner_fx_series_versions
            WHERE rtrim(recorded_at,'Z')<=rtrim(?,'Z') GROUP BY base_currency,quote_currency) latest ON latest.sequence=o.sequence
        ORDER BY o.sequence`, knownAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sequence int64
		var payload, recorded string
		if err := rows.Scan(&sequence, &payload, &recorded); err != nil {
			return nil, err
		}
		input, _, _, err := decodeOwnerFXSeries(ctx, []byte(payload))
		if err != nil {
			return nil, err
		}
		for i, point := range input.Points {
			id := fmt.Sprintf("owner_fx_%d_%d", sequence, i)
			observations = append(observations, FXObservation{ObservationID: id, Source: ownerFXSource, SourceObservationID: id, BaseCurrency: input.BaseCurrency, QuoteCurrency: input.QuoteCurrency, Rate: point.Rate, ObservedAt: point.ObservedAt, FetchedAt: input.FetchedAt, RecordedAt: recorded, OwnerVersion: input.SourceVersion, OwnerSample: input.Sample})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return observations, nil
}
