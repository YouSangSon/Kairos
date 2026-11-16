package ledgerdomain

import (
	"errors"
	"time"
)

func RequireReplayPolicy(policy string) error {
	if policy != FIFOCostBasisPolicy && policy != LegacyFIFOCostBasisPolicy {
		return errors.New("unsupported ledger replay policy")
	}
	return nil
}

func RequireCurrentReplayPolicy(policy string) error {
	if policy != FIFOCostBasisPolicy {
		return errors.New("ledger replay upgrade required; preview and activate the corrected replay before normal use")
	}
	return nil
}

func RequireReplayTransition(policy string, revision int64) error {
	if policy != LegacyFIFOCostBasisPolicy || revision <= 0 {
		return errors.New("ledger does not require a legacy replay transition")
	}
	return nil
}

func ValidateStoredReplayPolicy(policy string, legacyRevision, currentRevision, count int64, archival bool) error {
	if policy == LegacyFIFOCostBasisPolicy {
		if !archival || legacyRevision == 0 || currentRevision != legacyRevision || count != 0 {
			return errors.New("legacy replay state is not eligible for current activation")
		}
		return nil
	}
	if policy != FIFOCostBasisPolicy || (legacyRevision == 0 && count != 0) || (legacyRevision > 0 && count != 1) {
		return errors.New("ledger replay policy or transition count is invalid")
	}
	return nil
}

func ReplayActivationTimeEligible(captured, activation time.Time) bool {
	return !activation.Before(captured)
}

func RequireBrokerLedgerAccount(account string) error {
	if account != "account-main" {
		return errors.New("ledger account is outside the current account-main boundary")
	}
	return nil
}

type MockBrokerBinding struct {
	LedgerAccount, ExpectedAccount string
	SnapshotPresent                bool
	Environment                    BrokerEnvironment
	SnapshotAccount                string
}

func ValidateMockBrokerBinding(binding MockBrokerBinding) error {
	if binding.LedgerAccount != "account-main" || !BrokerAccountAlias(binding.ExpectedAccount) || !binding.SnapshotPresent || binding.Environment != "mock" || binding.SnapshotAccount != binding.ExpectedAccount {
		return errors.New("invalid Kiwoom mock account binding")
	}
	return nil
}

func ValidateBrokerQueryIdentity(environment BrokerEnvironment, exchange BrokerExchange, account string) error {
	if (environment != "production" && environment != "mock") || exchange != "KRX" || !BrokerAccountAlias(account) {
		return errors.New("invalid Kiwoom snapshot identity")
	}
	return nil
}

func ValidateStoredMockBinding(count int, environment BrokerEnvironment, account, expectedAccount string) error {
	if count > 1 || environment != "mock" || (expectedAccount != "" && account != expectedAccount) {
		return errors.New("stored Kiwoom KRX history has a different account binding")
	}
	return nil
}
