package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
)

func (j *writeScope) Evaluations() application.EvaluationsJournal { return j }
func (j *writeScope) Cutoffs(ctx context.Context) (int64, int64, error) {
	order, err := maxSequence(ctx, j.tx, "order_events")
	if err != nil {
		return 0, 0, err
	}
	market, err := maxSequence(ctx, j.tx, "paper_market_bar_observations")
	return order, market, err
}
func (j *writeScope) ReplayPaperAccountingStateWithCutoff(ctx context.Context, n int64, asOf string) (map[string]application.PaperAccountState, error) {
	return replayPaperAccountingStateWithCutoff(ctx, j.tx, &paperAccountingCutoff{OrderSequence: n, AsOf: asOf})
}
func (j *writeScope) OperationalSnapshot(ctx context.Context, accountRef, resultSHA256, selectionEventID string) (paperOperationalSnapshot, error) {
	return derivePaperOperationalSnapshot(ctx, j.tx, accountRef, resultSHA256, selectionEventID)
}
func (j *writeScope) LoadEvaluation(ctx context.Context, evaluationID string) (*PaperEvaluationEvent, bool, error) {
	return loadPaperEvaluationByID(ctx, j.tx, evaluationID)
}
func (j *writeScope) LatestEvaluationID(ctx context.Context, accountRef, selectionEventID string) (string, error) {
	return latestPaperEvaluationID(ctx, j.tx, accountRef, selectionEventID)
}
func (j *writeScope) InsertEvaluation(ctx context.Context, event PaperEvaluationEvent) error {
	return insertPaperEvaluationEvent(ctx, j.tx, event)
}
func (j *writeScope) ProvePaperPerformanceRecovery(ctx context.Context) (paperPerformanceRecoveryProof, error) {
	return provePaperPerformanceRecovery(ctx, j.tx)
}
func (j *writeScope) LoadPaperPerformanceByKey(ctx context.Context, accountRef, asOf string) (*PaperPerformanceEvent, bool, error) {
	return loadPaperPerformanceByKey(ctx, j.tx, accountRef, asOf)
}
func (j *writeScope) RejectLegacyPaperPerformanceAccount(ctx context.Context, accountRef string) error {
	return rejectLegacyPaperPerformanceAccount(ctx, j.tx, accountRef)
}
func (j *writeScope) CurrentPaperPerformanceSelection(ctx context.Context) (int64, string, string, error) {
	return currentPaperPerformanceSelection(ctx, j.tx)
}
func (j *writeScope) DerivePaperPerformanceMarks(ctx context.Context, state paperAccountState, startingCash, asOf, recordedAt string, marketCutoff int64) ([]paperPerformanceMark, paperdomain.Valuation, error) {
	return derivePaperPerformanceMarks(ctx, j.tx, state, startingCash, asOf, recordedAt, marketCutoff)
}
func (j *writeScope) PriorPaperPerformanceSeries(ctx context.Context, accountRef, sessionID, asOf string) (string, []string, error) {
	return priorPaperPerformanceSeries(ctx, j.tx, accountRef, sessionID, asOf)
}
func (j *writeScope) InsertPaperPerformanceEvent(ctx context.Context, event PaperPerformanceEvent) error {
	return insertPaperPerformanceEvent(ctx, j.tx, event)
}
func (j *writeScope) ProvePaperStrategyPerformanceRecovery(ctx context.Context) (paperStrategyPerformanceRecoveryProof, error) {
	return provePaperStrategyPerformanceRecovery(ctx, j.tx)
}
func (j *writeScope) LoadLatestPaperPerformance(ctx context.Context, accountRef string) (PaperPerformanceEvent, int64, bool, error) {
	return loadLatestPaperPerformance(ctx, j.tx, accountRef)
}
func (j *writeScope) LoadPaperStrategyPerformanceByKey(ctx context.Context, accountRef, selectionEventID,
	latestPerformanceID string,
) (*PaperStrategyPerformanceEvent, bool, error) {
	return loadPaperStrategyPerformanceByKey(ctx, j.tx, accountRef, selectionEventID, latestPerformanceID)
}
func (j *writeScope) LoadPaperStrategyPerformanceWindow(ctx context.Context, accountRef, sessionID, selectionEventID,
	selectedResult, latestPerformanceID string, latestSequence int64,
) (paperStrategyPerformanceWindow, error) {
	return loadPaperStrategyPerformanceWindow(ctx, j.tx, accountRef, sessionID, selectionEventID, selectedResult, latestPerformanceID, latestSequence)
}
func (j *writeScope) LatestPaperStrategyPerformanceID(ctx context.Context, accountRef, sessionID, selectionEventID string) (string, error) {
	return latestPaperStrategyPerformanceID(ctx, j.tx, accountRef, sessionID, selectionEventID)
}
func (j *writeScope) InsertPaperStrategyPerformanceEvent(ctx context.Context, event PaperStrategyPerformanceEvent) error {
	return insertPaperStrategyPerformanceEvent(ctx, j.tx, event)
}
