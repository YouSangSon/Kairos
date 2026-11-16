package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/exact"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
	riskdomain "omni-folio/services/core/internal/modules/trading/internal/domain/risk"
	"strconv"
	"strings"
	"time"
)

type AuthorityRecord struct {
	EventID                       string `json:"event_id"`
	AccountRef                    string `json:"account_ref"`
	Armed                         bool   `json:"armed"`
	LeaseOwner                    string `json:"lease_owner,omitempty"`
	FencingToken                  int64  `json:"fencing_token"`
	LeaseExpiresAt                string `json:"lease_expires_at,omitempty"`
	ReasonCode                    string `json:"reason_code"`
	PaperPerformancePolicyEventID string `json:"paper_performance_policy_event_id,omitempty"`
	RecordedAt                    string `json:"recorded_at"`
}

type RiskReservation struct {
	ReservationID    string `json:"reservation_id"`
	OrderID          string `json:"order_id"`
	AccountRef       string `json:"account_ref"`
	PolicyVersion    string `json:"policy_version"`
	AuthorityEventID string `json:"authority_event_id"`
	FencingToken     int64  `json:"fencing_token"`
	Quantity         string `json:"quantity"`
	LimitPrice       string `json:"limit_price"`
	LimitNotional    string `json:"limit_notional"`
	RiskEventID      string `json:"risk_event_id"`
	DispatchEventID  string `json:"dispatch_event_id"`
	ReservedAt       string `json:"reserved_at"`
}

type PaperAuthorization struct {
	AuthorizationID          string `json:"authorization_id"`
	SchemaVersion            string `json:"schema_version"`
	OrderID                  string `json:"order_id"`
	AccountRef               string `json:"account_ref"`
	PaperAccountingSessionID string `json:"paper_accounting_session_id"`
	ExecutionPolicySHA256    string `json:"execution_policy_sha256"`
	PolicyVersion            string `json:"policy_version"`
	Side                     string `json:"side"`
	Quantity                 string `json:"quantity"`
	AuthorityEventID         string `json:"authority_event_id"`
	FencingToken             int64  `json:"fencing_token"`
	RiskEventID              string `json:"risk_event_id"`
	DispatchEventID          string `json:"dispatch_event_id"`
	AuthorizedAt             string `json:"authorized_at"`
}

type ExecutionAuthorityState struct {
	AccountRef     string `json:"account_ref"`
	Armed          bool   `json:"armed"`
	LeaseOwner     string `json:"lease_owner,omitempty"`
	FencingToken   int64  `json:"fencing_token"`
	LeaseExpiresAt string `json:"lease_expires_at,omitempty"`
}

type OrderEvent = orderdomain.Event

type PaperPerformancePolicyEvent struct {
	PolicyEventID                          string `json:"policy_event_id"`
	SchemaVersion                          string `json:"schema_version"`
	PolicyVersion                          string `json:"policy_version"`
	AccountRef                             string `json:"account_ref"`
	PaperAccountingSessionID               string `json:"paper_accounting_session_id"`
	StrategySelectionEventID               string `json:"strategy_selection_event_id"`
	SelectedStrategyResultRef              string `json:"selected_strategy_result_ref"`
	StrategyPerformanceID                  string `json:"strategy_performance_id"`
	BaselinePerformanceID                  string `json:"baseline_performance_id"`
	LatestPerformanceID                    string `json:"latest_performance_id"`
	SampleCount                            int    `json:"sample_count"`
	ExpectedPreviousPolicyEventID          string `json:"expected_previous_policy_event_id"`
	StrategySelectionSequenceCutoff        int64  `json:"strategy_selection_sequence_cutoff"`
	PaperStrategyPerformanceSequenceCutoff int64  `json:"paper_strategy_performance_sequence_cutoff"`
	ExecutionAuthoritySequenceCutoff       int64  `json:"execution_authority_sequence_cutoff"`
	Decision                               string `json:"decision"`
	ReasonCode                             string `json:"reason_code"`
	RollbackSelectionEventID               string `json:"rollback_selection_event_id,omitempty"`
	AutomaticHaltCount                     int    `json:"automatic_halt_count"`
	RecordedAt                             string `json:"recorded_at"`
}

const (
	syntheticRiskPolicyVersion        = "credential_free_buy_v1"
	syntheticMaxOrderQuantity         = int64(10)
	syntheticMaxOrderNotional         = int64(1_000_000)
	paperExecutionAuthorizationSchema = "paper-execution-authorization.v1"
	paperFillPolicyVersion            = paperdomain.FillPolicyVersion
	paperPerformancePolicySchema      = "paper-strategy-performance-policy.v1"
)

func ValidateOrderEvent(event OrderEvent) error {
	if !safeOrderID(event.EventID) || !safeOrderID(event.OrderID) {
		return errors.New("order event identifiers are invalid")
	}
	if event.Source != "synthetic" && event.Source != "reconciliation" {
		return errors.New("order event source is invalid")
	}
	allowed := map[string]bool{
		"INTENT_RECORDED": true, "RISK_APPROVED": true, "RISK_REJECTED": true,
		"SUBMIT_DISPATCHED": true, "SUBMIT_ACKNOWLEDGED": true, "SUBMIT_REJECTED": true,
		"FILL_RECORDED": true, "CANCEL_DISPATCHED": true, "CANCEL_ACKNOWLEDGED": true, "CANCEL_REJECTED": true,
	}
	if !allowed[event.Type] {
		return fmt.Errorf("unsupported order event %q", event.Type)
	}
	paperFillMetadata := event.PaperAccountingSessionID != "" || event.PaperSignalEventID != "" || event.PaperBarObservationID != "" ||
		event.PaperFillPolicyVersion != "" || event.ExecutionAuthorityEventID != "" || event.ReferencePrice != "" ||
		event.Fee != "" || event.Tax != "" || event.Slippage != ""
	authorityMetadata := event.RiskReservationID != "" || event.PaperAuthorizationID != "" || event.RiskPolicyVersion != "" || event.FencingToken != 0
	if event.Type == "RISK_APPROVED" || event.Type == "SUBMIT_DISPATCHED" {
		synthetic := safeOrderID(event.RiskReservationID) && event.PaperAuthorizationID == "" && event.RiskPolicyVersion == syntheticRiskPolicyVersion
		paper := event.RiskReservationID == "" && safeOrderID(event.PaperAuthorizationID) && event.RiskPolicyVersion == paperAccountingPolicyVersion
		if authorityMetadata && ((!synthetic && !paper) || event.FencingToken <= 0 || event.Source != "synthetic") {
			return errors.New("authorized order event metadata is invalid")
		}
	} else if event.Type != "FILL_RECORDED" && authorityMetadata {
		return errors.New("authority metadata is invalid for this order event")
	}
	if event.Type != "FILL_RECORDED" && paperFillMetadata {
		return errors.New("paper fill metadata is invalid for this order event")
	}
	if event.Type == "SUBMIT_ACKNOWLEDGED" {
		if !orderAlias(event.ProviderOrderRef, "order") || event.ProviderExecutionRef != "" || event.Quantity != "" || event.Price != "" || event.OccurredAt != "" {
			return errors.New("submit acknowledgement payload is invalid")
		}
		return nil
	}
	if event.Type == "FILL_RECORDED" {
		if !orderAlias(event.ProviderOrderRef, "order") || !orderAlias(event.ProviderExecutionRef, "execution") ||
			!validOrderInteger(event.Quantity) || len(event.Price) == 0 || len(event.Price) > 64 {
			return errors.New("fill payload is invalid")
		}
		price, err := exact.ParseDecimal(event.Price)
		if err != nil || price.Sign() <= 0 {
			return errors.New("fill price must be a positive canonical decimal")
		}
		if _, err := time.Parse(time.RFC3339Nano, event.OccurredAt); err != nil {
			return errors.New("fill occurred_at is invalid")
		}
		capitalized := event.PaperAuthorizationID != "" || paperFillMetadata || event.FencingToken != 0
		if capitalized {
			if event.Source != "synthetic" || event.RiskReservationID != "" || event.RiskPolicyVersion != "" ||
				!safeOrderID(event.PaperAuthorizationID) || event.FencingToken <= 0 || !safeOrderID(event.PaperAccountingSessionID) ||
				!safeOrderID(event.PaperSignalEventID) || !safeOrderID(event.PaperBarObservationID) ||
				event.PaperFillPolicyVersion != paperFillPolicyVersion || !safeOrderID(event.ExecutionAuthorityEventID) ||
				!paperdomain.ValidCapitalizedQuantity(event.Quantity, false) {
				return errors.New("capitalized paper fill provenance is invalid")
			}
			for _, raw := range []string{event.ReferencePrice, event.Fee, event.Tax, event.Slippage} {
				value, err := exact.ParseDecimal(raw)
				if err != nil || value.Sign() < 0 {
					return errors.New("capitalized paper fill accounting is invalid")
				}
			}
		} else if authorityMetadata || paperFillMetadata {
			return errors.New("legacy fill authority metadata is invalid")
		}
		return nil
	}
	if event.ProviderOrderRef != "" || event.ProviderExecutionRef != "" || event.Quantity != "" || event.Price != "" || event.OccurredAt != "" {
		return errors.New("order event has fields that are not valid for its type")
	}
	return nil
}

func ValidateSyntheticBuyPolicy(intent OrderIntent) (string, *big.Rat, error) {
	if intent.Provider != "kiwoom" || (intent.Mode != "synthetic" && intent.Mode != "paper") || intent.Exchange != "KRX" || intent.Currency != "KRW" ||
		intent.OrderType != "LIMIT" || intent.Side != "BUY" || (intent.Symbol != "005930" && intent.Symbol != "000660") {
		return "", nil, errors.New("order is outside the fixed credential-free synthetic BUY policy")
	}
	quantity, ok := new(big.Int).SetString(intent.Quantity, 10)
	if !ok || quantity.Sign() <= 0 || quantity.Cmp(big.NewInt(syntheticMaxOrderQuantity)) > 0 {
		return "", nil, errors.New("order quantity exceeds the fixed synthetic BUY limit")
	}
	price, err := exact.ParseDecimal(intent.LimitPrice)
	if err != nil || price.Sign() <= 0 {
		return "", nil, errors.New("order price is invalid")
	}
	notional := new(big.Rat).Mul(new(big.Rat).SetInt(quantity), price)
	if notional.Cmp(big.NewRat(syntheticMaxOrderNotional, 1)) > 0 {
		return "", nil, errors.New("order notional exceeds the fixed synthetic BUY limit")
	}
	formatted, err := exact.FormatDecimal(notional)
	return formatted, notional, err
}

func ValidateExecutionAuthorityRecord(record AuthorityRecord, previous *AuthorityRecord) error {
	if err := ValidateExecutionAuthorityRecordBasic(record); err != nil {
		return err
	}
	recordedAt, _ := CanonicalUTCTime(record.RecordedAt)
	if previous == nil {
		if record.FencingToken != 1 || record.ReasonCode != "manual_arm" || !record.Armed || record.LeaseOwner != "" {
			return errors.New("execution authority history does not start with an explicit arm event")
		}
		return nil
	}
	if record.FencingToken != previous.FencingToken+1 {
		return errors.New("execution authority fencing token is not monotonic")
	}
	switch record.ReasonCode {
	case "manual_arm":
		if previous.Armed || !record.Armed || record.LeaseOwner != "" {
			return errors.New("execution authority arm transition is invalid")
		}
	case "manual_halt", "automatic_performance_halt":
		if !previous.Armed || record.Armed || record.LeaseOwner != "" {
			return errors.New("execution authority halt transition is invalid")
		}
	case "lease_acquired":
		if !previous.Armed || !record.Armed || record.LeaseOwner == "" {
			return errors.New("execution authority lease transition is invalid")
		}
		if previous.LeaseOwner != "" {
			previousExpiry, ok := CanonicalUTCTime(previous.LeaseExpiresAt)
			if !ok {
				return errors.New("execution authority prior expiry is invalid")
			}
			if recordedAt.Before(previousExpiry) {
				previousTime, timeOK := CanonicalUTCTime(previous.RecordedAt)
				nextExpiry, expiryOK := CanonicalUTCTime(record.LeaseExpiresAt)
				// Same-owner renewal advances the fence without rearming. An old
				// token cannot use the extended interval; foreign takeover waits.
				if record.LeaseOwner == previous.LeaseOwner && timeOK && expiryOK &&
					!recordedAt.Before(previousTime) && nextExpiry.After(previousExpiry) {
					break
				}
				return errors.New("execution authority replaced an unexpired lease")
			}
		}
	default:
		return errors.New("execution authority reason is invalid")
	}
	return nil
}

func ValidateExecutionAuthorityRecordBasic(record AuthorityRecord) error {
	recordedAt, ok := CanonicalUTCTime(record.RecordedAt)
	if !safeOrderID(record.EventID) || !orderAlias(record.AccountRef, "account") || !ok || record.FencingToken <= 0 ||
		(record.LeaseOwner == "") != (record.LeaseExpiresAt == "") || (!record.Armed && record.LeaseOwner != "") {
		return errors.New("execution authority record is invalid")
	}
	if record.LeaseOwner != "" {
		expires, ok := CanonicalUTCTime(record.LeaseExpiresAt)
		if !safeOrderID(record.LeaseOwner) || !ok || !recordedAt.Before(expires) {
			return errors.New("execution authority lease is invalid")
		}
	}
	switch record.ReasonCode {
	case "manual_arm", "manual_halt", "lease_acquired":
		if record.PaperPerformancePolicyEventID != "" {
			return errors.New("manual execution authority record has policy link")
		}
	case "automatic_performance_halt":
		if record.PaperPerformancePolicyEventID == "" {
			return errors.New("automatic execution authority record lacks policy link")
		}
	default:
		return errors.New("execution authority reason is invalid")
	}
	return nil
}

func ValidateExecutionAuthorityRecordStandalone(record AuthorityRecord) error {
	if record.ReasonCode != "lease_acquired" {
		return errors.New("reservation authority is not a lease")
	}
	recordedAt, ok := CanonicalUTCTime(record.RecordedAt)
	expires, expiryOK := CanonicalUTCTime(record.LeaseExpiresAt)
	if !safeOrderID(record.EventID) || !orderAlias(record.AccountRef, "account") || !safeOrderID(record.LeaseOwner) ||
		!record.Armed || record.FencingToken <= 0 || !ok || !expiryOK || !recordedAt.Before(expires) {
		return errors.New("reservation authority record is invalid")
	}
	return nil
}

func PaperPerformancePolicyID(event PaperPerformancePolicyEvent) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{
		event.PolicyVersion, event.AccountRef, event.StrategySelectionEventID, event.StrategyPerformanceID,
		strconv.FormatInt(event.StrategySelectionSequenceCutoff, 10),
		strconv.FormatInt(event.PaperStrategyPerformanceSequenceCutoff, 10),
		strconv.FormatInt(event.ExecutionAuthoritySequenceCutoff, 10),
	}, "\x00")))
	return "paper_performance_policy_" + hex.EncodeToString(hash[:16])
}

func ValidateOwnedExecutionLease(authority ExecutionAuthorityState, owner string, fencingToken int64, now time.Time) error {
	expires, expiryOK := CanonicalUTCTime(authority.LeaseExpiresAt)
	if !authority.Armed || authority.LeaseOwner != owner || authority.FencingToken != fencingToken ||
		!expiryOK || !now.Before(expires) {
		return errors.New("execution authority is halted, stale, expired, or owned by another process")
	}
	return nil
}

func ReservationIsActive(state *orderdomain.State) bool {
	if state.PendingAction != "" {
		return true
	}
	switch state.Status {
	case "SUBMIT_UNKNOWN", "OPEN", "PARTIALLY_FILLED", "CANCEL_UNKNOWN":
		return true
	default:
		return false
	}
}

func ValidateRiskReservationRecord(record RiskReservation) error {
	_, reservedOK := CanonicalUTCTime(record.ReservedAt)
	if !safeOrderID(record.ReservationID) || !safeOrderID(record.OrderID) || !orderAlias(record.AccountRef, "account") ||
		record.PolicyVersion != syntheticRiskPolicyVersion || !safeOrderID(record.AuthorityEventID) || record.FencingToken <= 0 ||
		!validOrderInteger(record.Quantity) || !positiveCanonicalDecimal(record.LimitPrice) || !positiveCanonicalDecimal(record.LimitNotional) ||
		!safeOrderID(record.RiskEventID) || !safeOrderID(record.DispatchEventID) || record.RiskEventID == record.DispatchEventID || !reservedOK {
		return errors.New("risk reservation is invalid")
	}
	return nil
}

func ValidatePaperExecutionAuthorizationRecord(authorization PaperAuthorization) error {
	_, timeOK := CanonicalUTCTime(authorization.AuthorizedAt)
	if !safeOrderID(authorization.AuthorizationID) || authorization.SchemaVersion != paperExecutionAuthorizationSchema ||
		!safeOrderID(authorization.OrderID) || !orderAlias(authorization.AccountRef, "account") ||
		!safeOrderID(authorization.PaperAccountingSessionID) || !strategySHA256Pattern.MatchString(authorization.ExecutionPolicySHA256) ||
		authorization.PolicyVersion != paperAccountingPolicyVersion || (authorization.Side != "BUY" && authorization.Side != "SELL") ||
		!validOrderInteger(authorization.Quantity) || !safeOrderID(authorization.AuthorityEventID) || authorization.FencingToken <= 0 ||
		!safeOrderID(authorization.RiskEventID) || !safeOrderID(authorization.DispatchEventID) ||
		authorization.RiskEventID == authorization.DispatchEventID || !timeOK {
		return errors.New("paper execution authorization is invalid")
	}
	return nil
}

func ValidatePaperPerformancePolicyEvent(event PaperPerformancePolicyEvent) error {
	if !safeOrderID(event.PolicyEventID) || event.SchemaVersion != paperPerformancePolicySchema ||
		event.PolicyVersion != riskdomain.PaperPerformancePolicyVersion || !orderAlias(event.AccountRef, "account") ||
		!safeOrderID(event.PaperAccountingSessionID) || !safeOrderID(event.StrategySelectionEventID) ||
		!strategySHA256Pattern.MatchString(event.SelectedStrategyResultRef) || !safeOrderID(event.StrategyPerformanceID) ||
		!safeOrderID(event.BaselinePerformanceID) || !safeOrderID(event.LatestPerformanceID) || event.SampleCount <= 0 ||
		!safeOrderID(event.ExpectedPreviousPolicyEventID) || event.StrategySelectionSequenceCutoff < 0 ||
		event.PaperStrategyPerformanceSequenceCutoff <= 0 || event.ExecutionAuthoritySequenceCutoff < 0 ||
		!canonicalPaperTimes(event.RecordedAt) || event.PolicyEventID != PaperPerformancePolicyID(event) {
		return errors.New("paper performance policy event is invalid")
	}
	switch event.Decision {
	case "INSUFFICIENT":
		if event.ReasonCode != "minimum_same_selection_samples_not_met" || event.RollbackSelectionEventID != "" || event.AutomaticHaltCount != 0 {
			return errors.New("paper performance insufficient policy event is invalid")
		}
	case "HOLD":
		if event.ReasonCode != "within_local_paper_safety_bounds" || event.RollbackSelectionEventID != "" || event.AutomaticHaltCount != 0 {
			return errors.New("paper performance hold policy event is invalid")
		}
	case "HALT_AND_ROLLBACK":
		if (event.ReasonCode != "max_drawdown_limit_reached" && event.ReasonCode != "cumulative_return_floor_reached") ||
			!safeOrderID(event.RollbackSelectionEventID) || event.AutomaticHaltCount < 0 {
			return errors.New("paper performance action policy event is invalid")
		}
	default:
		return errors.New("paper performance policy decision is invalid")
	}
	return nil
}

func ValidatePaperPerformanceWindow(sessionRecordedAt string, asOf, recordedAt string) error {
	if !canonicalPaperTimes(asOf, recordedAt) || !canonicalUTCString(sessionRecordedAt) {
		return errors.New("paper performance time window is invalid")
	}
	sessionTime, _ := parsePaperTime(sessionRecordedAt)
	asOfTime, _ := parsePaperTime(asOf)
	recordedTime, _ := parsePaperTime(recordedAt)
	if !sessionTime.Before(asOfTime) || asOfTime.After(recordedTime) {
		return errors.New("paper performance requires session.RecordedAt < as_of <= event.RecordedAt")
	}
	return nil
}

func ValidateRunnerLeaseTuple(record RunnerLeaseRecord) error {
	active := record.OwnerID != ""
	if !active {
		if record.AccountRef != "" || record.HeartbeatAtNS != 0 || record.ExpiresAtNS != 0 ||
			record.StrategySelectionEventID != "" || record.SelectedResultSHA256 != "" {
			return errors.New("paper runner released lease tuple is invalid")
		}
		return nil
	}
	if record.FencingToken <= 0 || !safeOrderID(record.OwnerID) || !orderAlias(record.AccountRef, "account") ||
		record.HeartbeatAtNS <= 0 || record.ExpiresAtNS-record.HeartbeatAtNS != int64(PaperRunnerLeaseTTL) ||
		!safeOrderID(record.StrategySelectionEventID) || !strategySHA256Pattern.MatchString(record.SelectedResultSHA256) {
		return errors.New("paper runner active lease tuple is invalid")
	}
	return nil
}
