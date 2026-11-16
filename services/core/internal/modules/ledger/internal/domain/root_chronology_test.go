package ledgerdomain

import "testing"

func TestBrokerChronologyKeepsClosedPositionQuantity(t *testing.T) {
	quantities, err := BrokerQuantities([]BrokerLedgerEvent{{Type: "BUY", Symbol: "005930", Quantity: "7", OccurredAt: "2026-01-02T00:00:00Z"}, {Type: "SELL", Symbol: "005930", Quantity: "7", OccurredAt: "2026-01-02T00:00:00.000000001Z"}})
	if err != nil || quantities["005930"] == nil || quantities["005930"].Sign() != 0 {
		t.Fatalf("valid chronological broker replay rejected: %v %v", quantities, err)
	}
}
