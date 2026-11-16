package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	"strings"
	"time"
)

const benchmarkSeriesSchema = "benchmark-index-series.v1"

var errBenchmarkSeriesInput = errors.New("benchmark index series contract is invalid")

var errBenchmarkSeriesNotFound = c.ErrBenchmarkNotFound

type benchmarkSeriesRecord = application.BenchmarkRecord

type benchmarkSeriesRecoveryProof = application.SeriesProof

func requireBenchmarkSeriesSchema(ctx context.Context, q queryer) error {
	script, err := migrationFiles.ReadFile("migrations/023_benchmark_series_versions.sql")
	if err != nil {
		return err
	}
	objects := map[string]string{"benchmark_series_versions": "TABLE", "benchmark_series_latest_idx": "INDEX", "benchmark_series_versions_no_update": "TRIGGER", "benchmark_series_versions_no_delete": "TRIGGER", "benchmark_series_versions_insert_guard": "TRIGGER"}
	for name, kind := range objects {
		prefix := "CREATE " + kind + " " + name + " "
		expected := ""

		for _, block := range strings.Split(string(script), "\n\n") {
			definition := strings.Join(strings.Fields(block), " ")
			if strings.HasPrefix(definition, prefix) && strings.HasSuffix(definition, ";") {
				expected = strings.TrimSuffix(definition, ";")
				break
			}
		}
		if expected == "" {
			return errors.New("benchmark schema definition is incomplete")
		}
		var actual string
		if err := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=? AND type=? AND tbl_name='benchmark_series_versions'`, name, strings.ToLower(kind)).Scan(&actual); err != nil {
			return errors.New("benchmark schema object is missing")
		}

		if strings.Join(strings.Fields(actual), " ") != strings.Join(strings.Fields(expected), " ") {
			return errors.New("benchmark schema object changed")
		}
	}
	// Include the implicit UNIQUE index and reject connection-local triggers too.
	var auxiliaryObjects int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
        SELECT name FROM sqlite_master WHERE tbl_name='benchmark_series_versions' AND type IN ('index','trigger')
        UNION ALL
        SELECT name FROM sqlite_temp_master WHERE tbl_name='benchmark_series_versions' AND type IN ('index','trigger')
    )`).Scan(&auxiliaryObjects); err != nil || auxiliaryObjects != 5 {
		return errors.New("benchmark schema object inventory changed")
	}
	return nil
}

func replayBenchmarkSeries(ctx context.Context, q queryer, id, version string) (*benchmarkSeriesRecord, benchmarkSeriesRecoveryProof, error) {
	var proof benchmarkSeriesRecoveryProof
	if err := requireBenchmarkSeriesSchema(ctx, q); err != nil {
		return nil, proof, err
	}
	rows, err := q.QueryContext(ctx, `SELECT sequence,benchmark_id,source_version,series_json,series_sha256,recorded_at,record_sha256 FROM benchmark_series_versions ORDER BY sequence`)
	if err != nil {
		return nil, proof, err
	}
	defer rows.Close()
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	definitions := map[string]BenchmarkSeriesInput{}
	var selected *benchmarkSeriesRecord
	previousRecorded := time.Time{}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, proof, err
		}
		var r benchmarkSeriesRecord
		var benchmarkID, sourceVersion, payload, storedSHA string
		if err := rows.Scan(&r.Sequence, &benchmarkID, &sourceVersion, &payload, &r.DataSHA256, &r.RecordedAt, &storedSHA); err != nil {
			return nil, proof, err
		}
		input, canonical, sha, err := decodeBenchmarkSeries(ctx, []byte(payload))
		if err != nil || r.Sequence != int64(proof.Versions+1) || string(canonical) != payload || sha != r.DataSHA256 || benchmarkID != input.BenchmarkID || sourceVersion != input.SourceVersion || !canonicalUTCString(r.RecordedAt) {
			return nil, proof, errors.New("stored benchmark series is invalid")
		}
		r.Input = *input
		at, _ := time.Parse(time.RFC3339Nano, r.RecordedAt)
		fetched, _ := time.Parse(time.RFC3339Nano, input.FetchedAt)
		if at.Before(fetched) || (proof.Versions > 0 && at.Before(previousRecorded)) {
			return nil, proof, errors.New("benchmark record time is invalid")
		}
		previousRecorded = at
		if prior, ok := definitions[benchmarkID]; ok && !sameBenchmarkDefinition(prior, *input) {
			return nil, proof, errors.New("benchmark identity changed")
		}

		definition := *input
		definition.Points = nil
		definitions[benchmarkID] = definition
		want, err := benchmarkRecordSHA(r)
		if err != nil || want != storedSHA {
			return nil, proof, errors.New("benchmark record hash mismatch")
		}
		if err := encoder.Encode([]any{r.Sequence, storedSHA}); err != nil {
			return nil, proof, err
		}
		proof.Versions++
		proof.Points += len(input.Points)
		if benchmarkID == id && (version == "" || sourceVersion == version) {
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

func proveBenchmarkSeriesRecovery(ctx context.Context, q queryer) (benchmarkSeriesRecoveryProof, error) {
	_, proof, err := replayBenchmarkSeries(ctx, q, "", "")
	return proof, err
}
