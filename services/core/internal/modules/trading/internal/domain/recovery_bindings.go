package domain

import "errors"

func ValidateCapitalizedFillProvenance(event OrderEvent, authorization PaperAuthorization, sessionID, signalID, authorityID string, fence int64, bar PaperMarketBarObservation, orderID string) error {
	if event.PaperAuthorizationID != authorization.AuthorizationID || event.PaperAccountingSessionID != sessionID ||
		event.PaperSignalEventID != signalID || event.PaperFillPolicyVersion != paperFillPolicyVersion ||
		event.ExecutionAuthorityEventID != authorityID || event.FencingToken != fence ||
		event.OccurredAt != bar.OpenAt || event.ProviderOrderRef != PaperProviderAlias("order", orderID) ||
		event.ProviderExecutionRef != PaperProviderAlias("execution", orderID, bar.ObservationID, paperFillPolicyVersion) ||
		event.EventID != PaperEventID("fill", orderID, bar.ObservationID, paperFillPolicyVersion) {
		return errors.New("capitalized paper fill provenance does not match its evidence")
	}
	return nil
}

func ValidatePerformancePolicyEvidence(event PaperPerformancePolicyEvent, performance PaperStrategyPerformanceEvent, sequence int64, loaded bool) error {
	if !loaded || sequence > event.PaperStrategyPerformanceSequenceCutoff || performance.AccountRef != event.AccountRef ||
		performance.PaperAccountingSessionID != event.PaperAccountingSessionID || performance.StrategySelectionEventID != event.StrategySelectionEventID ||
		performance.SelectedStrategyResultRef != event.SelectedStrategyResultRef || performance.BaselinePerformanceID != event.BaselinePerformanceID ||
		performance.LatestPerformanceID != event.LatestPerformanceID || performance.SampleCount != event.SampleCount {
		return errors.New("paper performance policy strategy evidence is invalid")
	}
	return nil
}

func ValidAutomaticHaltTransition(record AuthorityRecord, policyID string, previousFence int64) bool {
	return record.ReasonCode == "automatic_performance_halt" && record.PaperPerformancePolicyEventID == policyID && !record.Armed && record.LeaseOwner == "" && record.LeaseExpiresAt == "" && record.FencingToken == previousFence+1
}

func ValidAutomaticRollbackBinding(event PaperPerformancePolicyEvent, eventType, source, previous, selected, reason, policyID string) bool {
	return eventType == "ROLLBACK" && source == event.StrategySelectionEventID && previous == event.SelectedStrategyResultRef && selected != event.SelectedStrategyResultRef && reason == "automatic_performance_rollback" && policyID == event.PolicyEventID
}
