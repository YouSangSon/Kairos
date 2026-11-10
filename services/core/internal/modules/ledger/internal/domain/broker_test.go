package ledgerdomain

import (
	"math/big"
	"testing"
)

func TestBrokerQuantitiesPreserveExactChronologyAndRejectInvalidLedger(t *testing.T) {
	events := []BrokerLedgerEvent{{"BUY", "005930", "0.3", "2026-01-01T00:00:00Z"}, {"SELL", "005930", "0.1", "2026-01-01T00:00:00.1Z"}}
	quantities, err := BrokerQuantities(events)
	if err != nil || quantities["005930"].Cmp(big.NewRat(1, 5)) != 0 {
		t.Fatalf("exact broker replay changed: %v %v", quantities, err)
	}
	differences, match, err := PositionDifferences([]BrokerPosition{{Symbol: "005930", Quantity: "0.2"}}, quantities)
	if err != nil || !match || len(differences) != 1 || differences[0].Difference != "0" || differences[0].LedgerQuantity != "0.2" {
		t.Fatalf("exact comparison changed: %+v %v", differences, err)
	}
	for _, invalid := range [][]BrokerLedgerEvent{
		{events[1], events[0]},
		{{"BUY", "005930", "invalid", "2026-01-01T00:00:00Z"}},
		{{"BUY", "005930", "0", "2026-01-01T00:00:00Z"}},
		{{"BUY", "AAPL", "1", "2026-01-01T00:00:00.000Z"}},
	} {
		if _, err := BrokerQuantities(invalid); err == nil {
			t.Fatalf("invalid broker ledger accepted: %+v", invalid)
		}
	}
	empty, match, err := PositionDifferences(nil, nil)
	if err != nil || !match || empty == nil || len(empty) != 0 {
		t.Fatalf("empty reconciliation changed: %+v %v", empty, err)
	}
}
