package domain

import (
	"strings"
	"testing"
	"time"

	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	researchdomain "omni-folio/services/core/internal/modules/trading/internal/domain/research"
)

func TestOrderAdmissionPolicyBoundaries(t *testing.T) {
	intent := OrderIntent{Provider: "kiwoom", Mode: "synthetic", Exchange: "KRX", Currency: "KRW", OrderType: "LIMIT", Side: "BUY", Symbol: "005930", Quantity: "10", LimitPrice: "100000"}
	for _, tc := range []struct{ quantity, price, want string }{
		{"10", "100000", ""},
		{"11", "1", "order quantity exceeds the fixed synthetic BUY limit"},
		{"10", "100000.000000001", "order notional exceeds the fixed synthetic BUY limit"},
	} {
		intent.Quantity, intent.LimitPrice = tc.quantity, tc.price
		notional, _, err := ValidateSyntheticBuyPolicy(intent)
		if tc.want == "" {
			if err != nil || notional != "1000000" {
				t.Fatalf("notional=%q error=%v", notional, err)
			}
		} else if err == nil || err.Error() != tc.want {
			t.Fatalf("error=%v, want %q", err, tc.want)
		}
	}
	event := orderdomain.Event{EventID: "event", OrderID: "order", Type: "INTENT_RECORDED", Source: "synthetic"}
	if err := ValidateOrderEvent(event); err != nil {
		t.Fatal(err)
	}
	event.Fee = "0"
	if err := ValidateOrderEvent(event); err == nil || err.Error() != "paper fill metadata is invalid for this order event" {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tc := range []struct {
		status, pending string
		active          bool
	}{
		{"FILLED", "", false}, {"CANCELED", "CANCEL", true}, {"OPEN", "", true}, {"RECORDED", "", false},
	} {
		if got := ReservationIsActive(&orderdomain.State{Status: tc.status, PendingAction: tc.pending}); got != tc.active {
			t.Fatalf("%+v: active=%v", tc, got)
		}
	}
}

func TestAuthorityRenewalAndExpiryBoundaries(t *testing.T) {
	previous := AuthorityRecord{EventID: "previous", AccountRef: "kiwoom_account_abcdefghijklmnopqrstuvwx", Armed: true, LeaseOwner: "owner", FencingToken: 2, LeaseExpiresAt: "2026-01-01T00:00:30Z", ReasonCode: "lease_acquired", RecordedAt: "2026-01-01T00:00:00Z"}
	next := previous
	next.EventID, next.FencingToken, next.RecordedAt, next.LeaseExpiresAt = "next", 3, "2026-01-01T00:00:01Z", "2026-01-01T00:00:31Z"
	if err := ValidateExecutionAuthorityRecord(next, &previous); err != nil {
		t.Fatal(err)
	}
	next.LeaseOwner = "other"
	if err := ValidateExecutionAuthorityRecord(next, &previous); err == nil || err.Error() != "execution authority replaced an unexpired lease" {
		t.Fatalf("unexpected error: %v", err)
	}
	at, _ := time.Parse(time.RFC3339, previous.LeaseExpiresAt)
	state := ExecutionAuthorityState{Armed: true, LeaseOwner: "owner", FencingToken: 2, LeaseExpiresAt: previous.LeaseExpiresAt}
	if err := ValidateOwnedExecutionLease(state, "owner", 2, at.Add(-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOwnedExecutionLease(state, "owner", 2, at); err == nil {
		t.Fatal("expiry instant accepted")
	}
}

func TestPromotionGateBoundaryOwnership(t *testing.T) {
	windows := make([]PaperPromotionWindow, 3)
	for i := range windows {
		windows[i].Candidate = researchdomain.Metrics{AfterCostReturn: "0.01", MaxDrawdown: "0.02", Turnover: "0.1", TradeCount: "1"}
		windows[i].Incumbent = windows[i].Candidate
		windows[i].BuyAndHold.AfterCostReturn = "0"
	}
	if decision, reason, err := EvaluatePaperPromotionWindows(windows); err != nil || decision != "HOLD" || reason != "holdout_not_better_than_incumbent" {
		t.Fatalf("%q %q %v", decision, reason, err)
	}
	windows[2].Candidate.AfterCostReturn = "0.0100000000000000000000000001"
	if decision, reason, err := EvaluatePaperPromotionWindows(windows); err != nil || decision != "PROMOTE" || reason != "all_forward_gates_passed" {
		t.Fatalf("%q %q %v", decision, reason, err)
	}
	windows[0].Candidate.Turnover = "0.1000000000000000000000000001"
	if decision, reason, err := EvaluatePaperPromotionWindows(windows); err != nil || decision != "HOLD" || reason != "candidate_turnover_above_incumbent" {
		t.Fatalf("%q %q %v", decision, reason, err)
	}
}

func TestPerformanceAndRunnerPolicyBoundaries(t *testing.T) {
	if err := ValidatePaperPerformanceWindow("2026-01-01T00:00:00Z", "2026-01-01T00:00:01.000000000Z", "2026-01-01T00:00:01.000000000Z"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePaperPerformanceWindow("2026-01-01T00:00:01Z", "2026-01-01T00:00:01.000000000Z", "2026-01-01T00:00:02.000000000Z"); err == nil {
		t.Fatal("zero-duration performance window accepted")
	}
	lease := RunnerLeaseRecord{Scope: PaperRunnerLeaseScope, FencingToken: 1}
	if err := ValidateRunnerLeaseTuple(lease); err != nil {
		t.Fatal(err)
	}
	lease.AccountRef = "kiwoom_account_abcdefghijklmnopqrstuvwx"
	if err := ValidateRunnerLeaseTuple(lease); err == nil || err.Error() != "paper runner released lease tuple is invalid" {
		t.Fatalf("unexpected error: %v", err)
	}
	reservation := RiskReservation{ReservationID: "reservation", OrderID: "order", AccountRef: "kiwoom_account_abcdefghijklmnopqrstuvwx", PolicyVersion: "credential_free_buy_v1", AuthorityEventID: "authority", FencingToken: 1, Quantity: "1", LimitPrice: "1", LimitNotional: "1", RiskEventID: "risk", DispatchEventID: "dispatch", ReservedAt: "2026-01-01T00:00:00Z"}
	if err := ValidateRiskReservationRecord(reservation); err != nil {
		t.Fatal(err)
	}
	reservation.DispatchEventID = reservation.RiskEventID
	if err := ValidateRiskReservationRecord(reservation); err == nil {
		t.Fatal("same risk/dispatch identity accepted")
	}
	authorization := PaperAuthorization{AuthorizationID: "auth", SchemaVersion: "paper-execution-authorization.v1", OrderID: "order", AccountRef: "kiwoom_account_abcdefghijklmnopqrstuvwx", PaperAccountingSessionID: "session", ExecutionPolicySHA256: strings.Repeat("a", 64), PolicyVersion: "paper_accounting_v1", Side: "SELL", Quantity: "1", AuthorityEventID: "authority", FencingToken: 1, RiskEventID: "risk", DispatchEventID: "dispatch", AuthorizedAt: "2026-01-01T00:00:00Z"}
	if err := ValidatePaperExecutionAuthorizationRecord(authorization); err != nil {
		t.Fatal(err)
	}
	authorization.Side = "SHORT"
	if err := ValidatePaperExecutionAuthorizationRecord(authorization); err == nil {
		t.Fatal("short authorization accepted")
	}
}

func TestPerformancePolicyActionCompatibility(t *testing.T) {
	event := PaperPerformancePolicyEvent{SchemaVersion: "paper-strategy-performance-policy.v1", PolicyVersion: "paper-strategy-performance-safety.v1", AccountRef: "kiwoom_account_abcdefghijklmnopqrstuvwx", PaperAccountingSessionID: "session", StrategySelectionEventID: "selection", SelectedStrategyResultRef: strings.Repeat("a", 64), StrategyPerformanceID: "performance", BaselinePerformanceID: "baseline", LatestPerformanceID: "latest", SampleCount: 1, ExpectedPreviousPolicyEventID: "no_policy_event", PaperStrategyPerformanceSequenceCutoff: 1, RecordedAt: "2026-01-01T00:00:00.000000000Z", Decision: "INSUFFICIENT", ReasonCode: "minimum_same_selection_samples_not_met"}
	event.PolicyEventID = PaperPerformancePolicyID(event)
	if err := ValidatePaperPerformancePolicyEvent(event); err != nil {
		t.Fatal(err)
	}
	event.AutomaticHaltCount = 1
	if err := ValidatePaperPerformancePolicyEvent(event); err == nil || err.Error() != "paper performance insufficient policy event is invalid" {
		t.Fatalf("unexpected error: %v", err)
	}
}
