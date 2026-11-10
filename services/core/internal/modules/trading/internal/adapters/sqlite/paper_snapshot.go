package sqlite

import (
	"context"
	"errors"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/adapters/files"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
)

var paperSnapshotCSVHeader = []string{
	"bar_at", "symbol", "venue", "timezone", "interval", "open", "high", "low", "close", "volume",
	"open_at", "source_available_at", "fetched_at",
}

type PaperSnapshotImport = contracts.PaperSnapshotImport

func (s *Store) ImportPaperSnapshot(ctx context.Context, raw []byte) (*PaperSnapshotImport, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("paper snapshot importer is not configured")
	}
	return s.business().ImportPaperSnapshot(ctx, raw)
}

var parsePaperSnapshot = files.ParsePaperSnapshot

var paperSnapshotBar = files.PaperSnapshotBar

func loadPaperSnapshotRange(ctx context.Context, tx *transaction, first, last PaperMarketBarObservation) (map[string][]PaperMarketBarObservation, string, error) {
	var latest string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(close_at),'') FROM paper_market_bar_observations
		WHERE source=? AND symbol=? AND venue=? AND interval=? AND timezone=? AND price_adjustment=?`,
		first.Source, first.Symbol, first.Venue, first.Interval, first.Timezone, first.PriceAdjustment).Scan(&latest); err != nil {
		return nil, "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT observation_id,schema_version,source,source_observation_id,input_data_sha256,symbol,venue,currency,
		interval,timezone,price_adjustment,open,high,low,close,volume,open_at,close_at,source_available_at,fetched_at,recorded_at
		FROM paper_market_bar_observations WHERE source=? AND symbol=? AND venue=? AND interval=? AND timezone=? AND price_adjustment=?
		AND close_at>=? AND close_at<=? ORDER BY close_at,sequence`, first.Source, first.Symbol, first.Venue, first.Interval,
		first.Timezone, first.PriceAdjustment, first.CloseAt, last.CloseAt)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	result := make(map[string][]PaperMarketBarObservation)
	for rows.Next() {
		var bar PaperMarketBarObservation
		if err := rows.Scan(&bar.ObservationID, &bar.SchemaVersion, &bar.Source, &bar.SourceObservationID, &bar.InputDataSHA256,
			&bar.Symbol, &bar.Venue, &bar.Currency, &bar.Interval, &bar.Timezone, &bar.PriceAdjustment, &bar.Open, &bar.High, &bar.Low,
			&bar.Close, &bar.Volume, &bar.OpenAt, &bar.CloseAt, &bar.SourceAvailableAt, &bar.FetchedAt, &bar.RecordedAt); err != nil {
			return nil, "", err
		}
		result[bar.CloseAt] = append(result[bar.CloseAt], bar)
	}
	return result, latest, rows.Err()
}

func samePaperSnapshotBar(stored, supplied PaperMarketBarObservation) bool {
	return domain.SamePaperSnapshotBar(domain.PaperMarketBarObservation(stored), domain.PaperMarketBarObservation(supplied))
}
