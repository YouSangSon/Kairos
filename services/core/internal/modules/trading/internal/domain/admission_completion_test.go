package domain

import (
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	"testing"
	"time"
)

func TestReconciliationReadinessPreservesUncorrelatedBoundary(t *testing.T) {
	state := &orderdomain.State{Status: "SUBMIT_UNKNOWN", PendingAction: "SUBMIT"}
	if outcome, err := ReconciliationReadiness(state, false); err != nil || outcome != "INCOMPLETE" {
		t.Fatalf("outcome=%q error=%v", outcome, err)
	}
	if outcome, err := ReconciliationReadiness(state, true); err != nil || outcome != "UNCORRELATED" {
		t.Fatalf("outcome=%q error=%v", outcome, err)
	}
	state.Status, state.PendingAction, state.ProviderOrderRefs = "OPEN", "", []string{"ref"}
	if outcome, err := ReconciliationReadiness(state, true); err != nil || outcome != "" {
		t.Fatalf("outcome=%q error=%v", outcome, err)
	}
	state.ProviderOrderRefs = append(state.ProviderOrderRefs, "other")
	if _, err := ReconciliationReadiness(state, true); err == nil {
		t.Fatal("ambiguous refs accepted")
	}
	at, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	if err := ValidateReconciliationObservationTime(at, at); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReconciliationObservationTime(at.Add(-time.Nanosecond), at); err == nil {
		t.Fatal("pre-dispatch lookup accepted")
	}
}
func TestOperationalScopeAndClassification(t *testing.T) {
	snapshot := OperationalSnapshot{}
	CountOperationalOrder(&snapshot, &orderdomain.State{Status: "FILLED", PendingAction: "CANCEL"})
	CountOperationalOrder(&snapshot, &orderdomain.State{Status: "RISK_REJECTED"})
	CountOperationalOrder(&snapshot, &orderdomain.State{Status: "OPEN"})
	if snapshot.Orders != 3 || snapshot.Pending != 1 || snapshot.Terminal != 1 || snapshot.Active != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if err := ValidateStrategyPerformanceSelection("current", "none", "expected"); err == nil || err.Error() != "paper strategy performance selection is stale" {
		t.Fatalf("priority changed: %v", err)
	}
}
func TestArmingLegacyExemptionAndInputEligibility(t *testing.T) {
	if change, reason, fence := ManualArmingTransition(false, true, 0); !change || reason != "manual_arm" || fence != 1 {
		t.Fatalf("change=%v reason=%q fence=%d", change, reason, fence)
	}
	if change, _, _ := ManualArmingTransition(true, true, 4); change {
		t.Fatal("idempotent arm changed")
	}
	if OrderUsesStrategySelection(OrderIntent{}) {
		t.Fatal("unbound legacy intent needs current selection")
	}
	if !OrderUsesStrategySelection(OrderIntent{StrategyResultSHA256: "bound"}) {
		t.Fatal("partial binding was exempted")
	}
	if err := ValidatePaperInputResearch("no_promotion", "same", "same"); err == nil {
		t.Fatal("noncandidate paper input accepted")
	}
	if err := ValidatePromotionAbort("explicit_abort", false, ""); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePromotionAbort("explicit_abort", false, "selection"); err == nil {
		t.Fatal("aborted promotion selected a candidate")
	}
}

func TestRunnerLeaseStatusBoundaryPriority(t *testing.T) {
	lease := RunnerLeaseRecord{HeartbeatAtNS: 10, ExpiresAtNS: 20, StrategySelectionEventID: "selection", SelectedResultSHA256: "result"}
	for _, tc := range []struct {
		now             int64
		selection, want string
	}{{9, "different", "clock_regressed"}, {10, "selection", "lease_recorded"}, {19, "different", "selection_changed"}, {20, "different", "expired"}} {
		if got := RunnerLeaseStatus(lease, tc.now, tc.selection, "result"); got != tc.want {
			t.Fatalf("%+v: status=%q", tc, got)
		}
	}
	intent := OrderIntent{StrategyResultSHA256: "result", StrategySelectionEventID: "selection"}
	if !OrderInStrategySelection(intent, "result", "selection") || OrderInStrategySelection(intent, "result", "different") {
		t.Fatal("strategy membership changed")
	}
}
