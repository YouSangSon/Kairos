package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/application"
)

func (j *writeScope) Market() application.MarketJournal { return j }
func (j *writeScope) InsertMarketBar(ctx context.Context, bar PaperMarketBarObservation) (*PaperMarketBarObservation, error) {
	tx := j.tx
	recordJSON, recordSHA, err := orderJSONHash(bar)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_market_bar_observations(
		observation_id,schema_version,source,source_observation_id,input_data_sha256,symbol,venue,currency,interval,timezone,
		price_adjustment,open,high,low,close,volume,open_at,close_at,source_available_at,fetched_at,record_sha256,record_json,recorded_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		bar.ObservationID, bar.SchemaVersion, bar.Source, bar.SourceObservationID, bar.InputDataSHA256, bar.Symbol, bar.Venue, bar.Currency,
		bar.Interval, bar.Timezone, bar.PriceAdjustment, bar.Open, bar.High, bar.Low, bar.Close, bar.Volume, bar.OpenAt, bar.CloseAt,
		bar.SourceAvailableAt, bar.FetchedAt, recordSHA, string(recordJSON), bar.RecordedAt)
	if err != nil {
		return nil, err
	}
	return &bar, nil

}
func (j *writeScope) InsertSignal(ctx context.Context, event PaperSignalEvent) (*PaperSignalEvent, error) {
	tx := j.tx
	recordJSON, recordSHA, err := orderJSONHash(event)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_signal_events(
		event_id,schema_version,account_ref,paper_accounting_session_id,strategy_result_sha256,strategy_selection_event_id,
		execution_policy_sha256,signal_id,signal_bar_observation_id,data_sha256,symbol,target_quantity,data_as_of,generated_at,
		expires_at,market_observation_sequence_cutoff,record_sha256,record_json,recorded_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		event.EventID, event.SchemaVersion, event.AccountRef, event.PaperAccountingSessionID, event.StrategyResultSHA256,
		event.StrategySelectionEventID, event.ExecutionPolicySHA256, event.SignalID, event.SignalBarObservationID, event.DataSHA256,
		event.Symbol, event.TargetQuantity, event.DataAsOf, event.GeneratedAt, event.ExpiresAt, event.MarketObservationSequenceCutoff,
		recordSHA, string(recordJSON), event.RecordedAt)
	if err != nil {
		return nil, err
	}
	return &event, nil

}

func (j *writeScope) ProveMarketRecovery(ctx context.Context) (application.MarketRecoveryProof, error) {
	return replayPaperMarketRecovery(ctx, j.tx)
}
func (j *writeScope) LoadMarketBarBySource(ctx context.Context, source, id string) (*PaperMarketBarObservation, bool, error) {
	return loadPaperMarketBarBySource(ctx, j.tx, source, id)
}
func (j *writeScope) LoadMarketBarByID(ctx context.Context, id string) (*PaperMarketBarObservation, int64, error) {
	return loadPaperMarketBarByID(ctx, j.tx, id)
}
func (j *writeScope) LoadSignal(ctx context.Context, account, id string) (*PaperSignalEvent, bool, error) {
	return loadPaperSignalEvent(ctx, j.tx, account, id)
}
func (j *writeScope) ValidatePromotionSignalCutoff(ctx context.Context, result, selection, asOf string) error {
	return validatePaperPromotionSignalCutoff(ctx, j.tx, result, selection, asOf)
}
func (j *writeScope) SignalCutoff(ctx context.Context, bar PaperMarketBarObservation) (int64, int64, error) {
	cutoff, err := maxSequence(ctx, j.tx, "paper_market_bar_observations")
	if err != nil {
		return 0, 0, err
	}
	var latest int64
	err = j.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM paper_market_bar_observations WHERE source=? AND symbol=? AND venue=? AND interval=? AND timezone=? AND price_adjustment=? AND sequence<=?`, bar.Source, bar.Symbol, bar.Venue, bar.Interval, bar.Timezone, bar.PriceAdjustment, cutoff).Scan(&latest)
	return cutoff, latest, err
}
func (j *writeScope) LegacyOrderCount(ctx context.Context, account string) (int64, error) {
	var n int64
	err := j.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_idempotency WHERE mode='paper' AND account_ref=? AND json_extract(intent_json,'$.signal_schema_version') IN ('paper-signal.v1','paper-signal.v2')`, account).Scan(&n)
	return n, err
}
func (j *writeScope) SnapshotRange(ctx context.Context, first, last PaperMarketBarObservation) (map[string][]PaperMarketBarObservation, string, error) {
	return loadPaperSnapshotRange(ctx, j.tx, first, last)
}

func (j *writeScope) EvidenceArtifact(ctx context.Context, sha string) (string, error) {
	var raw string
	err := j.tx.QueryRowContext(ctx, `SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=?`, sha).Scan(&raw)
	return raw, err
}
func (j *writeScope) ProposalHistory(ctx context.Context, bar PaperMarketBarObservation, n int64) ([]application.ProposalBar, error) {
	rows, err := j.tx.QueryContext(ctx, `SELECT observation_id,close_at,close FROM paper_market_bar_observations WHERE source=? AND symbol=? AND venue=? AND interval=? AND timezone=? AND price_adjustment=? ORDER BY close_at DESC,sequence DESC LIMIT ?`, bar.Source, bar.Symbol, bar.Venue, bar.Interval, bar.Timezone, bar.PriceAdjustment, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bars []application.ProposalBar
	for rows.Next() {
		var b application.ProposalBar
		if err := rows.Scan(&b.ID, &b.CloseAt, &b.Close); err != nil {
			return nil, err
		}
		bars = append(bars, b)
	}
	return bars, rows.Err()
}
