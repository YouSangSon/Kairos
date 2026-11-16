package domain

import (
	"errors"
	"math/big"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	"time"
)

const syntheticMaxAccountNotional = int64(1_000_000)

func ValidateSyntheticAccountNotional(active, notionalValue *big.Rat) error {
	if active.Add(active, notionalValue).Cmp(big.NewRat(syntheticMaxAccountNotional, 1)) > 0 {
		return errors.New("active synthetic BUY reservations exceed the fixed account limit")
	}
	return nil
}

func RequireSyntheticExecutionArmed(armed bool) error {
	if !armed {
		return errors.New("synthetic execution is halted")
	}
	return nil
}

func ReuseSyntheticExecutionLease(current ExecutionAuthorityState, owner string, now time.Time) (bool, error) {
	if current.LeaseOwner != "" {
		expires, ok := CanonicalUTCTime(current.LeaseExpiresAt)
		if !ok {
			return false, errors.New("execution lease expiry is invalid")
		}
		if now.Before(expires) {
			if current.LeaseOwner != owner {
				return false, errors.New("synthetic execution lease is owned by another process")
			}
			return true, nil
		}
	}
	return false, nil
}

func ValidateExecutionHaltProvenance(reasonCode, policyEventID string) error {
	if (reasonCode == "manual_halt") != (policyEventID == "") ||
		(reasonCode != "manual_halt" && reasonCode != "automatic_performance_halt") {
		return errors.New("execution halt provenance is invalid")
	}
	return nil
}

func ValidateCapturedArmedAuthority(armed bool) error {
	if !armed {
		return errors.New("captured execution authority is no longer armed")
	}
	return nil
}

func ValidatePaperDispatchState(state *orderdomain.State) error {
	if state.Status != "RECORDED" || state.PendingAction != "" {
		return errors.New("paper authorization requires a recorded order")
	}
	return nil
}

func ValidateSyntheticDispatchState(state *orderdomain.State) error {
	if state.Status != "RECORDED" || state.PendingAction != "" {
		return errors.New("execution authorization requires a recorded order")
	}
	return nil
}

func ValidateAccountDispatchAvailable(blocked bool) error {
	if blocked {
		return errors.New("account has an unresolved order command")
	}
	return nil
}

func ValidateCapitalizedPaperIntent(intent OrderIntent) error {
	if intent.Mode != "paper" || intent.SignalSchemaVersion != capitalizedPaperSignalSchema || intent.OrderType != "PAPER_MARKET" || intent.LimitPrice != "" ||
		intent.PaperAccountingPolicyVersion != paperAccountingPolicyVersion {
		return errors.New("order is not a capitalized paper intent")
	}
	return nil
}

func ValidatePaperIntentSession(intent OrderIntent, found bool, sessionID, policySHA string) error {
	if !found || sessionID != intent.PaperAccountingSessionID || policySHA != intent.ExecutionPolicySHA256 {
		return errors.New("paper order accounting session binding is invalid")
	}
	return nil
}

func ValidatePaperIntentSignal(intent OrderIntent, found bool, signal *PaperSignalEvent) error {
	if !found || signal.EventID != intent.PaperSignalEventID || signal.PaperAccountingSessionID != intent.PaperAccountingSessionID ||
		signal.ExecutionPolicySHA256 != intent.ExecutionPolicySHA256 || signal.StrategyResultSHA256 != intent.StrategyResultSHA256 ||
		signal.StrategySelectionEventID != intent.StrategySelectionEventID || signal.DataSHA256 != intent.SignalDataSHA256 ||
		signal.Symbol != intent.Symbol || signal.TargetQuantity != intent.SignalTargetQuantity || signal.DataAsOf != intent.SignalDataAsOf ||
		signal.GeneratedAt != intent.SignalGeneratedAt || signal.ExpiresAt != intent.SignalExpiresAt {
		return errors.New("paper order signal binding is invalid")
	}
	return nil
}

func ValidatePaperAuthorizationIntent(authorization PaperAuthorization, intent OrderIntent) error {
	if authorization.AccountRef != intent.AccountRef || authorization.PaperAccountingSessionID != intent.PaperAccountingSessionID ||
		authorization.ExecutionPolicySHA256 != intent.ExecutionPolicySHA256 || authorization.Side != intent.Side || authorization.Quantity != intent.Quantity {
		return errors.New("paper authorization does not match its order intent")
	}
	return nil
}

func ValidatePaperAuthorizationAuthority(authorization PaperAuthorization, authority AuthorityRecord) error {
	if authority.AccountRef != authorization.AccountRef || authority.FencingToken != authorization.FencingToken {
		return errors.New("paper authorization does not match its execution authority")
	}
	authorizedAt, authorizedOK := CanonicalUTCTime(authorization.AuthorizedAt)
	authorityRecordedAt, recordedOK := CanonicalUTCTime(authority.RecordedAt)
	authorityExpiresAt, expiresOK := CanonicalUTCTime(authority.LeaseExpiresAt)
	if !authorizedOK || !recordedOK || !expiresOK || authorizedAt.Before(authorityRecordedAt) || !authorizedAt.Before(authorityExpiresAt) {
		return errors.New("paper authorization is outside its execution lease")
	}
	return nil
}

func ValidatePaperAuthorizedEvents(authorization PaperAuthorization, risk, dispatch OrderEvent) error {
	for _, event := range []OrderEvent{risk, dispatch} {
		if event.OrderID != authorization.OrderID || event.Source != "synthetic" || event.RiskReservationID != "" ||
			event.PaperAuthorizationID != authorization.AuthorizationID || event.RiskPolicyVersion != authorization.PolicyVersion ||
			event.FencingToken != authorization.FencingToken {
			return errors.New("paper authorized event does not match its authorization")
		}
	}
	return nil
}

func ValidatePaperAuthorizedState(state *orderdomain.State) error {
	switch state.Status {
	case "OPEN", "PARTIALLY_FILLED", "CANCEL_UNKNOWN", "FILLED", "CANCELED":
		return nil
	default:
		return errors.New("paper order is not in an authorized post-acknowledgement state")
	}
}

func ValidateReservationIntent(reservation RiskReservation, intent OrderIntent) error {
	notional, _, err := ValidateSyntheticBuyPolicy(intent)
	if err != nil || reservation.AccountRef != intent.AccountRef || reservation.Quantity != intent.Quantity ||
		reservation.LimitPrice != intent.LimitPrice || reservation.LimitNotional != notional {
		return errors.New("risk reservation does not match its order intent")
	}
	return nil
}

func ValidateReservationAuthority(reservation RiskReservation, authority AuthorityRecord, loaded bool) error {
	if !loaded || authority.AccountRef != reservation.AccountRef || authority.FencingToken != reservation.FencingToken ||
		authority.ReasonCode != "lease_acquired" || authority.LeaseOwner == "" {
		return errors.New("risk reservation does not match its execution authority")
	}
	return nil
}

func ValidateReservationEvents(reservation RiskReservation, risk, dispatch OrderEvent) error {
	for _, event := range []OrderEvent{risk, dispatch} {
		if event.OrderID != reservation.OrderID || event.Source != "synthetic" || event.RiskReservationID != reservation.ReservationID ||
			event.RiskPolicyVersion != reservation.PolicyVersion || event.FencingToken != reservation.FencingToken {
			return errors.New("authorized order event does not match its reservation")
		}
	}
	return nil
}

func ValidateReservedOrderState(state *orderdomain.State) error {
	switch state.Status {
	case "SUBMIT_UNKNOWN", "OPEN", "PARTIALLY_FILLED", "CANCEL_UNKNOWN", "REJECTED", "FILLED", "CANCELED":
		return nil
	default:
		return errors.New("reserved order is not in an authorized post-dispatch state")
	}
}

func ValidatePaperFillAuthority(record AuthorityRecord, loaded bool, accountRef string, event OrderEvent, recordedAt string) error {
	if !loaded || record.AccountRef != accountRef || !record.Armed || record.LeaseOwner == "" ||
		record.ReasonCode != "lease_acquired" || record.FencingToken != event.FencingToken {
		return errors.New("paper fill execution authority binding is invalid")
	}
	fillTime, fillOK := CanonicalUTCTime(recordedAt)
	recorded, recordedOK := CanonicalUTCTime(record.RecordedAt)
	expires, expiresOK := CanonicalUTCTime(record.LeaseExpiresAt)
	if !fillOK || !recordedOK || !expiresOK || fillTime.Before(recorded) || !fillTime.Before(expires) {
		return errors.New("paper fill is outside its execution lease")
	}
	return nil
}
