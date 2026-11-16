package domain

import (
	"errors"
	"math/big"
	"testing"
	"time"

	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
)

func TestSyntheticAccountLimitAndLeaseReuse(t *testing.T) {
	if err := ValidateSyntheticAccountNotional(big.NewRat(999999, 1), big.NewRat(1, 1)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSyntheticAccountNotional(big.NewRat(1000000, 1), big.NewRat(1, 1000000000)); err == nil {
		t.Fatal("account limit accepted an excess")
	}
	now, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	state := ExecutionAuthorityState{Armed: true, LeaseOwner: "owner", LeaseExpiresAt: "2026-01-01T00:00:30Z"}
	if reuse, err := ReuseSyntheticExecutionLease(state, "owner", now); err != nil || !reuse {
		t.Fatalf("reuse=%v err=%v", reuse, err)
	}
	if _, err := ReuseSyntheticExecutionLease(state, "other", now); err == nil {
		t.Fatal("foreign live lease reusable")
	}
	if reuse, err := ReuseSyntheticExecutionLease(state, "other", now.Add(30*time.Second)); err != nil || reuse {
		t.Fatalf("expired reuse=%v err=%v", reuse, err)
	}
}

func TestExecutionTermsAndSelectionPresenceBoundaries(t *testing.T) {
	terms := StrategyExecutionTerms{StartingCash: "1", Fee: "0", Tax: "1", SlippageBPS: "9999.99999999", DelayBars: "1", MaxParticipation: "1"}
	if delay, err := ValidateStrategyExecutionTerms(terms); err != nil || delay != 1 {
		t.Fatalf("delay=%d error=%v", delay, err)
	}
	terms.SlippageBPS = "10000"
	if _, err := ValidateStrategyExecutionTerms(terms); err == nil {
		t.Fatal("100% slippage accepted")
	}
	event := StrategySelectionEvent{EventID: "select", EventType: "SELECT", CandidateResultSHA256: "candidate", SelectedResultSHA256: "candidate", ReasonCode: "manual_selection"}
	if stack, err := ApplyStrategySelectionTransition(event, SelectionFieldPresence{}, "paper_candidate", "no_event", "no_strategy", nil, false); err != nil || len(stack) != 1 {
		t.Fatalf("stack=%v error=%v", stack, err)
	}
	if _, err := ApplyStrategySelectionTransition(event, SelectionFieldPresence{Source: true}, "paper_candidate", "no_event", "no_strategy", nil, false); err == nil {
		t.Fatal("present empty source accepted")
	}
	rollback := StrategySelectionEvent{EventID: "rollback", EventType: "ROLLBACK", SourceEventID: "select", SelectedResultSHA256: "no_strategy", ReasonCode: "manual_rollback"}
	if stack, err := ApplyStrategySelectionTransition(rollback, SelectionFieldPresence{Source: true}, "", "select", "candidate", []string{"candidate"}, true); err != nil || len(stack) != 0 {
		t.Fatalf("stack=%v error=%v", stack, err)
	}
}

func TestMarkSelectionSentinelAndAvailability(t *testing.T) {
	state := paperdomain.AccountState{Lots: map[string][]paperdomain.Lot{"005930": {{Quantity: "1", Cost: "1"}}}}
	selection, err := NewPerformanceMarkSelection(state, "2026-01-01T00:00:00.000000000Z", "2026-01-01T00:00:01.000000000Z", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := selection.Value("1", state); !errors.Is(err, ErrPaperRunnerIncompleteMarks) {
		t.Fatalf("missing sentinel: %v", err)
	}
	bar := PaperMarketBarObservation{Symbol: "005930", Source: "paper_fixture", Venue: "KRX", Currency: "KRW", Interval: "1d", Timezone: "Asia/Seoul", PriceAdjustment: "unspecified", CloseAt: "2026-01-01T00:00:00.000000000Z", SourceAvailableAt: "2026-01-01T00:00:00.000000000Z", FetchedAt: "2026-01-01T00:00:00.000000000Z", RecordedAt: "2026-01-01T00:00:02.000000000Z"}
	if err := selection.Consider(&bar, 1); err == nil || err.Error() != "paper performance mark was unavailable at record time" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalFrontierAndOrderEligibility(t *testing.T) {
	if IncludeLocalPolicyFrontier(true, "old", "latest") || !IncludeLocalPolicyFrontier(false, "old", "latest") || !IncludeLocalPolicyFrontier(true, "same", "same") {
		t.Fatal("frontier eligibility changed")
	}
	if got := ScheduledCloseAfterPriorPolicy("latest", "previous", false); got != "previous" {
		t.Fatalf("unfinished close=%q", got)
	}
	if got := ScheduledCloseAfterPriorPolicy("latest", "previous", true); got != "latest" {
		t.Fatalf("completed close=%q", got)
	}
	for _, tc := range []struct {
		status           string
		include, wantErr bool
	}{{"OPEN", true, false}, {"PARTIALLY_FILLED", true, false}, {"FILLED", false, false}, {"RISK_REJECTED", false, true}} {
		include, err := LocalPaperOrderExecutable(&orderdomain.State{Status: tc.status})
		if include != tc.include || (err != nil) != tc.wantErr {
			t.Fatalf("%s include=%v error=%v", tc.status, include, err)
		}
	}
	if _, err := LocalPaperOrderExecutable(&orderdomain.State{Status: "OPEN", PendingAction: "CANCEL"}); err == nil || err.Error() != "local paper has an unresolved order action" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLegacyFillAndRunnerBindingRemainExact(t *testing.T) {
	state := &orderdomain.State{LimitPrice: "10", Quantity: "3", FilledQuantity: "1"}
	if quantity, ok, err := LegacyPaperFillQuantity(state, big.NewRat(10, 1), big.NewRat(5, 1)); err != nil || !ok || quantity != "2" {
		t.Fatalf("quantity=%q eligible=%v error=%v", quantity, ok, err)
	}
	if _, ok, err := LegacyPaperFillQuantity(state, big.NewRat(10000000001, 1000000000), big.NewRat(5, 1)); err != nil || ok {
		t.Fatalf("eligible=%v error=%v", ok, err)
	}
	if LegacyTargetDelta(big.NewInt(1), big.NewInt(2)) != nil {
		t.Fatal("legacy target started a SELL")
	}
	row := RunnerLeaseRecord{OwnerID: "owner", AccountRef: "account", HeartbeatAtNS: 1, ExpiresAtNS: 31}
	if err := ValidateRunnerClaimBinding(true, row, row, "owner", "account"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRunnerClaimBinding(true, row, row, "other", "account"); err == nil {
		t.Fatal("foreign claim accepted")
	}
	if err := ValidateRunnerLeaseTime(row, 31); err == nil || err.Error() != "paper runner lease expired" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectionAndHistoricalTargetRules(t *testing.T) {
	intent := OrderIntent{Side: "BUY", Symbol: "005930", Quantity: "3", SignalTargetQuantity: "3"}
	state := orderdomain.State{Status: "PARTIALLY_FILLED", Quantity: "3", FilledQuantity: "1"}
	quantity := new(big.Int)
	active, err := AccumulatePaperProjection(quantity, 0, intent, &state)
	if err != nil || active != 1 || quantity.String() != "3" {
		t.Fatalf("projection=%s active=%d error=%v", quantity, active, err)
	}
	history := NewPaperIntentDeltaHistory()
	if err := history.RecordIntent("order", intent); err != nil {
		t.Fatal(err)
	}
	if err := history.RecordIntent("other", intent); err == nil || err.Error() != "capitalized paper intents cross an active order" {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := history.RecordFill("order", intent, "3"); err != nil {
		t.Fatal(err)
	}
	intent.Side, intent.Quantity, intent.SignalTargetQuantity = "SELL", "3", "0"
	if err := history.RecordIntent("sell", intent); err != nil {
		t.Fatal(err)
	}
	if err := history.RecordFill("sell", intent, "4"); err == nil || err.Error() != "capitalized paper SELL oversold holdings" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPaperBindingAndFreshRangeRules(t *testing.T) {
	intent := OrderIntent{Mode: "paper", SignalSchemaVersion: "paper-signal.v3", OrderType: "PAPER_MARKET", PaperAccountingPolicyVersion: "paper_accounting_v1", PaperAccountingSessionID: "session", ExecutionPolicySHA256: "policy"}
	if err := ValidateCapitalizedPaperIntent(intent); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePaperIntentSession(intent, true, "session", "different"); err == nil {
		t.Fatal("different execution policy accepted")
	}
	if err := ValidatePromotionResearchRange("2026-01-02T00:00:00Z", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"); err == nil {
		t.Fatal("overlapping research accepted")
	}
	if err := ValidatePromotionResearchRange("2026-01-02T00:00:01Z", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
}
