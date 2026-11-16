package application

import (
	"context"
	"errors"
	"fmt"
	"math"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
	riskdomain "omni-folio/services/core/internal/modules/trading/internal/domain/risk"

	"time"
)

func (s *UseCases) AdmitPaperSignalTx(ctx context.Context, tx WriteScope, accountRef string, signal PaperSignal, fencingToken int64) (*PaperSignalEvent, *OrderState, error) {
	event, err := tx.Paper().RecordPaperSignalEventTx(ctx, accountRef, signal)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.RequireCurrentSyntheticExecutionLease(ctx, tx, accountRef, fencingToken, s.now().UTC()); err != nil {
		return nil, nil, err
	}
	orderID, exists, err := tx.Paper().PaperOrderBySignalFrom(ctx, accountRef, signal.SignalID)
	if err != nil {
		return nil, nil, err
	}
	if exists {
		intent, err := tx.Orders().LoadOrderIntentFrom(ctx, orderID)
		if err != nil {
			return nil, nil, err
		}
		if _, boundSignal, err := tx.Paper().ValidateCapitalizedPaperOrderBindings(ctx, intent); err != nil || boundSignal.EventID != event.EventID {
			return nil, nil, errors.New("paper signal conflicts with its recorded order")
		}
		state, err := tx.Orders().LoadOrderStateFrom(ctx, orderID)
		if err != nil {
			return nil, nil, err
		}
		return event, state, nil
	}
	projection, err := tx.Paper().ReplayPaperProjectionFrom(ctx, accountRef, event.Symbol)
	if err != nil {
		return nil, nil, err
	}
	change, err := domain.ChangeForTarget(projection, event.TargetQuantity)
	if err != nil {
		return nil, nil, err
	}
	if change == nil {
		return event, nil, nil
	}
	intent := CapitalizedPaperOrderIntent(*event, change.Side, change.Quantity)
	state, err := s.RecordOrderIntentTx(ctx, tx, intent)
	if err != nil {
		return nil, nil, err
	}
	state, _, err = s.AuthorizePaperDispatchOnceTx(ctx, tx, state.OrderID, fencingToken)
	if err != nil {
		return nil, nil, err
	}
	return event, state, nil
}

func (s *UseCases) AuthorizePaperDispatchOnceTx(ctx context.Context, tx WriteScope, orderID string, fencingToken int64) (*OrderState, bool, error) {
	if !safeOrderID(orderID) || fencingToken <= 0 {
		return nil, false, errors.New("paper authorization identifiers are invalid")
	}
	authorization, found, err := tx.Paper().LoadPaperExecutionAuthorizationByOrder(ctx, orderID)
	if err != nil {
		return nil, false, err
	}
	if found {
		state, err := tx.Paper().ValidatePaperExecutionAuthorization(ctx, authorization)
		return state, false, err
	}
	state, err := tx.Orders().LoadOrderStateFrom(ctx, orderID)
	if err != nil {
		return nil, false, err
	}
	if err := domain.ValidatePaperDispatchState((*orderdomain.State)(state)); err != nil {
		return nil, false, err
	}
	intent, err := tx.Orders().LoadOrderIntentFrom(ctx, orderID)
	if err != nil {
		return nil, false, err
	}
	if _, _, err := tx.Paper().ValidateCapitalizedPaperOrderBindings(ctx, intent); err != nil {
		return nil, false, err
	}
	now := s.now().UTC()
	authority, err := s.RequireCurrentSyntheticExecutionLease(ctx, tx, intent.AccountRef, fencingToken, now)
	if err != nil {
		return nil, false, err
	}
	blocked, err := tx.Orders().AccountHasUnresolvedOrder(ctx, intent.AccountRef)
	if err != nil {
		return nil, false, err
	}
	if err := domain.ValidateAccountDispatchAvailable(blocked); err != nil {
		return nil, false, err
	}
	recordedAt := now.Format(time.RFC3339Nano)
	authorization = PaperAuthorization{
		AuthorizationID: paperEventID("authorization", orderID), SchemaVersion: paperExecutionAuthorizationSchema,
		OrderID: orderID, AccountRef: intent.AccountRef, PaperAccountingSessionID: intent.PaperAccountingSessionID,
		ExecutionPolicySHA256: intent.ExecutionPolicySHA256, PolicyVersion: paperAccountingPolicyVersion,
		Side: intent.Side, Quantity: intent.Quantity, AuthorityEventID: authority.EventID, FencingToken: fencingToken,
		RiskEventID: paperEventID("risk", orderID), DispatchEventID: paperEventID("dispatch", orderID), AuthorizedAt: recordedAt,
	}
	if err := tx.Paper().InsertPaperExecutionAuthorization(ctx, authorization); err != nil {
		return nil, false, err
	}
	risk := PaperAuthorizedOrderEvent(authorization.RiskEventID, orderID, "RISK_APPROVED", authorization)
	dispatch := PaperAuthorizedOrderEvent(authorization.DispatchEventID, orderID, "SUBMIT_DISPATCHED", authorization)
	ack := OrderEvent{
		EventID: paperEventID("ack", orderID), OrderID: orderID, Type: "SUBMIT_ACKNOWLEDGED", Source: "synthetic",
		ProviderOrderRef: paperProviderAlias("order", orderID),
	}
	for _, event := range []OrderEvent{risk, dispatch, ack} {
		if err := ValidateOrderEvent(event); err != nil {
			return nil, false, err
		}
		state, err = tx.Orders().AppendOrderEventTx(ctx, event, recordedAt)
		if err != nil {
			return nil, false, err
		}
	}
	if state.Status != "OPEN" || state.PendingAction != "" {
		return nil, false, errors.New("paper authorization did not finish with a local acknowledgement")
	}
	var riskSequence, dispatchSequence, ackSequence int64
	if value, err := tx.Orders().EventSequence(ctx, authorization.RiskEventID); err != nil {
		return nil, false, err
	} else {
		riskSequence = value
	}
	if value, err := tx.Orders().EventSequence(ctx, authorization.DispatchEventID); err != nil {
		return nil, false, err
	} else {
		dispatchSequence = value
	}
	if value, err := tx.Orders().EventSequence(ctx, ack.EventID); err != nil {
		return nil, false, err
	} else {
		ackSequence = value
	}
	if dispatchSequence != riskSequence+1 || ackSequence != dispatchSequence+1 {
		return nil, false, errors.New("paper authorization events are not consecutive")
	}
	return state, true, nil
}

func (s *UseCases) RunPaperOrderThroughCloseWithClaim(ctx context.Context, orderID string, fencingToken int64, claim *RunnerClaim, throughClose string) (*OrderState, error) {
	if s == nil || s.journal == nil || s.now == nil || !safeOrderID(orderID) || fencingToken <= 0 {
		return nil, errors.New("paper fill runner is not configured")
	}
	if throughClose != "" && !canonicalPaperTimes(throughClose) {
		return nil, errors.New("paper fill recovery close is invalid")
	}
	var result *OrderState
	err := s.journal.Write(ctx, func(scope WriteScope) error {
		var err error
		result, err = s.runPaperFillInScope(ctx, scope, orderID, fencingToken, claim, throughClose)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *UseCases) runPaperFillInScope(ctx context.Context, tx WriteScope, orderID string, fencingToken int64, claim *RunnerClaim, throughClose string) (*OrderState, error) {
	if _, err := tx.Recovery().ProvePaperAccountingRecovery(ctx); err != nil {
		return nil, fmt.Errorf("paper fill accounting recovery: %w", err)
	}
	intent, err := tx.Orders().LoadOrderIntentFrom(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if claim != nil {
		if err := tx.Runner().ValidatePaperRunnerLeaseTx(ctx, claim, intent.AccountRef, s.paperRunnerOwner, s.now()); err != nil {
			return nil, err
		}
	}
	session, signal, err := tx.Paper().ValidateCapitalizedPaperOrderBindings(ctx, intent)
	if err != nil {
		return nil, err
	}
	authorization, found, err := tx.Paper().LoadPaperExecutionAuthorizationByOrder(ctx, orderID)
	if err != nil || !found {
		return nil, errors.New("paper fill authorization is missing")
	}
	state, err := tx.Paper().ValidatePaperExecutionAuthorization(ctx, authorization)
	if err != nil {
		return nil, err
	}
	if !domain.PaperOrderNeedsFill((*orderdomain.State)(state)) {
		return state, nil
	}
	now := s.now().UTC()
	authority, err := s.RequireCurrentSyntheticExecutionLease(ctx, tx, intent.AccountRef, fencingToken, now)
	if err != nil {
		return nil, err
	}
	states, err := tx.Paper().ReplayPaperAccounting(ctx)
	if err != nil {
		return nil, err
	}
	account, ok := states[intent.AccountRef]
	if !ok || account.PaperAccountingSessionID != session.SessionID {
		return nil, errors.New("paper fill account state is missing")
	}
	policy, err := tx.Paper().ValidatePaperAccountingSession(*session)
	if err != nil {
		return nil, err
	}
	candidates, err := tx.Paper().PaperFillBars(ctx, *signal, policy.DelayBars)
	if err != nil {
		return nil, err
	}
	usedBars, err := tx.Paper().PaperOrderUsedFillBars(ctx, orderID)
	if err != nil {
		return nil, err
	}
	remaining, position, err := domain.PaperFillQuantities((*orderdomain.State)(state), account.Lots[intent.Symbol])
	if err != nil {
		return nil, err
	}
	var bar *PaperMarketBarObservation
	var calculated paperdomain.Fill
	for _, candidate := range candidates {
		if !domain.PaperFillBarEligible(domain.PaperMarketBarObservation(*candidate), usedBars[candidate.ObservationID], throughClose) {
			continue
		}
		consumed, err := tx.Paper().PaperConsumedBarCapacity(ctx, intent.AccountRef, intent.Symbol, candidate.ObservationID)
		if err != nil {
			return nil, err
		}
		candidateFill, fillOK, err := paperdomain.CalculateFill(PaperExecutionPolicy(policy), paperdomain.FillInput{
			Side: intent.Side, Open: candidate.Open, Volume: candidate.Volume, RemainingQuantity: remaining.String(), Cash: account.Cash,
			PositionQuantity: position.String(), ConsumedCapacity: consumed.String(),
		})
		if err != nil {
			return nil, err
		}
		if fillOK {
			bar, calculated = candidate, candidateFill
			break
		}
	}
	if bar == nil {
		return state, nil
	}
	event := OrderEvent{
		EventID: paperEventID("fill", orderID, bar.ObservationID, paperFillPolicyVersion), OrderID: orderID,
		Type: "FILL_RECORDED", Source: "synthetic", ProviderOrderRef: paperProviderAlias("order", orderID),
		ProviderExecutionRef: paperProviderAlias("execution", orderID, bar.ObservationID, paperFillPolicyVersion),
		Quantity:             calculated.Quantity, Price: calculated.Price, OccurredAt: bar.OpenAt,
		PaperAuthorizationID: authorization.AuthorizationID, FencingToken: fencingToken,
		PaperAccountingSessionID: session.SessionID, PaperSignalEventID: signal.EventID,
		PaperBarObservationID: bar.ObservationID, PaperFillPolicyVersion: paperFillPolicyVersion,
		ExecutionAuthorityEventID: authority.EventID, ReferencePrice: calculated.ReferencePrice,
		Fee: calculated.Fee, Tax: calculated.Tax, Slippage: calculated.Slippage,
	}
	if err := ValidateOrderEvent(event); err != nil {
		return nil, err
	}
	next, err := tx.Paper().AppendCapitalizedPaperFillTx(ctx, event, now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	proposed, err := tx.Paper().ReplayPaperAccounting(ctx)
	if err != nil {
		return nil, err
	}
	if proposed[intent.AccountRef].CapitalizedFills != account.CapitalizedFills+1 {
		return nil, errors.New("paper fill accounting did not advance exactly once")
	}
	return next, nil
}

func (s *UseCases) HeartbeatLocalPaperExecution(ctx context.Context, claim *RunnerClaim, fence int64) (*RunnerClaim, *ExecutionAuthorityState, error) {
	if s == nil || s.journal == nil || s.now == nil || claim == nil || s.executionOwner == "" || fence <= 0 || fence >= math.MaxInt64-1 {
		return nil, nil, errors.New("local paper heartbeat is not configured")
	}
	var result *RunnerClaim
	var authority *ExecutionAuthorityState
	err := s.journal.Write(ctx, func(scope WriteScope) error {
		var err error
		result, authority, err = s.heartbeatInScope(ctx, scope, claim, fence)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return result, authority, nil
}

func (s *UseCases) heartbeatInScope(ctx context.Context, tx WriteScope, claim *RunnerClaim, fence int64) (*RunnerClaim, *ExecutionAuthorityState, error) {
	if _, err := tx.Recovery().ProvePaperRunnerLeaseRecovery(ctx); err != nil {
		return nil, nil, err
	}
	now := s.now().UTC()
	if err := tx.Runner().ValidatePaperRunnerLeaseTx(ctx, claim, claim.AccountRef, s.paperRunnerOwner, now); err != nil {
		return nil, nil, err
	}
	session, found, err := tx.Paper().LoadPaperAccountingSession(ctx, claim.AccountRef)
	if err != nil {
		return nil, nil, err
	}
	policy, err := tx.Selections().LoadCurrentStrategyExecutionPolicy(ctx, claim.SelectedResultSHA256, claim.StrategySelectionEventID)
	var sessionPolicy string
	if err == nil && found {
		sessionPolicy = session.ExecutionPolicySHA256
	}
	if bindErr := domain.ValidateHeartbeatSession(err == nil, found, sessionPolicy, policy.SHA256); bindErr != nil {
		return nil, nil, bindErr
	}
	if err := tx.Paper().RequireIsolatedPaperAccount(ctx, claim.AccountRef); err != nil {
		return nil, nil, err
	}
	// The complete history was verified above in this same transaction, with
	// no intervening writes. Read only its indexed head; independent callers
	// still require loadExecutionAuthoritySnapshot's full replay.
	var currentID string
	currentID, err = tx.Authority().LatestAuthorityID(ctx, claim.AccountRef)
	if err != nil {
		return nil, nil, err
	}
	previous, err := tx.Authority().LoadExecutionAuthorityRecordByID(ctx, currentID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.ValidateOwnedExecutionLease(*AuthorityState(previous), fence, now); err != nil {
		return nil, nil, err
	}
	recordedAt, _ := canonicalUTCTime(previous.RecordedAt)
	if err := domain.ValidateHeartbeatClock(now, recordedAt); err != nil {
		return nil, nil, err
	}
	next := AuthorityRecord{
		EventID: s.id("execution_authority"), AccountRef: claim.AccountRef, Armed: true, LeaseOwner: s.executionOwner,
		FencingToken: fence + 1, LeaseExpiresAt: now.Add(syntheticExecutionLeaseTTL).Format(time.RFC3339Nano),
		ReasonCode: "lease_acquired", RecordedAt: now.Format(time.RFC3339Nano),
	}
	if err := ValidateExecutionAuthorityRecord(next, &previous); err != nil {
		return nil, nil, err
	}
	// ponytail: immutable renewals grow replay cost; measure sustained history
	// before promoting an always-on consumer, not a second mutable authority.
	if err := tx.Authority().InsertExecutionAuthorityRecord(ctx, next); err != nil {
		return nil, nil, err
	}
	nextClaim, err := tx.Runner().RenewPaperRunnerLeaseTx(ctx, claim, s.paperRunnerOwner, now)
	if err != nil {
		return nil, nil, err
	}
	return nextClaim, AuthorityState(next), nil
}

func (s *UseCases) ApplyPaperPerformancePolicyTx(ctx context.Context, accountRef, expectedSelectionEventID, expectedStrategyPerformanceID string, claim *RunnerClaim) (*PaperPerformancePolicyEvent, error) {
	if s == nil || s.journal == nil || s.now == nil || !orderAlias(accountRef, "account") ||
		!safeOrderID(expectedSelectionEventID) || !safeOrderID(expectedStrategyPerformanceID) {
		return nil, errors.New("paper performance policy identifiers are invalid")
	}
	var originalClaim *RunnerClaim
	if claim != nil {
		originalClaim = claim
		copy := *claim
		claim = &copy
	}
	var result *PaperPerformancePolicyEvent
	err := s.journal.Write(ctx, func(scope WriteScope) error {
		var err error
		result, err = s.applyPolicyInScope(ctx, scope, accountRef, expectedSelectionEventID, expectedStrategyPerformanceID, claim)
		return err
	})
	if err != nil {
		return nil, err
	}
	if originalClaim != nil {
		*originalClaim = *claim
	}
	return result, nil
}

func (s *UseCases) applyPolicyInScope(ctx context.Context, tx WriteScope, accountRef, expectedSelectionEventID, expectedStrategyPerformanceID string, claim *RunnerClaim) (*PaperPerformancePolicyEvent, error) {
	if claim != nil {
		if err := tx.Runner().ValidatePaperRunnerLeaseTx(ctx, claim, accountRef, s.paperRunnerOwner, s.now()); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Recovery().ProvePaperPerformancePolicyRecovery(ctx); err != nil {
		return nil, fmt.Errorf("paper performance policy recovery: %w", err)
	}
	if existing, found, err := tx.Policy().LoadPaperPerformancePolicyByKey(ctx, accountRef, expectedSelectionEventID, expectedStrategyPerformanceID); err != nil {
		return nil, err
	} else if found {
		if claim != nil {
			renewed, err := tx.Runner().RenewPaperRunnerLeaseTx(ctx, claim, s.paperRunnerOwner, s.now())
			if err != nil {
				return nil, err
			}
			*claim = *renewed
		}
		return existing, nil
	}
	strategy, err := tx.Selections().ReplayStrategyRegistry(ctx)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidatePerformancePolicySelection(strategy.CurrentEventID, strategy.SelectedResultSHA256, expectedSelectionEventID); err != nil {
		return nil, err
	}
	performance, _, err := tx.Policy().LoadPaperStrategyPerformanceByID(ctx, expectedStrategyPerformanceID)
	if err != nil {
		return nil, err
	}
	var latestID string
	latestID, err = tx.Policy().LatestStrategyPerformanceID(ctx, accountRef)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateLatestPolicyPerformance(domain.PaperStrategyPerformanceEvent(performance), latestID, expectedStrategyPerformanceID, accountRef, strategy.CurrentEventID, strategy.SelectedResultSHA256); err != nil {
		return nil, err
	}
	decision, err := riskdomain.EvaluatePaperPerformancePolicy(riskdomain.PaperPerformanceInput{
		SampleCount: performance.SampleCount, CumulativeReturn: performance.CumulativeReturn, MaxDrawdown: performance.MaxDrawdown,
	})
	if err != nil {
		return nil, fmt.Errorf("paper performance policy evidence: %w", err)
	}
	previous, err := tx.Policy().LatestPaperPerformancePolicyID(ctx, accountRef, strategy.CurrentEventID)
	if err != nil {
		return nil, err
	}
	selectionCutoff, err := tx.Selections().SelectionSequence(ctx)
	if err != nil {
		return nil, err
	}
	authorityCutoff, err := tx.Authority().AuthoritySequence(ctx)
	if err != nil {
		return nil, err
	}
	performanceCutoff, err := tx.Policy().StrategyPerformanceSequence(ctx)
	if err != nil {
		return nil, err
	}
	armed, err := tx.Authority().ArmedExecutionAccounts(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	event := PaperPerformancePolicyEvent{
		SchemaVersion: paperPerformancePolicySchema, PolicyVersion: riskdomain.PaperPerformancePolicyVersion,
		AccountRef: accountRef, PaperAccountingSessionID: performance.PaperAccountingSessionID,
		StrategySelectionEventID: performance.StrategySelectionEventID, SelectedStrategyResultRef: performance.SelectedStrategyResultRef,
		StrategyPerformanceID: performance.StrategyPerformanceID, BaselinePerformanceID: performance.BaselinePerformanceID,
		LatestPerformanceID: performance.LatestPerformanceID, SampleCount: performance.SampleCount,
		ExpectedPreviousPolicyEventID: previous, StrategySelectionSequenceCutoff: selectionCutoff,
		PaperStrategyPerformanceSequenceCutoff: performanceCutoff, ExecutionAuthoritySequenceCutoff: authorityCutoff,
		Decision: decision.Decision, ReasonCode: decision.ReasonCode, RecordedAt: now.Format(canonicalPaperTimeLayout),
	}
	event.PolicyEventID = PaperPerformancePolicyID(event)
	automaticHaltIDs := map[string]string(nil)
	if decision.Decision == "HALT_AND_ROLLBACK" {
		event.RollbackSelectionEventID = s.id("strategy_selection")
		event.AutomaticHaltCount = len(armed)
		automaticHaltIDs = make(map[string]string, len(armed))
		for _, armedAccountRef := range armed {
			automaticHaltIDs[armedAccountRef] = PaperPerformanceAutomaticHaltID(event.PolicyEventID, armedAccountRef)
		}
	}
	if err := tx.Policy().InsertPaperPerformancePolicyEvent(ctx, event); err != nil {
		return nil, err
	}
	if decision.Decision == "HALT_AND_ROLLBACK" {
		if _, err := s.HaltSyntheticExecutionTx(ctx, tx, now, "automatic_performance_halt", event.PolicyEventID, armed, automaticHaltIDs); err != nil {
			return nil, err
		}
		if _, err := s.RollbackPaperCandidateTx(ctx, tx, strategy, expectedSelectionEventID, expectedSelectionEventID,
			"automatic_performance_rollback", event.PolicyEventID, event.RollbackSelectionEventID, now); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Recovery().ProvePaperPerformancePolicyRecovery(ctx); err != nil {
		return nil, fmt.Errorf("proposed paper performance policy recovery: %w", err)
	}
	var renewed *RunnerClaim
	if claim != nil {
		if event.Decision == "HALT_AND_ROLLBACK" {
			renewed, err = tx.Runner().RenewPaperRunnerLeaseAfterPolicyRollbackTx(ctx, claim, event.PolicyEventID, event.RollbackSelectionEventID, s.paperRunnerOwner, s.now())
		} else {
			renewed, err = tx.Runner().RenewPaperRunnerLeaseTx(ctx, claim, s.paperRunnerOwner, s.now())
		}
		if err != nil {
			return nil, err
		}
	}
	if renewed != nil {
		*claim = *renewed
	}
	return &event, nil
}

func (s *UseCases) AuthorizeSyntheticDispatchOnceTx(ctx context.Context, tx WriteScope, orderID string, fencingToken int64) (*OrderState, bool, error) {
	if !safeOrderID(orderID) || fencingToken <= 0 {
		return nil, false, errors.New("execution authorization identifiers are invalid")
	}
	reservation, found, err := tx.Reservations().LoadRiskReservationByOrder(ctx, orderID)
	if err != nil {
		return nil, false, err
	}
	if found {
		state, err := tx.Reservations().ValidateAuthorizedReservation(ctx, reservation)
		return state, false, err
	}
	state, err := tx.Orders().LoadOrderStateFrom(ctx, orderID)
	if err != nil {
		return nil, false, err
	}
	if err := domain.ValidateSyntheticDispatchState((*orderdomain.State)(state)); err != nil {
		return nil, false, err
	}
	intent, err := tx.Orders().LoadOrderIntentFrom(ctx, orderID)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Selections().ValidateStrategyOrderSelection(ctx, intent); err != nil {
		return nil, false, err
	}
	notional, notionalValue, err := ValidateSyntheticBuyPolicy(intent)
	if err != nil {
		return nil, false, err
	}
	now := s.now().UTC()
	authority, err := s.RequireCurrentSyntheticExecutionLease(ctx, tx, intent.AccountRef, fencingToken, now)
	if err != nil {
		return nil, false, err
	}
	blocked, err := tx.Orders().AccountHasUnresolvedOrder(ctx, intent.AccountRef)
	if err != nil {
		return nil, false, err
	}
	if err := domain.ValidateAccountDispatchAvailable(blocked); err != nil {
		return nil, false, err
	}
	active, err := tx.Reservations().ActiveReservedNotional(ctx, intent.AccountRef)
	if err != nil {
		return nil, false, err
	}
	if err := domain.ValidateSyntheticAccountNotional(active, notionalValue); err != nil {
		return nil, false, err
	}
	recordedAt := now.Format(time.RFC3339Nano)
	reservation = RiskReservation{
		ReservationID: s.id("risk_reservation"), OrderID: orderID, AccountRef: intent.AccountRef,
		PolicyVersion: syntheticRiskPolicyVersion, AuthorityEventID: authority.EventID, FencingToken: fencingToken,
		Quantity: intent.Quantity, LimitPrice: intent.LimitPrice, LimitNotional: notional,
		RiskEventID: s.id("order_event"), DispatchEventID: s.id("order_event"), ReservedAt: recordedAt,
	}
	if err := tx.Reservations().InsertRiskReservation(ctx, reservation); err != nil {
		return nil, false, err
	}
	risk := AuthorizedOrderEvent(reservation.RiskEventID, orderID, "RISK_APPROVED", reservation)
	dispatch := AuthorizedOrderEvent(reservation.DispatchEventID, orderID, "SUBMIT_DISPATCHED", reservation)
	if err := ValidateOrderEvent(risk); err != nil {
		return nil, false, err
	}
	state, err = tx.Orders().AppendOrderEventTx(ctx, risk, recordedAt)
	if err != nil || state.Status != "READY" {
		return nil, false, fmt.Errorf("append risk approval: %w", err)
	}
	if err := ValidateOrderEvent(dispatch); err != nil {
		return nil, false, err
	}
	state, err = tx.Orders().AppendOrderEventTx(ctx, dispatch, recordedAt)
	if err != nil || state.Status != "SUBMIT_UNKNOWN" || state.PendingAction != "SUBMIT" {
		return nil, false, fmt.Errorf("append submit dispatch: %w", err)
	}
	var riskSequence, dispatchSequence int64
	if value, err := tx.Orders().EventSequence(ctx, reservation.RiskEventID); err != nil {
		return nil, false, err
	} else {
		riskSequence = value
	}
	if value, err := tx.Orders().EventSequence(ctx, reservation.DispatchEventID); err != nil {
		return nil, false, err
	} else {
		dispatchSequence = value
	}
	if dispatchSequence != riskSequence+1 {
		return nil, false, errors.New("authorized order events are not consecutive")
	}
	return state, true, nil
}

func (s *UseCases) AcquireSyntheticExecutionLeaseTx(ctx context.Context, tx WriteScope, accountRef string) (*ExecutionAuthorityState, error) {
	if _, err := tx.Recovery().ProvePaperPerformancePolicyRecovery(ctx); err != nil {
		return nil, fmt.Errorf("execution authority policy recovery: %w", err)
	}
	current, err := tx.Authority().LoadExecutionAuthoritySnapshot(ctx, accountRef)
	if err != nil {
		return nil, err
	}
	if err := domain.RequireSyntheticExecutionArmed(current.Armed); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	reuse, err := domain.ReuseSyntheticExecutionLease(domain.ExecutionAuthorityState(current.ExecutionAuthorityState), s.executionOwner, now)
	if err != nil {
		return nil, err
	}
	if reuse {
		state := current.ExecutionAuthorityState
		return &state, nil
	}
	record := AuthorityRecord{
		EventID: s.id("execution_authority"), AccountRef: accountRef, Armed: true, LeaseOwner: s.executionOwner,
		FencingToken: current.FencingToken + 1, LeaseExpiresAt: now.Add(syntheticExecutionLeaseTTL).Format(time.RFC3339Nano),
		ReasonCode: "lease_acquired", RecordedAt: now.Format(time.RFC3339Nano),
	}
	if err := tx.Authority().InsertExecutionAuthorityRecord(ctx, record); err != nil {
		return nil, err
	}
	return AuthorityState(record), nil
}

func (s *UseCases) HaltSyntheticExecutionTx(ctx context.Context, tx WriteScope, now time.Time, reasonCode, policyEventID string, accountRefs []string, automaticHaltIDs map[string]string) (int, error) {
	if err := domain.ValidateExecutionHaltProvenance(reasonCode, policyEventID); err != nil {
		return 0, err
	}
	recordedAt := now.Format(time.RFC3339Nano)
	for _, accountRef := range accountRefs {
		current, err := tx.Authority().LoadExecutionAuthoritySnapshot(ctx, accountRef)
		if err != nil {
			return 0, err
		}
		if err := domain.ValidateCapturedArmedAuthority(current.Armed); err != nil {
			return 0, err
		}
		eventID := s.id("execution_authority")
		if reasonCode == "automatic_performance_halt" {
			eventID = automaticHaltIDs[accountRef]
			if eventID != PaperPerformanceAutomaticHaltID(policyEventID, accountRef) {
				return 0, errors.New("automatic execution halt identifier is invalid")
			}
		}
		if err := tx.Authority().InsertExecutionAuthorityRecord(ctx, AuthorityRecord{
			EventID: eventID, AccountRef: accountRef,
			FencingToken: current.FencingToken + 1, ReasonCode: reasonCode,
			PaperPerformancePolicyEventID: policyEventID, RecordedAt: recordedAt,
		}); err != nil {
			return 0, err
		}
	}
	return len(accountRefs), nil
}

func (s *UseCases) RollbackPaperCandidateTx(ctx context.Context, tx WriteScope, state StrategyRegistryProof, expectedCurrentEventID, sourceEventID, reasonCode, policyEventID, eventID string, now time.Time) (*StrategySelectionState, error) {
	if err := domain.ValidateRollbackSource(state.CurrentEventID, expectedCurrentEventID, sourceEventID); err != nil {
		return nil, err
	}
	selected, err := domain.RollbackPaperSelection(state.Stack, reasonCode, policyEventID, eventID)
	if err != nil {
		return nil, err
	}
	event := StrategySelectionEvent{
		EventID: eventID, EventType: "ROLLBACK", ExpectedCurrentEventID: expectedCurrentEventID,
		SourceEventID: sourceEventID, PreviousSelectedResultSHA256: state.SelectedResultSHA256,
		SelectedResultSHA256: selected, ReasonCode: reasonCode, PaperPerformancePolicyEventID: policyEventID,
		RecordedAt: now.Format(time.RFC3339Nano),
	}
	sequence, err := tx.Selections().LatestPaperEvaluationSequence(ctx)
	if err != nil {
		return nil, err
	}
	event.PaperEvaluationSequence = sequence
	if err := tx.Selections().InsertStrategySelectionEvent(ctx, event); err != nil {
		return nil, err
	}
	return &StrategySelectionState{CurrentEventID: event.EventID, SelectedResultSHA256: selected}, nil
}

func (s *UseCases) RequireCurrentSyntheticExecutionLease(ctx context.Context, tx WriteScope, accountRef string, fencingToken int64, now time.Time) (AuthoritySnapshot, error) {
	authority, err := tx.Authority().LoadExecutionAuthoritySnapshot(ctx, accountRef)
	if err != nil {
		return AuthoritySnapshot{}, err
	}
	if err := s.ValidateOwnedExecutionLease(authority.ExecutionAuthorityState, fencingToken, now); err != nil {
		return AuthoritySnapshot{}, err
	}
	return authority, nil
}

func (s *UseCases) ValidateOwnedExecutionLease(authority ExecutionAuthorityState, fencingToken int64, now time.Time) error {
	return domain.ValidateOwnedExecutionLease(domain.ExecutionAuthorityState(authority), s.executionOwner, fencingToken, now)
}
