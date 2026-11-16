package paperdomain

import "testing"

func TestArchitecturePaperFillAndAccountingPureExamples(t *testing.T) {
	policy := ExecutionPolicy{Fee: "1", Tax: "0.001", SlippageBPS: "10", MaxParticipation: "0.5"}
	calculated, ok, err := CalculateFill(policy, FillInput{
		Side: "BUY", Open: "100", Volume: "5", RemainingQuantity: "10", Cash: "10000", ConsumedCapacity: "0",
	})
	wantCalculated := Fill{
		Quantity: "2", ReferencePrice: "100", Price: "100.1", Notional: "200.2",
		Fee: "1", Tax: "0", Slippage: "0.2", CashDelta: "-201.2",
	}
	if err != nil || !ok || calculated != wantCalculated {
		t.Fatalf("fill=%+v ok=%v err=%v want=%+v", calculated, ok, err, wantCalculated)
	}

	account, err := NewAccount("paper-account", "architecture-paper-session", "1000")
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		side string
		fill Fill
	}{
		{"BUY", Fill{Quantity: "2", Fee: "1", Tax: "0", Slippage: "0.2", CashDelta: "-201.2"}},
		{"BUY", Fill{Quantity: "1", Fee: "1", Tax: "0", Slippage: "0.15", CashDelta: "-151.15"}},
		{"SELL", Fill{Quantity: "1", Fee: "1", Tax: "0.12", Slippage: "0.1", CashDelta: "120"}},
		{"SELL", Fill{Quantity: "2", Fee: "1", Tax: "0.3", Slippage: "0.2", CashDelta: "300"}},
	}
	for _, step := range steps {
		if err := account.ApplyFill("005930", step.side, step.fill); err != nil {
			t.Fatal(err)
		}
	}
	state, err := account.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Cash != "1067.65" || state.Fees != "4" || state.Taxes != "0.42" || state.Slippage != "0.65" ||
		state.RealizedPnL != "67.65" || state.CapitalizedFills != 4 || len(state.Lots) != 0 {
		t.Fatalf("paper accounting=%+v", state)
	}
}
