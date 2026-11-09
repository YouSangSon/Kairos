package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type securityPriceObservationRecoveryProof struct {
	SHA256       string
	Observations int
}

var errSecurityPriceObservationNotFound = errors.New("security price observation not found")

func loadSecurityPriceObservationBySourceID(ctx context.Context, q orderQuerier, source, sourceID string) (*SecurityPriceObservation, error) {
	var storedSHA string

	var observation SecurityPriceObservation
	err := q.QueryRowContext(ctx, `SELECT observation_id,source,source_observation_id,instrument_id,symbol,venue,currency,price,price_adjustment,observed_at,fetched_at,record_sha256,recorded_at
		FROM security_price_observations WHERE source=? AND source_observation_id=?`, source, sourceID).Scan(
		&observation.ObservationID, &observation.Source, &observation.SourceObservationID, &observation.InstrumentID, &observation.Symbol,
		&observation.Venue, &observation.Currency, &observation.Price, &observation.PriceAdjustment, &observation.ObservedAt,
		&observation.FetchedAt, &storedSHA, &observation.RecordedAt)
	if err != nil {
		return nil, err
	}
	if err := validateSecurityPriceObservation(observation); err != nil {
		return nil, err
	}
	wantSHA, err := securityPriceObservationSHA(observation)
	if err != nil || wantSHA != storedSHA {
		return nil, errors.New("security price observation metadata or hash mismatch")
	}
	return &observation, nil
}

// ponytail: linear replay is sufficient for personal local-fixture prices; add a projection only after measured volume makes it hot.
func replaySecurityPriceObservations(ctx context.Context, q orderQuerier) ([]SecurityPriceObservation, securityPriceObservationRecoveryProof, error) {
	var storedSHA string

	rows, err := q.QueryContext(ctx, `SELECT sequence,observation_id,source,source_observation_id,instrument_id,symbol,venue,currency,price,price_adjustment,observed_at,fetched_at,record_sha256,recorded_at
		FROM security_price_observations ORDER BY sequence`)
	if err != nil {
		return nil, securityPriceObservationRecoveryProof{}, err
	}
	defer rows.Close()
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	observations := []SecurityPriceObservation{}
	for rows.Next() {
		var sequence int64
		var observation SecurityPriceObservation
		if err := rows.Scan(&sequence, &observation.ObservationID, &observation.Source, &observation.SourceObservationID,
			&observation.InstrumentID, &observation.Symbol, &observation.Venue, &observation.Currency, &observation.Price,
			&observation.PriceAdjustment, &observation.ObservedAt, &observation.FetchedAt, &storedSHA, &observation.RecordedAt); err != nil {
			return nil, securityPriceObservationRecoveryProof{}, err
		}
		if err := validateSecurityPriceObservation(observation); err != nil {
			return nil, securityPriceObservationRecoveryProof{}, fmt.Errorf("security price observation %q metadata mismatch: %w", observation.ObservationID, err)
		}
		wantSHA, err := securityPriceObservationSHA(observation)
		if err != nil || wantSHA != storedSHA {
			return nil, securityPriceObservationRecoveryProof{}, fmt.Errorf("security price observation %q metadata or hash mismatch", observation.ObservationID)
		}
		if err := encoder.Encode([]any{sequence, observation.ObservationID, observation.Source, observation.SourceObservationID,
			observation.InstrumentID, observation.Symbol, observation.Venue, observation.Currency, observation.Price, observation.PriceAdjustment,
			observation.ObservedAt, observation.FetchedAt, storedSHA, observation.RecordedAt}); err != nil {
			return nil, securityPriceObservationRecoveryProof{}, err
		}
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		return nil, securityPriceObservationRecoveryProof{}, err
	}
	return observations, securityPriceObservationRecoveryProof{SHA256: hex.EncodeToString(hash.Sum(nil)), Observations: len(observations)}, nil
}

func proveSecurityPriceObservationRecovery(ctx context.Context, q orderQuerier) (securityPriceObservationRecoveryProof, error) {
	_, proof, err := replaySecurityPriceObservations(ctx, q)
	return proof, err
}

func latestSecurityPriceObservation(ctx context.Context, q orderQuerier, source, instrumentID, symbol, venue, currency, adjustment, asOf string) (*SecurityPriceObservation, error) {
	if err := validateSecurityPriceObservationQuery(source, instrumentID, symbol, venue, currency, adjustment, asOf); err != nil {
		return nil, errors.New("invalid security price observation query")
	}
	cutoff, _ := time.Parse(time.RFC3339Nano, asOf)
	observations, _, err := replaySecurityPriceObservations(ctx, q)
	if err != nil {
		return nil, err
	}
	var selected *SecurityPriceObservation
	for i := range observations {
		observation := &observations[i]
		if observation.Source != source || observation.InstrumentID != instrumentID || observation.Symbol != symbol || observation.Venue != venue ||
			observation.Currency != currency || observation.PriceAdjustment != adjustment {
			continue
		}
		observed, _ := time.Parse(time.RFC3339Nano, observation.ObservedAt)
		fetched, _ := time.Parse(time.RFC3339Nano, observation.FetchedAt)
		recorded, _ := time.Parse(time.RFC3339Nano, observation.RecordedAt)
		if observed.After(cutoff) || fetched.After(cutoff) || recorded.After(cutoff) {
			continue
		}
		if selected == nil {
			selected = observation
			continue
		}
		selectedObserved, _ := time.Parse(time.RFC3339Nano, selected.ObservedAt)
		if observed.After(selectedObserved) || (observed.Equal(selectedObserved) && observation.ObservationID > selected.ObservationID) {
			selected = observation
		}
	}
	if selected == nil {
		return nil, errSecurityPriceObservationNotFound
	}
	return selected, nil
}
