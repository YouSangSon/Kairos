package domain

import (
	"errors"
	"math"
	"math/big"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
)

func ValidatePerformancePolicySelection(current, selected, expectedSelectionEventID string) error {
	if current != expectedSelectionEventID || selected == noStrategySelection {
		return errors.New("paper performance policy selection is stale or no_strategy")
	}
	return nil
}

func ValidateLatestPolicyPerformance(performance PaperStrategyPerformanceEvent, latestID, expectedStrategyPerformanceID, accountRef, current, selected string) error {
	if latestID != expectedStrategyPerformanceID || performance.AccountRef != accountRef ||
		performance.StrategySelectionEventID != current || performance.SelectedStrategyResultRef != selected {
		return errors.New("paper performance policy strategy evidence is stale")
	}
	return nil
}

func PaperFillQuantities(state *orderdomain.State, lots []paperdomain.Lot) (*big.Int, *big.Int, error) {
	total, _ := new(big.Int).SetString(state.Quantity, 10)
	filled, _ := new(big.Int).SetString(state.FilledQuantity, 10)
	remaining := new(big.Int).Sub(total, filled)
	position := new(big.Int)
	for _, lot := range lots {
		quantity, ok := new(big.Int).SetString(lot.Quantity, 10)
		if !ok {
			return nil, nil, errors.New("paper fill position is invalid")
		}
		position.Add(position, quantity)
	}
	return remaining, position, nil
}

func PaperFillBarEligible(bar PaperMarketBarObservation, used bool, throughClose string) bool {
	return !used && (throughClose == "" || bar.CloseAt <= throughClose)
}

func PaperOrderNeedsFill(state *orderdomain.State) bool {
	return state.Status == "OPEN" || state.Status == "PARTIALLY_FILLED"
}

func CapitalizedPaperOrderIntent(signal PaperSignalEvent, side, quantity string) OrderIntent {
	return OrderIntent{
		ClientOrderID: "paper_" + signal.SignalID, Provider: "kiwoom", Mode: "paper", AccountRef: signal.AccountRef,
		Symbol: signal.Symbol, Exchange: "KRX", Side: side, OrderType: "PAPER_MARKET", Quantity: quantity,
		Currency: "KRW", StrategyResultSHA256: signal.StrategyResultSHA256,
		StrategySelectionEventID: signal.StrategySelectionEventID, SignalSchemaVersion: signal.SchemaVersion,
		SignalID: signal.SignalID, SignalDataSHA256: signal.DataSHA256, SignalDataAsOf: signal.DataAsOf,
		SignalGeneratedAt: signal.GeneratedAt, SignalExpiresAt: signal.ExpiresAt, SignalTargetQuantity: signal.TargetQuantity,
		PaperAccountingSessionID: signal.PaperAccountingSessionID, PaperAccountingPolicyVersion: paperAccountingPolicyVersion,
		PaperSignalEventID: signal.EventID, ExecutionPolicySHA256: signal.ExecutionPolicySHA256,
	}
}

func PaperAuthorizedOrderEvent(eventID, orderID, eventType string, authorization PaperAuthorization) OrderEvent {
	return OrderEvent{
		EventID: eventID, OrderID: orderID, Type: eventType, Source: "synthetic",
		PaperAuthorizationID: authorization.AuthorizationID, RiskPolicyVersion: authorization.PolicyVersion,
		FencingToken: authorization.FencingToken,
	}
}

func AuthorizedOrderEvent(eventID, orderID, eventType string, reservation RiskReservation) OrderEvent {
	return OrderEvent{
		EventID: eventID, OrderID: orderID, Type: eventType, Source: "synthetic",
		RiskReservationID: reservation.ReservationID, RiskPolicyVersion: reservation.PolicyVersion,
		FencingToken: reservation.FencingToken,
	}
}

func ValidateIsolatedPaperAccount(mixed bool) error {
	if mixed {
		return errors.New("local paper requires an isolated paper account")
	}
	return nil
}

func ValidateStrategyChangeRunnerLease(record RunnerLeaseRecord, nowNS int64) error {
	if nowNS < record.HeartbeatAtNS {
		return errors.New("paper runner lease clock regressed")
	}
	if nowNS < record.ExpiresAtNS {
		return errors.New("paper runner is active; stop and release it before changing strategy selection")
	}
	return nil
}

func ValidateExpiredPaperRunCompleteness(incomplete int) error {
	if incomplete != 0 {
		return errors.New("expired paper runner has an incomplete paper performance chain")
	}
	return nil
}

func ValidateNoPendingPromotion(pending int) error {
	if pending != 0 {
		return errors.New("a paper comparison is pending; inspect or explicitly abort it first")
	}
	return nil
}

func PaperImprovementDisposition(found bool, target, selected, result string) string {
	if !found {
		if target == "no_promotion" {
			return "research_not_candidate"
		}
		if selected == result {
			return "already_selected"
		}
	}
	return ""
}

func ValidateLocalProposalSelection(selection, result, claimSelection, claimResult string) error {
	if selection != claimSelection || result != claimResult {
		return errors.New("local paper proposal selection changed")
	}
	return nil
}

func ValidateLocalWarmup(slow, bars int) error {
	if slow < 2 || bars <= slow {
		return errors.New("paper snapshot lacks the complete strategy warmup window")
	}
	return nil
}

func ValidateLocalPolicyCloses(closes []string, latest string) error {
	if len(closes) == 0 || closes[len(closes)-1] != latest {
		return errors.New("local paper policy snapshot regressed or has no available close")
	}
	return nil
}

func ValidateLocalPaperIntent(intent OrderIntent) error {
	if intent.SignalSchemaVersion != capitalizedPaperSignalSchema || intent.OrderType != "PAPER_MARKET" {
		return errors.New("local paper cannot execute legacy orders")
	}
	return nil
}

func LocalPaperOrderExecutable(state *orderdomain.State) (bool, error) {
	if state.PendingAction != "" {
		return false, errors.New("local paper has an unresolved order action")
	}
	switch state.Status {
	case "OPEN", "PARTIALLY_FILLED":
		return true, nil
	case "FILLED", "CANCELED", "REJECTED":
		return false, nil
	default:
		return false, errors.New("local paper has an unexecutable order")
	}
}

func ValidateLocalExecutionSelection(loaded bool, currentEvent, result, selectionEventID string) error {
	if !loaded || currentEvent != selectionEventID || result == noStrategySelection {
		return errors.New("local paper selection changed")
	}
	return nil
}

func ValidateLocalExecutionSession(loaded, found bool, sessionPolicy, policySHA string) error {
	if !loaded || !found || sessionPolicy != policySHA {
		return errors.New("local paper requires a matching initialized session")
	}
	return nil
}

func ValidateLocalExecutionFence(fence int64) error {
	if fence >= math.MaxInt64-1 {
		return errors.New("local paper execution fence overflow")
	}
	return nil
}

func ValidateLocalCleanupLease(current ExecutionAuthorityState, owner string, fence int64) error {
	if current.LeaseOwner != owner || current.FencingToken != fence || fence == math.MaxInt64 {
		return errors.New("local paper cleanup does not own the current lease")
	}
	return nil
}

func ValidateScheduledStrategy(selectedResult string) error {
	if selectedResult == noStrategySelection {
		return errors.New("current paper strategy is missing")
	}
	return nil
}

func IncludeLocalPolicyFrontier(complete bool, previous, latest string) bool {
	return !complete || previous == latest
}

func IncludeCommittedPolicyFrontier(points int, include bool) bool { return points > 0 && include }

func PaperPolicyStopsExecution(decision string) bool { return decision == "HALT_AND_ROLLBACK" }

func PriorPolicyNeedsInspection(previous, asOf string) bool { return previous != "" && previous < asOf }

func PriorPolicyMatchesSelection(point PaperPerformanceEvent, selection string) bool {
	return point.StrategySelectionEventID == selection
}

func ScheduledCloseAfterPriorPolicy(asOf, previous string, complete bool) string {
	if !complete {
		return previous
	}
	return asOf
}
