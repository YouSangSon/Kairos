package sqlite

import (
	"context"
	"time"

	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
)

func (s *Store) HasOwnerPrices(ctx context.Context, ref storage.ObservationRef) (bool, error) {
	handle, err := s.reader.Resolve(ref)
	if err != nil {
		return false, err
	}
	var present bool
	err = handle.QueryOne(ctx, `SELECT EXISTS(SELECT 1 FROM owner_price_series_versions)`).Scan(&present)
	return present, err
}

func (s *Store) ReadInputs(ctx context.Context, ref storage.ObservationRef, admitted map[string]struct{}, query c.ValuationQuery) (c.ValuationInputs, error) {
	result := c.ValuationInputs{FX: []c.FXObservation{}, Prices: []c.SecurityPriceObservation{}, Benchmarks: []c.BenchmarkVersion{}}
	handle, err := s.reader.Resolve(ref)
	if err != nil {
		return result, err
	}
	result.Cutoff, err = s.reader.Cutoff(ref)
	if err != nil {
		return result, err
	}
	q := scopedReader{handle, s.canonical}
	if query.FX {
		result.FX, err = valuationFXObservations(ctx, q, result.Cutoff)
		if err != nil {
			return result, err
		}
	}
	if query.Prices {
		result.Prices, err = valuationPriceObservations(ctx, q, admitted, result.Cutoff)
		if err != nil {
			return result, err
		}
	}
	if query.Benchmarks {
		proof, err := proveBenchmarkSeriesRecovery(ctx, q)
		if err != nil {
			return result, err
		}
		result.BenchmarkVersionCount = proof.Versions
		rows, err := q.QueryContext(ctx, `SELECT b.sequence,b.series_json,b.series_sha256,b.recorded_at FROM benchmark_series_versions b JOIN (SELECT benchmark_id,MAX(sequence) AS sequence FROM benchmark_series_versions WHERE rtrim(recorded_at,'Z')<=rtrim(?,'Z') GROUP BY benchmark_id) latest ON latest.sequence=b.sequence ORDER BY b.benchmark_id`, result.Cutoff.Format(time.RFC3339Nano))
		if err != nil {
			return result, err
		}
		defer rows.Close()
		for rows.Next() {
			var record c.BenchmarkVersion
			var raw string
			if err := rows.Scan(&record.Sequence, &raw, &record.DataSHA256, &record.RecordedAt); err != nil {
				return result, err
			}
			input, _, _, err := decodeBenchmarkSeries(ctx, []byte(raw))
			if err != nil {
				return result, err
			}
			record.Input = *input
			result.Benchmarks = append(result.Benchmarks, record)
		}
		if err := rows.Err(); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *Store) Verify(ctx context.Context, ref storage.ObservationRef, admitted map[string]struct{}, query c.BackupQuery) (c.RecoveryProof, error) {
	var result c.RecoveryProof
	handle, err := s.reader.Resolve(ref)
	if err != nil {
		return result, err
	}
	q := scopedReader{handle, s.canonical}
	if query.Schema >= 10 {
		proof, err := proveFXObservationRecovery(ctx, q)
		if err != nil {
			return result, err
		}
		result.FXSHA256, result.FXObservations = proof.SHA256, proof.Observations
	}
	if query.Schema >= 11 {
		proof, err := proveSecurityPriceObservationRecovery(ctx, q)
		if err != nil {
			return result, err
		}
		result.PricesSHA256, result.PriceObservations = proof.SHA256, proof.Observations
	}
	if query.Schema >= 13 {
		proof, err := proveInstrumentListingRecovery(ctx, q)
		if err != nil {
			return result, err
		}
		result.ListingsSHA256, result.ListingEvents, result.ActiveListings = proof.SHA256, proof.Events, proof.Active
	}
	if query.Schema >= 23 {
		proof, err := proveBenchmarkSeriesRecovery(ctx, q)
		if err != nil {
			return result, err
		}
		result.BenchmarkSHA256, result.BenchmarkVersions, result.BenchmarkPoints = proof.SHA256, proof.Versions, proof.Points
	}
	if query.Schema >= 24 {
		proof, err := proveOwnerFXSeriesRecovery(ctx, q)
		if err != nil {
			return result, err
		}
		result.OwnerFXSHA256, result.OwnerFXVersions, result.OwnerFXPoints = proof.SHA256, proof.Versions, proof.Points
	}
	if query.Schema >= 25 {
		proof, err := proveOwnerPriceSeriesRecovery(ctx, q, admitted)
		if err != nil {
			return result, err
		}
		result.OwnerPriceSHA256, result.OwnerPriceVersions, result.OwnerPricePoints = proof.SHA256, proof.Versions, proof.Points
	}
	return result, nil
}

func (s *Store) ProveBenchmark(ctx context.Context, ref storage.ObservationRef) error {
	handle, err := s.reader.Resolve(ref)
	if err != nil {
		return err
	}
	_, err = proveBenchmarkSeriesRecovery(ctx, scopedReader{handle, s.canonical})
	return err
}
func (s *Store) ProveOwnerFX(ctx context.Context, ref storage.ObservationRef) error {
	handle, err := s.reader.Resolve(ref)
	if err != nil {
		return err
	}
	_, err = proveOwnerFXSeriesRecovery(ctx, scopedReader{handle, s.canonical})
	return err
}
func (s *Store) ProveOwnerPrice(ctx context.Context, ref storage.ObservationRef, admitted map[string]struct{}) error {
	handle, err := s.reader.Resolve(ref)
	if err != nil {
		return err
	}
	_, err = proveOwnerPriceSeriesRecovery(ctx, scopedReader{handle, s.canonical}, admitted)
	return err
}
