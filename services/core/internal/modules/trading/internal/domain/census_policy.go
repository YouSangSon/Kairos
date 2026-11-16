package domain

import (
	"errors"
	"math/big"
	"omni-folio/services/core/internal/exact"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	"time"
)

func ValidateMarketBarFetchTime(bar PaperMarketBarObservation, now time.Time) error {
	if fetched, _ := parsePaperTime(bar.FetchedAt); fetched.After(now) {
		return errors.New("paper market bar was fetched in the future")
	}
	return nil
}

func ValidateSignalAccountingPolicy(found bool, sessionPolicy, policySHA string) error {
	if !found || sessionPolicy != policySHA {
		return errors.New("paper signal accounting policy does not match the account session")
	}
	return nil
}

func ValidateSignalMarketBinding(signal PaperSignal, bar PaperMarketBarObservation) error {
	if bar.Symbol != signal.Symbol || bar.InputDataSHA256 != signal.DataSHA256 || bar.CloseAt != signal.DataAsOf ||
		bar.Source != "paper_fixture" || bar.Venue != "KRX" || bar.Currency != "KRW" || bar.Interval != "1d" ||
		bar.Timezone != "Asia/Seoul" || bar.PriceAdjustment != "unspecified" {
		return errors.New("paper signal bar metadata is invalid")
	}
	availableAt, _ := parsePaperTime(bar.SourceAvailableAt)
	generatedAt, _ := parsePaperTime(signal.GeneratedAt)
	if generatedAt.Before(availableAt) {
		return errors.New("paper signal predates source availability")
	}
	return nil
}

func ValidateCapitalizedSignalHistory(legacyOrders int64) error {
	if legacyOrders != 0 {
		return errors.New("paper signal cannot follow a legacy paper order")
	}
	return nil
}

func ValidateRunnerClaimBinding(present bool, claim, row RunnerLeaseRecord, ownerID, accountRef string) error {
	if !present || ownerID == "" || claim.OwnerID != ownerID || claim != row || claim.AccountRef != accountRef {
		return errors.New("paper runner lease was lost")
	}
	return nil
}

func ValidateRunnerLeaseTime(row RunnerLeaseRecord, nowNS int64) error {
	if nowNS < row.HeartbeatAtNS {
		return errors.New("paper runner lease clock regressed")
	}
	if nowNS >= row.ExpiresAtNS {
		return errors.New("paper runner lease expired")
	}
	return nil
}

func ValidateRunnerSelectionBinding(loaded bool, claim RunnerLeaseRecord, eventID, selectedResult string) error {
	if !loaded || eventID != claim.StrategySelectionEventID || selectedResult != claim.SelectedResultSHA256 {
		return errors.New("paper runner strategy selection changed")
	}
	return nil
}

type RunnerRollbackBinding struct{ EventType, SourceEventID, PreviousResult, ReasonCode, PolicyID, Decision, RollbackEventID string }

func ValidateRunnerRollbackBinding(loaded bool, claim RunnerLeaseRecord, facts RunnerRollbackBinding, currentEventID, rollbackEventID, policyEventID string) error {
	if !loaded || currentEventID != rollbackEventID || facts.EventType != "ROLLBACK" ||
		facts.SourceEventID != claim.StrategySelectionEventID || facts.PreviousResult != claim.SelectedResultSHA256 ||
		facts.ReasonCode != "automatic_performance_rollback" || facts.PolicyID != policyEventID ||
		facts.Decision != "HALT_AND_ROLLBACK" || facts.RollbackEventID != rollbackEventID {
		return errors.New("paper runner automatic rollback lease transition is invalid")
	}
	return nil
}

func LegacyPaperFillQuantity(state *orderdomain.State, ask, available *big.Rat) (string, bool, error) {
	limit, _ := exact.ParseDecimal(state.LimitPrice)
	if ask.Cmp(limit) > 0 {
		return "", false, nil
	}
	total, _ := exact.ParseDecimal(state.Quantity)
	filled, _ := exact.ParseDecimal(state.FilledQuantity)
	remaining := new(big.Rat).Sub(total, filled)
	fillQuantity := new(big.Rat).Set(available)
	if fillQuantity.Cmp(remaining) > 0 {
		fillQuantity.Set(remaining)
	}
	quantity, err := exact.FormatDecimal(fillQuantity)
	if err != nil {
		return "", false, err
	}
	return quantity, true, nil
}

func PaperSignalActive(now, generatedAt, expiresAt time.Time) bool {
	return !now.Before(generatedAt) && now.Before(expiresAt)
}
func LegacyTargetDelta(target, projected *big.Int) *big.Int {
	delta := new(big.Int).Sub(target, projected)
	if delta.Sign() <= 0 {
		return nil
	}
	return delta
}
