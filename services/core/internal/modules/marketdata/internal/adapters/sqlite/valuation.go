package sqlite

import (
	"context"
	"fmt"
	"time"
)

func valuationPriceObservations(ctx context.Context, q queryer, admitted map[string]struct{}, knownAt time.Time) ([]SecurityPriceObservation, error) {
	observations, _, err := replaySecurityPriceObservations(ctx, q)
	if err != nil {
		return nil, err
	}
	if _, err := proveOwnerPriceSeriesRecovery(ctx, q, admitted); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT o.sequence,o.series_json,o.recorded_at FROM owner_price_series_versions o
        JOIN (SELECT instrument_id,currency,MAX(sequence) AS sequence FROM owner_price_series_versions
            WHERE rtrim(recorded_at,'Z')<=rtrim(?,'Z') GROUP BY instrument_id,currency) latest ON latest.sequence=o.sequence
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
		input, _, _, err := decodeOwnerPriceSeries(ctx, []byte(payload), q.CanonicalInstrumentID)
		if err != nil {
			return nil, err
		}
		for i, p := range input.Points {
			id := fmt.Sprintf("owner_price_%d_%d", sequence, i)
			observations = append(observations, SecurityPriceObservation{ObservationID: id, Source: ownerPriceSource, SourceObservationID: id, InstrumentID: input.InstrumentID, Symbol: input.Symbol, Venue: input.Venue, Currency: input.Currency, Price: p.Price, PriceAdjustment: input.PriceAdjustment, ObservedAt: p.ObservedAt, FetchedAt: input.FetchedAt, RecordedAt: recorded, OwnerVersion: input.SourceVersion, OwnerSample: input.Sample})
		}
	}
	return observations, rows.Err()
}
