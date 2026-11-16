package domain

import (
	"errors"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
	researchdomain "omni-folio/services/core/internal/modules/trading/internal/domain/research"
	"time"
)

func PaperResearchWholeSecond(raw string) (time.Time, bool) {
	value, err := time.Parse(time.RFC3339, raw)
	return value, err == nil && value.Location() == time.UTC && value.Nanosecond() == 0 && value.Format(time.RFC3339) == raw
}
func ValidatePromotionResearchRange(startRaw, firstRaw, lastRaw string) error {
	start, _ := PaperResearchWholeSecond(startRaw)
	first, firstOK := PaperResearchWholeSecond(firstRaw)
	last, lastOK := PaperResearchWholeSecond(lastRaw)
	if !firstOK || !lastOK || last.Before(first) {
		return errors.New("registered research interval cannot be verified")
	}
	if !start.After(last) {
		return errors.New("paper comparison must follow all registered research data")
	}
	return nil
}

func ValidatePromotionSessionCount(count int) error {
	if count != 1 {
		return errors.New("paper promotion requires exactly one existing paper account session")
	}
	return nil
}

func ValidatePromotionFlatAccount(state paperdomain.AccountState) error {
	for _, lots := range state.Lots {
		if len(lots) != 0 {
			return errors.New("paper promotion requires a flat paper account")
		}
	}
	return nil
}

func ValidatePromotionAuthoritiesHalted(armed int) error {
	if armed != 0 {
		return errors.New("paper promotion requires all execution authorities halted")
	}
	return nil
}

func ValidatePromotionTerminalOrder(order *orderdomain.State) error {
	if order.PendingAction != "" || (order.Status != "FILLED" && order.Status != "CANCELED" && order.Status != "REJECTED" && order.Status != "RISK_REJECTED") {
		return errors.New("paper promotion requires every account order terminal")
	}
	return nil
}

func ValidatePromotionIncumbent(current, selected, expectedSelection, candidateSHA string) error {
	if current != expectedSelection || selected == candidateSHA || selected == noStrategySelection {
		return errors.New("paper promotion requires a distinct candidate and the current incumbent")
	}
	return nil
}

func ValidatePromotionClaimBars(candidate, incumbent researchdomain.Strategy, bars []researchdomain.Bar, now time.Time) (int, error) {
	warmup := int(max(candidate.SlowWindow, incumbent.SlowWindow))
	if len(bars) != warmup+90 {
		return 0, errors.New("paper promotion requires common warmup and three fresh 30-bar windows")
	}
	end, _ := PaperResearchWholeSecond(bars[len(bars)-1].At)
	if end.After(now) {
		return 0, errors.New("paper promotion input contains future bars")
	}
	return warmup, nil
}

func ValidatePromotionSignalTime(asOf, cutoff string) error {
	at, ok := parsePaperTime(asOf)
	end, endOK := PaperResearchWholeSecond(cutoff)
	if !ok || !endOK || !at.After(end) {
		return errors.New("paper signal must follow the automatic comparison interval")
	}
	return nil
}

func ValidatePromotionPrefixFlat(account paperdomain.AccountState) error {
	for _, lots := range account.Lots {
		if len(lots) != 0 {
			return errors.New("paper promotion account prefix is not flat")
		}
	}
	return nil
}

func ValidatePromotionInterval(startRaw, endRaw string) error {
	start, ok := PaperResearchWholeSecond(startRaw)
	end, endOK := PaperResearchWholeSecond(endRaw)
	if !ok || !endOK || end.Before(start) {
		return errors.New("paper promotion interval is invalid")
	}
	return nil
}

func ValidatePromotionUnconsumedRange(overlap int) error {
	if overlap != 0 {
		return errors.New("paper comparison interval has already been consumed")
	}
	return nil
}

func InitialPromotionOutcome(abort, failed bool, decision, reason string) (string, string) {
	switch {
	case abort:
		return "ABORTED", "explicit_abort"
	case failed:
		return "ERROR", "comparison_failed"
	default:
		return decision, reason
	}
}

func PromotionOutcomeNeedsState(abort, failed bool) bool { return !abort && !failed }

func BoundPromotionOutcome(decision, reason string, captured bool, stateSHA, expectedSHA, current, expectedCurrent string) (string, string) {
	if !captured || stateSHA != expectedSHA || current != expectedCurrent {
		return "STALE", "account_or_selection_changed"
	}
	return decision, reason
}
