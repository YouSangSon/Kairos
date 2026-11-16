package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
	"time"
)

const paperEvaluationSchema = "paper-operational-evaluation.v1"
const paperEvaluationPolicy = "paper-operational-safety.v1"
const paperEvaluationInsufficient = "INSUFFICIENT"
const paperEvaluationPass = "PASS"
const paperEvaluationDegraded = "DEGRADED"
const paperPerformanceSchema = "paper-performance-evaluation.v1"
const paperPerformancePolicy = "paper-performance-account-v1"
const paperStrategyPerformanceSchema = "paper-strategy-window-performance.v1"
const paperStrategyPerformancePolicy = "paper-strategy-window-performance-v1"

type OperationalSnapshot = domain.OperationalSnapshot
type paperOperationalSnapshot = OperationalSnapshot

var PaperOperationalDecision = domain.PaperOperationalDecision

func PaperEvaluationID(event PaperEvaluationEvent) string {
	return domain.PaperEvaluationID(domain.PaperEvaluationEvent(event))
}

func SamePaperEvaluationSnapshot(left *PaperEvaluationEvent, right PaperEvaluationEvent) bool {
	return domain.SamePaperEvaluationSnapshot((*domain.PaperEvaluationEvent)(left), domain.PaperEvaluationEvent(right))
}

func ValidatePaperEvaluationEvent(event PaperEvaluationEvent) error {
	return domain.ValidatePaperEvaluationEvent(domain.PaperEvaluationEvent(event))
}

var PaperPerformanceID = domain.PaperPerformanceID

func ValidatePaperPerformanceWindow(session PaperAccountingSession, asOf, recordedAt string) error {
	return domain.ValidatePaperPerformanceWindow(session.RecordedAt, asOf, recordedAt)
}

func PaperStrategyPerformanceID(event PaperStrategyPerformanceEvent) string {
	return domain.PaperStrategyPerformanceID(domain.PaperStrategyPerformanceEvent(event))
}

var paperOperationalDecision = PaperOperationalDecision
var paperEvaluationID = PaperEvaluationID
var samePaperEvaluationSnapshot = SamePaperEvaluationSnapshot
var paperPerformanceID = PaperPerformanceID
var validatePaperPerformanceWindow = ValidatePaperPerformanceWindow
var paperStrategyPerformanceID = PaperStrategyPerformanceID

func (s *UseCases) EvaluatePaperOperations(ctx context.Context, accountRef, resultSHA256, selectionEventID string) (*PaperEvaluationEvent, error) {
	if s == nil || s.journal == nil || !orderAlias(accountRef, "account") ||
		!strategySHA256Pattern.MatchString(resultSHA256) || !safeOrderID(selectionEventID) {
		return nil, errors.New("paper evaluation identity is invalid")
	}
	return decideResult(ctx, s.journal, func(tx WriteScope) (*PaperEvaluationEvent, bool, error) {
		if _, err := tx.Recovery().ProveOrderRecovery(ctx); err != nil {
			return nil, false, fmt.Errorf("paper evaluation order recovery: %w", err)
		}
		registry, err := tx.Selections().ReplayStrategyRegistry(ctx)
		if err != nil {
			return nil, false, err
		}
		if err := domain.ValidateEvaluationSelection(registry.SelectedResultSHA256, registry.CurrentEventID, resultSHA256, selectionEventID); err != nil {
			return nil, false, err
		}
		snapshot, err := tx.Evaluations().OperationalSnapshot(ctx, accountRef, resultSHA256, selectionEventID)
		if err != nil {
			return nil, false, err
		}
		event := PaperEvaluationEvent{
			SchemaVersion: paperEvaluationSchema, PolicyVersion: paperEvaluationPolicy, AccountRef: accountRef,
			StrategyResultSHA256: resultSHA256, StrategySelectionEventID: selectionEventID,
			PaperOrderStateSHA256: snapshot.SHA256, OrderCount: snapshot.Orders,
			TerminalOrderCount: snapshot.Terminal, ActiveOrderCount: snapshot.Active, PendingActionCount: snapshot.Pending,
		}
		event.Decision, event.ReasonCode = paperOperationalDecision(snapshot)
		event.EvaluationID = paperEvaluationID(event)
		stored, found, err := tx.Evaluations().LoadEvaluation(ctx, event.EvaluationID)
		if err != nil {
			return nil, false, err
		}
		if found {
			if !samePaperEvaluationSnapshot(stored, event) {
				return nil, false, errors.New("paper evaluation identity is already bound to different evidence")
			}
			return stored, false, nil
		}
		event.ExpectedPreviousEvaluationID, err = tx.Evaluations().LatestEvaluationID(ctx, accountRef, selectionEventID)
		if err != nil {
			return nil, false, err
		}
		event.RecordedAt = s.now().UTC().Format(time.RFC3339Nano)
		if err := tx.Evaluations().InsertEvaluation(ctx, event); err != nil {
			return nil, false, err
		}
		return &event, true, nil
	})
}
func (s *UseCases) evaluatePaperPerformanceTx(ctx context.Context, accountRef, asOf string, claim *paperRunnerClaim) (*PaperPerformanceEvent, error) {
	if s == nil || s.journal == nil || s.now == nil || !orderAlias(accountRef, "account") {
		return nil, errors.New("paper performance evaluator is not configured")
	}
	canonicalAsOf, ok := canonicalPaperTime(asOf)
	if !ok {
		return nil, errors.New("paper performance as_of is invalid")
	}
	asOf = canonicalAsOf
	var renewed *paperRunnerClaim
	result, err := decideResult(ctx, s.journal, func(tx WriteScope) (*PaperPerformanceEvent, bool, error) {
		return s.evaluatePaperPerformanceTxInScope(ctx, tx, accountRef, asOf, claim, &renewed)
	})
	if err == nil && renewed != nil {
		*claim = *renewed
	}
	return result, err
}
func (s *UseCases) evaluatePaperPerformanceTxInScope(ctx context.Context, tx WriteScope, accountRef, asOf string, claim *paperRunnerClaim, renewedClaim **paperRunnerClaim) (*PaperPerformanceEvent, bool, error) {
	if claim != nil {
		if err := tx.Runner().ValidatePaperRunnerLeaseTx(ctx, claim, accountRef, s.paperRunnerOwner, s.now()); err != nil {
			return nil, false, err
		}
	}
	if _, err := tx.Evaluations().ProvePaperPerformanceRecovery(ctx); err != nil {
		return nil, false, fmt.Errorf("paper performance recovery: %w", err)
	}
	existing, found, err := tx.Evaluations().LoadPaperPerformanceByKey(ctx, accountRef, asOf)
	if err != nil {
		return nil, false, err
	}
	if found {
		if claim != nil {
			if existing.StrategySelectionEventID != claim.StrategySelectionEventID || existing.SelectedStrategyResultRef != claim.SelectedResultSHA256 {
				return nil, false, ErrPaperRunnerPriorSelection
			}
			renewed, err := tx.Runner().RenewPaperRunnerLeaseTx(ctx, claim, s.paperRunnerOwner, s.now())
			if err != nil {
				return nil, false, err
			}
			*renewedClaim = renewed
		}
		return existing, claim != nil, nil
	}

	session, found, err := tx.Paper().LoadPaperAccountingSession(ctx, accountRef)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, errors.New("paper performance accounting session is missing")
	}
	if err := tx.Evaluations().RejectLegacyPaperPerformanceAccount(ctx, accountRef); err != nil {
		return nil, false, err
	}
	recordedAt := s.now().UTC().Format(canonicalPaperTimeLayout)
	if err := validatePaperPerformanceWindow(*session, asOf, recordedAt); err != nil {
		return nil, false, err
	}
	selectionSequence, selectionEventID, selectedResult, err := tx.Evaluations().CurrentPaperPerformanceSelection(ctx)
	if err != nil {
		return nil, false, err
	}
	orderCutoff, marketCutoff, err := tx.Evaluations().Cutoffs(ctx)
	if err != nil {
		return nil, false, err
	}
	states, err := tx.Evaluations().ReplayPaperAccountingStateWithCutoff(ctx, orderCutoff, asOf)
	if err != nil {
		return nil, false, err
	}
	state, exists := states[accountRef]
	if !exists || state.PaperAccountingSessionID != session.SessionID {
		return nil, false, errors.New("paper performance account state is missing")
	}
	marks, valuation, err := tx.Evaluations().DerivePaperPerformanceMarks(ctx, state, session.StartingCash, asOf, recordedAt, marketCutoff)
	if err != nil {
		return nil, false, err
	}
	accountJSON, accountSHA, err := orderJSONHash(state)
	if err != nil || len(accountJSON) == 0 {
		return nil, false, errors.Join(errors.New("paper performance account state is invalid"), err)
	}
	marksJSON, marksSHA, err := orderJSONHash(marks)
	if err != nil {
		return nil, false, err
	}
	previousID, equities, err := tx.Evaluations().PriorPaperPerformanceSeries(ctx, accountRef, session.SessionID, asOf)
	if err != nil {
		return nil, false, err
	}
	points, err := paperdomain.CalculatePerformance(session.StartingCash, append(equities, valuation.Equity))
	if err != nil || len(points) == 0 {
		return nil, false, errors.Join(errors.New("paper performance series is invalid"), err)
	}
	point := points[len(points)-1]
	event := PaperPerformanceEvent{
		SchemaVersion: paperPerformanceSchema, PolicyVersion: paperPerformancePolicy, AccountRef: accountRef,
		PaperAccountingSessionID: session.SessionID, StrategySelectionEventID: selectionEventID,
		SelectedStrategyResultRef: selectedResult, ExpectedPreviousPerformanceID: previousID,
		StrategySelectionSequenceCutoff: selectionSequence, OrderEventSequenceCutoff: orderCutoff,
		PaperMarketSequenceCutoff: marketCutoff, AsOf: asOf, PaperAccountStateSHA256: accountSHA,
		MarksSHA256: marksSHA, MarksJSON: string(marksJSON), MarkCount: len(marks), Cash: valuation.Cash,
		OpenCost: valuation.OpenCost, MarketValue: valuation.MarketValue, RealizedPnL: valuation.RealizedPnL,
		UnrealizedPnL: valuation.UnrealizedPnL, TotalPnL: valuation.TotalPnL, Equity: valuation.Equity,
		PeakEquity: point.PeakEquity, PeriodReturnState: point.PeriodReturnState, PeriodReturn: point.PeriodReturn,
		CumulativeReturn: point.CumulativeReturn, Drawdown: point.Drawdown, MaxDrawdown: point.MaxDrawdown,
		RecordedAt: recordedAt,
	}
	event.PerformanceID = paperPerformanceID(event.PolicyVersion, event.AccountRef, event.PaperAccountingSessionID, event.AsOf)
	if err := tx.Evaluations().InsertPaperPerformanceEvent(ctx, event); err != nil {
		return nil, false, err
	}
	if _, err := tx.Evaluations().ProvePaperPerformanceRecovery(ctx); err != nil {
		return nil, false, fmt.Errorf("proposed paper performance recovery: %w", err)
	}
	var renewed *paperRunnerClaim
	if claim != nil {
		renewed, err = tx.Runner().RenewPaperRunnerLeaseTx(ctx, claim, s.paperRunnerOwner, s.now())
		if err != nil {
			return nil, false, err
		}
	}
	*renewedClaim = renewed
	return &event, true, nil
}
func (s *UseCases) EvaluatePaperPerformance(ctx context.Context, accountRef, asOf string, claim *RunnerClaim) (*PaperPerformanceEvent, error) {
	return s.evaluatePaperPerformanceTx(ctx, accountRef, asOf, claim)
}
func (s *UseCases) evaluatePaperStrategyPerformanceTx(
	ctx context.Context,
	accountRef string,
	expectedSelectionEventID string,
	expectedLatestPerformanceID string,
	claim *paperRunnerClaim,
) (*PaperStrategyPerformanceEvent, error) {
	if s == nil || s.journal == nil || s.now == nil || !orderAlias(accountRef, "account") ||
		!safeOrderID(expectedSelectionEventID) || !safeOrderID(expectedLatestPerformanceID) {
		return nil, errors.New("paper strategy performance identifiers are invalid")
	}
	var renewed *paperRunnerClaim
	result, err := decideResult(ctx, s.journal, func(tx WriteScope) (*PaperStrategyPerformanceEvent, bool, error) {
		return s.evaluatePaperStrategyPerformanceTxInScope(ctx, tx, accountRef, expectedSelectionEventID, expectedLatestPerformanceID, claim, &renewed)
	})
	if err == nil && renewed != nil {
		*claim = *renewed
	}
	return result, err
}
func (s *UseCases) evaluatePaperStrategyPerformanceTxInScope(ctx context.Context, tx WriteScope, accountRef, expectedSelectionEventID, expectedLatestPerformanceID string, claim *paperRunnerClaim, renewedClaim **paperRunnerClaim) (*PaperStrategyPerformanceEvent, bool, error) {
	if claim != nil {
		if err := tx.Runner().ValidatePaperRunnerLeaseTx(ctx, claim, accountRef, s.paperRunnerOwner, s.now()); err != nil {
			return nil, false, err
		}
	}
	if _, err := tx.Evaluations().ProvePaperStrategyPerformanceRecovery(ctx); err != nil {
		return nil, false, fmt.Errorf("paper strategy performance recovery: %w", err)
	}
	_, selectionEventID, selectedResult, err := tx.Evaluations().CurrentPaperPerformanceSelection(ctx)
	if err != nil {
		return nil, false, err
	}
	if err := domain.ValidateStrategyPerformanceSelection(selectionEventID, selectedResult, expectedSelectionEventID); err != nil {
		return nil, false, err
	}
	latest, latestSequence, found, err := tx.Evaluations().LoadLatestPaperPerformance(ctx, accountRef)
	if err != nil {
		return nil, false, err
	}
	if err := domain.ValidateLatestStrategyPerformance(found, domain.PaperPerformanceEvent(latest), expectedLatestPerformanceID, selectionEventID, selectedResult); err != nil {
		return nil, false, err
	}
	if existing, found, err := tx.Evaluations().LoadPaperStrategyPerformanceByKey(ctx, accountRef, selectionEventID, latest.PerformanceID); err != nil {
		return nil, false, err
	} else if found {
		if claim != nil {
			renewed, err := tx.Runner().RenewPaperRunnerLeaseTx(ctx, claim, s.paperRunnerOwner, s.now())
			if err != nil {
				return nil, false, err
			}
			*renewedClaim = renewed
		}
		return existing, claim != nil, nil
	}
	window, err := tx.Evaluations().LoadPaperStrategyPerformanceWindow(ctx, accountRef, latest.PaperAccountingSessionID,
		selectionEventID, selectedResult, latest.PerformanceID, latestSequence)
	if err != nil {
		return nil, false, err
	}
	points, err := paperdomain.CalculatePerformance(window.Baseline.Equity, window.Equities)
	if err != nil || len(points) == 0 {
		return nil, false, errors.Join(errors.New("paper strategy performance window is invalid"), err)
	}
	point := points[len(points)-1]
	previousID, err := tx.Evaluations().LatestPaperStrategyPerformanceID(ctx, accountRef, latest.PaperAccountingSessionID, selectionEventID)
	if err != nil {
		return nil, false, err
	}
	event := PaperStrategyPerformanceEvent{
		SchemaVersion: paperStrategyPerformanceSchema, PolicyVersion: paperStrategyPerformancePolicy,
		AccountRef: accountRef, PaperAccountingSessionID: latest.PaperAccountingSessionID,
		StrategySelectionEventID: selectionEventID, SelectedStrategyResultRef: selectedResult,
		ExpectedPreviousStrategyPerformanceID: previousID, BaselinePerformanceID: window.Baseline.PerformanceID,
		LatestPerformanceID: latest.PerformanceID, BaselineAsOf: window.Baseline.AsOf, LatestAsOf: latest.AsOf,
		SampleCount: len(window.Equities), BaselineEquity: window.Baseline.Equity, LatestEquity: latest.Equity,
		PeakEquity: point.PeakEquity, PeriodReturnState: point.PeriodReturnState, PeriodReturn: point.PeriodReturn,
		CumulativeReturn: point.CumulativeReturn, Drawdown: point.Drawdown, MaxDrawdown: point.MaxDrawdown,
		RecordedAt: s.now().UTC().Format(canonicalPaperTimeLayout),
	}
	event.StrategyPerformanceID = paperStrategyPerformanceID(event)
	if err := tx.Evaluations().InsertPaperStrategyPerformanceEvent(ctx, event); err != nil {
		return nil, false, err
	}
	if _, err := tx.Evaluations().ProvePaperStrategyPerformanceRecovery(ctx); err != nil {
		return nil, false, fmt.Errorf("proposed paper strategy performance recovery: %w", err)
	}
	var renewed *paperRunnerClaim
	if claim != nil {
		renewed, err = tx.Runner().RenewPaperRunnerLeaseTx(ctx, claim, s.paperRunnerOwner, s.now())
		if err != nil {
			return nil, false, err
		}
	}
	*renewedClaim = renewed
	return &event, true, nil
}
func (s *UseCases) EvaluatePaperStrategyPerformance(ctx context.Context, accountRef, expectedSelectionEventID, expectedLatestPerformanceID string, claim *RunnerClaim) (*PaperStrategyPerformanceEvent, error) {
	return s.evaluatePaperStrategyPerformanceTx(ctx, accountRef, expectedSelectionEventID, expectedLatestPerformanceID, claim)
}

type PerformanceMark = domain.PerformanceMark
type StrategyPerformanceWindow = domain.StrategyPerformanceWindow

type OperationalOrderReads interface {
	OperationalOrderIDs(context.Context, string) ([]string, error)
	LoadOperationalIntent(context.Context, string) (OrderIntent, error)
	LoadOperationalState(context.Context, string) (*OrderState, error)
}

func DerivePaperOperationalSnapshot(ctx context.Context, q OperationalOrderReads, accountRef, resultSHA256, selectionEventID string) (paperOperationalSnapshot, error) {
	orderIDs, err := q.OperationalOrderIDs(ctx, accountRef)
	if err != nil {
		return paperOperationalSnapshot{}, err
	}
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	result := paperOperationalSnapshot{}
	for _, orderID := range orderIDs {
		intent, err := q.LoadOperationalIntent(ctx, orderID)
		if err != nil {
			return paperOperationalSnapshot{}, err
		}
		if !domain.OrderInStrategySelection(domain.OrderIntent(intent), resultSHA256, selectionEventID) {
			continue
		}
		state, err := q.LoadOperationalState(ctx, orderID)
		if err != nil {
			return paperOperationalSnapshot{}, err
		}
		if err := domain.ValidateOperationalOrderScope(domain.OrderIntent(intent), (*orderdomain.State)(state), accountRef); err != nil {
			return paperOperationalSnapshot{}, err
		}
		intentJSON, _, err := orderJSONHash(intent)
		if err != nil {
			return paperOperationalSnapshot{}, err
		}
		stateJSON, _, err := orderJSONHash(state)
		if err != nil {
			return paperOperationalSnapshot{}, err
		}
		if err := encoder.Encode([]any{"paper_order", orderID, string(intentJSON), string(stateJSON)}); err != nil {
			return paperOperationalSnapshot{}, err
		}
		domain.CountOperationalOrder(&result, (*orderdomain.State)(state))
	}
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}
