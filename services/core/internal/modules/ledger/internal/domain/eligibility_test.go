package ledgerdomain

import (
	"testing"
	"time"
)

func TestReplayEligibilityPreservesCurrentAndArchivalRules(t *testing.T) {
	if RequireReplayPolicy(FIFOCostBasisPolicy) != nil || RequireReplayPolicy(LegacyFIFOCostBasisPolicy) != nil || RequireReplayPolicy("unknown") == nil {
		t.Fatal("supported replay policies changed")
	}
	if RequireCurrentReplayPolicy(FIFOCostBasisPolicy) != nil || RequireCurrentReplayPolicy(LegacyFIFOCostBasisPolicy) == nil {
		t.Fatal("normal use accepted a legacy selector")
	}
	if RequireReplayTransition(LegacyFIFOCostBasisPolicy, 3) != nil || RequireReplayTransition(LegacyFIFOCostBasisPolicy, 0) == nil || RequireReplayTransition(FIFOCostBasisPolicy, 3) == nil {
		t.Fatal("explicit nonempty legacy transition boundary changed")
	}
	for _, tc := range []struct {
		name, policy                 string
		legacy, current, transitions int64
		archival, allowed            bool
	}{
		{"legacy archive", LegacyFIFOCostBasisPolicy, 3, 3, 0, true, true},
		{"legacy activation", LegacyFIFOCostBasisPolicy, 3, 3, 0, false, false},
		{"empty legacy", LegacyFIFOCostBasisPolicy, 0, 0, 0, true, false},
		{"changed legacy revision", LegacyFIFOCostBasisPolicy, 3, 4, 0, true, false},
		{"fresh current", FIFOCostBasisPolicy, 0, 0, 0, false, true},
		{"current transition", FIFOCostBasisPolicy, 3, 4, 1, false, true},
		{"missing transition", FIFOCostBasisPolicy, 3, 4, 0, false, false},
		{"repeated transition", FIFOCostBasisPolicy, 3, 4, 2, false, false},
		{"unexpected transition", FIFOCostBasisPolicy, 0, 3, 1, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateStoredReplayPolicy(tc.policy, tc.legacy, tc.current, tc.transitions, tc.archival)
			if (err == nil) != tc.allowed {
				t.Fatalf("policy result=%v; allowed=%v", err, tc.allowed)
			}
		})
	}
	captured := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if ReplayActivationTimeEligible(captured, captured.Add(-time.Nanosecond)) || !ReplayActivationTimeEligible(captured, captured) || !ReplayActivationTimeEligible(captured, captured.Add(time.Nanosecond)) {
		t.Fatal("activation capture boundary changed")
	}
}

func TestBrokerAdmissionKeepsAccountAndMockBindingBoundaries(t *testing.T) {
	account := "kiwoom_account_0123456789abcdef01234567"
	if RequireBrokerLedgerAccount("account-main") != nil || RequireBrokerLedgerAccount("another-account") == nil {
		t.Fatal("ledger account boundary changed")
	}
	valid := MockBrokerBinding{LedgerAccount: "account-main", ExpectedAccount: account, SnapshotPresent: true, Environment: "mock", SnapshotAccount: account}
	if ValidateMockBrokerBinding(valid) != nil {
		t.Fatal("valid mock account binding rejected")
	}
	for _, change := range []func(*MockBrokerBinding){
		func(v *MockBrokerBinding) { v.LedgerAccount = "other" },
		func(v *MockBrokerBinding) { v.ExpectedAccount = "invalid" },
		func(v *MockBrokerBinding) { v.SnapshotPresent = false },
		func(v *MockBrokerBinding) { v.Environment = "production" },
		func(v *MockBrokerBinding) { v.SnapshotAccount = "different" },
	} {
		v := valid
		change(&v)
		if ValidateMockBrokerBinding(v) == nil {
			t.Fatalf("invalid binding accepted: %+v", v)
		}
	}
	if ValidateBrokerQueryIdentity("mock", "KRX", account) != nil || ValidateBrokerQueryIdentity("production", "KRX", account) != nil || ValidateBrokerQueryIdentity("mock", "other", account) == nil {
		t.Fatal("query identity boundary changed")
	}
	if ValidateStoredMockBinding(1, "mock", account, "") != nil || ValidateStoredMockBinding(1, "mock", account, account) != nil || ValidateStoredMockBinding(2, "mock", account, account) == nil || ValidateStoredMockBinding(1, "production", account, account) == nil || ValidateStoredMockBinding(1, "mock", account, "different") == nil {
		t.Fatal("stored mock history binding changed")
	}
}
