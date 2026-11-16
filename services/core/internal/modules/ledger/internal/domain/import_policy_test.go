package ledgerdomain

import (
	"reflect"
	"testing"
)

func TestCashVoidPolicyRequiresExactEligibleReversal(t *testing.T) {
	target := Transaction{SourceEventID: "deposit", Type: "DEPOSIT", OccurredAt: "2026-01-01T00:00:00Z", Currency: "USD", Amount: "0.01"}
	correction := Transaction{Type: "CASH_VOID", OccurredAt: "2026-01-01T00:00:00Z", Currency: "USD", Amount: "-0.01"}
	for _, kind := range []string{"DEPOSIT", "WITHDRAWAL", "DIVIDEND", "FEE", "TAX"} {
		t.Run(kind, func(t *testing.T) {
			value := target
			value.Type = kind
			if err := ValidateCashVoidTarget(correction, value); err != nil {
				t.Fatalf("exact reversal rejected: %+v", err)
			}
		})
	}
	for _, tc := range []struct{ name, field, value string }{
		{"trade target", "type", "BUY"},
		{"correction chain", "type", "CASH_VOID"},
		{"other currency", "currency", "KRW"},
		{"future target", "time", "2026-01-01T00:00:00.000000001Z"},
		{"invalid target clock", "time", "not-a-clock"},
		{"different amount", "amount", "0.02"},
		{"noncanonical amount", "amount", "0.010"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := target
			switch tc.field {
			case "type":
				value.Type = tc.value
			case "currency":
				value.Currency = tc.value
			case "time":
				value.OccurredAt = tc.value
			case "amount":
				value.Amount = tc.value
			}
			err := ValidateCashVoidTarget(correction, value)
			want := &APIError{Code: "invalid_correction", Message: "cash void does not exactly reverse one eligible existing cash event", Field: "corrects_source_event_id"}
			if !reflect.DeepEqual(err, want) {
				t.Fatalf("rejection=%+v; want %+v", err, want)
			}
		})
	}
	for _, uses := range []int{0, 1, -1} {
		if got := ValidateCashVoidUse(uses); (got != nil) != (uses != 0) {
			t.Fatalf("uses=%d rejection=%+v", uses, got)
		}
	}
}

func TestImportIdentityAndResolutionPolicy(t *testing.T) {
	left := Transaction{EventID: "first", AccountID: "account-main", SourceEventID: "source", Symbol: "AAPL", Currency: "USD", Amount: "10"}
	right := left
	right.EventID = "second"
	if !SameTransaction(&left, &right) {
		t.Fatal("generated event identity changed source equivalence")
	}
	right.Amount = "11"
	if SameTransaction(&left, &right) || SameTransaction(nil, &left) || SameTransaction(nil, nil) {
		t.Fatal("distinct or absent source facts matched")
	}
	if ResolveImportTransaction(left) != nil {
		t.Fatal("supported transaction unresolved")
	}
	left.AccountID, left.Symbol = "another-account", "MSFT"
	want := &ImportResolution{Kind: "account", SourceField: "account_id", SourceValue: "another-account", RequiredAction: "select_account", CandidateIDs: []string{"account-main"}}
	if got := ResolveImportTransaction(left); !reflect.DeepEqual(got, want) {
		t.Fatalf("account resolution=%+v", got)
	}
	left.AccountID = "account-main"
	want = &ImportResolution{Kind: "instrument", SourceField: "symbol", SourceValue: "MSFT", RequiredAction: "select_instrument", CandidateIDs: []string{"instrument_aapl"}}
	if got := ResolveImportTransaction(left); !reflect.DeepEqual(got, want) {
		t.Fatalf("instrument resolution=%+v", got)
	}
	left.Symbol = ""
	if ResolveImportTransaction(left) != nil {
		t.Fatal("cash-only input requires instrument resolution")
	}
}
