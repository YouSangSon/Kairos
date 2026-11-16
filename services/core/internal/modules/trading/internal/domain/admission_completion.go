package domain

import (
	"errors"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	"time"
)

func ValidateReconciliationAuthority(provider, mode, account string, intent OrderIntent) error {
	if provider != intent.Provider || mode != intent.Mode || account != intent.AccountRef {
		return errors.New("lookup authority does not match the order")
	}
	return nil
}

func ValidateReconciliationObservationTime(observedAt, dispatchedAt time.Time) error {
	if observedAt.Before(dispatchedAt) {
		return errors.New("lookup observation predates dispatch")
	}
	return nil
}

func ReconciliationReadiness(state *orderdomain.State, complete bool) (string, error) {
	if !complete {
		return "INCOMPLETE", nil
	}
	if state.Status == "SUBMIT_UNKNOWN" && state.PendingAction == "SUBMIT" {
		return "UNCORRELATED", nil
	}
	if len(state.ProviderOrderRefs) != 1 ||
		(state.Status != "OPEN" && state.Status != "PARTIALLY_FILLED" && state.Status != "FILLED" &&
			state.Status != "CANCEL_UNKNOWN" && state.Status != "CANCELED") {
		return "", errors.New("order state cannot accept lookup reconciliation")
	}
	return "", nil
}

func FindReconciliationObservation(observations []ValidatedOrderObservation, ref string) *ValidatedOrderObservation {
	var match *ValidatedOrderObservation
	for i := range observations {
		if observations[i].Order.ProviderOrderRef == ref {
			match = &observations[i]
			break
		}
	}
	return match
}

func ValidateMatchedReconciliationObservation(match ValidatedOrderObservation, intent OrderIntent, dispatchedAt, observedAt time.Time) error {
	order := match.Order
	if match.Submitted.Before(dispatchedAt) || match.Submitted.After(observedAt) || order.Symbol != intent.Symbol ||
		order.Exchange != intent.Exchange || order.Side != intent.Side || order.OrderType != intent.OrderType ||
		order.Currency != intent.Currency || order.Quantity != intent.Quantity || order.LimitPrice != intent.LimitPrice {
		return errors.New("provider order observation conflicts with the local order")
	}
	return nil
}

func ValidateEvaluationSelection(selected, current, resultSHA256, selectionEventID string) error {
	if selected != resultSHA256 || current != selectionEventID {
		return errors.New("paper evaluation is not bound to the current strategy selection")
	}
	return nil
}

func ValidateStrategyPerformanceSelection(selectionEventID, selectedResult, expectedSelectionEventID string) error {
	if selectionEventID != expectedSelectionEventID {
		return errors.New("paper strategy performance selection is stale")
	}
	if selectedResult == noStrategySelection {
		return errors.New("paper strategy performance is not attributable to no_strategy")
	}
	return nil
}

func ValidateLatestStrategyPerformance(found bool, latest PaperPerformanceEvent, expectedLatestPerformanceID, selectionEventID, selectedResult string) error {
	if !found || latest.PerformanceID != expectedLatestPerformanceID {
		return errors.New("paper strategy performance latest point is stale")
	}
	if latest.StrategySelectionEventID != selectionEventID || latest.SelectedStrategyResultRef != selectedResult {
		return errors.New("paper strategy performance requires a current-selection point")
	}
	return nil
}

func ValidateOperationalOrderScope(intent OrderIntent, state *orderdomain.State, accountRef string) error {
	if intent.Mode != "paper" || intent.AccountRef != accountRef || state.AccountRef != accountRef {
		return errors.New("paper evaluation order scope is invalid")
	}
	return nil
}

func CountOperationalOrder(result *OperationalSnapshot, state *orderdomain.State) {
	result.Orders++
	switch {
	case state.PendingAction != "":
		result.Pending++
	case state.Status == "FILLED" || state.Status == "CANCELED" || state.Status == "REJECTED" || state.Status == "RISK_REJECTED":
		result.Terminal++
	default:
		result.Active++
	}
}

func ValidatePromotionInputCompatibility(candidateQuantity, incumbentQuantity, candidatePolicy, incumbentPolicy, policySHA string) error {
	if candidateQuantity != incumbentQuantity || candidatePolicy != incumbentPolicy || candidatePolicy != policySHA {
		return errors.New("paper promotion requires identical quantity and execution policy")
	}
	return nil
}

func ValidatePaperInputSelection(selected, result string) error {
	if selected != result {
		return errors.New("paper input selection differs")
	}
	return nil
}

func ValidatePaperInputResearch(target, parameterSHA, proposalParameterSHA string) error {
	if target != "paper_candidate" || parameterSHA != proposalParameterSHA {
		return errors.New("paper input research binding differs")
	}
	return nil
}

func ValidatePaperInputSession(loaded, found bool, sessionPolicy, policySHA string) error {
	if !loaded || !found || sessionPolicy != policySHA {
		return errors.New("paper input session binding differs")
	}
	return nil
}

func ValidatePaperInputCSV(ok bool, inputSHA, proposalSHA string, last PaperMarketBarObservation, symbol, asOf string) error {
	if !ok || inputSHA != proposalSHA || last.Symbol != symbol || last.CloseAt != asOf {
		return errors.New("paper input CSV binding differs")
	}
	return nil
}

func ValidatePromotionAbort(reason string, comparison bool, selectionID string) error {
	if reason != "explicit_abort" || comparison || selectionID != "" {
		return errors.New("paper promotion abort is invalid")
	}
	return nil
}

func ValidatePromotionFailure(reason string, comparison bool, selectionID string) error {
	if reason != "comparison_failed" || comparison || selectionID != "" {
		return errors.New("paper promotion failure is invalid")
	}
	return nil
}

func ValidatePromotionStale(reason, selectionID string) error {
	if reason != "account_or_selection_changed" || selectionID != "" {
		return errors.New("paper promotion stale decision is invalid")
	}
	return nil
}

func ValidatePromotionHold(selectionID string) error {
	if selectionID != "" {
		return errors.New("held paper comparison changed selection")
	}
	return nil
}

func ValidatePromotionSelection(decision, selectionID string) error {
	if decision != "PROMOTE" || !safeOrderID(selectionID) {
		return errors.New("paper promotion decision is unsupported")
	}
	return nil
}

func ValidateSyntheticSubmitIntent(intent OrderIntent) error {
	if intent.Mode != "synthetic" {
		return errors.New("limit order submit requires synthetic mode")
	}
	return nil
}

func DispatchProviderMatches(provider, expected string) bool { return provider == expected }

func DispatchModeMatches(mode, expected string) bool { return mode == expected }

func ValidateInitialPaperOrderCount(count int) error {
	if count != 0 {
		return errors.New("paper accounting session cannot follow a paper order")
	}
	return nil
}

func ManualArmingTransition(current, target bool, fence int64) (bool, string, int64) {
	if current == target {
		return false, "", 0
	}
	reason := "manual_halt"
	if target {
		reason = "manual_arm"
	}
	return true, reason, fence + 1
}

func OrderUsesStrategySelection(intent OrderIntent) bool {
	return intent.StrategyResultSHA256 != "" || intent.StrategySelectionEventID != ""
}

func ValidateRunnerSelectedStrategy(loaded bool, selected string) error {
	if !loaded || selected == noStrategySelection {
		return errors.New("paper runner requires a current strategy selection")
	}
	return nil
}

func ValidateHeartbeatSession(loaded, found bool, sessionPolicy, policySHA string) error {
	if !loaded || !found || sessionPolicy != policySHA {
		return errors.New("local paper heartbeat session changed")
	}
	return nil
}

func ValidateHeartbeatClock(now, recordedAt time.Time) error {
	if now.Before(recordedAt) {
		return errors.New("local paper heartbeat clock regressed")
	}
	return nil
}

func ValidatePerformanceLegacyOrderCount(legacy int) error {
	if legacy != 0 {
		return errors.New("paper performance cannot include a legacy paper account")
	}
	return nil
}

func ValidateBoundedAccountingLegacyOrderCount(legacyOrders int) error {
	if legacyOrders != 0 {
		return errors.New("bounded paper accounting cannot include a legacy paper account")
	}
	return nil
}

func RunnerLeaseStatus(lease RunnerLeaseRecord, nowNS int64, currentSelection, selectedResult string) string {
	switch {
	case nowNS < lease.HeartbeatAtNS:
		return "clock_regressed"
	case nowNS >= lease.ExpiresAtNS:
		return "expired"
	case lease.StrategySelectionEventID != currentSelection || lease.SelectedResultSHA256 != selectedResult:
		return "selection_changed"
	}
	return "lease_recorded"
}

func OrderInStrategySelection(intent OrderIntent, resultSHA256, selectionEventID string) bool {
	return intent.StrategyResultSHA256 == resultSHA256 && intent.StrategySelectionEventID == selectionEventID
}
