package application

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/codec"
	"omni-folio/services/core/internal/exact"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	"omni-folio/services/core/internal/paperdomain"

	"regexp"
	"strconv"
	"strings"
	"time"
)

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

func PaperAuthorizedOrderEvent(eventID, orderID, eventType string, authorization paperExecutionAuthorization) OrderEvent {
	return OrderEvent{
		EventID: eventID, OrderID: orderID, Type: eventType, Source: "synthetic",
		PaperAuthorizationID: authorization.AuthorizationID, RiskPolicyVersion: authorization.PolicyVersion,
		FencingToken: authorization.FencingToken,
	}
}

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
		price, err := parseDecimal(event.Price)
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
				value, err := parseDecimal(raw)
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
	price, err := parseDecimal(intent.LimitPrice)
	if err != nil || price.Sign() <= 0 {
		return "", nil, errors.New("order price is invalid")
	}
	notional := new(big.Rat).Mul(new(big.Rat).SetInt(quantity), price)
	if notional.Cmp(big.NewRat(syntheticMaxOrderNotional, 1)) > 0 {
		return "", nil, errors.New("order notional exceeds the fixed synthetic BUY limit")
	}
	formatted, err := formatDecimal(notional)
	return formatted, notional, err
}

func AuthorityState(record executionAuthorityRecord) *ExecutionAuthorityState {
	return &ExecutionAuthorityState{
		AccountRef: record.AccountRef, Armed: record.Armed, LeaseOwner: record.LeaseOwner,
		FencingToken: record.FencingToken, LeaseExpiresAt: record.LeaseExpiresAt,
	}
}

func AuthorizedOrderEvent(eventID, orderID, eventType string, reservation riskReservationRecord) OrderEvent {
	return OrderEvent{
		EventID: eventID, OrderID: orderID, Type: eventType, Source: "synthetic",
		RiskReservationID: reservation.ReservationID, RiskPolicyVersion: reservation.PolicyVersion,
		FencingToken: reservation.FencingToken,
	}
}

func ValidateExecutionAuthorityRecord(record executionAuthorityRecord, previous *executionAuthorityRecord) error {
	if err := ValidateExecutionAuthorityRecordBasic(record); err != nil {
		return err
	}
	recordedAt, _ := canonicalUTCTime(record.RecordedAt)
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
			previousExpiry, ok := canonicalUTCTime(previous.LeaseExpiresAt)
			if !ok {
				return errors.New("execution authority prior expiry is invalid")
			}
			if recordedAt.Before(previousExpiry) {
				previousTime, timeOK := canonicalUTCTime(previous.RecordedAt)
				nextExpiry, expiryOK := canonicalUTCTime(record.LeaseExpiresAt)
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

func ValidateExecutionAuthorityRecordBasic(record executionAuthorityRecord) error {
	recordedAt, ok := canonicalUTCTime(record.RecordedAt)
	if !safeOrderID(record.EventID) || !orderAlias(record.AccountRef, "account") || !ok || record.FencingToken <= 0 ||
		(record.LeaseOwner == "") != (record.LeaseExpiresAt == "") || (!record.Armed && record.LeaseOwner != "") {
		return errors.New("execution authority record is invalid")
	}
	if record.LeaseOwner != "" {
		expires, ok := canonicalUTCTime(record.LeaseExpiresAt)
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

func ValidateExecutionAuthorityRecordStandalone(record executionAuthorityRecord) error {
	if record.ReasonCode != "lease_acquired" {
		return errors.New("reservation authority is not a lease")
	}
	recordedAt, ok := canonicalUTCTime(record.RecordedAt)
	expires, expiryOK := canonicalUTCTime(record.LeaseExpiresAt)
	if !safeOrderID(record.EventID) || !orderAlias(record.AccountRef, "account") || !safeOrderID(record.LeaseOwner) ||
		!record.Armed || record.FencingToken <= 0 || !ok || !expiryOK || !recordedAt.Before(expires) {
		return errors.New("reservation authority record is invalid")
	}
	return nil
}

func PaperExecutionPolicy(policy strategyExecutionPolicy) paperdomain.ExecutionPolicy {
	return paperdomain.ExecutionPolicy{
		Fee: policy.Fee, Tax: policy.Tax, SlippageBPS: policy.SlippageBPS, MaxParticipation: policy.MaxParticipation,
	}
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

func PaperPerformanceAutomaticHaltID(policyEventID, accountRef string) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{policyEventID, accountRef}, "\x00")))
	return "execution_authority_" + hex.EncodeToString(hash[:16])
}

const (
	syntheticExecutionLeaseTTL        = 30 * time.Second
	syntheticRiskPolicyVersion        = "credential_free_buy_v1"
	syntheticMaxOrderQuantity         = int64(10)
	syntheticMaxOrderNotional         = int64(1_000_000)
	syntheticMaxAccountNotional       = int64(1_000_000)
	paperExecutionAuthorizationSchema = "paper-execution-authorization.v1"
	paperAccountingPolicyVersion      = "paper_accounting_v1"
	paperFillPolicyVersion            = paperdomain.FillPolicyVersion
	paperPerformancePolicySchema      = "paper-strategy-performance-policy.v1"
	noStrategySelection               = "no_strategy"
	canonicalPaperTimeLayout          = "2006-01-02T15:04:05.000000000Z"
)

var (
	parseDecimal       = exact.ParseDecimal
	formatDecimal      = exact.FormatDecimal
	safeOrderID        = domain.SafeOrderID
	orderAlias         = domain.OrderAlias
	canonicalUTCTime   = domain.CanonicalUTCTime
	paperEventID       = domain.PaperEventID
	paperProviderAlias = domain.PaperProviderAlias
)

func validOrderInteger(raw string) bool {
	return len(raw) <= 32 && orderIntegerPattern.MatchString(raw)
}
func canonicalPaperTimes(values ...string) bool {
	for _, raw := range values {
		canonical, ok := canonicalPaperTime(raw)
		if !ok || canonical != raw {
			return false
		}
	}
	return true
}
func parsePaperTime(raw string) (time.Time, bool) {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || value.Location() != time.UTC {
		return time.Time{}, false
	}
	return value, value.Format(time.RFC3339Nano) == raw || value.Format(canonicalPaperTimeLayout) == raw
}

var orderIntegerPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

func canonicalPaperTime(raw string) (string, bool) {
	value, ok := parsePaperTime(raw)
	if !ok {
		return "", false
	}
	return value.Format(canonicalPaperTimeLayout), true
}

var orderJSONHash = codec.JSONHash
