package ledgerdomain

import "testing"

func TestNormalizeDerivesInstrumentFromSourceSymbol(t *testing.T) {
	input := Transaction{EventID: "event_1", SourceEventID: "source_1", AccountID: "account-main", Type: "DEPOSIT", OccurredAt: "2026-01-01T00:00:00Z", Currency: "USD", Amount: "10", InstrumentID: "stored-invalid"}
	transaction, issues := Normalize(input)
	if len(issues) != 0 || transaction.InstrumentID != "" || input.InstrumentID != "stored-invalid" {
		t.Fatalf("normalization trusted stored identity or changed input: %+v %+v", transaction, issues)
	}
	input.Type, input.Symbol, input.Quantity, input.Price, input.Fee, input.Amount = "BUY", "AAPL", "1", "10", "0", "-10"
	transaction, issues = Normalize(input)
	if len(issues) != 0 || transaction.InstrumentID != "instrument_aapl" {
		t.Fatalf("normalization failed to derive identity: %+v %+v", transaction, issues)
	}
}
