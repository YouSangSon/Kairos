package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	"regexp"
)

var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

type fxObservationRecoveryProof struct {
	SHA256       string
	Observations int
}

var errFXObservationNotFound = c.ErrFXNotFound

func loadFXObservationBySourceID(ctx context.Context, q orderQuerier, source, sourceID string) (*FXObservation, error) {
	var storedSHA string

	var observation FXObservation
	err := q.QueryRowContext(ctx, `SELECT observation_id,source,source_observation_id,base_currency,quote_currency,rate,observed_at,fetched_at,record_sha256,recorded_at
		FROM fx_observations WHERE source=? AND source_observation_id=?`, source, sourceID).Scan(
		&observation.ObservationID, &observation.Source, &observation.SourceObservationID, &observation.BaseCurrency,
		&observation.QuoteCurrency, &observation.Rate, &observation.ObservedAt, &observation.FetchedAt, &storedSHA, &observation.RecordedAt)
	if err != nil {
		return nil, err
	}
	if err := validateFXObservation(observation); err != nil {
		return nil, err
	}
	wantSHA, err := fxObservationSHA(observation)
	if err != nil || wantSHA != storedSHA {
		return nil, errors.New("FX observation metadata or hash mismatch")
	}
	return &observation, nil
}

func latestDirectFXObservation(ctx context.Context, q orderQuerier, source, base, quote, asOf string) (*FXObservation, error) {
	var storedSHA string

	if err := validateFXObservationQuery(source, base, quote, asOf); err != nil {
		return nil, errors.New("invalid direct FX observation query")
	}
	var observation FXObservation
	err := q.QueryRowContext(ctx, `SELECT observation_id,source,source_observation_id,base_currency,quote_currency,rate,observed_at,fetched_at,record_sha256,recorded_at
		FROM fx_observations WHERE source=? AND base_currency=? AND quote_currency=? AND observed_at<=? AND fetched_at<=?
		ORDER BY observed_at DESC,sequence DESC LIMIT 1`, source, base, quote, asOf, asOf).Scan(
		&observation.ObservationID, &observation.Source, &observation.SourceObservationID, &observation.BaseCurrency,
		&observation.QuoteCurrency, &observation.Rate, &observation.ObservedAt, &observation.FetchedAt, &storedSHA, &observation.RecordedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errFXObservationNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := validateFXObservation(observation); err != nil {
		return nil, err
	}
	wantSHA, err := fxObservationSHA(observation)
	if err != nil || wantSHA != storedSHA {
		return nil, errors.New("FX observation metadata or hash mismatch")
	}
	return &observation, nil
}

// ponytail: linear replay is sufficient for a personal FX series; add a projection only after measured volume makes it hot.
func replayFXObservations(ctx context.Context, q orderQuerier) ([]FXObservation, fxObservationRecoveryProof, error) {
	var storedSHA string

	rows, err := q.QueryContext(ctx, `SELECT sequence,observation_id,source,source_observation_id,base_currency,quote_currency,rate,observed_at,fetched_at,record_sha256,recorded_at
		FROM fx_observations ORDER BY sequence`)
	if err != nil {
		return nil, fxObservationRecoveryProof{}, err
	}
	defer rows.Close()
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	observations := []FXObservation{}
	for rows.Next() {
		var sequence int64
		var observation FXObservation
		if err := rows.Scan(&sequence, &observation.ObservationID, &observation.Source, &observation.SourceObservationID,
			&observation.BaseCurrency, &observation.QuoteCurrency, &observation.Rate, &observation.ObservedAt,
			&observation.FetchedAt, &storedSHA, &observation.RecordedAt); err != nil {
			return nil, fxObservationRecoveryProof{}, err
		}
		if err := validateFXObservation(observation); err != nil {
			return nil, fxObservationRecoveryProof{}, fmt.Errorf("FX observation %q metadata mismatch: %w", observation.ObservationID, err)
		}
		wantSHA, err := fxObservationSHA(observation)
		if err != nil || wantSHA != storedSHA {
			return nil, fxObservationRecoveryProof{}, fmt.Errorf("FX observation %q metadata or hash mismatch", observation.ObservationID)
		}
		if err := encoder.Encode([]any{sequence, observation.ObservationID, observation.Source, observation.SourceObservationID,
			observation.BaseCurrency, observation.QuoteCurrency, observation.Rate, observation.ObservedAt,
			observation.FetchedAt, storedSHA, observation.RecordedAt}); err != nil {
			return nil, fxObservationRecoveryProof{}, err
		}
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		return nil, fxObservationRecoveryProof{}, err
	}
	return observations, fxObservationRecoveryProof{SHA256: hex.EncodeToString(hash.Sum(nil)), Observations: len(observations)}, nil
}

func proveFXObservationRecovery(ctx context.Context, q orderQuerier) (fxObservationRecoveryProof, error) {
	_, proof, err := replayFXObservations(ctx, q)
	return proof, err
}
