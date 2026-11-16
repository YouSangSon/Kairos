package domain

import (
	"math/big"
	"testing"
)

func TestTargetChangesRespectFilledQuantityAndActiveOrders(t *testing.T) {
	for _, test := range []struct {
		name, current, target, side, quantity string
		active, invalid                       bool
	}{
		{name: "open position", current: "0", target: "3", side: "BUY", quantity: "3"},
		{name: "reduce position", current: "8", target: "3", side: "SELL", quantity: "5"},
		{name: "close position", current: "8", target: "0", side: "SELL", quantity: "8"},
		{name: "unchanged", current: "8", target: "8"},
		{name: "same active target", current: "8", target: "8", active: true},
		{name: "replace active target", current: "8", target: "9", active: true, invalid: true},
		{name: "negative target", current: "8", target: "-1", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			current, _ := new(big.Int).SetString(test.current, 10)
			change, err := ChangeForTarget(Projection{Quantity: current, Active: test.active}, test.target)
			if (err != nil) != test.invalid {
				t.Fatalf("change=%+v err=%v", change, err)
			}
			if err != nil {
				return
			}
			if test.side == "" {
				if change != nil {
					t.Fatalf("unexpected change %+v", change)
				}
			} else if change == nil || change.Side != test.side || change.Quantity != test.quantity {
				t.Fatalf("change=%+v", change)
			}
			if current.String() != test.current {
				t.Fatal("target calculation mutated the projection")
			}
		})
	}
}
