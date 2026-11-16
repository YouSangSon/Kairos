package application

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"omni-folio/services/core/internal/exact"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"

	"regexp"
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
	return domain.ValidateOrderEvent(domain.OrderEvent(event))
}

func ValidateSyntheticBuyPolicy(intent OrderIntent) (string, *big.Rat, error) {
	return domain.ValidateSyntheticBuyPolicy(domain.OrderIntent(intent))
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
	return domain.ValidateExecutionAuthorityRecord(record, previous)
}

func ValidateExecutionAuthorityRecordBasic(record executionAuthorityRecord) error {
	return domain.ValidateExecutionAuthorityRecordBasic(record)
}

func ValidateExecutionAuthorityRecordStandalone(record executionAuthorityRecord) error {
	return domain.ValidateExecutionAuthorityRecordStandalone(record)
}

func PaperExecutionPolicy(policy strategyExecutionPolicy) paperdomain.ExecutionPolicy {
	return paperdomain.ExecutionPolicy{
		Fee: policy.Fee, Tax: policy.Tax, SlippageBPS: policy.SlippageBPS, MaxParticipation: policy.MaxParticipation,
	}
}

func PaperPerformancePolicyID(event PaperPerformancePolicyEvent) string {
	return domain.PaperPerformancePolicyID(domain.PaperPerformancePolicyEvent(event))
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

var orderIntegerPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

var orderJSONHash = domain.JSONHash
