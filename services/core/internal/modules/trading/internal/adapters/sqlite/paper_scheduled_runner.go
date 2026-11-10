package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"time"
)

type PaperScheduledRunResult = contracts.PaperScheduledRunResult

var (
	errPaperRunnerNoAvailableClose = application.ErrPaperRunnerNoAvailableClose
	errPaperRunnerIncompleteMarks  = application.ErrPaperRunnerIncompleteMarks
	errPaperRunnerPriorSelection   = application.ErrPaperRunnerPriorSelection
)

const (
	paperScheduledRunnerContentionTimeout = 5 * time.Second
	paperScheduledRunnerContentionPoll    = 5 * time.Millisecond
	paperScheduledRunnerReleaseTimeout    = 5 * time.Second
	paperRunnerStageTimeout               = 20 * time.Second
)

func (s *Store) RunDuePaperPerformancePolicy(ctx context.Context, accountRef string) (*PaperScheduledRunResult, error) {
	return s.business().RunDuePaperPerformancePolicy(ctx, accountRef)
}

func (s *Store) prepareDuePaperPerformancePolicy(ctx context.Context, accountRef string) (string, *PaperScheduledRunResult, error) {
	return s.business().PrepareDuePaperPerformancePolicy(ctx, accountRef)
}

func (s *Store) prepareDuePaperPerformancePolicyWithClaim(ctx context.Context, accountRef string, claim *paperRunnerClaim) (string, *PaperScheduledRunResult, error) {
	return s.business().PrepareDuePaperPerformancePolicyWithClaim(ctx, accountRef, claim)
}

func (s *Store) runDuePaperPerformancePolicyWithClaim(ctx context.Context, accountRef string, claim *paperRunnerClaim) (*PaperScheduledRunResult, error) {
	return s.business().RunDuePaperPerformancePolicyWithClaim(ctx, accountRef, claim)
}

func (s *Store) runPaperPerformancePolicyAtCloseWithClaim(ctx context.Context, accountRef, asOf string, claim *paperRunnerClaim) (*PaperScheduledRunResult, error) {
	return s.business().RunPaperPerformancePolicyAtCloseWithClaim(ctx, accountRef, asOf, claim)
}

func (s *Store) waitForPaperRunnerLeaseOrCompletion(ctx context.Context, accountRef string) (*paperRunnerClaim, *PaperScheduledRunResult, error) {
	return s.business().WaitForPaperRunnerLeaseOrCompletion(ctx, accountRef)
}

func latestAvailablePaperClose(ctx context.Context, q orderQuerier, now time.Time) (string, error) {
	recordedAt := now.UTC().Format(canonicalPaperTimeLayout)
	var asOf string
	err := q.QueryRowContext(ctx, `SELECT close_at FROM paper_market_bar_observations
		WHERE source='paper_fixture' AND venue='KRX' AND currency='KRW' AND interval='1d'
		  AND timezone='Asia/Seoul' AND price_adjustment='unspecified'
		  AND source_available_at<=? AND fetched_at<=? AND recorded_at<=?
		GROUP BY close_at ORDER BY close_at DESC LIMIT 1`, recordedAt, recordedAt, recordedAt).Scan(&asOf)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errPaperRunnerNoAvailableClose
	}
	if err != nil {
		return "", err
	}
	if !canonicalPaperTimes(asOf) {
		return "", errors.New("scheduled paper runner close time is invalid")
	}
	return asOf, nil
}

func loadScheduledPaperRunResult(ctx context.Context, q orderQuerier, accountRef, asOf string) (*PaperScheduledRunResult, bool, error) {
	point, found, err := loadPaperPerformanceByKey(ctx, q, accountRef, asOf)
	if err != nil || !found {
		return nil, false, err
	}
	window, found, err := loadPaperStrategyPerformanceByKey(ctx, q, accountRef, point.StrategySelectionEventID, point.PerformanceID)
	if err != nil || !found {
		return nil, false, err
	}
	policy, found, err := loadPaperPerformancePolicyByKey(ctx, q, accountRef, window.StrategySelectionEventID, window.StrategyPerformanceID)
	if err != nil || !found {
		return nil, false, err
	}
	return scheduledPaperRunResult(asOf, *point, *window, *policy), true, nil
}

func scheduledPaperRunResult(asOf string, point PaperPerformanceEvent, window PaperStrategyPerformanceEvent, policy PaperPerformancePolicyEvent) *PaperScheduledRunResult {
	return application.ScheduledPaperRunResult(asOf, point, window, policy)
}
