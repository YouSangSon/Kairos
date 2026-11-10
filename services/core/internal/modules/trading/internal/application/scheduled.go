package application

import (
	"context"
	"errors"
	"time"
)

var errPaperRunnerNoAvailableClose = errors.New("scheduled paper runner has no available local close")
var errPaperRunnerIncompleteMarks = errors.New("paper performance marks are incomplete")
var errPaperRunnerPriorSelection = ErrPaperRunnerPriorSelection
var ErrPaperRunnerNoAvailableClose = errPaperRunnerNoAvailableClose
var ErrPaperRunnerIncompleteMarks = errPaperRunnerIncompleteMarks

const paperScheduledRunnerContentionTimeout = 5 * time.Second
const paperScheduledRunnerContentionPoll = 5 * time.Millisecond
const paperScheduledRunnerReleaseTimeout = 5 * time.Second
const paperRunnerStageTimeout = 20 * time.Second
const paperRunnerLoopHeartbeatInterval = 10 * time.Second
const paperRunnerLoopDueEveryHeartbeats = 6
const paperRunnerLoopReleaseTimeout = 5 * time.Second

func (s *UseCases) RunDuePaperPerformancePolicy(ctx context.Context, accountRef string) (*PaperScheduledRunResult, error) {
	if s == nil || s.journal == nil || s.now == nil || !orderAlias(accountRef, "account") {
		return nil, errors.New("scheduled paper runner is not configured")
	}
	_, completed, err := s.PrepareDuePaperPerformancePolicy(ctx, accountRef)
	if err != nil {
		return nil, err
	}
	if completed != nil {
		return completed, nil
	}
	claim, err := s.AcquirePaperRunnerLease(ctx, accountRef)
	if errors.Is(err, errPaperRunnerLeaseHeld) {
		claim, completed, err = s.WaitForPaperRunnerLeaseOrCompletion(ctx, accountRef)
	}
	if err != nil {
		_, completed, prepareErr := s.PrepareDuePaperPerformancePolicy(ctx, accountRef)
		if prepareErr != nil {
			return nil, errors.Join(err, prepareErr)
		}
		if completed != nil {
			return completed, nil
		}
		return nil, err
	}
	if completed != nil {
		return completed, nil
	}
	result, runErr := s.RunDuePaperPerformancePolicyWithClaim(ctx, accountRef, claim)
	releaseCtx, cancel := context.WithTimeout(context.Background(), paperScheduledRunnerReleaseTimeout)
	defer cancel()
	releaseErr := s.ReleasePaperRunnerLease(releaseCtx, claim)
	if runErr != nil || releaseErr != nil {
		return nil, errors.Join(runErr, releaseErr)
	}
	return result, nil
}
func (s *UseCases) PrepareDuePaperPerformancePolicy(ctx context.Context, accountRef string) (string, *PaperScheduledRunResult, error) {
	return s.PrepareDuePaperPerformancePolicyWithClaim(ctx, accountRef, nil)
}
func (s *UseCases) PrepareDuePaperPerformancePolicyWithClaim(ctx context.Context, accountRef string, claim *paperRunnerClaim) (string, *PaperScheduledRunResult, error) {
	result, err := inspectResult(ctx, s.journal, func(tx InspectionScope) (scheduledPreparation, error) {
		if claim != nil {
			if err := ValidatePaperRunnerLeaseTx(ctx, tx.Runner(), claim, accountRef, s.paperRunnerOwner, s.now()); err != nil {
				return scheduledPreparation{}, err
			}
		}
		if _, err := tx.Recovery().ProvePaperRunnerLeaseRecovery(ctx); err != nil {
			return scheduledPreparation{}, errors.Join(errors.New("scheduled paper runner lease recovery failed"), err)
		}
		now := s.now().UTC()
		asOf, err := tx.Evaluations().LatestAvailableClose(ctx, now)
		if err != nil {
			return scheduledPreparation{}, err
		}
		_, selection, selectedResult, err := tx.Evaluations().CurrentPaperPerformanceSelection(ctx)
		if err != nil {
			return scheduledPreparation{}, err
		}
		previous, err := tx.Evaluations().LatestPerformanceClose(ctx, accountRef)
		if err != nil {
			return scheduledPreparation{}, err
		}
		if previous != "" && previous < asOf {
			point, found, err := tx.Evaluations().LoadPaperPerformanceByKey(ctx, accountRef, previous)
			if err != nil || !found {
				return scheduledPreparation{}, errors.Join(errors.New("scheduled paper runner frontier is missing"), err)
			}
			if point.StrategySelectionEventID == selection {
				if _, complete, err := tx.Evaluations().ScheduledResult(ctx, accountRef, previous); err != nil {
					return scheduledPreparation{}, err
				} else if !complete {
					// A newer C3/D would make this committed loss policy stale.
					asOf = previous
				}
			}
		}
		if result, found, err := tx.Evaluations().ScheduledResult(ctx, accountRef, asOf); err != nil {
			return scheduledPreparation{}, err
		} else if found {
			return scheduledPreparation{asOf, result}, nil
		}
		if selectedResult == noStrategySelection {
			return scheduledPreparation{}, errors.New("current paper strategy is missing")
		}
		session, found, err := tx.Paper().LoadPaperAccountingSession(ctx, accountRef)
		if err != nil {
			return scheduledPreparation{}, err
		}
		if !found {
			return scheduledPreparation{}, errors.New("paper performance accounting session is missing")
		}
		if err := validatePaperPerformanceWindow(*session, asOf, now.Format(canonicalPaperTimeLayout)); err != nil {
			return scheduledPreparation{}, errors.Join(errPaperRunnerNoAvailableClose, err)
		}
		return scheduledPreparation{asOf, nil}, nil

	})
	return result.asOf, result.completed, err
}
func (s *UseCases) RunDuePaperPerformancePolicyWithClaim(ctx context.Context, accountRef string, claim *paperRunnerClaim) (*PaperScheduledRunResult, error) {
	if s == nil || s.journal == nil || s.now == nil || !orderAlias(accountRef, "account") || claim == nil {
		return nil, errors.New("scheduled paper runner claim is not configured")
	}
	asOf, completed, err := s.PrepareDuePaperPerformancePolicyWithClaim(ctx, accountRef, claim)
	if err != nil || completed != nil {
		return completed, err
	}
	return s.RunPaperPerformancePolicyAtCloseWithClaim(ctx, accountRef, asOf, claim)
}
func (s *UseCases) RunPaperPerformancePolicyAtCloseWithClaim(ctx context.Context, accountRef, asOf string, claim *paperRunnerClaim) (*PaperScheduledRunResult, error) {
	point, err := s.EvaluatePaperPerformanceWithClaim(ctx, accountRef, asOf, claim)
	if err != nil {
		return nil, err
	}
	window, err := s.EvaluatePaperStrategyPerformanceWithClaim(ctx, accountRef, point.StrategySelectionEventID, point.PerformanceID, claim)
	if err != nil {
		return nil, err
	}
	policy, err := s.ApplyPaperPerformancePolicyWithClaim(ctx, accountRef, window.StrategySelectionEventID, window.StrategyPerformanceID, claim)
	if err != nil {
		return nil, err
	}
	return ScheduledPaperRunResult(point.AsOf, *point, *window, *policy), nil
}
func (s *UseCases) WaitForPaperRunnerLeaseOrCompletion(ctx context.Context, accountRef string) (*paperRunnerClaim, *PaperScheduledRunResult, error) {
	waitCtx, cancel := context.WithTimeout(ctx, paperScheduledRunnerContentionTimeout)
	defer cancel()
	for {
		_, completed, err := s.PrepareDuePaperPerformancePolicy(waitCtx, accountRef)
		if err != nil {
			return nil, nil, err
		}
		if completed != nil {
			return nil, completed, nil
		}
		timer := time.NewTimer(paperScheduledRunnerContentionPoll)
		select {
		case <-waitCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, nil, waitCtx.Err()
		case <-timer.C:
		}
		claim, err := s.AcquirePaperRunnerLease(waitCtx, accountRef)
		if err == nil {
			return claim, nil, nil
		}
		if !errors.Is(err, errPaperRunnerLeaseHeld) {
			return nil, nil, err
		}
	}
}
func ScheduledPaperRunResult(asOf string, point PaperPerformanceEvent, window PaperStrategyPerformanceEvent, policy PaperPerformancePolicyEvent) *PaperScheduledRunResult {
	return &PaperScheduledRunResult{
		AccountRef:               policy.AccountRef,
		AsOf:                     asOf,
		PerformanceID:            point.PerformanceID,
		StrategyPerformanceID:    window.StrategyPerformanceID,
		PolicyEventID:            policy.PolicyEventID,
		Decision:                 policy.Decision,
		ReasonCode:               policy.ReasonCode,
		AutomaticHaltCount:       policy.AutomaticHaltCount,
		RollbackSelectionEventID: policy.RollbackSelectionEventID,
	}
}

type scheduledPreparation struct {
	asOf      string
	completed *PaperScheduledRunResult
}

func (s *UseCases) EvaluatePaperPerformanceWithClaim(ctx context.Context, accountRef, asOf string, claim *paperRunnerClaim) (*PaperPerformanceEvent, error) {
	if s == nil || s.journal == nil || s.now == nil || claim == nil || claim.AccountRef != accountRef || !orderAlias(accountRef, "account") {
		return nil, errors.New("paper performance runner claim is missing")
	}
	if _, ok := canonicalPaperTime(asOf); !ok {
		return nil, errors.New("paper performance as_of is invalid")
	}
	stageCtx, cancel := context.WithTimeout(ctx, paperRunnerStageTimeout)
	defer cancel()
	renewed, err := s.HeartbeatPaperRunnerLease(stageCtx, claim)
	if err != nil {
		return nil, err
	}
	*claim = *renewed
	return s.evaluatePaperPerformanceTx(stageCtx, accountRef, asOf, claim)
}
func (s *UseCases) EvaluatePaperStrategyPerformanceWithClaim(
	ctx context.Context,
	accountRef string,
	expectedSelectionEventID string,
	expectedLatestPerformanceID string,
	claim *paperRunnerClaim,
) (*PaperStrategyPerformanceEvent, error) {
	if s == nil || s.journal == nil || s.now == nil || claim == nil || claim.AccountRef != accountRef ||
		!orderAlias(accountRef, "account") || !safeOrderID(expectedSelectionEventID) || !safeOrderID(expectedLatestPerformanceID) {
		return nil, errors.New("paper strategy performance runner claim is missing")
	}
	stageCtx, cancel := context.WithTimeout(ctx, paperRunnerStageTimeout)
	defer cancel()
	renewed, err := s.HeartbeatPaperRunnerLease(stageCtx, claim)
	if err != nil {
		return nil, err
	}
	*claim = *renewed
	return s.evaluatePaperStrategyPerformanceTx(stageCtx, accountRef, expectedSelectionEventID, expectedLatestPerformanceID, claim)
}
func (s *UseCases) ApplyPaperPerformancePolicyWithClaim(ctx context.Context, accountRef, expectedSelectionEventID, expectedStrategyPerformanceID string, claim *paperRunnerClaim) (*PaperPerformancePolicyEvent, error) {
	if s == nil || s.journal == nil || s.now == nil || claim == nil || claim.AccountRef != accountRef ||
		!orderAlias(accountRef, "account") || !safeOrderID(expectedSelectionEventID) || !safeOrderID(expectedStrategyPerformanceID) {
		return nil, errors.New("paper performance policy runner claim is missing")
	}
	stageCtx, cancel := context.WithTimeout(ctx, paperRunnerStageTimeout)
	defer cancel()
	renewed, err := s.HeartbeatPaperRunnerLease(stageCtx, claim)
	if err != nil {
		return nil, err
	}
	*claim = *renewed
	return s.ApplyPaperPerformancePolicyTx(stageCtx, accountRef, expectedSelectionEventID, expectedStrategyPerformanceID, claim)
}
func (s *UseCases) RunPaperPerformanceLoop(ctx context.Context, accountRef string) error {
	return s.RunPaperPerformanceLoopWithWait(ctx, accountRef, WaitPaperRunnerLoop)
}
func (s *UseCases) RunPaperPerformanceLoopWithWait(ctx context.Context, accountRef string, wait func(context.Context, time.Duration) error) (resultErr error) {
	if ctx == nil || wait == nil {
		return errors.New("paper runner loop is not configured")
	}
	claim, err := s.AcquirePaperRunnerLease(ctx, accountRef)
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), paperRunnerLoopReleaseTimeout)
		defer cancel()
		releaseErr := s.ReleasePaperRunnerLease(cleanupCtx, claim)
		if errors.Is(resultErr, context.Canceled) {
			resultErr = releaseErr
		} else if releaseErr != nil {
			resultErr = errors.Join(resultErr, releaseErr)
		}
	}()

	if stop, err := s.RunPaperPerformanceLoopDue(ctx, accountRef, claim); err != nil {
		return err
	} else if stop {
		return nil
	}

	completedHeartbeats := 0
	for {
		if err := wait(ctx, paperRunnerLoopHeartbeatInterval); err != nil {
			return err
		}
		claim, err = s.HeartbeatPaperRunnerLease(ctx, claim)
		if err != nil {
			return err
		}
		completedHeartbeats++
		if completedHeartbeats%paperRunnerLoopDueEveryHeartbeats != 0 {
			continue
		}
		if stop, err := s.RunPaperPerformanceLoopDue(ctx, accountRef, claim); err != nil {
			return err
		} else if stop {
			return nil
		}
	}
}
func (s *UseCases) RunPaperPerformanceLoopDue(ctx context.Context, accountRef string, claim *paperRunnerClaim) (bool, error) {
	result, err := s.RunDuePaperPerformancePolicyWithClaim(ctx, accountRef, claim)
	if err != nil {
		if errors.Is(err, errPaperRunnerNoAvailableClose) || errors.Is(err, errPaperRunnerIncompleteMarks) || errors.Is(err, errPaperRunnerPriorSelection) {
			return false, nil
		}
		return false, err
	}
	return result != nil && result.Decision == "HALT_AND_ROLLBACK", nil
}
func WaitPaperRunnerLoop(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
